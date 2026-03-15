package agentflow

import (
	"context"
	"net/http"
	"testing"
)

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

func TestResolveLinearTaskUsesIssueIdentity(t *testing.T) {
	t.Parallel()

	ref, id, err := resolveLinearTask("/tmp/example-repo", LinearIssue{
		ID:         "issue-1",
		Identifier: "af-123",
		Title:      "Fix auth flow",
		URL:        "https://linear.app/example/issue/AF-123",
	})
	if err != nil {
		t.Fatalf("resolveLinearTask returned error: %v", err)
	}

	if ref.Source != taskSourceLinear {
		t.Fatalf("unexpected source: %q", ref.Source)
	}
	if ref.Key != "AF-123" {
		t.Fatalf("unexpected key: %q", ref.Key)
	}
	if ref.Title != "AF-123 Fix auth flow" {
		t.Fatalf("unexpected title: %q", ref.Title)
	}
	if ref.Slug != "af-123-fix-auth-flow" {
		t.Fatalf("unexpected slug: %q", ref.Slug)
	}
	if ref.ID != "issue-1" || ref.URL != "https://linear.app/example/issue/AF-123" {
		t.Fatalf("expected issue metadata to be preserved, got %+v", ref)
	}
	if id != taskID("/tmp/example-repo", taskSourceLinear, "issue-1") {
		t.Fatalf("unexpected task id: %q", id)
	}
}

func TestResolveTaskRefPrefersExistingManualTaskOverLinearLookup(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	repoRoot := "/tmp/example-repo"
	ref, id, err := resolveManualTask(repoRoot, "AF-123")
	if err != nil {
		t.Fatalf("resolveManualTask returned error: %v", err)
	}
	state := TaskState{
		TaskID:   id,
		TaskRef:  ref,
		RepoRoot: repoRoot,
		RepoID:   repoID(repoRoot),
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	runtime := RuntimeConfig{
		RepoRoot:   repoRoot,
		RepoID:     repoID(repoRoot),
		ConfigPath: "/tmp/example-repo/.agentflow/config.toml",
		EffectiveConfig: EffectiveConfig{
			Linear: LinearConfig{APIKeyEnv: "LINEAR_API_KEY"},
		},
	}

	resolved, taskID, err := app.resolveTaskRef(context.Background(), runtime, "AF-123")
	if err != nil {
		t.Fatalf("resolveTaskRef returned error: %v", err)
	}
	if resolved.Source != taskSourceManual {
		t.Fatalf("expected manual task, got %+v", resolved)
	}
	if taskID != id {
		t.Fatalf("expected manual task id %q, got %q", id, taskID)
	}
}

func TestResolveTaskRefLoadsExistingLinearTaskWithoutConfig(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	repoRoot := "/tmp/example-repo"
	ref := TaskRef{
		Source: taskSourceLinear,
		Key:    "AF-123",
		Title:  "AF-123 Fix auth flow",
		Slug:   "af-123-fix-auth-flow",
	}
	id := linearTaskID(repoRoot, ref.ID, ref.Key)
	state := TaskState{
		TaskID:   id,
		TaskRef:  ref,
		RepoRoot: repoRoot,
		RepoID:   repoID(repoRoot),
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	runtime := RuntimeConfig{
		RepoRoot: repoRoot,
		RepoID:   repoID(repoRoot),
	}

	resolved, taskID, err := app.resolveTaskRef(context.Background(), runtime, "AF-123")
	if err != nil {
		t.Fatalf("resolveTaskRef returned error: %v", err)
	}
	if resolved.Source != taskSourceLinear {
		t.Fatalf("expected linear task, got %+v", resolved)
	}
	if taskID != id {
		t.Fatalf("expected linear task id %q, got %q", id, taskID)
	}
}

func TestResolveTaskRefLoadsExistingLinearTaskFromDisplayTitle(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	repoRoot := "/tmp/example-repo"
	ref := TaskRef{
		Source: taskSourceLinear,
		Key:    "AF-123",
		Title:  "AF-123 Fix auth flow",
		Slug:   "af-123-fix-auth-flow",
	}
	id := legacyLinearTaskID(repoRoot, ref.Key)
	state := TaskState{
		TaskID:   id,
		TaskRef:  ref,
		RepoRoot: repoRoot,
		RepoID:   repoID(repoRoot),
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	runtime := RuntimeConfig{
		RepoRoot: repoRoot,
		RepoID:   repoID(repoRoot),
	}

	resolved, taskID, err := app.resolveTaskRef(context.Background(), runtime, "AF-123 Fix auth flow")
	if err != nil {
		t.Fatalf("resolveTaskRef returned error: %v", err)
	}
	if resolved.Source != taskSourceLinear {
		t.Fatalf("expected linear task, got %+v", resolved)
	}
	if taskID != id {
		t.Fatalf("expected linear task id %q, got %q", id, taskID)
	}
}

func TestLoadTaskByInputResolvesMovedLinearIssueByStableID(t *testing.T) {
	app, _, _ := newTestApp(t)
	app.linear = newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		return linearHTTPResponse(t, map[string]any{
			"data": map[string]any{
				"issue": map[string]any{
					"id":         "issue-1",
					"identifier": "PLAT-42",
					"title":      "Fix auth flow",
					"url":        "https://linear.app/example/issue/PLAT-42",
					"team":       map[string]any{"id": "team-2", "key": "PLAT", "name": "Platform"},
					"state":      map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
				},
			},
		}), nil
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	repoRoot := "/tmp/example-repo"
	ref := TaskRef{
		Source: taskSourceLinear,
		Key:    "AF-42",
		Title:  "AF-42 Fix auth flow",
		Slug:   "af-42-fix-auth-flow",
		ID:     "issue-1",
		URL:    "https://linear.app/example/issue/AF-42",
	}
	id := legacyLinearTaskID(repoRoot, ref.Key)
	state := TaskState{
		TaskID:   id,
		TaskRef:  ref,
		RepoRoot: repoRoot,
		RepoID:   repoID(repoRoot),
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	runtime := RuntimeConfig{
		RepoRoot: repoRoot,
		RepoID:   repoID(repoRoot),
		Trusted:  true,
		EffectiveConfig: EffectiveConfig{
			Linear: LinearConfig{APIKeyEnv: "LINEAR_API_KEY"},
		},
	}

	loaded, err := app.loadTaskByInput(context.Background(), runtime, "PLAT-42")
	if err != nil {
		t.Fatalf("loadTaskByInput returned error: %v", err)
	}
	if loaded.TaskID != id {
		t.Fatalf("expected legacy task id %q, got %q", id, loaded.TaskID)
	}
	if loaded.TaskRef.Key != "PLAT-42" {
		t.Fatalf("expected refreshed issue key, got %+v", loaded.TaskRef)
	}
	if loaded.TaskRef.ID != "issue-1" {
		t.Fatalf("expected stable issue id to be preserved, got %+v", loaded.TaskRef)
	}
}
