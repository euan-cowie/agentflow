package agentflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type App struct {
	exec   Executor
	git    GitOps
	gh     GitHubOps
	tmux   TmuxOps
	runner AgentRunner
	state  *StateStore
	trust  *TrustStore
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	now    func() time.Time
}

type CommonOptions struct {
	RepoPath string
}

type UpOptions struct {
	CommonOptions
	Task    string
	Surface string
}

type VerifyOptions struct {
	CommonOptions
	Task       string
	Surface    string
	Foreground bool
}

type DownOptions struct {
	CommonOptions
	Task         string
	DeleteBranch bool
}

type SyncOptions struct {
	CommonOptions
	Task       string
	All        bool
	Push       bool
	Foreground bool
}

type SubmitOptions struct {
	CommonOptions
	Task  string
	Draft bool
	Ready bool
}

type LandOptions struct {
	CommonOptions
	Task  string
	Watch bool
}

type GCOptions struct {
	CommonOptions
	Task string
}

type DoctorOptions struct {
	CommonOptions
}

func NewApp(stdin io.Reader, stdout, stderr io.Writer) (*App, error) {
	stateRoot, err := stateRootPath()
	if err != nil {
		return nil, err
	}
	exec := Executor{}
	return &App{
		exec:   exec,
		git:    NewGitOps(exec),
		gh:     NewGitHubOps(exec),
		tmux:   NewTmuxOps(exec),
		runner: AgentRunner{},
		state:  NewStateStore(stateRoot),
		trust:  NewTrustStore(stateRoot),
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

func (a *App) resolveRepoRoot(ctx context.Context, repoArg string) (string, error) {
	base := repoArg
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	root, err := a.git.RepoRoot(ctx, base)
	if err != nil {
		return "", fmt.Errorf("resolve repo root from %s: %w", base, err)
	}
	return root, nil
}

func (a *App) loadRuntime(ctx context.Context, repoArg string) (RuntimeConfig, error) {
	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		return RuntimeConfig{}, err
	}
	runtime, err := resolveRuntimeConfig(repoRoot)
	if err != nil {
		return RuntimeConfig{}, err
	}
	trusted, err := a.trust.IsTrusted(runtime.RepoID, runtime.RepoRoot, runtime.WorkflowFingerprint)
	if err == nil {
		runtime.Trusted = trusted
	}
	return runtime, err
}

func (a *App) lockRepo(runtime RuntimeConfig) (func(), error) {
	lock, err := a.state.RepoLock(runtime.RepoID)
	if err != nil {
		return nil, err
	}
	if err := lock.Lock(); err != nil {
		return nil, err
	}
	return func() {
		_ = lock.Unlock()
	}, nil
}

func (a *App) Up(ctx context.Context, opts UpOptions) (TaskSummary, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return TaskSummary{}, err
	}
	if err := requireUpWorkflow(runtime); err != nil {
		return TaskSummary{}, err
	}
	unlock, err := a.lockRepo(runtime)
	if err != nil {
		return TaskSummary{}, err
	}
	defer unlock()

	ref, taskID, err := resolveManualTask(runtime.RepoRoot, opts.Task)
	if err != nil {
		return TaskSummary{}, err
	}

	if existing, err := a.state.Load(runtime.RepoID, taskID); err == nil {
		existing, discardable, err := a.recoverCreatingState(ctx, runtime, existing)
		if err != nil {
			return TaskSummary{}, err
		}
		if discardable {
			fmt.Fprintf(a.stderr, "Discarding stale task state for %q after an interrupted create\n", existing.TaskRef.Title)
			if err := a.state.Delete(existing.RepoID, existing.TaskID); err != nil {
				return TaskSummary{}, err
			}
		} else {
			return a.reconcileExisting(ctx, runtime, existing)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return TaskSummary{}, err
	}

	surface := opts.Surface
	if surface == "" {
		surface = runtime.EffectiveConfig.Repo.DefaultSurface
	}
	state := TaskState{
		TaskID:              taskID,
		TaskRef:             ref,
		Status:              StatusCreating,
		RepoRoot:            runtime.RepoRoot,
		RepoID:              runtime.RepoID,
		BaseBranch:          runtime.EffectiveConfig.Repo.BaseBranch,
		Surface:             surface,
		Branch:              branchName(runtime.EffectiveConfig, ref, taskID),
		TmuxSession:         renderSessionName(runtime.EffectiveConfig, ref, taskID),
		WorkflowFingerprint: runtime.WorkflowFingerprint,
		CreatedAt:           a.now(),
		UpdatedAt:           a.now(),
		Delivery: TaskDeliveryState{
			State:        DeliveryStateLocal,
			Remote:       strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote),
			RemoteBranch: branchName(runtime.EffectiveConfig, ref, taskID),
			BaseRef:      normalizeBaseBranch(runtime.EffectiveConfig.Repo.BaseBranch, strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote)),
		},
	}
	if agentWindow := primaryAgentWindow(runtime.EffectiveConfig); agentWindow != nil {
		state.PrimaryAgentWindow = agentWindow.Name
	}

	entries := workflowTrustEntries(runtime.Config)
	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ConfigPath, runtime.WorkflowFingerprint, entries, a.stdin, a.stdout); err != nil {
		return TaskSummary{}, err
	}

	worktreeRoot, err := resolveWorktreeRoot(runtime)
	if err != nil {
		return a.failState(state, err)
	}
	state.WorktreePath = filepath.Join(worktreeRoot, ref.Slug+"-"+taskID[:6])

	resolvedBase, fellBack, err := a.git.ResolveBaseRef(ctx, runtime.RepoRoot, state.BaseBranch)
	if err != nil {
		return a.failState(state, err)
	}
	if fellBack {
		fmt.Fprintf(a.stderr, "Configured base branch %q not found; using current branch %q\n", state.BaseBranch, resolvedBase)
	}
	state.BaseBranch = resolvedBase
	state.Delivery.BaseRef = normalizeBaseBranch(state.BaseBranch, state.Delivery.Remote)

	managedEnvFiles, portBindings, err := buildTaskEnvState(runtime.EffectiveConfig)
	if err != nil {
		return a.failState(state, err)
	}
	state.ManagedEnvFiles = managedEnvFiles
	state.PortBindings = portBindings
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	if err := a.git.CreateWorktree(ctx, runtime.RepoRoot, state.Branch, state.WorktreePath, state.BaseBranch); err != nil {
		return a.failState(state, err)
	}
	if runtime.EffectiveConfig.GitHub.Enabled {
		if err := a.git.SetBranchMergeBase(ctx, runtime.RepoRoot, state.Branch, state.Delivery.Remote, state.BaseBranch); err != nil {
			return a.failState(state, err)
		}
	}
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	if err := syncManagedEnvFiles(state); err != nil {
		return a.failState(state, err)
	}
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	if err := seedEnvFiles(state.WorktreePath, runtime.EffectiveConfig.Bootstrap.EnvFiles); err != nil {
		return a.failState(state, err)
	}
	if err := a.runBootstrap(ctx, runtime, state); err != nil {
		return a.failState(state, err)
	}

	if err := a.ensureTmux(ctx, runtime, state, true); err != nil {
		return a.failState(state, err)
	}

	state.Status = StatusReady
	state.FailureReason = ""
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	return TaskSummary{
		TaskID:    state.TaskID,
		TaskTitle: state.TaskRef.Title,
		RepoRoot:  state.RepoRoot,
		Worktree:  state.WorktreePath,
		Branch:    state.Branch,
		Session:   state.TmuxSession,
		Surface:   state.Surface,
		Status:    state.Status,
		Delivery:  state.Delivery,
	}, nil
}

func (a *App) reconcileExisting(ctx context.Context, runtime RuntimeConfig, state TaskState) (TaskSummary, error) {
	configDrift := state.WorkflowFingerprint != runtime.WorkflowFingerprint
	if err := a.git.ValidateTaskWorktree(ctx, state); err != nil {
		_, failErr := a.failState(state, fmt.Errorf("task %q is broken: %w. Run `agentflow down %q` to remove stale state, or `agentflow repair %q` if the worktree still exists", state.TaskRef.Title, err, state.TaskRef.Title, state.TaskRef.Title))
		return TaskSummary{}, failErr
	}
	if err := syncManagedEnvFiles(state); err != nil {
		return a.failState(state, err)
	}
	if err := a.ensureTmux(ctx, runtime, state, false); err != nil {
		return a.failState(state, err)
	}
	state.Status = StatusReady
	state.FailureReason = ""
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}
	return TaskSummary{
		TaskID:      state.TaskID,
		TaskTitle:   state.TaskRef.Title,
		RepoRoot:    state.RepoRoot,
		Worktree:    state.WorktreePath,
		Branch:      state.Branch,
		Session:     state.TmuxSession,
		Surface:     state.Surface,
		Status:      state.Status,
		ConfigDrift: configDrift,
		Delivery:    state.Delivery,
	}, nil
}

func (a *App) Attach(ctx context.Context, opts CommonOptions, task string) (TaskSummary, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return TaskSummary{}, err
	}
	ref, taskID, err := resolveManualTask(runtime.RepoRoot, task)
	if err != nil {
		return TaskSummary{}, err
	}
	_ = ref
	state, err := a.state.Load(runtime.RepoID, taskID)
	if err != nil {
		return TaskSummary{}, err
	}
	if err := a.ensureTmux(ctx, runtime, state, false); err != nil {
		return TaskSummary{}, err
	}
	if err := a.tmux.Attach(ctx, state.TmuxSession); err != nil {
		return TaskSummary{}, err
	}
	return TaskSummary{
		TaskID:    state.TaskID,
		TaskTitle: state.TaskRef.Title,
		RepoRoot:  state.RepoRoot,
		Worktree:  state.WorktreePath,
		Branch:    state.Branch,
		Session:   state.TmuxSession,
		Surface:   state.Surface,
		Status:    state.Status,
		Delivery:  state.Delivery,
	}, nil
}

func (a *App) Codex(ctx context.Context, opts CommonOptions, task string) (TaskSummary, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return TaskSummary{}, err
	}
	ref, taskID, err := resolveManualTask(runtime.RepoRoot, task)
	if err != nil {
		return TaskSummary{}, err
	}
	_ = ref
	state, err := a.state.Load(runtime.RepoID, taskID)
	if err != nil {
		return TaskSummary{}, err
	}
	if err := a.ensureTmux(ctx, runtime, state, false); err != nil {
		return TaskSummary{}, err
	}
	if state.PrimaryAgentWindow == "" {
		return TaskSummary{}, errors.New("task has no primary agent window")
	}
	if err := a.tmux.SelectWindow(ctx, state.TmuxSession, state.PrimaryAgentWindow); err != nil {
		return TaskSummary{}, err
	}
	if err := a.tmux.Attach(ctx, state.TmuxSession); err != nil {
		return TaskSummary{}, err
	}
	return TaskSummary{
		TaskID:    state.TaskID,
		TaskTitle: state.TaskRef.Title,
		RepoRoot:  state.RepoRoot,
		Worktree:  state.WorktreePath,
		Branch:    state.Branch,
		Session:   state.TmuxSession,
		Surface:   state.Surface,
		Status:    state.Status,
		Delivery:  state.Delivery,
	}, nil
}

func (a *App) Verify(ctx context.Context, opts VerifyOptions, name string) (TaskSummary, error) {
	return a.runNamedCommand(ctx, opts, name)
}

func (a *App) Review(ctx context.Context, opts VerifyOptions) (TaskSummary, error) {
	return a.runNamedCommand(ctx, opts, "review")
}

func (a *App) runNamedCommand(ctx context.Context, opts VerifyOptions, name string) (TaskSummary, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return TaskSummary{}, err
	}
	ref, taskID, err := resolveManualTask(runtime.RepoRoot, opts.Task)
	if err != nil {
		return TaskSummary{}, err
	}
	_ = ref
	state, err := a.state.Load(runtime.RepoID, taskID)
	if err != nil {
		return TaskSummary{}, err
	}
	command, resolvedName, err := resolveCommand(runtime.EffectiveConfig, state, name, opts.Surface)
	if err != nil {
		return TaskSummary{}, err
	}
	logPath, err := a.state.NewRunLogPath(state.RepoID, state.TaskID, resolvedName, a.now())
	if err != nil {
		return TaskSummary{}, err
	}
	entries := workflowTrustEntries(runtime.Config)
	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ConfigPath, runtime.WorkflowFingerprint, entries, a.stdin, a.stdout); err != nil {
		return TaskSummary{}, err
	}

	if !opts.Foreground && a.tmux.HasSession(ctx, state.TmuxSession) && a.tmux.WindowExists(ctx, state.TmuxSession, "verify") {
		window := TmuxWindowConfig{Name: "verify"}
		command = shellCommandWithEnv(command, taskEnv(state))
		if err := a.tmux.RespawnWindow(ctx, state.TmuxSession, state.WorktreePath, window, command+" | tee "+shellQuote(logPath)); err != nil {
			return TaskSummary{}, err
		}
	} else {
		if err := a.exec.RunLogged(ctx, state.WorktreePath, taskEnv(state), logPath, a.stdout, command); err != nil {
			return TaskSummary{}, err
		}
	}
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}
	return TaskSummary{
		TaskID:    state.TaskID,
		TaskTitle: state.TaskRef.Title,
		RepoRoot:  state.RepoRoot,
		Worktree:  state.WorktreePath,
		Branch:    state.Branch,
		Session:   state.TmuxSession,
		Surface:   state.Surface,
		Status:    state.Status,
		LogPath:   logPath,
		Delivery:  state.Delivery,
	}, nil
}

func (a *App) Down(ctx context.Context, opts DownOptions) (TaskSummary, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return TaskSummary{}, err
	}
	unlock, err := a.lockRepo(runtime)
	if err != nil {
		return TaskSummary{}, err
	}
	defer unlock()

	_, taskID, err := resolveManualTask(runtime.RepoRoot, opts.Task)
	if err != nil {
		return TaskSummary{}, err
	}
	state, err := a.state.Load(runtime.RepoID, taskID)
	if err != nil {
		return TaskSummary{}, err
	}
	state, discardable, err := a.recoverCreatingState(ctx, runtime, state)
	if err != nil {
		return TaskSummary{}, err
	}
	if state.Status == StatusCreating {
		if discardable {
			if err := a.state.Delete(state.RepoID, state.TaskID); err != nil {
				return TaskSummary{}, err
			}
			return TaskSummary{
				TaskID:    state.TaskID,
				TaskTitle: state.TaskRef.Title,
				RepoRoot:  state.RepoRoot,
				Worktree:  state.WorktreePath,
				Branch:    state.Branch,
				Session:   state.TmuxSession,
				Surface:   state.Surface,
				Status:    "deleted",
				Delivery:  state.Delivery,
			}, nil
		}
		fmt.Fprintf(a.stderr, "Resuming teardown for task %q after an interrupted create\n", state.TaskRef.Title)
	}
	if state.Status == StatusDeleting {
		fmt.Fprintf(a.stderr, "Resuming teardown for task %q after a previous failed delete\n", state.TaskRef.Title)
	}
	worktreeValid := a.git.ValidateTaskWorktree(ctx, state) == nil
	if worktreeValid {
		dirty, err := a.git.IsDirtyIgnoring(ctx, state.WorktreePath, state.EffectiveManagedEnvFiles())
		if err != nil {
			return TaskSummary{}, err
		}
		if dirty {
			return TaskSummary{}, errors.New("refusing to remove dirty worktree")
		}
		checkedOutElsewhere, err := a.git.BranchCheckedOutElsewhere(ctx, state.RepoRoot, state.Branch, state.WorktreePath)
		if err != nil {
			return TaskSummary{}, err
		}
		if checkedOutElsewhere {
			return TaskSummary{}, errors.New("branch is checked out in another worktree")
		}
	} else {
		fmt.Fprintf(a.stderr, "Skipping worktree validation during teardown for broken or partial task %q\n", state.TaskRef.Title)
	}

	state.Status = StatusDeleting
	state.FailureReason = ""
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	if a.tmux.HasSession(ctx, state.TmuxSession) {
		if err := a.tmux.KillSession(ctx, state.TmuxSession); err != nil {
			return a.failState(state, err)
		}
	}
	if worktreeValid {
		if err := removeManagedEnvFiles(state.WorktreePath, state.EffectiveManagedEnvFiles()); err != nil {
			return a.failState(state, err)
		}
		if err := a.git.RemoveWorktree(ctx, state.RepoRoot, state.WorktreePath); err != nil {
			return a.failState(state, err)
		}
	} else {
		_ = a.git.PruneWorktrees(ctx, state.RepoRoot)
	}
	if opts.DeleteBranch {
		if a.git.RefExists(ctx, state.RepoRoot, "refs/heads/"+state.Branch) {
			merged, err := a.git.IsBranchMerged(ctx, state.RepoRoot, state.BaseBranch, state.Branch)
			if err != nil {
				return a.failState(state, err)
			}
			if !merged {
				return a.failState(state, errors.New("refusing to delete branch that is not merged"))
			}
			if err := a.git.DeleteBranch(ctx, state.RepoRoot, state.Branch); err != nil {
				return a.failState(state, err)
			}
		}
	}
	if err := a.state.Delete(state.RepoID, state.TaskID); err != nil {
		return a.failState(state, err)
	}
	return TaskSummary{
		TaskID:    state.TaskID,
		TaskTitle: state.TaskRef.Title,
		RepoRoot:  state.RepoRoot,
		Worktree:  state.WorktreePath,
		Branch:    state.Branch,
		Session:   state.TmuxSession,
		Surface:   state.Surface,
		Status:    "deleted",
		Delivery:  state.Delivery,
	}, nil
}

func (a *App) List(ctx context.Context, repoPath string) ([]TaskState, error) {
	if repoPath == "" {
		return a.state.List("")
	}
	runtime, err := a.loadRuntime(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	return a.state.List(runtime.RepoID)
}

func (a *App) Repair(ctx context.Context, opts CommonOptions, task string) (TaskSummary, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return TaskSummary{}, err
	}
	unlock, err := a.lockRepo(runtime)
	if err != nil {
		return TaskSummary{}, err
	}
	defer unlock()

	_, taskID, err := resolveManualTask(runtime.RepoRoot, task)
	if err != nil {
		return TaskSummary{}, err
	}
	state, err := a.state.Load(runtime.RepoID, taskID)
	if err != nil {
		return TaskSummary{}, err
	}

	if _, err := os.Stat(state.WorktreePath); err == nil {
		_ = a.git.RepairWorktree(ctx, state.RepoRoot, state.WorktreePath)
	}
	if err := a.git.ValidateTaskWorktree(ctx, state); err != nil {
		_, failErr := a.failState(state, fmt.Errorf("task %q cannot be repaired automatically: %w. If the worktree is gone, run `agentflow down %q` to remove stale state", state.TaskRef.Title, err, state.TaskRef.Title))
		return TaskSummary{}, failErr
	}

	if err := syncManagedEnvFiles(state); err != nil {
		return a.failState(state, err)
	}

	entries := workflowTrustEntries(runtime.Config)
	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ConfigPath, runtime.WorkflowFingerprint, entries, a.stdin, a.stdout); err != nil {
		return a.failState(state, err)
	}

	if err := a.ensureTmux(ctx, runtime, state, false); err != nil {
		return a.failState(state, err)
	}
	state.Status = StatusReady
	state.FailureReason = ""
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}
	return TaskSummary{
		TaskID:    state.TaskID,
		TaskTitle: state.TaskRef.Title,
		RepoRoot:  state.RepoRoot,
		Worktree:  state.WorktreePath,
		Branch:    state.Branch,
		Session:   state.TmuxSession,
		Surface:   state.Surface,
		Status:    state.Status,
		Delivery:  state.Delivery,
	}, nil
}

func (a *App) Doctor(ctx context.Context, opts DoctorOptions) ([]DoctorCheck, error) {
	checks := make([]DoctorCheck, 0)
	required := []string{"git", "tmux", "codex"}
	repoRoot, repoErr := a.resolveRepoRoot(ctx, opts.RepoPath)
	var runtime RuntimeConfig
	var runtimeLoaded bool
	if repoErr == nil {
		configPath := ResolvedConfigPath(repoRoot)
		exists := fileExists(configPath)
		checks = append(checks, DoctorCheck{
			Name:    "config",
			OK:      exists,
			Details: configPath,
		})
		if exists {
			runtime, repoErr = resolveRuntimeConfig(repoRoot)
			if repoErr != nil {
				checks = append(checks, DoctorCheck{
					Name:    "config-valid",
					OK:      false,
					Details: repoErr.Error(),
				})
			} else {
				runtimeLoaded = true
				required = uniqueStrings(append(required, runtime.Config.Requirements.Binaries...))
			}
		}
	}

	for _, binary := range uniqueStrings(required) {
		_, err := a.exec.Run(ctx, "", nil, "sh", "-lc", "command -v "+shellQuote(binary))
		checks = append(checks, DoctorCheck{
			Name:    "binary:" + binary,
			OK:      err == nil,
			Details: binary,
		})
	}

	_, err := a.exec.Run(ctx, "", nil, "codex", "login", "status")
	checks = append(checks, DoctorCheck{
		Name:    "codex-login",
		OK:      err == nil,
		Details: "codex login status",
	})

	if runtimeLoaded {
		if runtime.EffectiveConfig.GitHub.Enabled {
			_, err := a.exec.Run(ctx, "", nil, "sh", "-lc", "command -v gh")
			checks = append(checks, DoctorCheck{
				Name:    "binary:gh",
				OK:      err == nil,
				Details: "gh",
			})
			authErr := a.gh.AuthStatus(ctx, runtime.RepoRoot)
			checks = append(checks, DoctorCheck{
				Name:    "gh-auth",
				OK:      authErr == nil,
				Details: "gh auth status",
			})
		}

		for _, server := range runtime.Config.Requirements.MCPServers {
			result, err := a.exec.Run(ctx, "", nil, "codex", "mcp", "list", "--json")
			ok := err == nil && strings.Contains(result.Stdout, server)
			checks = append(checks, DoctorCheck{
				Name:    "mcp:" + server,
				OK:      ok,
				Details: "best-effort experimental MCP check",
			})
		}

		for name, command := range runtime.Config.Commands {
			checks = append(checks, DoctorCheck{
				Name:    "command:" + name,
				OK:      headBinaryExists(ctx, a.exec, command),
				Details: command,
			})
		}

		result, err := a.exec.Run(ctx, runtime.RepoRoot, nil, "git", "config", "--bool", "rerere.enabled")
		details := "consider enabling git rerere to reuse conflict resolutions during repeated syncs"
		if err == nil && strings.TrimSpace(result.Stdout) == "true" {
			details = "git rerere enabled"
		}
		checks = append(checks, DoctorCheck{
			Name:    "advice:rerere",
			OK:      true,
			Details: details,
		})
	}

	return checks, nil
}

func syncManagedEnvFiles(state TaskState) error {
	_, err := writeManagedEnvFiles(state.WorktreePath, state.EffectiveManagedEnvFiles(), portBindingValues(state.EffectivePortBindings()))
	return err
}

func (a *App) failState(state TaskState, err error) (TaskSummary, error) {
	state.Status = StatusBroken
	state.FailureReason = err.Error()
	state.UpdatedAt = a.now()
	_ = a.state.Save(state)
	return TaskSummary{
		TaskID:    state.TaskID,
		TaskTitle: state.TaskRef.Title,
		RepoRoot:  state.RepoRoot,
		Worktree:  state.WorktreePath,
		Branch:    state.Branch,
		Session:   state.TmuxSession,
		Surface:   state.Surface,
		Status:    state.Status,
		Delivery:  state.Delivery,
	}, err
}

func resolveWorktreeRoot(runtime RuntimeConfig) (string, error) {
	root := renderWorktreeRoot(runtime)
	if filepath.IsAbs(root) {
		root = filepath.Clean(root)
		if err := ensureDir(root); err != nil {
			return "", err
		}
		return canonicalPath(root), nil
	}
	resolved := filepath.Join(runtime.RepoRoot, root)
	resolved = filepath.Clean(resolved)
	if err := ensureDir(resolved); err != nil {
		return "", err
	}
	return canonicalPath(resolved), nil
}

func renderWorktreeRoot(runtime RuntimeConfig) string {
	root := strings.TrimSpace(runtime.EffectiveConfig.Repo.WorktreeRoot)
	if root == "" {
		root = defaultWorktreeRootTemplate
	}
	replacer := strings.NewReplacer(
		"{{agentflow_state_home}}", runtime.StateRoot,
		"{{repo_id}}", runtime.RepoID,
		"{{repo}}", slugify(runtime.EffectiveConfig.Repo.Name),
	)
	return replacer.Replace(root)
}

func plannedTaskWorktreePath(runtime RuntimeConfig, ref TaskRef, taskID string) string {
	root := renderWorktreeRoot(runtime)
	if !filepath.IsAbs(root) {
		root = filepath.Join(runtime.RepoRoot, root)
	}
	return filepath.Clean(filepath.Join(root, ref.Slug+"-"+taskID[:6]))
}

func (a *App) recoverCreatingState(ctx context.Context, runtime RuntimeConfig, state TaskState) (TaskState, bool, error) {
	if state.Status != StatusCreating {
		return state, false, nil
	}

	expectedPath := strings.TrimSpace(state.WorktreePath)
	if expectedPath == "" {
		expectedPath = plannedTaskWorktreePath(runtime, state.TaskRef, state.TaskID)
	}

	match, err := a.git.FindWorktree(ctx, state.RepoRoot, state.Branch, expectedPath)
	if err != nil {
		return state, false, err
	}
	if match != nil {
		resolvedPath := canonicalPath(match.Path)
		if state.WorktreePath != resolvedPath {
			state.WorktreePath = resolvedPath
			state.UpdatedAt = a.now()
			if err := a.state.Save(state); err != nil {
				return state, false, err
			}
		}
		return state, false, nil
	}

	branchExists := state.Branch != "" && a.git.RefExists(ctx, state.RepoRoot, "refs/heads/"+state.Branch)
	pathExists := expectedPath != "" && fileExists(expectedPath)
	if strings.TrimSpace(state.WorktreePath) == "" && expectedPath != "" && (branchExists || pathExists) {
		state.WorktreePath = expectedPath
		state.UpdatedAt = a.now()
		if err := a.state.Save(state); err != nil {
			return state, false, err
		}
	}

	return state, !branchExists && !pathExists, nil
}

func requireUpWorkflow(runtime RuntimeConfig) error {
	if !runtime.ConfigExists {
		return fmt.Errorf("repo config missing at %s; run `agentflow config write` and define the repo workflow", runtime.ConfigPath)
	}
	if strings.TrimSpace(runtime.EffectiveConfig.Repo.BaseBranch) == "" {
		return fmt.Errorf("repo.base_branch must be configured in %s", runtime.ConfigPath)
	}
	if len(runtime.EffectiveConfig.Tmux.Windows) == 0 {
		return fmt.Errorf("tmux.windows must be configured in %s", runtime.ConfigPath)
	}
	return nil
}

func primaryAgentWindow(cfg EffectiveConfig) *TmuxWindowConfig {
	for idx := range cfg.Tmux.Windows {
		window := &cfg.Tmux.Windows[idx]
		if window.Agent != "" {
			return window
		}
	}
	return nil
}

func (a *App) runBootstrap(ctx context.Context, runtime RuntimeConfig, state TaskState) error {
	logPath, err := a.state.NewRunLogPath(state.RepoID, state.TaskID, "bootstrap", a.now())
	if err != nil {
		return err
	}
	for _, command := range runtime.EffectiveConfig.Bootstrap.Commands {
		if command == "" {
			continue
		}
		if err := a.exec.RunLogged(ctx, state.WorktreePath, taskEnv(state), logPath, nil, command); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) ensureTmux(ctx context.Context, runtime RuntimeConfig, state TaskState, firstCreate bool) error {
	if err := a.git.ValidateTaskWorktree(ctx, state); err != nil {
		return err
	}
	sessionExists := a.tmux.HasSession(ctx, state.TmuxSession)
	windows := runtime.EffectiveConfig.Tmux.Windows
	if len(windows) == 0 {
		if sessionExists {
			return nil
		}
		return fmt.Errorf("tmux.windows must be configured in %s", runtime.ConfigPath)
	}
	needCreate := !sessionExists
	if sessionExists {
		for _, window := range windows {
			if !a.tmux.WindowExists(ctx, state.TmuxSession, window.Name) {
				needCreate = true
				break
			}
		}
	}
	if needCreate {
		entries := workflowTrustEntries(runtime.Config)
		if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ConfigPath, runtime.WorkflowFingerprint, entries, a.stdin, a.stdout); err != nil {
			return err
		}
	}

	for idx, window := range windows {
		command, err := a.windowCommand(runtime.EffectiveConfig, state, window, sessionExists && a.tmux.WindowExists(ctx, state.TmuxSession, window.Name))
		if err != nil {
			return err
		}
		if !sessionExists && idx == 0 {
			if err := a.tmux.NewSession(ctx, state.TmuxSession, state.WorktreePath, window, command); err != nil {
				return err
			}
			sessionExists = true
			continue
		}
		if !sessionExists || !a.tmux.WindowExists(ctx, state.TmuxSession, window.Name) {
			if window.Agent != "" && !sessionExists {
				if err := a.tmux.NewSession(ctx, state.TmuxSession, state.WorktreePath, window, command); err != nil {
					return err
				}
				sessionExists = true
				continue
			}
			if err := a.tmux.AddWindow(ctx, state.TmuxSession, state.WorktreePath, window, command); err != nil {
				return err
			}
		} else if firstCreate && window.Agent != "" {
			if err := a.tmux.RespawnWindow(ctx, state.TmuxSession, state.WorktreePath, window, command); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) windowCommand(cfg EffectiveConfig, state TaskState, window TmuxWindowConfig, exists bool) (string, error) {
	if window.Command != "" {
		return shellCommandWithEnv(window.Command, taskEnv(state)), nil
	}
	agent := cfg.Agents[window.Agent]
	if agent.Runner == "" {
		agent.Runner = "codex"
	}
	prompt := strings.TrimSpace(agent.PrimePrompt)
	if exists && agent.ResumePrompt != "" {
		prompt = agent.ResumePrompt
	}
	contextPrompt := fmt.Sprintf("%s\nTask: %s\nTask ID: %s\nWorktree: %s", prompt, state.TaskRef.Title, state.TaskID, state.WorktreePath)
	command, err := a.runner.commandString(agent, state.WorktreePath, contextPrompt, exists)
	if err != nil {
		return "", err
	}
	return shellCommandWithEnv(command, taskEnv(state)), nil
}

func resolveCommand(cfg EffectiveConfig, state TaskState, name string, surface string) (string, string, error) {
	if name == "review" {
		command := cfg.Commands["review"]
		if command == "" {
			return "", "", errors.New("commands.review is not configured")
		}
		return command, "review", nil
	}
	if name != "verify" {
		if command, ok := cfg.Commands[name]; ok && command != "" {
			return command, name, nil
		}
		return "", "", fmt.Errorf("commands.%s is not configured", name)
	}
	if surface == "" {
		surface = state.Surface
	}
	keys := []string{}
	if surface != "" {
		keys = append(keys, "verify_"+surface)
	}
	keys = append(keys, "verify_quick")
	for _, key := range keys {
		if command, ok := cfg.Commands[key]; ok && command != "" {
			return command, key, nil
		}
	}
	return "", "", fmt.Errorf("no verify command configured for surface %q", surface)
}

func headBinaryExists(ctx context.Context, exec Executor, command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	_, err := exec.Run(ctx, "", nil, "sh", "-lc", "command -v "+shellQuote(fields[0]))
	return err == nil
}
