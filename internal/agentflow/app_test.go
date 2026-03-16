package agentflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testRepoWorkflowConfig = `
[repo]
name = "agentflow-test"
base_branch = "main"
default_surface = "default"

[env]
targets = [{ path = ".env.agentflow" }]

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review", "verify"]
cleanup = "async"

[agents.default]
runner = "codex"
command = "codex --no-alt-screen -s workspace-write -a on-request"
prime_prompt = "Read AGENTS.md and wait for my next instruction."
resume_prompt = "Resume the current task and wait for my next instruction."

[tmux]
session_name = "{{repo}}-{{task}}-{{id}}"

[[tmux.windows]]
name = "editor"
command = "nvim ."

[[tmux.windows]]
name = "verify"
command = "clear"

[[tmux.windows]]
name = "codex"
agent = "default"
`

func TestDownResumesDeletingAndRemovesManagedEnvFiles(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}
	if _, err := writeManagedEnvFiles(worktree, []string{".env.agentflow"}, map[string]map[string]string{
		".env.agentflow": {"VITE_PORT": "4101"},
	}); err != nil {
		t.Fatalf("writeManagedEnvFiles returned error: %v", err)
	}

	app, _, stderr := newTestApp(t)
	now := time.Now().UTC()
	state := TaskState{
		TaskID:          taskID,
		TaskRef:         ref,
		Status:          StatusDeleting,
		RepoRoot:        repoRoot,
		RepoID:          repoID(repoRoot),
		WorktreePath:    worktree,
		Branch:          branch,
		BaseBranch:      "main",
		Surface:         "default",
		TmuxSession:     renderSessionName(cfg, ref, taskID),
		ManagedEnvFiles: []string{".env.agentflow"},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	summary, err := app.Down(ctx, DownOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
	})
	if err != nil {
		t.Fatalf("Down returned error: %v", err)
	}
	if summary.Status != "deleted" {
		t.Fatalf("expected deleted summary, got %+v", summary)
	}
	if _, err := app.state.Load(state.RepoID, state.TaskID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state to be deleted, load err=%v", err)
	}
	if _, err := os.Stat(worktree); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected worktree to be removed, stat err=%v", err)
	}
	if !strings.Contains(stderr.String(), "Resuming teardown for task") {
		t.Fatalf("expected resumable teardown message, got %q", stderr.String())
	}
}

func TestDownStillRefusesUserDirtyFiles(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}
	if _, err := writeManagedEnvFiles(worktree, []string{".env.agentflow"}, map[string]map[string]string{
		".env.agentflow": {"VITE_PORT": "4101"},
	}); err != nil {
		t.Fatalf("writeManagedEnvFiles returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "notes.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatalf("write user file: %v", err)
	}

	app, _, _ := newTestApp(t)
	now := time.Now().UTC()
	state := TaskState{
		TaskID:          taskID,
		TaskRef:         ref,
		Status:          StatusReady,
		RepoRoot:        repoRoot,
		RepoID:          repoID(repoRoot),
		WorktreePath:    worktree,
		Branch:          branch,
		BaseBranch:      "main",
		Surface:         "default",
		TmuxSession:     renderSessionName(cfg, ref, taskID),
		ManagedEnvFiles: []string{".env.agentflow"},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	_, err = app.Down(ctx, DownOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
	})
	if err == nil {
		t.Fatal("expected Down to refuse a dirty user worktree")
	}
	if !strings.Contains(err.Error(), "refusing to remove dirty worktree") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("expected worktree to remain, stat err=%v", err)
	}
	loaded, err := app.state.Load(state.RepoID, state.TaskID)
	if err != nil {
		t.Fatalf("expected state to remain after refusal: %v", err)
	}
	if loaded.Status != StatusReady {
		t.Fatalf("expected state to remain ready, got %+v", loaded)
	}
}

func TestDownForceDeclinedLeavesDirtyWorktree(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "notes.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatalf("write user file: %v", err)
	}

	app, stdout, _ := newTestApp(t)
	app.stdin = bytes.NewBufferString("no\n")
	now := time.Now().UTC()
	state := TaskState{
		TaskID:       taskID,
		TaskRef:      ref,
		Status:       StatusReady,
		RepoRoot:     repoRoot,
		RepoID:       repoID(repoRoot),
		WorktreePath: worktree,
		Branch:       branch,
		BaseBranch:   "main",
		Surface:      "default",
		TmuxSession:  renderSessionName(cfg, ref, taskID),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	_, err = app.Down(ctx, DownOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
		Force:         true,
	})
	if err == nil {
		t.Fatal("expected Down to abort when force teardown is declined")
	}
	if !strings.Contains(err.Error(), "forced teardown declined") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Force remove dirty worktree") {
		t.Fatalf("expected force confirmation prompt, got %q", stdout.String())
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("expected worktree to remain, stat err=%v", err)
	}
	if _, err := app.state.Load(state.RepoID, state.TaskID); err != nil {
		t.Fatalf("expected state to remain after declining force teardown: %v", err)
	}
}

func TestDownForceRemovesDirtyWorktreeAfterConfirmation(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "notes.txt"), []byte("discard me\n"), 0o644); err != nil {
		t.Fatalf("write user file: %v", err)
	}

	app, stdout, _ := newTestApp(t)
	app.stdin = bytes.NewBufferString("yes\n")
	now := time.Now().UTC()
	state := TaskState{
		TaskID:       taskID,
		TaskRef:      ref,
		Status:       StatusReady,
		RepoRoot:     repoRoot,
		RepoID:       repoID(repoRoot),
		WorktreePath: worktree,
		Branch:       branch,
		BaseBranch:   "main",
		Surface:      "default",
		TmuxSession:  renderSessionName(cfg, ref, taskID),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	summary, err := app.Down(ctx, DownOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
		Force:         true,
	})
	if err != nil {
		t.Fatalf("Down returned error: %v", err)
	}
	if summary.Status != "deleted" {
		t.Fatalf("expected deleted summary, got %+v", summary)
	}
	if !strings.Contains(stdout.String(), "Force remove dirty worktree") {
		t.Fatalf("expected force confirmation prompt, got %q", stdout.String())
	}
	if _, err := os.Stat(worktree); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected worktree to be removed, stat err=%v", err)
	}
	if _, err := app.state.Load(state.RepoID, state.TaskID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state to be deleted, load err=%v", err)
	}
}

func TestUpExistingRestoresManagedEnvFiles(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}

	app, _, _ := newTestApp(t)
	now := time.Now().UTC()
	state := TaskState{
		TaskID:             taskID,
		TaskRef:            ref,
		Status:             StatusBroken,
		FailureReason:      "managed env file missing",
		RepoRoot:           repoRoot,
		RepoID:             repoID(repoRoot),
		WorktreePath:       worktree,
		Branch:             branch,
		BaseBranch:         "main",
		Surface:            "default",
		TmuxSession:        renderSessionName(cfg, ref, taskID),
		PrimaryAgentWindow: "codex",
		ManagedEnvFiles:    []string{".env.agentflow"},
		PortBindings: []PortBindingState{
			{Target: ".env.agentflow", Key: "VITE_PORT", Port: 4101},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	summary, err := app.Up(ctx, UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}
	if summary.Status != StatusReady {
		t.Fatalf("expected ready summary, got %+v", summary)
	}

	data, err := os.ReadFile(filepath.Join(worktree, ".env.agentflow"))
	if err != nil {
		t.Fatalf("read restored env file: %v", err)
	}
	if !strings.Contains(string(data), "VITE_PORT=4101") {
		t.Fatalf("expected restored env file to contain VITE_PORT, got %q", string(data))
	}

	loaded, err := app.state.Load(state.RepoID, state.TaskID)
	if err != nil {
		t.Fatalf("load updated state: %v", err)
	}
	if loaded.Status != StatusReady || loaded.FailureReason != "" {
		t.Fatalf("expected state to be repaired to ready, got %+v", loaded)
	}
}

func TestUpRequiresExplicitRepoWorkflow(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)

	app, _, _ := newTestApp(t)
	_, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
	})
	if err == nil {
		t.Fatal("expected up to require repo config")
	}
	if !strings.Contains(err.Error(), "repo config missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpTrustDeclinedLeavesNoTaskState(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	app, _, _ := newTestApp(t)
	app.stdin = bytes.NewBufferString("no\n")

	_, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
	})
	if err == nil {
		t.Fatal("expected trust decline to abort up")
	}
	if !strings.Contains(err.Error(), "repo trust declined") {
		t.Fatalf("unexpected error: %v", err)
	}

	repoRoot, repoErr := filepath.EvalSymlinks(repo)
	if repoErr != nil {
		t.Fatalf("EvalSymlinks returned error: %v", repoErr)
	}
	_, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}
	if _, err := app.state.Load(repoID(repoRoot), taskID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no saved task state, load err=%v", err)
	}
}

func TestUpDeclinedTrustDoesNotFetchLinearIssue(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	stateRoot := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exec := Executor{}
	app := &App{
		exec: exec,
		git:  NewGitOps(exec),
		gh:   NewGitHubOps(exec),
		linear: newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected Linear request before trust: %s", r.URL.String())
			return nil, nil
		}),
		tmux:   NewTmuxOps(exec),
		runner: AgentRunner{},
		state:  NewStateStore(stateRoot),
		trust:  NewTrustStore(stateRoot),
		creds:  NewCredentialStore(stateRoot),
		stdin:  bytes.NewBufferString("no\n"),
		stdout: stdout,
		stderr: stderr,
		now:    func() time.Time { return time.Now().UTC() },
	}
	t.Setenv("LINEAR_API_KEY", "test-token")

	_, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "AF-123",
	})
	if err == nil {
		t.Fatal("expected trust decline to abort linear up")
	}
	if !strings.Contains(err.Error(), "repo trust declined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpDoesNotTransitionLinearIssueBeforeTmuxStarts(t *testing.T) {
	repo := initCommittedRepo(t)
	installFailingTmuxOnNewSession(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	issueUpdateCalls := 0
	app, _, _ := newTestApp(t)
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		switch {
		case strings.Contains(query, "query Issue"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id":         "issue-1",
						"identifier": "AF-123",
						"title":      "Fix auth flow",
						"url":        "https://linear.app/example/issue/AF-123",
						"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
						"state":      map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
					},
				},
			}), nil
		case strings.Contains(query, "mutation IssueUpdate"):
			issueUpdateCalls++
			t.Fatal("unexpected Linear transition before tmux startup succeeded")
		}
		t.Fatalf("unexpected query: %s", query)
		return nil, nil
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	_, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "AF-123",
	})
	if err == nil {
		t.Fatal("expected Up to fail when tmux startup fails")
	}
	if issueUpdateCalls != 0 {
		t.Fatalf("expected no Linear transitions, got %d", issueUpdateCalls)
	}
}

func TestReconcileExistingDoesNotTransitionLinearIssueBeforeTmuxStarts(t *testing.T) {
	repo := initCommittedRepo(t)
	installFailingTmuxOnNewSession(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveLinearTask(repoRoot, LinearIssue{
		ID:         "issue-1",
		Identifier: "AF-123",
		Title:      "Fix auth flow",
		URL:        "https://linear.app/example/issue/AF-123",
	})
	if err != nil {
		t.Fatalf("resolveLinearTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}

	issueUpdateCalls := 0
	app, _, _ := newTestApp(t)
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		switch {
		case strings.Contains(query, "query Issue"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id":          "issue-1",
						"identifier":  "AF-123",
						"title":       "Fix auth flow",
						"url":         "https://linear.app/example/issue/AF-123",
						"description": "Fresh prompt context",
						"team":        map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
						"state":       map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
					},
				},
			}), nil
		case strings.Contains(query, "query WorkflowStates"), strings.Contains(query, "mutation IssueUpdate"):
			issueUpdateCalls++
			t.Fatal("unexpected Linear completion before tmux startup succeeded")
		}
		t.Fatalf("unexpected query: %s", query)
		return nil, nil
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	runtime, err := app.loadRuntime(ctx, repo)
	if err != nil {
		t.Fatalf("loadRuntime returned error: %v", err)
	}
	now := time.Now().UTC()
	state := TaskState{
		TaskID:       taskID,
		TaskRef:      ref,
		Status:       StatusReady,
		RepoRoot:     repoRoot,
		RepoID:       repoID(repoRoot),
		WorktreePath: worktree,
		Branch:       branch,
		BaseBranch:   "main",
		Surface:      runtime.EffectiveConfig.Repo.DefaultSurface,
		TmuxSession:  renderSessionName(runtime.EffectiveConfig, ref, taskID),
		Delivery: TaskDeliveryState{
			State: DeliveryStateMerged,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = app.reconcileExisting(ctx, runtime, state)
	if err == nil {
		t.Fatal("expected reconcileExisting to fail when tmux startup fails")
	}
	if issueUpdateCalls != 0 {
		t.Fatalf("expected no Linear transitions, got %d", issueUpdateCalls)
	}
}

func TestReconcileExistingRecreatesTmuxDuringLinearOutage(t *testing.T) {
	repo := initCommittedRepo(t)
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	installRecordingTmux(t, logPath)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveLinearTask(repoRoot, LinearIssue{
		ID:         "issue-1",
		Identifier: "AF-123",
		Title:      "Fix auth flow",
		URL:        "https://linear.app/example/issue/AF-123",
	})
	if err != nil {
		t.Fatalf("resolveLinearTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}

	app, _, _ := newTestApp(t)
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("linear unavailable")
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	runtime, err := app.loadRuntime(ctx, repo)
	if err != nil {
		t.Fatalf("loadRuntime returned error: %v", err)
	}
	now := time.Now().UTC()
	state := TaskState{
		TaskID:       taskID,
		TaskRef:      ref,
		Status:       StatusReady,
		RepoRoot:     repoRoot,
		RepoID:       repoID(repoRoot),
		WorktreePath: worktree,
		Branch:       branch,
		BaseBranch:   "main",
		Surface:      runtime.EffectiveConfig.Repo.DefaultSurface,
		TmuxSession:  renderSessionName(runtime.EffectiveConfig, ref, taskID),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	_, err = app.reconcileExisting(ctx, runtime, state)
	if err == nil {
		t.Fatal("expected reconcileExisting to report the Linear outage")
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	if !strings.Contains(string(logData), "new-session") {
		t.Fatalf("expected tmux recovery to create a session, log=%q", string(logData))
	}
}

func TestAttachRefreshesLinearContextBeforeRecreatingTmux(t *testing.T) {
	repo := initCommittedRepo(t)
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	installRecordingTmux(t, logPath)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveLinearTask(repoRoot, LinearIssue{
		ID:         "issue-1",
		Identifier: "AF-123",
		Title:      "Fix auth flow",
		URL:        "https://linear.app/example/issue/AF-123",
	})
	if err != nil {
		t.Fatalf("resolveLinearTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}

	app, _, _ := newTestApp(t)
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		if !strings.Contains(query, "query Issue") {
			t.Fatalf("unexpected query: %s", query)
		}
		return linearHTTPResponse(t, map[string]any{
			"data": map[string]any{
				"issue": map[string]any{
					"id":          "issue-1",
					"identifier":  "AF-123",
					"title":       "Fix auth flow",
					"url":         "https://linear.app/example/issue/AF-123",
					"description": "Fresh prompt context",
					"team":        map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
					"state":       map[string]any{"id": "state-2", "name": "In Progress", "type": "started"},
					"comments": map[string]any{
						"nodes": []map[string]any{
							{
								"id":        "comment-1",
								"body":      "Include this comment in the agent prompt.",
								"createdAt": "2026-03-15T10:00:00Z",
								"url":       "https://linear.app/example/comment/comment-1",
								"user":      map[string]any{"name": "Alice"},
								"parent":    nil,
							},
						},
					},
				},
			},
		}), nil
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	now := time.Now().UTC()
	state := TaskState{
		TaskID:       taskID,
		TaskRef:      ref,
		Status:       StatusReady,
		RepoRoot:     repoRoot,
		RepoID:       repoID(repoRoot),
		WorktreePath: worktree,
		Branch:       branch,
		BaseBranch:   "main",
		Surface:      "default",
		TmuxSession:  renderSessionName(cfg, ref, taskID),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if _, err := app.Attach(ctx, CommonOptions{RepoPath: repo}, "AF-123"); err != nil {
		t.Fatalf("Attach returned error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	if !strings.Contains(string(logData), "Fresh prompt context") || !strings.Contains(string(logData), "Include this comment in the agent prompt.") {
		t.Fatalf("expected recreated tmux command to include refreshed Linear context, log=%q", string(logData))
	}
	loaded, err := app.state.Load(repoID(repoRoot), taskID)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.IssueContext == nil || loaded.IssueContext.Description != "Fresh prompt context" {
		t.Fatalf("expected refreshed issue context to be saved, got %+v", loaded.IssueContext)
	}
}

func TestUpLaunchesAgentWithStartedLinearState(t *testing.T) {
	repo := initCommittedRepo(t)
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	installRecordingTmux(t, logPath)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	app, _, _ := newTestApp(t)
	issueQueryCount := 0
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		switch {
		case strings.Contains(query, "query WorkflowStates"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"workflowStates": map[string]any{
						"nodes": []map[string]any{
							{"id": "state-1", "name": "Todo", "type": "unstarted"},
							{"id": "state-2", "name": "In Progress", "type": "started"},
						},
					},
				},
			}), nil
		case strings.Contains(query, "mutation IssueUpdate"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issueUpdate": map[string]any{
						"success": true,
						"issue":   map[string]any{"id": "issue-1"},
					},
				},
			}), nil
		case strings.Contains(query, "query Issue("):
			issueQueryCount++
			stateName := "Todo"
			stateType := "unstarted"
			if issueQueryCount > 1 {
				stateName = "In Progress"
				stateType = "started"
			}
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id":         "issue-1",
						"identifier": "AF-123",
						"title":      "Fix auth flow",
						"url":        "https://linear.app/example/issue/AF-123",
						"updatedAt":  "2026-03-15T10:00:00Z",
						"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
						"state":      map[string]any{"id": "state-1", "name": stateName, "type": stateType},
					},
				},
			}), nil
		default:
			t.Fatalf("unexpected query: %s", query)
			return nil, nil
		}
	})
	t.Setenv("LINEAR_API_KEY", "test-token")
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)

	if _, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "AF-123",
	}); err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	logData := string(logBytes)
	if !strings.Contains(logData, "State: In Progress") {
		t.Fatalf("expected codex launch to include the started Linear state, log=%q", logData)
	}
	if strings.Contains(logData, "State: Todo") {
		t.Fatalf("did not expect codex launch to include the stale unstarted state, log=%q", logData)
	}
}

func TestReconcileExistingLaunchesAgentWithCompletedLinearState(t *testing.T) {
	repo := initCommittedRepo(t)
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	installRecordingTmux(t, logPath)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveLinearTask(repoRoot, LinearIssue{
		ID:         "issue-1",
		Identifier: "AF-123",
		Title:      "Fix auth flow",
		URL:        "https://linear.app/example/issue/AF-123",
	})
	if err != nil {
		t.Fatalf("resolveLinearTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}

	app, _, _ := newTestApp(t)
	issueQueryCount := 0
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		switch {
		case strings.Contains(query, "query WorkflowStates"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"workflowStates": map[string]any{
						"nodes": []map[string]any{
							{"id": "state-1", "name": "In Review", "type": "started"},
							{"id": "state-2", "name": "Done", "type": "completed"},
						},
					},
				},
			}), nil
		case strings.Contains(query, "mutation IssueUpdate"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issueUpdate": map[string]any{
						"success": true,
						"issue":   map[string]any{"id": "issue-1"},
					},
				},
			}), nil
		case strings.Contains(query, "query Issue("):
			issueQueryCount++
			stateName := "In Review"
			stateType := "started"
			if issueQueryCount > 1 {
				stateName = "Done"
				stateType = "completed"
			}
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id":         "issue-1",
						"identifier": "AF-123",
						"title":      "Fix auth flow",
						"url":        "https://linear.app/example/issue/AF-123",
						"updatedAt":  "2026-03-15T10:00:00Z",
						"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
						"state":      map[string]any{"id": "state-1", "name": stateName, "type": stateType},
					},
				},
			}), nil
		default:
			t.Fatalf("unexpected query: %s", query)
			return nil, nil
		}
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	runtime, err := app.loadRuntime(ctx, repo)
	if err != nil {
		t.Fatalf("loadRuntime returned error: %v", err)
	}
	now := time.Now().UTC()
	state := TaskState{
		TaskID:       taskID,
		TaskRef:      ref,
		Status:       StatusReady,
		RepoRoot:     repoRoot,
		RepoID:       repoID(repoRoot),
		WorktreePath: worktree,
		Branch:       branch,
		BaseBranch:   "main",
		Surface:      runtime.EffectiveConfig.Repo.DefaultSurface,
		TmuxSession:  renderSessionName(runtime.EffectiveConfig, ref, taskID),
		Delivery: TaskDeliveryState{
			State: DeliveryStateMerged,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := app.reconcileExisting(ctx, runtime, state); err != nil {
		t.Fatalf("reconcileExisting returned error: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tmux log: %v", err)
	}
	logData := string(logBytes)
	if !strings.Contains(logData, "State: Done") {
		t.Fatalf("expected codex launch to include the completed Linear state, log=%q", logData)
	}
	if strings.Contains(logData, "State: In Review") {
		t.Fatalf("did not expect codex launch to include the stale started state, log=%q", logData)
	}
}

func TestUpDoesNotTransitionLinearIssueWhenCodexWindowCreationFails(t *testing.T) {
	repo := initCommittedRepo(t)
	installFailingTmuxOnWindow(t, "codex")
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	issueUpdateCalls := 0
	app, _, _ := newTestApp(t)
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		switch {
		case strings.Contains(query, "query Issue"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id":         "issue-1",
						"identifier": "AF-123",
						"title":      "Fix auth flow",
						"url":        "https://linear.app/example/issue/AF-123",
						"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
						"state":      map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
					},
				},
			}), nil
		case strings.Contains(query, "query WorkflowStates"), strings.Contains(query, "mutation IssueUpdate"):
			issueUpdateCalls++
			t.Fatal("unexpected Linear transition when codex window creation failed")
		}
		t.Fatalf("unexpected query: %s", query)
		return nil, nil
	})
	t.Setenv("LINEAR_API_KEY", "test-token")
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)

	_, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "AF-123",
	})
	if err == nil {
		t.Fatal("expected Up to fail when codex window creation fails")
	}
	if issueUpdateCalls != 0 {
		t.Fatalf("expected no Linear transitions, got %d", issueUpdateCalls)
	}
}

func TestReconcileExistingDoesNotTransitionLinearIssueWhenCodexWindowCreationFails(t *testing.T) {
	repo := initCommittedRepo(t)
	installFailingTmuxOnWindow(t, "codex")
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "LINEAR_API_KEY"
`)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveLinearTask(repoRoot, LinearIssue{
		ID:         "issue-1",
		Identifier: "AF-123",
		Title:      "Fix auth flow",
		URL:        "https://linear.app/example/issue/AF-123",
	})
	if err != nil {
		t.Fatalf("resolveLinearTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}

	issueUpdateCalls := 0
	app, _, _ := newTestApp(t)
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		switch {
		case strings.Contains(query, "query Issue"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id":         "issue-1",
						"identifier": "AF-123",
						"title":      "Fix auth flow",
						"url":        "https://linear.app/example/issue/AF-123",
						"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
						"state":      map[string]any{"id": "state-1", "name": "In Review", "type": "started"},
					},
				},
			}), nil
		case strings.Contains(query, "query WorkflowStates"), strings.Contains(query, "mutation IssueUpdate"):
			issueUpdateCalls++
			t.Fatal("unexpected Linear transition when codex window creation failed")
		}
		t.Fatalf("unexpected query: %s", query)
		return nil, nil
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	runtime, err := app.loadRuntime(ctx, repo)
	if err != nil {
		t.Fatalf("loadRuntime returned error: %v", err)
	}
	now := time.Now().UTC()
	state := TaskState{
		TaskID:       taskID,
		TaskRef:      ref,
		Status:       StatusReady,
		RepoRoot:     repoRoot,
		RepoID:       repoID(repoRoot),
		WorktreePath: worktree,
		Branch:       branch,
		BaseBranch:   "main",
		Surface:      runtime.EffectiveConfig.Repo.DefaultSurface,
		TmuxSession:  renderSessionName(runtime.EffectiveConfig, ref, taskID),
		Delivery: TaskDeliveryState{
			State: DeliveryStateMerged,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = app.reconcileExisting(ctx, runtime, state)
	if err == nil {
		t.Fatal("expected reconcileExisting to fail when codex window creation fails")
	}
	if issueUpdateCalls != 0 {
		t.Fatalf("expected no Linear transitions, got %d", issueUpdateCalls)
	}
}

func TestUpSavesStateBeforeCreatingWorktree(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	repoRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	_, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	statePath := app.state.taskPath(repoID(repoRoot), taskID)

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("LookPath git returned error: %v", err)
	}
	wrapperDir := t.TempDir()
	wrapperPath := filepath.Join(wrapperDir, "git")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "worktree" ] && [ "$2" = "add" ]; then
  if [ ! -f %s ]; then
    echo "state missing before worktree add" >&2
    exit 99
  fi
fi
exec %s "$@"
`, shellQuote(statePath), shellQuote(realGit))
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write git wrapper: %v", err)
	}
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}
	if summary.Status != StatusReady {
		t.Fatalf("expected ready summary, got %+v", summary)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected task state to exist, stat err=%v", err)
	}
}

func TestDownDeletesDiscardableCreatingState(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	now := time.Now().UTC()
	state := TaskState{
		TaskID:      taskID,
		TaskRef:     ref,
		Status:      StatusCreating,
		RepoRoot:    repoRoot,
		RepoID:      repoID(repoRoot),
		Branch:      branchName(cfg, ref, taskID),
		BaseBranch:  "main",
		Surface:     "default",
		TmuxSession: renderSessionName(cfg, ref, taskID),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	app, _, _ := newTestApp(t)
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	summary, err := app.Down(ctx, DownOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
	})
	if err != nil {
		t.Fatalf("Down returned error: %v", err)
	}
	if summary.Status != "deleted" {
		t.Fatalf("expected deleted summary, got %+v", summary)
	}
	if _, err := app.state.Load(state.RepoID, state.TaskID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected state to be deleted, load err=%v", err)
	}
}

func TestUpRecoversLegacyCreatingStateWithBlankPath(t *testing.T) {
	repo := initCommittedRepo(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	ctx := context.Background()
	exec := Executor{}
	git := NewGitOps(exec)
	repoRoot, err := git.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	ref, taskID, err := resolveManualTask(repoRoot, "smoke test")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = filepath.Base(repo)
	branch := branchName(cfg, ref, taskID)
	worktree := filepath.Join(t.TempDir(), ref.Slug+"-"+taskID[:6])
	if err := git.CreateWorktree(ctx, repo, branch, worktree, "main"); err != nil {
		t.Fatalf("CreateWorktree returned error: %v", err)
	}

	app, _, stderr := newTestApp(t)
	now := time.Now().UTC()
	state := TaskState{
		TaskID:             taskID,
		TaskRef:            ref,
		Status:             StatusCreating,
		RepoRoot:           repoRoot,
		RepoID:             repoID(repoRoot),
		Branch:             branch,
		BaseBranch:         "main",
		Surface:            "default",
		TmuxSession:        renderSessionName(cfg, ref, taskID),
		PrimaryAgentWindow: "codex",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	summary, err := app.Up(ctx, UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "smoke test",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}
	if summary.Status != StatusReady {
		t.Fatalf("expected ready summary, got %+v", summary)
	}
	if summary.Worktree != canonicalPath(worktree) {
		t.Fatalf("expected recovered worktree %q, got %q", canonicalPath(worktree), summary.Worktree)
	}
	loaded, err := app.state.Load(state.RepoID, state.TaskID)
	if err != nil {
		t.Fatalf("load updated state: %v", err)
	}
	if loaded.WorktreePath != canonicalPath(worktree) {
		t.Fatalf("expected saved state to recover worktree path, got %+v", loaded)
	}
	if strings.Contains(stderr.String(), "Discarding stale task state") {
		t.Fatalf("did not expect recoverable creating state to be discarded, got %q", stderr.String())
	}
}

func TestBootstrapDoctorDetailsWarnsWhenLockfileNeedsBootstrap(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "bun.lock"), []byte("lockfile"), 0o644); err != nil {
		t.Fatalf("write bun.lock: %v", err)
	}

	details := bootstrapDoctorDetails(RuntimeConfig{RepoRoot: repo})
	if !strings.Contains(details, "bun.lock detected") {
		t.Fatalf("expected bun.lock advisory, got %q", details)
	}
	if !strings.Contains(details, "[bootstrap].commands") {
		t.Fatalf("expected bootstrap guidance, got %q", details)
	}
}

func TestBootstrapDoctorDetailsReportsConfiguredBootstrap(t *testing.T) {
	t.Parallel()

	details := bootstrapDoctorDetails(RuntimeConfig{
		EffectiveConfig: EffectiveConfig{
			Bootstrap: BootstrapConfig{
				Commands: []string{"bun install --frozen-lockfile"},
			},
		},
	})
	if details != "bootstrap commands configured" {
		t.Fatalf("unexpected bootstrap details: %q", details)
	}
}

func newTestApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	stateRoot := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exec := Executor{}
	return &App{
		exec:   exec,
		git:    NewGitOps(exec),
		gh:     NewGitHubOps(exec),
		tmux:   NewTmuxOps(exec),
		runner: AgentRunner{},
		state:  NewStateStore(stateRoot),
		trust:  NewTrustStore(stateRoot),
		creds:  NewCredentialStore(stateRoot),
		stdin:  bytes.NewBufferString("yes\n"),
		stdout: stdout,
		stderr: stderr,
		now:    func() time.Time { return time.Now().UTC() },
	}, stdout, stderr
}

func writeTestRepoConfig(t *testing.T, repo string, contents string) {
	t.Helper()
	dir := filepath.Join(repo, ".agentflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
}

func installFakeTmux(t *testing.T) {
	t.Helper()

	binDir := t.TempDir()
	script := filepath.Join(binDir, "tmux")
	content := "#!/bin/sh\ncase \"$1\" in\n  has-session)\n    exit 1\n    ;;\n  kill-session)\n    exit 0\n    ;;\n  *)\n    exit 0\n    ;;\nesac\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installFailingTmuxOnNewSession(t *testing.T) {
	t.Helper()

	binDir := t.TempDir()
	script := filepath.Join(binDir, "tmux")
	content := "#!/bin/sh\ncase \"$1\" in\n  has-session)\n    exit 1\n    ;;\n  new-session)\n    echo \"tmux startup failed\" >&2\n    exit 1\n    ;;\n  *)\n    exit 0\n    ;;\nesac\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write failing tmux: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installRecordingTmux(t *testing.T, logPath string) {
	t.Helper()

	binDir := t.TempDir()
	script := filepath.Join(binDir, "tmux")
	content := fmt.Sprintf(`#!/bin/sh
{
  printf 'cmd:'
  for arg in "$@"; do
    printf ' %%s' "$arg"
  done
  printf '\n'
} >> %s
case "$1" in
  has-session)
    exit 1
    ;;
  list-windows)
    exit 0
    ;;
  display-message)
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
`, shellQuote(logPath))
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write recording tmux: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installFailingTmuxOnWindow(t *testing.T, windowName string) {
	t.Helper()

	binDir := t.TempDir()
	script := filepath.Join(binDir, "tmux")
	content := fmt.Sprintf(`#!/bin/sh
case "$1" in
  has-session)
    exit 1
    ;;
  new-window)
    shift
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "-n" ]; then
        shift
        if [ "$1" = %s ]; then
          echo "tmux window failed" >&2
          exit 1
        fi
      fi
      shift
    done
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`, shellQuote(windowName))
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write failing tmux window helper: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
