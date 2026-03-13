package agentflow

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

func newTestApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	stateRoot := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exec := Executor{}
	return &App{
		exec:       exec,
		git:        NewGitOps(exec),
		tmux:       NewTmuxOps(exec),
		runner:     AgentRunner{},
		state:      NewStateStore(stateRoot),
		trust:      NewTrustStore(stateRoot),
		stdin:      bytes.NewBufferString(""),
		stdout:     stdout,
		stderr:     stderr,
		now:        func() time.Time { return time.Now().UTC() },
		configPath: "",
	}, stdout, stderr
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
