package agentflow

import (
	"context"
	"fmt"
	"strings"
)

func (a *App) LinearIssues(ctx context.Context, opts CommonOptions) ([]LinearIssue, error) {
	runtime, err := a.loadRuntime(ctx, opts.RepoPath)
	if err != nil {
		return nil, err
	}
	if !linearConfigured(runtime.EffectiveConfig.Linear) {
		return nil, fmt.Errorf("linear is not configured in %s", runtime.ConfigPath)
	}
	if err := a.ensureWorkflowTrusted(&runtime); err != nil {
		return nil, err
	}
	apiKey, _, err := a.resolveLinearCredential(runtime.EffectiveConfig.Linear)
	if err != nil {
		return nil, err
	}
	return a.linear.PickerIssues(ctx, apiKey, runtime.EffectiveConfig.Linear)
}

func (a *App) startLinearIssueIfNeeded(ctx context.Context, runtime RuntimeConfig, state *TaskState) error {
	if !isLinearTask(*state) || !linearConfigured(runtime.EffectiveConfig.Linear) {
		return nil
	}
	apiKey, _, err := a.resolveLinearCredential(runtime.EffectiveConfig.Linear)
	if err != nil {
		return err
	}
	issue, err := a.fetchLinearIssue(ctx, runtime, *state, apiKey)
	if err != nil || issue == nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(issue.State.Type)) {
	case "completed", "canceled":
		return fmt.Errorf("linear issue %q is already %s", issue.Identifier, issue.State.Name)
	case "started":
		a.applyLinearIssue(state, *issue)
		return nil
	}
	updated, err := a.linear.TransitionIssue(ctx, apiKey, *issue, runtime.EffectiveConfig.Linear.StartedState, "started")
	if err != nil {
		return err
	}
	a.applyLinearIssue(state, updated)
	return nil
}

func (a *App) refreshLinearIssueSnapshot(ctx context.Context, runtime RuntimeConfig, state *TaskState) error {
	if !isLinearTask(*state) || !linearConfigured(runtime.EffectiveConfig.Linear) {
		return nil
	}
	apiKey, ok, err := a.resolveOptionalLinearCredential(runtime.EffectiveConfig.Linear)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	issue, err := a.fetchLinearIssue(ctx, runtime, *state, apiKey)
	if err != nil || issue == nil {
		return err
	}
	a.applyLinearIssue(state, *issue)
	return nil
}

func (a *App) resolveOptionalLinearCredential(cfg LinearConfig) (string, bool, error) {
	apiKey, status, err := a.resolveLinearCredential(cfg)
	if err != nil {
		if !status.Available {
			return "", false, nil
		}
		return "", false, err
	}
	return apiKey, true, nil
}

func (a *App) reconcileLinearTask(ctx context.Context, runtime RuntimeConfig, state *TaskState) error {
	if !isLinearTask(*state) || !linearConfigured(runtime.EffectiveConfig.Linear) {
		return nil
	}
	apiKey, ok, err := a.resolveOptionalLinearCredential(runtime.EffectiveConfig.Linear)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	issue, err := a.fetchLinearIssue(ctx, runtime, *state, apiKey)
	if err != nil || issue == nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(issue.State.Type)) {
	case "completed", "canceled":
		a.applyLinearIssue(state, *issue)
		return nil
	}
	if state.Delivery.State == DeliveryStateMerged {
		issueValue, err := a.linear.TransitionIssue(ctx, apiKey, *issue, runtime.EffectiveConfig.Linear.CompletedState, "completed")
		if err != nil {
			return err
		}
		issue = &issueValue
	}
	a.applyLinearIssue(state, *issue)
	return nil
}

func (a *App) ensureLinearPullRequestLink(ctx context.Context, runtime RuntimeConfig, state *TaskState) error {
	if !isLinearTask(*state) || !linearConfigured(runtime.EffectiveConfig.Linear) || strings.TrimSpace(state.Delivery.PullRequestURL) == "" {
		return nil
	}
	apiKey, ok, err := a.resolveOptionalLinearCredential(runtime.EffectiveConfig.Linear)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	issue, err := a.fetchLinearIssue(ctx, runtime, *state, apiKey)
	if err != nil || issue == nil {
		return err
	}
	title := "Pull request"
	if state.Delivery.PullRequestNumber != 0 {
		title = fmt.Sprintf("PR #%d", state.Delivery.PullRequestNumber)
	}
	subtitle := "Open pull request"
	if state.Delivery.State == DeliveryStateMerged {
		subtitle = "Merged pull request"
	}
	if err := a.linear.EnsureAttachment(ctx, apiKey, issue.ID, title, subtitle, state.Delivery.PullRequestURL); err != nil {
		return err
	}
	a.applyLinearIssue(state, *issue)
	return nil
}

func (a *App) fetchLinearIssue(ctx context.Context, runtime RuntimeConfig, state TaskState, apiKey string) (*LinearIssue, error) {
	if !isLinearTask(state) || !linearConfigured(runtime.EffectiveConfig.Linear) {
		return nil, nil
	}
	if lookup := strings.TrimSpace(state.TaskRef.ID); lookup != "" {
		issue, err := a.linear.IssueByID(ctx, apiKey, lookup)
		if err != nil {
			return nil, err
		}
		if issue == nil {
			return nil, fmt.Errorf("linear issue %q not found", state.TaskRef.Key)
		}
		return issue, nil
	}
	issue, err := a.linear.Issue(ctx, apiKey, state.TaskRef.Key)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, fmt.Errorf("linear issue %q not found", state.TaskRef.Key)
	}
	return issue, nil
}

func (a *App) applyLinearIssue(state *TaskState, issue LinearIssue) {
	ref, err := buildLinearTaskRef(issue)
	if err == nil {
		if strings.TrimSpace(state.TaskRef.Slug) != "" {
			ref.Slug = state.TaskRef.Slug
		}
		state.TaskRef.ID = ref.ID
		state.TaskRef.Key = ref.Key
		state.TaskRef.URL = ref.URL
		state.TaskRef.Title = ref.Title
		if strings.TrimSpace(state.TaskRef.Slug) == "" {
			state.TaskRef.Slug = ref.Slug
		}
	}
	state.IssueState = strings.TrimSpace(issue.State.Name)
	state.IssueContext = &issue.Context
}
