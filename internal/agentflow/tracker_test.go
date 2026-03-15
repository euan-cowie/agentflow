package agentflow

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinearIssuesDeclinedTrustDoesNotFetchPickerIssues(t *testing.T) {
	repo := initCommittedRepo(t)
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
			t.Fatalf("unexpected Linear picker request before trust: %s", r.URL.String())
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

	_, err := app.LinearIssues(context.Background(), CommonOptions{RepoPath: repo})
	if err == nil {
		t.Fatal("expected LinearIssues to fail when trust is declined")
	}
	if !strings.Contains(err.Error(), "repo trust declined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusSkipsLinearReconcileWhenRepoUntrusted(t *testing.T) {
	repo := initCommittedRepo(t)
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
		t.Fatalf("unexpected Linear status request before trust: %s", r.URL.String())
		return nil, nil
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
		IssueState:   "In Progress",
		Delivery: TaskDeliveryState{
			State: DeliveryStateMerged,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	statuses, err := app.Status(ctx, CommonOptions{RepoPath: repo}, "")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected one status, got %d", len(statuses))
	}
	if statuses[0].IssueState != "In Progress" {
		t.Fatalf("expected cached issue state, got %+v", statuses[0])
	}
}

func TestStatusPersistsRefreshedLinearIssueContext(t *testing.T) {
	repo := initCommittedRepo(t)
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
					"description": "Fresh description from Linear",
					"team":        map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
					"state":       map[string]any{"id": "state-2", "name": "In Progress", "type": "started"},
					"labels": map[string]any{
						"nodes": []map[string]any{
							{"name": "Frontend"},
						},
					},
					"comments": map[string]any{
						"nodes": []map[string]any{
							{
								"id":        "comment-1",
								"body":      "Use the new copy deck.",
								"createdAt": "2026-03-15T10:00:00Z",
								"url":       "https://linear.app/example/comment/comment-1",
								"user":      map[string]any{"name": "Alice"},
								"parent":    nil,
							},
						},
					},
					"attachments": map[string]any{
						"nodes": []map[string]any{
							{
								"id":         "attachment-1",
								"title":      "Spec",
								"subtitle":   "Figma",
								"url":        "https://example.com/spec",
								"sourceType": "link",
								"createdAt":  "2026-03-15T09:00:00Z",
							},
						},
					},
				},
			},
		}), nil
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	runtime, err := app.loadRuntime(ctx, repo)
	if err != nil {
		t.Fatalf("loadRuntime returned error: %v", err)
	}
	if err := app.ensureWorkflowTrusted(&runtime); err != nil {
		t.Fatalf("ensureWorkflowTrusted returned error: %v", err)
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
		Surface:      "default",
		TmuxSession:  renderSessionName(cfg, ref, taskID),
		IssueState:   "In Progress",
		IssueContext: &LinearIssueContext{
			Description: "Old description",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := app.state.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	statuses, err := app.Status(ctx, CommonOptions{RepoPath: repo}, "AF-123")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected one status, got %d", len(statuses))
	}

	loaded, err := app.state.Load(repoID(repoRoot), taskID)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.IssueContext == nil || loaded.IssueContext.Description != "Fresh description from Linear" {
		t.Fatalf("expected refreshed issue context to be persisted, got %+v", loaded.IssueContext)
	}
	if len(loaded.IssueContext.Comments) != 1 || loaded.IssueContext.Comments[0].Author != "Alice" {
		t.Fatalf("expected refreshed comments to be persisted, got %+v", loaded.IssueContext)
	}
}

func TestReconcileLinearTaskPreservesCanceledIssues(t *testing.T) {
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
						"state":      map[string]any{"id": "state-9", "name": "Canceled", "type": "canceled"},
					},
				},
			}), nil
		case strings.Contains(query, "mutation IssueUpdate"):
			t.Fatal("unexpected completion transition for canceled Linear issue")
		}
		t.Fatalf("unexpected query: %s", query)
		return nil, nil
	})
	t.Setenv("LINEAR_API_KEY", "test-token")

	runtime := RuntimeConfig{
		RepoRoot: "/tmp/example-repo",
		EffectiveConfig: EffectiveConfig{
			Linear: LinearConfig{APIKeyEnv: "LINEAR_API_KEY"},
		},
	}
	state := TaskState{
		RepoRoot: "/tmp/example-repo",
		TaskRef: TaskRef{
			Source: taskSourceLinear,
			Key:    "AF-123",
			Title:  "AF-123 Fix auth flow",
		},
		IssueState: "In Progress",
		Delivery: TaskDeliveryState{
			State: DeliveryStateMerged,
		},
	}

	if err := app.reconcileLinearTask(context.Background(), runtime, &state); err != nil {
		t.Fatalf("reconcileLinearTask returned error: %v", err)
	}
	if state.IssueState != "Canceled" {
		t.Fatalf("expected canceled issue state to be preserved, got %+v", state)
	}
}

func TestReconcileLinearTaskFallsBackToCachedStateWithoutCredential(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	runtime := RuntimeConfig{
		RepoRoot: "/tmp/example-repo",
		EffectiveConfig: EffectiveConfig{
			Linear: LinearConfig{APIKeyEnv: "LINEAR_API_KEY"},
		},
	}
	state := TaskState{
		RepoRoot: "/tmp/example-repo",
		TaskRef: TaskRef{
			Source: taskSourceLinear,
			Key:    "AF-123",
			Title:  "AF-123 Fix auth flow",
			ID:     "issue-1",
			URL:    "https://linear.app/example/issue/AF-123",
		},
		IssueState: "In Progress",
		Delivery: TaskDeliveryState{
			State: DeliveryStateMerged,
		},
	}

	if err := app.reconcileLinearTask(context.Background(), runtime, &state); err != nil {
		t.Fatalf("reconcileLinearTask returned error: %v", err)
	}
	if state.IssueState != "In Progress" {
		t.Fatalf("expected cached issue state to be preserved, got %+v", state)
	}
}
