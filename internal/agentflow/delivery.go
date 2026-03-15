package agentflow

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"
)

func (a *App) Status(ctx context.Context, opts CommonOptions, task string) ([]TaskStatus, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return nil, err
	}

	var states []TaskState
	if strings.TrimSpace(task) != "" {
		state, err := a.loadTaskByInput(ctx, runtime, task)
		if err != nil {
			return nil, err
		}
		states = []TaskState{state}
	} else {
		states, err = a.state.List(runtime.RepoID)
		if err != nil {
			return nil, err
		}
	}

	statuses := make([]TaskStatus, 0, len(states))
	for _, state := range states {
		status, err := a.inspectTaskStatus(ctx, runtime, state)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (a *App) Sync(ctx context.Context, opts SyncOptions) ([]TaskSummary, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return nil, err
	}
	unlock, err := a.lockRepo(runtime)
	if err != nil {
		return nil, err
	}
	defer unlock()

	states, err := a.syncTargets(ctx, runtime, opts.Task, opts.All)
	if err != nil {
		return nil, err
	}

	results := make([]TaskSummary, 0, len(states))
	for _, state := range states {
		summary, err := a.syncTask(ctx, runtime, state, opts.Push)
		if err != nil {
			return nil, err
		}
		results = append(results, summary)
	}
	return results, nil
}

func (a *App) Submit(ctx context.Context, opts SubmitOptions) (TaskSummary, error) {
	runtime, state, err := a.loadTaskForMutation(ctx, opts.CommonOptions, opts.Task)
	if err != nil {
		return TaskSummary{}, err
	}
	unlock, err := a.lockRepo(runtime)
	if err != nil {
		return TaskSummary{}, err
	}
	defer unlock()

	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ConfigPath, runtime.WorkflowFingerprint, workflowTrustEntries(runtime.Config), a.stdin, a.stdout); err != nil {
		return TaskSummary{}, err
	}

	previousRemote := strings.TrimSpace(state.Delivery.Remote)
	state, rewrote, err := a.syncTaskState(ctx, runtime, state)
	if err != nil {
		return TaskSummary{}, err
	}
	if state.Delivery.State == DeliveryStateMerged {
		if err := a.ensureLinearPullRequestLink(ctx, runtime, &state); err != nil {
			return a.failState(state, err)
		}
		if err := a.reconcileLinearTask(ctx, runtime, &state); err != nil {
			return a.failState(state, err)
		}
		state.UpdatedAt = a.now()
		if err := a.state.Save(state); err != nil {
			return TaskSummary{}, err
		}
		return a.summaryForState(state), nil
	}

	remote, err := requiredDeliveryRemote(runtime)
	if err != nil {
		return TaskSummary{}, err
	}
	if err := a.pushTaskBranch(ctx, runtime, state, remote, rewrote); err != nil {
		return a.failState(state, err)
	}

	state.Delivery.Remote = remote
	state.Delivery.RemoteBranch = state.Branch
	state.Delivery.LastSubmittedAt = a.now()

	if !runtime.EffectiveConfig.GitHub.Enabled {
		state.UpdatedAt = a.now()
		if err := a.state.Save(state); err != nil {
			return TaskSummary{}, err
		}
		return a.summaryForState(state), nil
	}

	baseBranch := githubBaseBranchName(ctx, a.git, runtime, state)
	pr, err := a.ensurePullRequest(ctx, runtime, state, previousRemote, baseBranch, opts)
	if err != nil {
		return a.failState(state, err)
	}
	if pr != nil {
		updateDeliveryFromPullRequest(&state, *pr)
		state.Delivery.LastSubmittedAt = a.now()
	}
	if err := a.ensureLinearPullRequestLink(ctx, runtime, &state); err != nil {
		return a.failState(state, err)
	}
	if err := a.reconcileLinearTask(ctx, runtime, &state); err != nil {
		return a.failState(state, err)
	}
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}
	return a.summaryForState(state), nil
}

func (a *App) Land(ctx context.Context, opts LandOptions) (TaskSummary, error) {
	runtime, state, err := a.loadTaskForMutation(ctx, opts.CommonOptions, opts.Task)
	if err != nil {
		return TaskSummary{}, err
	}
	if !runtime.EffectiveConfig.GitHub.Enabled {
		return TaskSummary{}, fmt.Errorf("land requires [github].enabled = true in %s", runtime.ConfigPath)
	}

	unlock, err := a.lockRepo(runtime)
	if err != nil {
		return TaskSummary{}, err
	}
	defer unlock()

	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ConfigPath, runtime.WorkflowFingerprint, workflowTrustEntries(runtime.Config), a.stdin, a.stdout); err != nil {
		return TaskSummary{}, err
	}

	previousRemote := strings.TrimSpace(state.Delivery.Remote)
	state, rewrote, err := a.syncTaskState(ctx, runtime, state)
	if err != nil {
		return TaskSummary{}, err
	}
	if state.Delivery.State == DeliveryStateMerged {
		if err := a.ensureLinearPullRequestLink(ctx, runtime, &state); err != nil {
			return a.failState(state, err)
		}
		if err := a.reconcileLinearTask(ctx, runtime, &state); err != nil {
			return a.failState(state, err)
		}
		state.UpdatedAt = a.now()
		if err := a.state.Save(state); err != nil {
			return TaskSummary{}, err
		}
		return a.summaryForState(state), nil
	}

	if err := a.runDeliveryPreflight(ctx, runtime, state); err != nil {
		return TaskSummary{}, err
	}
	dirtyAfterPreflight, err := a.git.IsDirtyIgnoring(ctx, state.WorktreePath, state.EffectiveManagedEnvFiles())
	if err != nil {
		return TaskSummary{}, err
	}
	if dirtyAfterPreflight {
		return TaskSummary{}, fmt.Errorf("delivery preflight left uncommitted changes in %s; commit or stash them before landing", state.WorktreePath)
	}

	remote, err := requiredDeliveryRemote(runtime)
	if err != nil {
		return TaskSummary{}, err
	}
	if err := a.pushTaskBranch(ctx, runtime, state, remote, rewrote); err != nil {
		return a.failState(state, err)
	}

	baseBranch := githubBaseBranchName(ctx, a.git, runtime, state)
	pr, err := a.ensurePullRequest(ctx, runtime, state, previousRemote, baseBranch, SubmitOptions{
		CommonOptions: opts.CommonOptions,
		Task:          opts.Task,
		Ready:         true,
	})
	if err != nil {
		return a.failState(state, err)
	}
	if pr == nil {
		return TaskSummary{}, fmt.Errorf("failed to create or load pull request for %q", state.TaskRef.Title)
	}
	if strings.EqualFold(pr.State, "MERGED") {
		updateDeliveryFromPullRequest(&state, *pr)
		if err := a.ensureLinearPullRequestLink(ctx, runtime, &state); err != nil {
			return a.failState(state, err)
		}
		if err := a.reconcileLinearTask(ctx, runtime, &state); err != nil {
			return a.failState(state, err)
		}
		state.UpdatedAt = a.now()
		if err := a.state.Save(state); err != nil {
			return TaskSummary{}, err
		}
		return a.summaryForState(state), nil
	}
	if pr.IsDraft {
		if err := a.gh.ReadyPullRequest(ctx, state.WorktreePath, fmt.Sprintf("%d", pr.Number)); err != nil {
			return a.failState(state, err)
		}
		pr, err = a.gh.ViewPullRequest(ctx, state.WorktreePath, fmt.Sprintf("%d", pr.Number))
		if err != nil {
			return a.failState(state, err)
		}
	}

	headSHA, err := a.git.RevParse(ctx, state.WorktreePath, "HEAD")
	if err != nil {
		return a.failState(state, err)
	}
	mergeOpts, err := a.resolveGitHubMergeOptions(ctx, runtime, state)
	if err != nil {
		return a.failState(state, err)
	}
	if err := a.gh.MergePullRequest(ctx, state.WorktreePath, fmt.Sprintf("%d", pr.Number), headSHA, mergeOpts); err != nil {
		return a.failState(state, err)
	}

	pr, err = a.gh.ViewPullRequest(ctx, state.WorktreePath, fmt.Sprintf("%d", pr.Number))
	if err != nil {
		return a.failState(state, err)
	}
	updateDeliveryFromPullRequest(&state, *pr)
	if state.Delivery.State == DeliveryStateSubmitted && runtime.EffectiveConfig.GitHub.AutoMerge {
		state.Delivery.State = DeliveryStateQueued
	}

	if opts.Watch {
		watched, err := a.watchPullRequest(ctx, state.WorktreePath, pr.Number)
		if err != nil {
			return a.failState(state, err)
		}
		updateDeliveryFromPullRequest(&state, watched)
	}
	if err := a.ensureLinearPullRequestLink(ctx, runtime, &state); err != nil {
		return a.failState(state, err)
	}
	if err := a.reconcileLinearTask(ctx, runtime, &state); err != nil {
		return a.failState(state, err)
	}

	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}
	return a.summaryForState(state), nil
}

func (a *App) GC(ctx context.Context, opts GCOptions) ([]TaskSummary, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return nil, err
	}
	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ConfigPath, runtime.WorkflowFingerprint, workflowTrustEntries(runtime.Config), a.stdin, a.stdout); err != nil {
		return nil, err
	}
	if remote := strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote); remote != "" {
		if err := a.git.FetchPrune(ctx, runtime.RepoRoot, remote); err != nil {
			return nil, err
		}
	}

	states, err := a.syncTargets(ctx, runtime, opts.Task, strings.TrimSpace(opts.Task) == "")
	if err != nil {
		return nil, err
	}

	results := make([]TaskSummary, 0, len(states))
	for _, state := range states {
		if err := a.fetchTaskBaseRemote(ctx, runtime, state); err != nil {
			return nil, err
		}
		merged, refreshed, err := a.isTaskMerged(ctx, runtime, state)
		if err != nil {
			return nil, err
		}
		state = refreshed
		if !merged {
			summary := a.summaryForState(state)
			results = append(results, summary)
			continue
		}
		if err := a.reconcileLinearTask(ctx, runtime, &state); err != nil {
			return nil, err
		}
		state.UpdatedAt = a.now()
		if err := a.state.Save(state); err != nil {
			return nil, err
		}

		summary, err := a.Down(ctx, DownOptions{
			CommonOptions: opts.CommonOptions,
			Task:          taskLookupInput(state),
			DeleteBranch:  false,
		})
		if err != nil {
			return nil, err
		}
		if a.git.RefExists(ctx, state.RepoRoot, "refs/heads/"+state.Branch) {
			if err := a.git.DeleteBranchForce(ctx, state.RepoRoot, state.Branch); err != nil {
				return nil, err
			}
		}
		if runtime.EffectiveConfig.GitHub.DeleteRemoteBranch && state.Delivery.Remote != "" && state.Delivery.RemoteBranch != "" {
			_ = a.git.DeleteRemoteBranch(ctx, state.RepoRoot, state.Delivery.Remote, state.Delivery.RemoteBranch)
		}
		results = append(results, summary)
	}
	return results, nil
}

func (a *App) loadTaskForMutation(ctx context.Context, opts CommonOptions, task string) (RuntimeConfig, TaskState, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return RuntimeConfig{}, TaskState{}, err
	}
	state, err := a.loadTaskByInput(ctx, runtime, task)
	if err != nil {
		return RuntimeConfig{}, TaskState{}, err
	}
	return runtime, state, nil
}

func (a *App) syncTargets(ctx context.Context, runtime RuntimeConfig, task string, all bool) ([]TaskState, error) {
	if all {
		states, err := a.state.List(runtime.RepoID)
		if err != nil {
			return nil, err
		}
		return states, nil
	}
	state, err := a.loadTaskByInput(ctx, runtime, task)
	if err != nil {
		return nil, err
	}
	return []TaskState{state}, nil
}

func (a *App) inspectTaskStatus(ctx context.Context, runtime RuntimeConfig, state TaskState) (TaskStatus, error) {
	status := TaskStatus{
		TaskID:        state.TaskID,
		TaskTitle:     state.TaskRef.Title,
		RepoRoot:      state.RepoRoot,
		Worktree:      state.WorktreePath,
		Branch:        state.Branch,
		Session:       state.TmuxSession,
		Surface:       state.Surface,
		Status:        state.Status,
		FailureReason: state.FailureReason,
		ConfigDrift:   state.WorkflowFingerprint != runtime.WorkflowFingerprint,
		Delivery:      state.Delivery,
	}
	if isLinearTask(state) {
		status.Issue = state.TaskRef.Key
		status.IssueURL = state.TaskRef.URL
		status.IssueState = state.IssueState
	}
	if err := a.git.ValidateTaskWorktree(ctx, state); err != nil {
		if status.FailureReason == "" {
			status.FailureReason = err.Error()
		}
		return status, nil
	}
	dirty, err := a.git.IsDirtyIgnoring(ctx, state.WorktreePath, state.EffectiveManagedEnvFiles())
	if err != nil {
		return status, err
	}
	status.Dirty = dirty

	baseRef := taskBaseRef(ctx, a.git, runtime, state)
	if baseRef != "" {
		behind, ahead, err := a.git.RevListCounts(ctx, state.WorktreePath, baseRef, "HEAD")
		if err == nil {
			status.Ahead = ahead
			status.Behind = behind
		}
	}

	if runtime.EffectiveConfig.GitHub.Enabled {
		pr, err := a.findCurrentPullRequest(ctx, runtime, state)
		if err != nil {
			return status, err
		}
		if pr != nil {
			delivery := state.Delivery
			updateDeliveryFromPullRequest(&state, *pr)
			delivery = state.Delivery
			status.Delivery = delivery
			status.MergeState = pr.MergeStateStatus
			checkState, err := a.gh.RequiredChecksState(ctx, state.WorktreePath, fmt.Sprintf("%d", pr.Number))
			if err == nil {
				status.ChecksState = checkState
			}
		}
	}
	if isLinearTask(state) {
		before := state
		if runtime.Trusted && linearConfigured(runtime.EffectiveConfig.Linear) {
			if err := a.reconcileLinearTask(ctx, runtime, &state); err != nil {
				return status, err
			}
		}
		status.TaskTitle = state.TaskRef.Title
		status.Issue = state.TaskRef.Key
		status.IssueURL = state.TaskRef.URL
		status.IssueState = state.IssueState
		status.Delivery = state.Delivery
		if linearTaskSnapshotChanged(before, state) {
			state.UpdatedAt = a.now()
			if err := a.state.Save(state); err != nil {
				return status, err
			}
		}
	}
	return status, nil
}

func (a *App) syncTask(ctx context.Context, runtime RuntimeConfig, state TaskState, push bool) (TaskSummary, error) {
	if _, err := a.trust.EnsureTrusted(runtime.RepoID, runtime.RepoRoot, runtime.ConfigPath, runtime.WorkflowFingerprint, workflowTrustEntries(runtime.Config), a.stdin, a.stdout); err != nil {
		return TaskSummary{}, err
	}
	state, rewrote, err := a.syncTaskState(ctx, runtime, state)
	if err != nil {
		return TaskSummary{}, err
	}
	if push {
		remote, err := requiredDeliveryRemote(runtime)
		if err != nil {
			return TaskSummary{}, err
		}
		if err := a.pushTaskBranch(ctx, runtime, state, remote, rewrote); err != nil {
			return a.failState(state, err)
		}
		state.Delivery.Remote = remote
		state.Delivery.RemoteBranch = state.Branch
		state.UpdatedAt = a.now()
		if err := a.state.Save(state); err != nil {
			return TaskSummary{}, err
		}
	}
	before := state
	if err := a.reconcileLinearTask(ctx, runtime, &state); err != nil {
		return a.failState(state, err)
	}
	if linearTaskSnapshotChanged(before, state) {
		state.UpdatedAt = a.now()
		if err := a.state.Save(state); err != nil {
			return TaskSummary{}, err
		}
	}
	summary := a.summaryForState(state)
	summary.Dirty = false
	baseRef := taskBaseRef(ctx, a.git, runtime, state)
	if baseRef != "" {
		behind, ahead, err := a.git.RevListCounts(ctx, state.WorktreePath, baseRef, "HEAD")
		if err == nil {
			summary.Ahead = ahead
			summary.Behind = behind
		}
	}
	return summary, nil
}

func (a *App) syncTaskState(ctx context.Context, runtime RuntimeConfig, state TaskState) (TaskState, bool, error) {
	if err := a.git.ValidateTaskWorktree(ctx, state); err != nil {
		return state, false, err
	}
	dirty, err := a.git.IsDirtyIgnoring(ctx, state.WorktreePath, state.EffectiveManagedEnvFiles())
	if err != nil {
		return state, false, err
	}
	if dirty {
		return state, false, fmt.Errorf("task %q has uncommitted changes; commit or stash before syncing", state.TaskRef.Title)
	}

	remote, err := requiredDeliveryRemote(runtime)
	if err != nil {
		return state, false, err
	}
	if err := a.git.FetchPrune(ctx, runtime.RepoRoot, remote); err != nil {
		return state, false, err
	}
	if err := a.fetchTaskBaseRemote(ctx, runtime, state); err != nil {
		return state, false, err
	}
	baseRef := taskBaseRef(ctx, a.git, runtime, state)
	behind, ahead, err := a.git.RevListCounts(ctx, state.WorktreePath, baseRef, "HEAD")
	if err != nil {
		return state, false, err
	}

	rewrote := false
	operation := effectiveSyncStrategy(runtime.EffectiveConfig)
	if behind > 0 {
		switch operation {
		case "merge":
			err = a.git.Merge(ctx, state.WorktreePath, baseRef)
		default:
			operation = "rebase"
			err = a.git.Rebase(ctx, state.WorktreePath, baseRef)
			rewrote = err == nil
		}
		if err != nil {
			state.Delivery.State = DeliveryStateBlocked
			state.FailureReason = syncFailureReason(operation, err)
			state.UpdatedAt = a.now()
			_ = a.state.Save(state)
			return state, false, fmt.Errorf("%s", state.FailureReason)
		}
	}

	baseSHA, _ := a.git.RevParse(ctx, runtime.RepoRoot, baseRef)
	headSHA, _ := a.git.RevParse(ctx, state.WorktreePath, "HEAD")
	state.Delivery.Remote = remote
	state.Delivery.RemoteBranch = state.Branch
	state.Delivery.BaseRef = normalizeBaseBranch(baseRef, remote)
	state.Delivery.LastBaseSHA = baseSHA
	state.Delivery.LastHeadSHA = headSHA
	state.Delivery.LastSyncedAt = a.now()
	if state.Delivery.State == "" || state.Delivery.State == DeliveryStateBlocked {
		state.Delivery.State = DeliveryStateLocal
	}
	if state.Delivery.PullRequestNumber > 0 {
		if runtime.EffectiveConfig.GitHub.Enabled {
			pr, err := a.findCurrentPullRequest(ctx, runtime, state)
			if err == nil && pr != nil {
				updateDeliveryFromPullRequest(&state, *pr)
			}
		}
	}
	state.FailureReason = ""
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return state, false, err
	}

	if behind == 0 && ahead == 0 {
		rewrote = false
	}
	return state, rewrote, nil
}

func linearTaskSnapshotChanged(before, after TaskState) bool {
	return after.TaskRef.Title != before.TaskRef.Title ||
		after.TaskRef.URL != before.TaskRef.URL ||
		after.TaskRef.ID != before.TaskRef.ID ||
		after.IssueState != before.IssueState ||
		!reflect.DeepEqual(after.IssueContext, before.IssueContext) ||
		after.Delivery != before.Delivery
}

func requiredDeliveryRemote(runtime RuntimeConfig) (string, error) {
	remote := strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote)
	if remote == "" {
		return "", fmt.Errorf("delivery.remote must be configured in %s", runtime.ConfigPath)
	}
	return remote, nil
}

func effectiveSyncStrategy(cfg EffectiveConfig) string {
	if strings.TrimSpace(cfg.Delivery.SyncStrategy) == "" {
		return "rebase"
	}
	return cfg.Delivery.SyncStrategy
}

func effectivePreflight(cfg EffectiveConfig) []string {
	if len(cfg.Delivery.Preflight) == 0 {
		return []string{"review", "verify"}
	}
	return append([]string(nil), cfg.Delivery.Preflight...)
}

func taskBaseRef(ctx context.Context, git GitOps, runtime RuntimeConfig, state TaskState) string {
	remote := strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote)
	if remote == "" {
		remote = strings.TrimSpace(state.Delivery.Remote)
	}
	if remote != "" {
		return git.RemoteTrackingRef(ctx, runtime.RepoRoot, remote, state.BaseBranch)
	}
	return state.BaseBranch
}

func githubBaseBranchName(ctx context.Context, git GitOps, runtime RuntimeConfig, state TaskState) string {
	baseRemote := git.RemoteNameForRef(ctx, runtime.RepoRoot, state.BaseBranch)
	if baseRemote == "" {
		baseRemote = git.RemoteNameForRef(ctx, runtime.RepoRoot, taskBaseRef(ctx, git, runtime, state))
	}
	return normalizeBaseBranch(state.BaseBranch, baseRemote)
}

func (a *App) fetchTaskBaseRemote(ctx context.Context, runtime RuntimeConfig, state TaskState) error {
	baseRemote := a.git.RemoteNameForRef(ctx, runtime.RepoRoot, state.BaseBranch)
	deliveryRemote := strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote)
	if baseRemote != "" && baseRemote != deliveryRemote {
		return a.git.FetchPrune(ctx, runtime.RepoRoot, baseRemote)
	}
	return nil
}

func (a *App) pushTaskBranch(ctx context.Context, runtime RuntimeConfig, state TaskState, remote string, rewrote bool) error {
	remoteRef := "refs/remotes/" + remote + "/" + state.Branch
	setUpstream := !a.git.RefExists(ctx, runtime.RepoRoot, remoteRef)
	forceWithLease := rewrote && effectiveSyncStrategy(runtime.EffectiveConfig) == "rebase"
	return a.git.Push(ctx, state.WorktreePath, remote, state.Branch, setUpstream, forceWithLease)
}

func (a *App) ensurePullRequest(ctx context.Context, runtime RuntimeConfig, state TaskState, previousRemote, baseBranch string, opts SubmitOptions) (*PullRequest, error) {
	pr, err := a.findReusablePullRequest(ctx, runtime, state, previousRemote)
	if err != nil {
		return nil, err
	}
	if pr != nil {
		if strings.EqualFold(pr.State, "MERGED") {
			return pr, nil
		}
		if !strings.EqualFold(pr.State, "OPEN") {
			pr = nil
		}
	}
	if pr != nil {
		if pr.BaseRefName != "" && pr.BaseRefName != baseBranch {
			return nil, fmt.Errorf("existing pull request for %q targets %q instead of %q", state.TaskRef.Title, pr.BaseRefName, baseBranch)
		}
		if opts.Ready && pr.IsDraft {
			if err := a.gh.ReadyPullRequest(ctx, state.WorktreePath, fmt.Sprintf("%d", pr.Number)); err != nil {
				return nil, err
			}
			return a.gh.ViewPullRequest(ctx, state.WorktreePath, fmt.Sprintf("%d", pr.Number))
		}
		return pr, nil
	}

	draft := runtime.EffectiveConfig.GitHub.DraftOnSubmit
	if opts.Draft {
		draft = true
	}
	if opts.Ready {
		draft = false
	}
	if err := a.gh.CreatePullRequest(ctx, state.WorktreePath, baseBranch, draft, runtime.EffectiveConfig.GitHub.Labels, runtime.EffectiveConfig.GitHub.Reviewers); err != nil {
		return nil, err
	}
	return a.findBranchPullRequest(ctx, runtime, state)
}

func (a *App) findCurrentPullRequest(ctx context.Context, runtime RuntimeConfig, state TaskState) (*PullRequest, error) {
	if state.Delivery.PullRequestNumber > 0 {
		pr, err := a.gh.ViewPullRequest(ctx, state.WorktreePath, fmt.Sprintf("%d", state.Delivery.PullRequestNumber))
		if err == nil && pr != nil {
			return pr, nil
		}
	}
	return a.findBranchPullRequest(ctx, runtime, state)
}

func (a *App) findBranchPullRequest(ctx context.Context, runtime RuntimeConfig, state TaskState) (*PullRequest, error) {
	return a.gh.FindPullRequest(ctx, state.WorktreePath, state.Branch, a.currentHeadRepositoryIdentity(ctx, runtime))
}

func (a *App) findReusablePullRequest(ctx context.Context, runtime RuntimeConfig, state TaskState, previousRemote string) (*PullRequest, error) {
	pr, err := a.findCurrentPullRequest(ctx, runtime, state)
	if err != nil || pr == nil {
		return pr, err
	}
	currentRemote := strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote)
	if currentRemote != "" && previousRemote != "" && currentRemote != previousRemote && strings.EqualFold(pr.State, "OPEN") {
		return nil, nil
	}
	return pr, nil
}

func (a *App) currentHeadRepositoryIdentity(ctx context.Context, runtime RuntimeConfig) string {
	remote := strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote)
	if remote == "" {
		return ""
	}
	remoteURL, err := a.git.RemoteURL(ctx, runtime.RepoRoot, remote)
	if err != nil {
		return remoteRepositoryIdentity(remote, "")
	}
	return remoteRepositoryIdentity(remote, remoteURL)
}

func (a *App) baseRepositoryIdentity(ctx context.Context, runtime RuntimeConfig, state TaskState) string {
	baseRemote := a.git.RemoteNameForRef(ctx, runtime.RepoRoot, state.BaseBranch)
	if baseRemote == "" {
		baseRemote = strings.TrimSpace(runtime.EffectiveConfig.Delivery.Remote)
	}
	if baseRemote == "" {
		return ""
	}
	remoteURL, err := a.git.RemoteURL(ctx, runtime.RepoRoot, baseRemote)
	if err != nil {
		return remoteRepositoryIdentity(baseRemote, "")
	}
	return remoteRepositoryIdentity(baseRemote, remoteURL)
}

func (a *App) resolveGitHubMergeOptions(ctx context.Context, runtime RuntimeConfig, state TaskState) (GitHubMergeOptions, error) {
	method := strings.TrimSpace(runtime.EffectiveConfig.GitHub.MergeMethod)
	if method == "" {
		method = "auto"
	}

	slug := a.baseRepositoryIdentity(ctx, runtime, state)
	owner, repo, ok := splitRepositorySlug(slug)
	if !ok {
		return GitHubMergeOptions{
			Method: resolveAutoMergeMethod(GitHubMergePolicy{}),
			Auto:   runtime.EffectiveConfig.GitHub.AutoMerge,
		}, nil
	}

	policy, err := a.gh.RepositoryMergePolicy(ctx, state.WorktreePath, owner, repo, githubBaseBranchName(ctx, a.git, runtime, state))
	if err != nil {
		return GitHubMergeOptions{}, err
	}

	resolved, err := resolveConfiguredGitHubMergeMethod(method, policy)
	if err != nil {
		return GitHubMergeOptions{}, err
	}
	opts := GitHubMergeOptions{Method: resolved, Auto: runtime.EffectiveConfig.GitHub.AutoMerge}
	if policy.RequiresMergeQueue {
		opts.Method = ""
		opts.Auto = false
	}
	return opts, nil
}

func resolveConfiguredGitHubMergeMethod(configured string, policy GitHubMergePolicy) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = "auto"
	}
	switch configured {
	case "auto":
		resolved := resolveAutoMergeMethod(policy)
		if resolved == "" && !policy.RequiresMergeQueue {
			return "", fmt.Errorf("github.merge_method=%q could not be resolved from the GitHub merge policy for the base branch", configured)
		}
		return resolved, nil
	case "merge", "squash", "rebase":
		if !gitHubMergeMethodAllowed(configured, policy) {
			return "", fmt.Errorf("github.merge_method=%q is not allowed by the GitHub merge policy for the base branch", configured)
		}
		return configured, nil
	default:
		return "", fmt.Errorf("unsupported GitHub merge method %q", configured)
	}
}

func resolveAutoMergeMethod(policy GitHubMergePolicy) string {
	if policy.RequiresMergeQueue {
		return ""
	}
	if policy.RequiresLinearHistory {
		switch {
		case policy.SquashMergeAllowed:
			return "squash"
		case policy.RebaseMergeAllowed:
			return "rebase"
		}
	}
	switch {
	case policy.MergeCommitAllowed || mergePolicyIsZero(policy):
		return "merge"
	case policy.SquashMergeAllowed:
		return "squash"
	case policy.RebaseMergeAllowed:
		return "rebase"
	default:
		return ""
	}
}

func mergePolicyIsZero(policy GitHubMergePolicy) bool {
	return !policy.MergeCommitAllowed &&
		!policy.RebaseMergeAllowed &&
		!policy.SquashMergeAllowed &&
		!policy.RequiresLinearHistory &&
		!policy.RequiresMergeQueue
}

func gitHubMergeMethodAllowed(method string, policy GitHubMergePolicy) bool {
	if mergePolicyIsZero(policy) {
		return true
	}
	switch method {
	case "merge":
		return policy.MergeCommitAllowed
	case "squash":
		return policy.SquashMergeAllowed
	case "rebase":
		return policy.RebaseMergeAllowed
	default:
		return false
	}
}

func describeGitHubMergePolicy(policy GitHubMergePolicy) string {
	methods := make([]string, 0, 3)
	if mergePolicyIsZero(policy) {
		methods = append(methods, "merge", "squash", "rebase")
	} else {
		if policy.MergeCommitAllowed {
			methods = append(methods, "merge")
		}
		if policy.SquashMergeAllowed {
			methods = append(methods, "squash")
		}
		if policy.RebaseMergeAllowed {
			methods = append(methods, "rebase")
		}
	}
	return fmt.Sprintf("methods=%s linear_history=%t merge_queue=%t", strings.Join(methods, ","), policy.RequiresLinearHistory, policy.RequiresMergeQueue)
}

func splitRepositorySlug(slug string) (string, string, bool) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(slug), "/")
	if !ok || owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}

func updateDeliveryFromPullRequest(state *TaskState, pr PullRequest) {
	state.Delivery.PullRequestNumber = pr.Number
	state.Delivery.PullRequestURL = pr.URL
	state.Delivery.PullRequestState = strings.ToUpper(pr.State)
	state.Delivery.BaseRef = pr.BaseRefName
	state.Delivery.RemoteBranch = pr.HeadRefName
	state.Delivery.LastHeadSHA = pr.HeadRefOID
	switch strings.ToUpper(pr.State) {
	case "MERGED":
		state.Delivery.State = DeliveryStateMerged
		if pr.MergedAt != nil {
			state.Delivery.MergedAt = *pr.MergedAt
		}
	case "CLOSED":
		state.Delivery.State = DeliveryStateClosed
	case "OPEN":
		if pr.IsDraft {
			state.Delivery.State = DeliveryStateDraft
		} else if strings.Contains(strings.ToUpper(pr.MergeStateStatus), "QUEUE") {
			state.Delivery.State = DeliveryStateQueued
		} else {
			state.Delivery.State = DeliveryStateSubmitted
		}
	}
}

func (a *App) runDeliveryPreflight(ctx context.Context, runtime RuntimeConfig, state TaskState) error {
	for _, name := range effectivePreflight(runtime.EffectiveConfig) {
		switch name {
		case "review":
			if _, err := a.runConfiguredCommand(ctx, runtime, state, "review", true); err != nil {
				return err
			}
		case "verify":
			if _, err := a.runConfiguredCommand(ctx, runtime, state, "verify", true); err != nil {
				return err
			}
		default:
			if _, err := a.runConfiguredCommand(ctx, runtime, state, name, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) runConfiguredCommand(ctx context.Context, runtime RuntimeConfig, state TaskState, name string, foreground bool) (TaskSummary, error) {
	command, resolvedName, err := resolveCommand(runtime.EffectiveConfig, state, name, "")
	if err != nil {
		return TaskSummary{}, err
	}
	logPath, err := a.state.NewRunLogPath(state.RepoID, state.TaskID, resolvedName, a.now())
	if err != nil {
		return TaskSummary{}, err
	}
	if foreground {
		if err := a.exec.RunLogged(ctx, state.WorktreePath, taskEnv(state), logPath, a.stdout, command); err != nil {
			return TaskSummary{}, err
		}
	} else {
		if err := a.exec.RunLogged(ctx, state.WorktreePath, taskEnv(state), logPath, nil, command); err != nil {
			return TaskSummary{}, err
		}
	}
	state.UpdatedAt = a.now()
	if err := a.state.Save(state); err != nil {
		return TaskSummary{}, err
	}
	summary := a.summaryForState(state)
	summary.LogPath = logPath
	return summary, nil
}

func (a *App) watchPullRequest(ctx context.Context, cwd string, number int) (PullRequest, error) {
	selector := fmt.Sprintf("%d", number)
	for {
		pr, err := a.gh.ViewPullRequest(ctx, cwd, selector)
		if err != nil {
			return PullRequest{}, err
		}
		switch strings.ToUpper(pr.State) {
		case "MERGED", "CLOSED":
			return *pr, nil
		}
		select {
		case <-ctx.Done():
			return PullRequest{}, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (a *App) isTaskMerged(ctx context.Context, runtime RuntimeConfig, state TaskState) (bool, TaskState, error) {
	if runtime.EffectiveConfig.GitHub.Enabled {
		pr, err := a.findCurrentPullRequest(ctx, runtime, state)
		if err == nil && pr != nil {
			updateDeliveryFromPullRequest(&state, *pr)
			if state.Delivery.State == DeliveryStateMerged {
				return true, state, nil
			}
			if state.Delivery.State == DeliveryStateClosed {
				return false, state, nil
			}
		}
	}

	baseRef := taskBaseRef(ctx, a.git, runtime, state)
	if strings.TrimSpace(baseRef) == "" {
		return false, state, nil
	}
	merged, err := a.git.MergeBaseIsAncestor(ctx, runtime.RepoRoot, state.Branch, baseRef)
	if err != nil {
		return false, state, err
	}
	if merged {
		state.Delivery.State = DeliveryStateMerged
		if state.Delivery.MergedAt.IsZero() {
			state.Delivery.MergedAt = a.now()
		}
	}
	return merged, state, nil
}

func syncFailureReason(operation string, err error) string {
	switch operation {
	case "merge":
		return fmt.Sprintf("%v; resolve conflicts in the task worktree, then run `git merge --continue` or `git merge --abort` before retrying `agentflow sync`", err)
	default:
		return fmt.Sprintf("%v; resolve conflicts in the task worktree, then run `git rebase --continue` or `git rebase --abort` before retrying `agentflow sync`", err)
	}
}

func (a *App) summaryForState(state TaskState) TaskSummary {
	summary := TaskSummary{
		TaskID:    state.TaskID,
		TaskTitle: state.TaskRef.Title,
		RepoRoot:  state.RepoRoot,
		Worktree:  state.WorktreePath,
		Branch:    state.Branch,
		Session:   state.TmuxSession,
		Surface:   state.Surface,
		Status:    state.Status,
		Delivery:  state.Delivery,
	}
	if isLinearTask(state) {
		summary.Issue = state.TaskRef.Key
		summary.IssueURL = state.TaskRef.URL
		summary.IssueState = state.IssueState
	}
	return summary
}
