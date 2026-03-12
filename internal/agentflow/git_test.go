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
