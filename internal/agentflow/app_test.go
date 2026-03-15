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
