package agentflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRepoGitHubConfig = `
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

[commands]
review = "true"
verify_quick = "true"

[github]
enabled = true
draft_on_submit = true
merge_method = "auto"
auto_merge = true
delete_remote_branch = true

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

const testRepoLandOrderConfig = `
[repo]
name = "agentflow-test"
base_branch = "main"
default_surface = "default"

[env]
targets = [{ path = ".env.agentflow" }]

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review"]
cleanup = "async"

[commands]
review = "git rev-parse HEAD > $(git rev-parse --git-dir)/preflight-head"
verify_quick = "true"

[github]
enabled = true
draft_on_submit = true
merge_method = "auto"
auto_merge = true
delete_remote_branch = true

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

const testRepoUpstreamBaseConfig = `
[repo]
name = "agentflow-test"
base_branch = "upstream/main"
default_surface = "default"

[env]
targets = [{ path = ".env.agentflow" }]

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review", "verify"]
cleanup = "async"

[commands]
review = "true"
verify_quick = "true"

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

const testRepoGitHubUpstreamBaseConfig = `
[repo]
name = "agentflow-test"
base_branch = "upstream/main"
default_surface = "default"

[env]
targets = [{ path = ".env.agentflow" }]

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review", "verify"]
cleanup = "async"

[commands]
review = "true"
verify_quick = "true"

[github]
enabled = true
draft_on_submit = true
merge_method = "auto"
auto_merge = true
delete_remote_branch = true

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

const testRepoFailingLandPreflightConfig = `
[repo]
name = "agentflow-test"
base_branch = "main"
default_surface = "default"

[env]
targets = [{ path = ".env.agentflow" }]

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review"]
cleanup = "async"

[commands]
review = "false"
verify_quick = "true"

[github]
enabled = true
draft_on_submit = true
merge_method = "auto"
auto_merge = true
delete_remote_branch = true

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

const testRepoDirtyLandPreflightConfig = `
[repo]
name = "agentflow-test"
base_branch = "main"
default_surface = "default"

[env]
targets = [{ path = ".env.agentflow" }]

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review"]
cleanup = "async"

[commands]
review = "touch preflight-dirty.txt"
verify_quick = "true"

[github]
enabled = true
draft_on_submit = true
merge_method = "auto"
auto_merge = true
delete_remote_branch = true

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

func TestSyncRebasesTaskBranch(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "sync smoke",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "task.txt"), []byte("task\n"), 0o644); err != nil {
		t.Fatalf("write task file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "task.txt")
	runGit(t, summary.Worktree, "commit", "-m", "task commit")

	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write base file: %v", err)
	}
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "base commit")
	runGit(t, repo, "push", "origin", "main")

	summaries, err := app.Sync(context.Background(), SyncOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "sync smoke",
	})
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one sync summary, got %d", len(summaries))
	}
	if summaries[0].Delivery.LastSyncedAt.IsZero() {
		t.Fatalf("expected last_synced_at to be recorded, got %+v", summaries[0].Delivery)
	}

	git := NewGitOps(Executor{})
	baseRef := git.RemoteTrackingRef(context.Background(), repo, "origin", "main")
	behind, ahead, err := git.RevListCounts(context.Background(), summary.Worktree, baseRef, "HEAD")
	if err != nil {
		t.Fatalf("RevListCounts returned error: %v", err)
	}
	if behind != 0 {
		t.Fatalf("expected branch to be up to date with base, got behind=%d ahead=%d", behind, ahead)
	}

	loaded, err := app.state.Load(repoID(canonicalPath(repo)), summaries[0].TaskID)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.Delivery.State != DeliveryStateLocal {
		t.Fatalf("expected local delivery state after sync, got %+v", loaded.Delivery)
	}
	if loaded.Delivery.LastBaseSHA == "" || loaded.Delivery.LastHeadSHA == "" {
		t.Fatalf("expected sync to record base/head sha, got %+v", loaded.Delivery)
	}
}

func TestSubmitCreatesDraftPullRequest(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	_ = installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "submit smoke",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "submit.txt"), []byte("submit\n"), 0o644); err != nil {
		t.Fatalf("write submit file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "submit.txt")
	runGit(t, summary.Worktree, "commit", "-m", "submit commit")

	submitted, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "submit smoke",
	})
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if submitted.Delivery.PullRequestNumber != 17 {
		t.Fatalf("expected PR number to be recorded, got %+v", submitted.Delivery)
	}
	if submitted.Delivery.State != DeliveryStateDraft {
		t.Fatalf("expected draft delivery state, got %+v", submitted.Delivery)
	}

	loaded, err := app.state.Load(repoID(canonicalPath(repo)), submitted.TaskID)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.Delivery.PullRequestURL == "" || loaded.Delivery.RemoteBranch == "" {
		t.Fatalf("expected submit to persist PR metadata, got %+v", loaded.Delivery)
	}
}

func TestSubmitUsesBranchNameForUpstreamBasePullRequest(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	ghStateDir := installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubUpstreamBaseConfig)

	upstream := filepath.Join(t.TempDir(), "upstream.git")
	runGit(t, "", "init", "--bare", upstream)
	runGit(t, repo, "remote", "add", "upstream", upstream)
	runGit(t, repo, "push", "upstream", "main")

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "upstream submit",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "upstream-submit.txt"), []byte("submit\n"), 0o644); err != nil {
		t.Fatalf("write submit file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "upstream-submit.txt")
	runGit(t, summary.Worktree, "commit", "-m", "upstream submit commit")

	if _, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "upstream submit",
	}); err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}

	prState := readFakePullRequestState(t, ghStateDir)
	if prState.Base != "main" {
		t.Fatalf("expected GitHub base branch to be main, got %q", prState.Base)
	}
}

func TestSubmitReadyPromotesExistingDraftPullRequest(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	_ = installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "submit ready",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "submit-ready.txt"), []byte("submit ready\n"), 0o644); err != nil {
		t.Fatalf("write submit-ready file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "submit-ready.txt")
	runGit(t, summary.Worktree, "commit", "-m", "submit ready commit")

	if _, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "submit ready",
	}); err != nil {
		t.Fatalf("initial Submit returned error: %v", err)
	}

	submitted, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "submit ready",
		Ready:         true,
	})
	if err != nil {
		t.Fatalf("Submit --ready returned error: %v", err)
	}
	if submitted.Delivery.State != DeliveryStateSubmitted {
		t.Fatalf("expected submitted delivery state, got %+v", submitted.Delivery)
	}
}

func TestLandRunsPreflightAfterSync(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	_ = installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoLandOrderConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "land order",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "land-order.txt"), []byte("land order\n"), 0o644); err != nil {
		t.Fatalf("write task file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "land-order.txt")
	runGit(t, summary.Worktree, "commit", "-m", "land order task commit")

	if err := os.WriteFile(filepath.Join(repo, "base-land.txt"), []byte("base land\n"), 0o644); err != nil {
		t.Fatalf("write base file: %v", err)
	}
	runGit(t, repo, "add", "base-land.txt")
	runGit(t, repo, "commit", "-m", "base for land")
	runGit(t, repo, "push", "origin", "main")

	landed, err := app.Land(context.Background(), LandOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "land order",
	})
	if err != nil {
		t.Fatalf("Land returned error: %v", err)
	}
	if landed.Delivery.State != DeliveryStateQueued {
		t.Fatalf("expected queued delivery state, got %+v", landed.Delivery)
	}

	gitDir := strings.TrimSpace(runGitCapture(t, summary.Worktree, "rev-parse", "--git-dir"))
	recordedHeadBytes, err := os.ReadFile(filepath.Join(gitDir, "preflight-head"))
	if err != nil {
		t.Fatalf("read preflight head: %v", err)
	}
	recordedHead := strings.TrimSpace(string(recordedHeadBytes))
	currentHead := runGitCapture(t, summary.Worktree, "rev-parse", "HEAD")
	if recordedHead != currentHead {
		t.Fatalf("expected preflight to run after sync on head %q, got %q", currentHead, recordedHead)
	}
}

func TestLandPreflightFailureDoesNotBreakTask(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	_ = installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoFailingLandPreflightConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "land failing preflight",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	err = func() error {
		_, landErr := app.Land(context.Background(), LandOptions{
			CommonOptions: CommonOptions{RepoPath: repo},
			Task:          "land failing preflight",
		})
		return landErr
	}()
	if err == nil {
		t.Fatal("expected land preflight to fail")
	}

	loaded, err := app.state.Load(repoID(canonicalPath(repo)), summary.TaskID)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.Status == StatusBroken {
		t.Fatalf("expected task to remain structurally healthy after preflight failure, got %+v", loaded)
	}
}

func TestLandRejectsDirtyPreflightSideEffects(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	_ = installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoDirtyLandPreflightConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "land dirty preflight",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	_, err = app.Land(context.Background(), LandOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "land dirty preflight",
	})
	if err == nil {
		t.Fatal("expected dirty preflight to stop land")
	}
	if !strings.Contains(err.Error(), "delivery preflight left uncommitted changes") {
		t.Fatalf("unexpected land error: %v", err)
	}

	loaded, err := app.state.Load(repoID(canonicalPath(repo)), summary.TaskID)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.Status == StatusBroken {
		t.Fatalf("expected task to remain non-broken after dirty preflight, got %+v", loaded)
	}
}

func TestGCCleansMergedTask(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc smoke",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "gc.txt"), []byte("gc\n"), 0o644); err != nil {
		t.Fatalf("write gc file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "gc.txt")
	runGit(t, summary.Worktree, "commit", "-m", "gc commit")

	runGit(t, repo, "merge", "--no-edit", summary.Branch)
	runGit(t, repo, "push", "origin", "main")

	results, err := app.GC(context.Background(), GCOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc smoke",
	})
	if err != nil {
		t.Fatalf("GC returned error: %v", err)
	}
	if len(results) != 1 || results[0].Status != "deleted" {
		t.Fatalf("expected GC to delete merged task, got %+v", results)
	}
	if _, err := app.state.Load(repoID(canonicalPath(repo)), results[0].TaskID); !os.IsNotExist(err) {
		t.Fatalf("expected GC to delete state, load err=%v", err)
	}
}

func TestGCCleansPRMergedBranchEvenWhenNotMergedLocally(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	ghStateDir := installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc pr merged",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "gc-pr.txt"), []byte("gc pr\n"), 0o644); err != nil {
		t.Fatalf("write gc-pr file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "gc-pr.txt")
	runGit(t, summary.Worktree, "commit", "-m", "gc pr commit")

	submitted, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc pr merged",
	})
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}

	writeFakePullRequestState(t, ghStateDir, fakePullRequestState{
		Number:     17,
		URL:        "https://example.com/pr/17",
		State:      "MERGED",
		IsDraft:    false,
		Base:       "main",
		Head:       submitted.Branch,
		HeadOID:    runGitCapture(t, summary.Worktree, "rev-parse", "HEAD"),
		HeadOwner:  "origin",
		HeadRepo:   "origin",
		MergeState: "MERGED",
		MergedAt:   "2026-03-14T00:00:00Z",
	})

	results, err := app.GC(context.Background(), GCOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc pr merged",
	})
	if err != nil {
		t.Fatalf("GC returned error: %v", err)
	}
	if len(results) != 1 || results[0].Status != "deleted" {
		t.Fatalf("expected GC to delete PR-merged task, got %+v", results)
	}
	if NewGitOps(Executor{}).RefExists(context.Background(), repo, "refs/heads/"+submitted.Branch) {
		t.Fatalf("expected local branch %q to be deleted after PR merge cleanup", submitted.Branch)
	}
}

func TestSubmitIgnoresClosedPullRequestAndCreatesNewOne(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	ghStateDir := installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "closed pr reuse",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "closed-pr.txt"), []byte("closed pr\n"), 0o644); err != nil {
		t.Fatalf("write closed-pr file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "closed-pr.txt")
	runGit(t, summary.Worktree, "commit", "-m", "closed pr commit")

	writeFakePullRequestState(t, ghStateDir, fakePullRequestState{
		Number:     16,
		URL:        "https://example.com/pr/16",
		State:      "CLOSED",
		IsDraft:    false,
		Base:       "main",
		Head:       summary.Branch,
		HeadOID:    runGitCapture(t, summary.Worktree, "rev-parse", "HEAD"),
		HeadOwner:  "origin",
		HeadRepo:   "origin",
		MergeState: "CLOSED",
	})

	submitted, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "closed pr reuse",
	})
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if submitted.Delivery.State != DeliveryStateDraft {
		t.Fatalf("expected a new open draft PR, got %+v", submitted.Delivery)
	}
	prState := readFakePullRequestState(t, ghStateDir)
	if prState.State != "OPEN" {
		t.Fatalf("expected closed PR to be replaced by a new open PR, got %+v", prState)
	}
}

func TestSubmitPreservesMergedPullRequestState(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	ghStateDir := installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "merged pr preserve",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "merged-pr.txt"), []byte("merged pr\n"), 0o644); err != nil {
		t.Fatalf("write merged-pr file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "merged-pr.txt")
	runGit(t, summary.Worktree, "commit", "-m", "merged pr commit")

	if _, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "merged pr preserve",
	}); err != nil {
		t.Fatalf("initial Submit returned error: %v", err)
	}

	writeFakePullRequestState(t, ghStateDir, fakePullRequestState{
		Number:     17,
		URL:        "https://example.com/pr/17",
		State:      "MERGED",
		IsDraft:    false,
		Base:       "main",
		Head:       summary.Branch,
		HeadOID:    runGitCapture(t, summary.Worktree, "rev-parse", "HEAD"),
		HeadOwner:  "origin",
		HeadRepo:   "origin",
		MergeState: "MERGED",
		MergedAt:   "2026-03-15T00:00:00Z",
	})

	submitted, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "merged pr preserve",
	})
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if submitted.Delivery.State != DeliveryStateMerged {
		t.Fatalf("expected merged delivery state to be preserved, got %+v", submitted.Delivery)
	}
	prState := readFakePullRequestState(t, ghStateDir)
	if prState.State != "MERGED" {
		t.Fatalf("expected merged PR state to remain merged, got %+v", prState)
	}
}

func TestSubmitDoesNotRecreateRemoteBranchForMergedPullRequest(t *testing.T) {
	repo, origin := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	ghStateDir := installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "merged submit no push",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "merged-submit-no-push.txt"), []byte("merged submit no push\n"), 0o644); err != nil {
		t.Fatalf("write merged-submit-no-push file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "merged-submit-no-push.txt")
	runGit(t, summary.Worktree, "commit", "-m", "merged submit no push commit")

	submitted, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "merged submit no push",
	})
	if err != nil {
		t.Fatalf("initial Submit returned error: %v", err)
	}

	writeFakePullRequestState(t, ghStateDir, fakePullRequestState{
		Number:     submitted.Delivery.PullRequestNumber,
		URL:        submitted.Delivery.PullRequestURL,
		State:      "MERGED",
		IsDraft:    false,
		Base:       "main",
		Head:       summary.Branch,
		HeadOID:    runGitCapture(t, summary.Worktree, "rev-parse", "HEAD"),
		HeadOwner:  "origin",
		HeadRepo:   "origin",
		MergeState: "MERGED",
		MergedAt:   "2026-03-15T00:00:00Z",
	})
	runGit(t, repo, "push", origin, "--delete", submitted.Branch)

	resubmitted, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "merged submit no push",
	})
	if err != nil {
		t.Fatalf("Submit after merge returned error: %v", err)
	}
	if resubmitted.Delivery.State != DeliveryStateMerged {
		t.Fatalf("expected merged delivery state to remain merged, got %+v", resubmitted.Delivery)
	}

	remoteRef := strings.TrimSpace(runGitCapture(t, repo, "ls-remote", origin, "refs/heads/"+submitted.Branch))
	if remoteRef != "" {
		t.Fatalf("expected merged task submit to leave remote branch deleted, got %q", remoteRef)
	}
}

func TestSyncFetchesBaseRemoteWhenDifferentFromPushRemote(t *testing.T) {
	repo, origin := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoUpstreamBaseConfig)

	upstream := filepath.Join(t.TempDir(), "upstream.git")
	runGit(t, "", "init", "--bare", upstream)
	runGit(t, repo, "remote", "add", "upstream", upstream)
	runGit(t, repo, "push", "upstream", "main")

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "fork sync",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "fork-sync.txt"), []byte("fork sync\n"), 0o644); err != nil {
		t.Fatalf("write fork-sync file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "fork-sync.txt")
	runGit(t, summary.Worktree, "commit", "-m", "fork sync task commit")
	runGit(t, summary.Worktree, "push", origin, summary.Branch)

	upstreamClone := t.TempDir()
	runGit(t, "", "clone", upstream, upstreamClone)
	runGit(t, upstreamClone, "config", "user.name", "Agentflow Test")
	runGit(t, upstreamClone, "config", "user.email", "agentflow@example.com")
	if err := os.WriteFile(filepath.Join(upstreamClone, "upstream.txt"), []byte("upstream\n"), 0o644); err != nil {
		t.Fatalf("write upstream file: %v", err)
	}
	runGit(t, upstreamClone, "add", "upstream.txt")
	runGit(t, upstreamClone, "commit", "-m", "advance upstream")
	runGit(t, upstreamClone, "push", "origin", "main")
	expectedUpstreamHead := runGitCapture(t, upstreamClone, "rev-parse", "HEAD")

	summaries, err := app.Sync(context.Background(), SyncOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "fork sync",
	})
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one sync summary, got %d", len(summaries))
	}

	actualUpstreamHead := runGitCapture(t, repo, "rev-parse", "upstream/main")
	if actualUpstreamHead != expectedUpstreamHead {
		t.Fatalf("expected sync to refresh upstream/main to %q, got %q", expectedUpstreamHead, actualUpstreamHead)
	}
}

func TestSyncUsesCurrentDeliveryRemoteAfterConfigChange(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	fork := filepath.Join(t.TempDir(), "fork.git")
	runGit(t, "", "init", "--bare", fork)
	runGit(t, repo, "remote", "add", "fork", fork)
	runGit(t, repo, "push", "fork", "main")

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "remote switch",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "remote-switch.txt"), []byte("remote switch\n"), 0o644); err != nil {
		t.Fatalf("write remote-switch file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "remote-switch.txt")
	runGit(t, summary.Worktree, "commit", "-m", "remote switch task commit")

	forkClone := t.TempDir()
	runGit(t, "", "clone", fork, forkClone)
	runGit(t, forkClone, "config", "user.name", "Agentflow Test")
	runGit(t, forkClone, "config", "user.email", "agentflow@example.com")
	if err := os.WriteFile(filepath.Join(forkClone, "fork-main.txt"), []byte("fork main\n"), 0o644); err != nil {
		t.Fatalf("write fork-main file: %v", err)
	}
	runGit(t, forkClone, "add", "fork-main.txt")
	runGit(t, forkClone, "commit", "-m", "advance fork main")
	runGit(t, forkClone, "push", "origin", "main")

	writeTestRepoConfig(t, repo, strings.Replace(testRepoWorkflowConfig, `remote = "origin"`, `remote = "fork"`, 1))
	app.stdin = strings.NewReader("yes\n")

	summaries, err := app.Sync(context.Background(), SyncOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "remote switch",
	})
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one sync summary, got %d", len(summaries))
	}
	if summaries[0].Delivery.Remote != "fork" {
		t.Fatalf("expected sync to adopt new delivery remote, got %+v", summaries[0].Delivery)
	}
	behind, _, err := NewGitOps(Executor{}).RevListCounts(context.Background(), summary.Worktree, "fork/main", "HEAD")
	if err != nil {
		t.Fatalf("RevListCounts returned error: %v", err)
	}
	if behind != 0 {
		t.Fatalf("expected sync to compare against fork/main after config change, got behind=%d", behind)
	}
}

func TestGCRequiresTrustBeforeTouchingUpdatedDeliveryConfig(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	if _, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc trust",
	}); err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	writeTestRepoConfig(t, repo, strings.Replace(testRepoWorkflowConfig, `remote = "origin"`, `remote = "missing-remote"`, 1))
	app.stdin = strings.NewReader("no\n")

	_, err := app.GC(context.Background(), GCOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc trust",
	})
	if err == nil {
		t.Fatal("expected GC trust prompt to fail")
	}
	if !strings.Contains(err.Error(), "repo trust declined") {
		t.Fatalf("expected trust error before any remote operations, got %v", err)
	}
}

func TestGCDeletesRemoteBranchOnlyAfterLocalCleanupSucceeds(t *testing.T) {
	repo, origin := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	ghStateDir := installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubConfig)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc remote ordering",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "gc-remote-order.txt"), []byte("gc remote order\n"), 0o644); err != nil {
		t.Fatalf("write gc-remote-order file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "gc-remote-order.txt")
	runGit(t, summary.Worktree, "commit", "-m", "gc remote order commit")

	submitted, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc remote ordering",
	})
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}

	writeFakePullRequestState(t, ghStateDir, fakePullRequestState{
		Number:     submitted.Delivery.PullRequestNumber,
		URL:        submitted.Delivery.PullRequestURL,
		State:      "MERGED",
		IsDraft:    false,
		Base:       "main",
		Head:       submitted.Branch,
		HeadOID:    runGitCapture(t, summary.Worktree, "rev-parse", "HEAD"),
		HeadOwner:  "origin",
		HeadRepo:   "origin",
		MergeState: "MERGED",
		MergedAt:   "2026-03-15T00:00:00Z",
	})
	if err := os.WriteFile(filepath.Join(summary.Worktree, "dirty.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	_, err = app.GC(context.Background(), GCOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "gc remote ordering",
	})
	if err == nil {
		t.Fatal("expected GC to refuse dirty worktree")
	}
	if !strings.Contains(err.Error(), "refusing to remove dirty worktree") {
		t.Fatalf("unexpected GC error: %v", err)
	}

	remoteRef := strings.TrimSpace(runGitCapture(t, repo, "ls-remote", origin, "refs/heads/"+submitted.Branch))
	if remoteRef == "" {
		t.Fatalf("expected remote branch %q to remain until local GC succeeds", submitted.Branch)
	}
}

func TestSubmitCreatesNewPullRequestAfterDeliveryRemoteChange(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	ghStateDir := installFakeGitHubCLI(t)
	writeTestRepoConfig(t, repo, testRepoGitHubConfig)

	fork := filepath.Join(t.TempDir(), "fork.git")
	runGit(t, "", "init", "--bare", fork)
	runGit(t, repo, "remote", "add", "fork", fork)
	runGit(t, repo, "push", "fork", "main")

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "remote pr switch",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "remote-pr-switch.txt"), []byte("remote pr switch\n"), 0o644); err != nil {
		t.Fatalf("write remote-pr-switch file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "remote-pr-switch.txt")
	runGit(t, summary.Worktree, "commit", "-m", "remote pr switch commit")

	first, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "remote pr switch",
	})
	if err != nil {
		t.Fatalf("initial Submit returned error: %v", err)
	}

	writeTestRepoConfig(t, repo, strings.Replace(testRepoGitHubConfig, `remote = "origin"`, `remote = "fork"`, 1))
	app.stdin = strings.NewReader("yes\n")

	second, err := app.Submit(context.Background(), SubmitOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "remote pr switch",
	})
	if err != nil {
		t.Fatalf("Submit after remote change returned error: %v", err)
	}
	if second.Delivery.PullRequestNumber == first.Delivery.PullRequestNumber {
		t.Fatalf("expected a new PR after switching delivery remote, got %+v", second.Delivery)
	}

	prState := readFakePullRequestState(t, ghStateDir)
	if prState.HeadOwner != "fork" || prState.HeadRepo != "fork" {
		t.Fatalf("expected recreated PR to target the new head repo, got %+v", prState)
	}
}

func TestLandUsesConcreteMergeFlagForAutoMethod(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	ghStateDir := installFakeGitHubCLI(t)
	config := strings.Replace(testRepoGitHubConfig, "auto_merge = true", "auto_merge = false", 1)
	writeTestRepoConfig(t, repo, config)

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "land auto merge method",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "land-auto-merge-method.txt"), []byte("land auto merge method\n"), 0o644); err != nil {
		t.Fatalf("write land-auto-merge-method file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "land-auto-merge-method.txt")
	runGit(t, summary.Worktree, "commit", "-m", "land auto merge method commit")

	if _, err := app.Land(context.Background(), LandOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "land auto merge method",
	}); err != nil {
		t.Fatalf("Land returned error: %v", err)
	}

	mergeState := readFakeMergeState(t, ghStateDir)
	if mergeState.Mode != "merge" {
		t.Fatalf("expected auto merge_method to pass --merge, got %+v", mergeState)
	}
	if mergeState.Auto {
		t.Fatalf("expected auto_merge=false to omit --auto, got %+v", mergeState)
	}
}

func TestGCFetchesBaseRemoteWhenDifferentFromPushRemote(t *testing.T) {
	repo, _ := initCommittedRepoWithRemote(t)
	installFakeTmux(t)
	writeTestRepoConfig(t, repo, testRepoUpstreamBaseConfig)

	upstream := filepath.Join(t.TempDir(), "upstream.git")
	runGit(t, "", "init", "--bare", upstream)
	runGit(t, repo, "remote", "add", "upstream", upstream)
	runGit(t, repo, "push", "upstream", "main")

	app, _, _ := newTestApp(t)
	t.Setenv("AGENTFLOW_STATE_HOME", app.state.root)
	summary, err := app.Up(context.Background(), UpOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "fork gc",
	})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(summary.Worktree, "fork-gc.txt"), []byte("fork gc\n"), 0o644); err != nil {
		t.Fatalf("write fork-gc file: %v", err)
	}
	runGit(t, summary.Worktree, "add", "fork-gc.txt")
	runGit(t, summary.Worktree, "commit", "-m", "fork gc task commit")
	runGit(t, summary.Worktree, "push", "upstream", summary.Branch)

	upstreamClone := t.TempDir()
	runGit(t, "", "clone", upstream, upstreamClone)
	runGit(t, upstreamClone, "config", "user.name", "Agentflow Test")
	runGit(t, upstreamClone, "config", "user.email", "agentflow@example.com")
	runGit(t, upstreamClone, "fetch", "origin", summary.Branch+":"+summary.Branch)
	runGit(t, upstreamClone, "merge", "--no-edit", summary.Branch)
	runGit(t, upstreamClone, "push", "origin", "main")

	results, err := app.GC(context.Background(), GCOptions{
		CommonOptions: CommonOptions{RepoPath: repo},
		Task:          "fork gc",
	})
	if err != nil {
		t.Fatalf("GC returned error: %v", err)
	}
	if len(results) != 1 || results[0].Status != "deleted" {
		t.Fatalf("expected GC to delete upstream-merged task, got %+v", results)
	}
}

func installFakeGitHubCLI(t *testing.T) string {
	t.Helper()

	binDir := t.TempDir()
	stateDir := t.TempDir()
	script := filepath.Join(binDir, "gh")
	content := fmt.Sprintf(`#!/bin/sh
set -eu
state_dir=%s
pr_file="$state_dir/pr.env"
merge_file="$state_dir/merge.env"

load_pr() {
  if [ -f "$pr_file" ]; then
    . "$pr_file"
  else
    NUMBER=
    URL=
    STATE=
    IS_DRAFT=
    BASE=
    HEAD=
    HEAD_OID=
    HEAD_OWNER=
    HEAD_REPO=
    MERGE_STATE=
    MERGED_AT=
  fi
}

save_pr() {
  cat >"$pr_file" <<EOF
NUMBER=$NUMBER
URL=$URL
STATE=$STATE
IS_DRAFT=$IS_DRAFT
BASE=$BASE
HEAD=$HEAD
HEAD_OID=$HEAD_OID
HEAD_OWNER=$HEAD_OWNER
HEAD_REPO=$HEAD_REPO
MERGE_STATE=$MERGE_STATE
MERGED_AT=$MERGED_AT
EOF
}

print_pr() {
  merged_at=null
  if [ -n "$MERGED_AT" ]; then
    merged_at="\"$MERGED_AT\""
  fi
  head_repo_owner=null
  if [ -n "$HEAD_OWNER" ]; then
    head_repo_owner="{\"login\":\"$HEAD_OWNER\"}"
  fi
  head_repo=null
  if [ -n "$HEAD_REPO" ]; then
    head_repo="{\"name\":\"$HEAD_REPO\"}"
  fi
  printf '{"number":%%s,"url":"%%s","state":"%%s","isDraft":%%s,"baseRefName":"%%s","headRefName":"%%s","headRefOid":"%%s","headRepositoryOwner":%%s,"headRepository":%%s,"mergeStateStatus":"%%s","mergedAt":%%s}\n' \
    "$NUMBER" "$URL" "$STATE" "$IS_DRAFT" "$BASE" "$HEAD" "$HEAD_OID" "$head_repo_owner" "$head_repo" "$MERGE_STATE" "$merged_at"
}

load_pr
case "$1 $2" in
  "auth status")
    exit 0
    ;;
  "pr list")
    if [ -f "$pr_file" ]; then
      printf '['
      print_pr | tr -d '\n'
      printf ']\n'
    else
      printf '[]\n'
    fi
    ;;
  "pr create")
    BASE=main
    IS_DRAFT=false
    while [ $# -gt 0 ]; do
      case "$1" in
        --base)
          shift
          BASE="$1"
          ;;
        --draft)
          IS_DRAFT=true
          ;;
      esac
      shift
    done
    HEAD=$(git branch --show-current)
    HEAD_OID=$(git rev-parse HEAD)
    HEAD_REMOTE=$(git config --get branch."$HEAD".remote || true)
    HEAD_OWNER=$HEAD_REMOTE
    HEAD_REPO=$HEAD_REMOTE
    if [ -n "$NUMBER" ]; then
      NUMBER=$((NUMBER + 1))
    else
      NUMBER=17
    fi
    URL=https://example.com/pr/$NUMBER
    STATE=OPEN
    MERGE_STATE=CLEAN
    MERGED_AT=
    save_pr
    printf '%%s\n' "$URL"
    ;;
  "pr view")
    print_pr
    ;;
  "pr ready")
    IS_DRAFT=false
    save_pr
    ;;
  "pr merge")
    MERGE_MODE=
    MERGE_AUTO=false
    while [ $# -gt 0 ]; do
      case "$1" in
        --merge)
          MERGE_MODE=merge
          ;;
        --squash)
          MERGE_MODE=squash
          ;;
        --rebase)
          MERGE_MODE=rebase
          ;;
        --auto)
          MERGE_AUTO=true
          ;;
      esac
      shift
    done
    cat >"$merge_file" <<EOF
MODE=$MERGE_MODE
AUTO=$MERGE_AUTO
EOF
    MERGE_STATE=QUEUED
    save_pr
    ;;
  "pr checks")
    printf '[]\n'
    ;;
  *)
    echo "unexpected gh invocation: $*" >&2
    exit 1
    ;;
esac
`, shellQuote(stateDir))
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stateDir
}

type fakePullRequestState struct {
	Number     int
	URL        string
	State      string
	IsDraft    bool
	Base       string
	Head       string
	HeadOID    string
	HeadOwner  string
	HeadRepo   string
	MergeState string
	MergedAt   string
}

type fakeMergeState struct {
	Mode string
	Auto bool
}

func writeFakePullRequestState(t *testing.T, stateDir string, state fakePullRequestState) {
	t.Helper()
	content := fmt.Sprintf("NUMBER=%d\nURL=%s\nSTATE=%s\nIS_DRAFT=%t\nBASE=%s\nHEAD=%s\nHEAD_OID=%s\nHEAD_OWNER=%s\nHEAD_REPO=%s\nMERGE_STATE=%s\nMERGED_AT=%s\n",
		state.Number,
		state.URL,
		state.State,
		state.IsDraft,
		state.Base,
		state.Head,
		state.HeadOID,
		state.HeadOwner,
		state.HeadRepo,
		state.MergeState,
		state.MergedAt,
	)
	if err := os.WriteFile(filepath.Join(stateDir, "pr.env"), []byte(content), 0o644); err != nil {
		t.Fatalf("write fake PR state: %v", err)
	}
}

func readFakePullRequestState(t *testing.T, stateDir string) fakePullRequestState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, "pr.env"))
	if err != nil {
		t.Fatalf("read fake PR state: %v", err)
	}
	state := fakePullRequestState{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "NUMBER":
			fmt.Sscanf(value, "%d", &state.Number)
		case "URL":
			state.URL = value
		case "STATE":
			state.State = value
		case "IS_DRAFT":
			state.IsDraft = value == "true"
		case "BASE":
			state.Base = value
		case "HEAD":
			state.Head = value
		case "HEAD_OID":
			state.HeadOID = value
		case "HEAD_OWNER":
			state.HeadOwner = value
		case "HEAD_REPO":
			state.HeadRepo = value
		case "MERGE_STATE":
			state.MergeState = value
		case "MERGED_AT":
			state.MergedAt = value
		}
	}
	return state
}

func readFakeMergeState(t *testing.T, stateDir string) fakeMergeState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, "merge.env"))
	if err != nil {
		t.Fatalf("read fake merge state: %v", err)
	}
	state := fakeMergeState{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "MODE":
			state.Mode = value
		case "AUTO":
			state.Auto = value == "true"
		}
	}
	return state
}
