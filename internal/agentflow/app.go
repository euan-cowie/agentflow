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
	exec       Executor
	git        GitOps
	tmux       TmuxOps
	runner     AgentRunner
	state      *StateStore
	trust      *TrustStore
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	now        func() time.Time
	configPath string
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

type DoctorOptions struct {
	CommonOptions
}

func NewApp(stdin io.Reader, stdout, stderr io.Writer, configPath string) (*App, error) {
	stateRoot, err := stateRootPath()
	if err != nil {
		return nil, err
	}
	exec := Executor{}
	return &App{
		exec:       exec,
		git:        NewGitOps(exec),
		tmux:       NewTmuxOps(exec),
		runner:     AgentRunner{},
		state:      NewStateStore(stateRoot),
		trust:      NewTrustStore(stateRoot),
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderr,
		now:        func() time.Time { return time.Now().UTC() },
		configPath: configPath,
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
	runtime, err := resolveRuntimeConfig(repoRoot, a.configPath)
	if err != nil {
		return RuntimeConfig{}, err
	}
	trusted, err := a.trust.IsTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ManifestFingerprint)
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
		return a.reconcileExisting(ctx, runtime, existing)
	} else if !errors.Is(err, os.ErrNotExist) {
		return TaskSummary{}, err
	}

	surface := opts.Surface
	if surface == "" {
		surface = runtime.Config.Repo.DefaultSurface
	}
	state := TaskState{
		TaskID:              taskID,
		TaskRef:             ref,
		Status:              StatusCreating,
		RepoRoot:            runtime.RepoRoot,
		RepoID:              runtime.RepoID,
		BaseBranch:          runtime.Config.Repo.BaseBranch,
		Surface:             surface,
		Branch:              branchName(runtime.Config, ref, taskID),
		TmuxSession:         renderSessionName(runtime.Config, ref, taskID),
		ManagedEnvFile:      runtime.Config.Env.ManagedFile,
		ManifestFingerprint: runtime.ManifestFingerprint,
		CreatedAt:           a.now(),
		UpdatedAt:           a.now(),
	}
	if agentWindow := primaryAgentWindow(runtime.Config); agentWindow != nil {
		state.PrimaryAgentWindow = agentWindow.Name
	}

	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	entries := manifestExecutableEntries(runtime.ManifestConfig)
	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ManifestPath, runtime.ManifestFingerprint, entries, a.stdin, a.stdout); err != nil {
		return a.failState(state, err)
	}

	worktreeRoot, err := resolveWorktreeRoot(runtime.Config, runtime.RepoRoot)
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

	var envVars map[string]string
	if runtime.Config.Ports.Enabled {
		port, err := allocatePreferredPort(runtime.Config.Ports.Start, runtime.Config.Ports.End)
		if err != nil {
			return a.failState(state, err)
		}
		state.AllocatedPort = port
		state.PortKey = runtime.Config.Ports.Key
		envVars = map[string]string{
			runtime.Config.Ports.Key: fmt.Sprintf("%d", port),
		}
	} else {
		envVars = map[string]string{}
	}

	if err := a.git.CreateWorktree(ctx, runtime.RepoRoot, state.Branch, state.WorktreePath, state.BaseBranch); err != nil {
		return a.failState(state, err)
	}
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	if _, err := writeManagedEnvFile(state.WorktreePath, state.ManagedEnvFile, envVars); err != nil {
		return a.failState(state, err)
	}
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	if err := seedEnvFiles(state.WorktreePath, runtime.Config.Bootstrap.EnvFiles); err != nil {
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
		TaskID:   state.TaskID,
		RepoRoot: state.RepoRoot,
		Worktree: state.WorktreePath,
		Branch:   state.Branch,
		Session:  state.TmuxSession,
		Surface:  state.Surface,
		Status:   state.Status,
	}, nil
}

func (a *App) reconcileExisting(ctx context.Context, runtime RuntimeConfig, state TaskState) (TaskSummary, error) {
	manifestDrift := state.ManifestFingerprint != runtime.ManifestFingerprint
	if err := a.git.ValidateTaskWorktree(ctx, state); err != nil {
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
		TaskID:        state.TaskID,
		RepoRoot:      state.RepoRoot,
		Worktree:      state.WorktreePath,
		Branch:        state.Branch,
		Session:       state.TmuxSession,
		Surface:       state.Surface,
		Status:        state.Status,
		ManifestDrift: manifestDrift,
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
		TaskID:   state.TaskID,
		RepoRoot: state.RepoRoot,
		Worktree: state.WorktreePath,
		Branch:   state.Branch,
		Session:  state.TmuxSession,
		Surface:  state.Surface,
		Status:   state.Status,
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
		TaskID:   state.TaskID,
		RepoRoot: state.RepoRoot,
		Worktree: state.WorktreePath,
		Branch:   state.Branch,
		Session:  state.TmuxSession,
		Surface:  state.Surface,
		Status:   state.Status,
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
	command, resolvedName, err := resolveCommand(runtime.Config, state, name, opts.Surface)
	if err != nil {
		return TaskSummary{}, err
	}
	logPath, err := a.state.NewRunLogPath(state.RepoID, state.TaskID, resolvedName, a.now())
	if err != nil {
		return TaskSummary{}, err
	}
	entries := manifestExecutableEntries(runtime.ManifestConfig)
	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ManifestPath, runtime.ManifestFingerprint, entries, a.stdin, a.stdout); err != nil {
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
		TaskID:   state.TaskID,
		RepoRoot: state.RepoRoot,
		Worktree: state.WorktreePath,
		Branch:   state.Branch,
		Session:  state.TmuxSession,
		Surface:  state.Surface,
		Status:   state.Status,
		LogPath:  logPath,
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
	if state.Status == StatusCreating || state.Status == StatusDeleting {
		return TaskSummary{}, fmt.Errorf("task is currently %s", state.Status)
	}
	worktreeValid := a.git.ValidateTaskWorktree(ctx, state) == nil
	if worktreeValid {
		dirty, err := a.git.IsDirty(ctx, state.WorktreePath)
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
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}

	if a.tmux.HasSession(ctx, state.TmuxSession) {
		if err := a.tmux.KillSession(ctx, state.TmuxSession); err != nil {
			return TaskSummary{}, err
		}
	}
	if worktreeValid {
		if err := a.git.RemoveWorktree(ctx, state.RepoRoot, state.WorktreePath); err != nil {
			return TaskSummary{}, err
		}
	} else {
		_ = a.git.PruneWorktrees(ctx, state.RepoRoot)
	}
	if opts.DeleteBranch {
		if a.git.RefExists(ctx, state.RepoRoot, "refs/heads/"+state.Branch) {
			merged, err := a.git.IsBranchMerged(ctx, state.RepoRoot, state.BaseBranch, state.Branch)
			if err != nil {
				return TaskSummary{}, err
			}
			if !merged {
				return TaskSummary{}, errors.New("refusing to delete branch that is not merged")
			}
			if err := a.git.DeleteBranch(ctx, state.RepoRoot, state.Branch); err != nil {
				return TaskSummary{}, err
			}
		}
	}
	if err := a.state.Delete(state.RepoID, state.TaskID); err != nil {
		return TaskSummary{}, err
	}
	return TaskSummary{
		TaskID:   state.TaskID,
		RepoRoot: state.RepoRoot,
		Worktree: state.WorktreePath,
		Branch:   state.Branch,
		Session:  state.TmuxSession,
		Surface:  state.Surface,
		Status:   "deleted",
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
		return a.failState(state, err)
	}

	envVars := map[string]string{}
	if state.AllocatedPort != 0 {
		key := state.PortKey
		if key == "" {
			key = "AGENTFLOW_PORT"
		}
		envVars[key] = fmt.Sprintf("%d", state.AllocatedPort)
	}
	if _, err := writeManagedEnvFile(state.WorktreePath, state.ManagedEnvFile, envVars); err != nil {
		return a.failState(state, err)
	}

	entries := manifestExecutableEntries(runtime.ManifestConfig)
	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ManifestPath, runtime.ManifestFingerprint, entries, a.stdin, a.stdout); err != nil {
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
		TaskID:   state.TaskID,
		RepoRoot: state.RepoRoot,
		Worktree: state.WorktreePath,
		Branch:   state.Branch,
		Session:  state.TmuxSession,
		Surface:  state.Surface,
		Status:   state.Status,
	}, nil
}

func (a *App) Doctor(ctx context.Context, opts DoctorOptions) ([]DoctorCheck, error) {
	checks := make([]DoctorCheck, 0)
	defaults := defaultWorkflowConfig()
	required := defaults.Requirements.Binaries
	var runtime RuntimeConfig
	var err error
	runtime, err = a.loadRuntime(ctx, opts.RepoPath)
	if err == nil {
		required = uniqueStrings(append(required, runtime.Config.Requirements.Binaries...))
	}

	for _, binary := range uniqueStrings(required) {
		_, err := a.exec.Run(ctx, "", nil, "sh", "-lc", "command -v "+shellQuote(binary))
		checks = append(checks, DoctorCheck{
			Name:    "binary:" + binary,
			OK:      err == nil,
			Details: binary,
		})
	}

	_, err = a.exec.Run(ctx, "", nil, "codex", "login", "status")
	checks = append(checks, DoctorCheck{
		Name:    "codex-login",
		OK:      err == nil,
		Details: "codex login status",
	})

	if opts.RepoPath != "" {
		checks = append(checks, DoctorCheck{
			Name:    "manifest",
			OK:      runtime.ManifestExists,
			Details: runtime.ManifestPath,
		})

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
	}

	return checks, nil
}

func (a *App) failState(state TaskState, err error) (TaskSummary, error) {
	state.Status = StatusBroken
	state.FailureReason = err.Error()
	state.UpdatedAt = a.now()
	_ = a.state.Save(state)
	return TaskSummary{
		TaskID:   state.TaskID,
		RepoRoot: state.RepoRoot,
		Worktree: state.WorktreePath,
		Branch:   state.Branch,
		Session:  state.TmuxSession,
		Surface:  state.Surface,
		Status:   state.Status,
	}, err
}

func resolveWorktreeRoot(cfg WorkflowConfig, repoRoot string) (string, error) {
	root := cfg.Repo.WorktreeRoot
	if filepath.IsAbs(root) {
		return root, ensureDir(root)
	}
	resolved := filepath.Join(repoRoot, root)
	return resolved, ensureDir(resolved)
}

func primaryAgentWindow(cfg WorkflowConfig) *TmuxWindowConfig {
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
	for _, command := range runtime.Config.Bootstrap.Commands {
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
	windows := runtime.Config.Tmux.Windows
	if len(windows) == 0 {
		windows = defaultWorkflowConfig().Tmux.Windows
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
		entries := manifestExecutableEntries(runtime.ManifestConfig)
		if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ManifestPath, runtime.ManifestFingerprint, entries, a.stdin, a.stdout); err != nil {
			return err
		}
	}

	for idx, window := range windows {
		command, err := a.windowCommand(runtime.Config, state, window, sessionExists && a.tmux.WindowExists(ctx, state.TmuxSession, window.Name))
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

func (a *App) windowCommand(cfg WorkflowConfig, state TaskState, window TmuxWindowConfig, exists bool) (string, error) {
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

func resolveCommand(cfg WorkflowConfig, state TaskState, name string, surface string) (string, string, error) {
	if name == "review" {
		command := cfg.Commands["review"]
		if command == "" {
			return "", "", errors.New("commands.review is not configured")
		}
		return command, "review", nil
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
