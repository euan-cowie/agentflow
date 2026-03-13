package agentflow

import "testing"

func TestResolveManualTaskDeterministic(t *testing.T) {
	t.Parallel()

	repoRoot := "/tmp/example-repo"
	ref, id, err := resolveManualTask(repoRoot, "  Fix Auth Flow  ")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}

	if ref.Key != "fix auth flow" {
		t.Fatalf("unexpected key: %q", ref.Key)
	}
	if ref.Title != "Fix Auth Flow" {
		t.Fatalf("unexpected title: %q", ref.Title)
	}
	if ref.Slug != "fix-auth-flow" {
		t.Fatalf("unexpected slug: %q", ref.Slug)
	}

	ref2, id2, err := resolveManualTask(repoRoot, "fix auth flow")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}
	if id != id2 {
		t.Fatalf("expected stable task id, got %q and %q", id, id2)
	}
	if ref2.Slug != ref.Slug {
		t.Fatalf("expected stable slug, got %q and %q", ref.Slug, ref2.Slug)
	}
}

func TestBranchAndSessionNamesIncludeStableID(t *testing.T) {
	t.Parallel()

	cfg := defaultEffectiveConfig()
	cfg.Repo.Name = "Coach Connect"
	cfg.Repo.BranchPrefix = "feature"

	ref := TaskRef{Slug: "fix-auth"}
	taskID := "1234567890abcdef"

	branch := branchName(cfg, ref, taskID)
	if branch != "feature/fix-auth-123456" {
		t.Fatalf("unexpected branch name: %q", branch)
	}

	session := renderSessionName(cfg, ref, taskID)
	if session != "coach-connect-fix-auth-123456" {
		t.Fatalf("unexpected session name: %q", session)
	}
}
