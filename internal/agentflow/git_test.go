package agentflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBaseRefFallsBackToCurrentBranch(t *testing.T) {
	t.Parallel()

	repo := initCommittedRepo(t)
	git := NewGitOps(Executor{})

	base, fellBack, err := git.ResolveBaseRef(context.Background(), repo, "origin/main")
	if err != nil {
		t.Fatalf("ResolveBaseRef returned error: %v", err)
	}
	if base != "main" {
		t.Fatalf("expected fallback to main, got %q", base)
	}
	if !fellBack {
		t.Fatal("expected fallback flag to be true")
	}
}

func TestResolveBaseRefRejectsUnbornRepo(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	git := NewGitOps(Executor{})

	_, _, err := git.ResolveBaseRef(context.Background(), repo, "origin/main")
	if err == nil {
		t.Fatal("expected unborn repo error")
	}
	if !strings.Contains(err.Error(), "repo has no commits yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsDirtyIgnoringManagedEnvFiles(t *testing.T) {
	t.Parallel()

	repo := initCommittedRepo(t)
	git := NewGitOps(Executor{})

	if err := os.WriteFile(filepath.Join(repo, ".env.agentflow"), []byte("VITE_PORT=4101\n"), 0o644); err != nil {
		t.Fatalf("write managed env file: %v", err)
	}

	dirty, err := git.IsDirty(context.Background(), repo)
	if err != nil {
		t.Fatalf("IsDirty returned error: %v", err)
	}
	if !dirty {
		t.Fatal("expected untracked managed env file to make repo dirty without ignore rules")
	}

	dirty, err = git.IsDirtyIgnoring(context.Background(), repo, []string{".env.agentflow"})
	if err != nil {
		t.Fatalf("IsDirtyIgnoring returned error: %v", err)
	}
	if dirty {
		t.Fatal("expected managed env file to be ignored in dirty check")
	}
}

func initCommittedRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Agentflow Test")
	runGit(t, repo, "config", "user.email", "agentflow@example.com")
	path := filepath.Join(repo, "README.md")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	exec := Executor{}
	if _, err := exec.Run(context.Background(), dir, nil, "git", args...); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}
