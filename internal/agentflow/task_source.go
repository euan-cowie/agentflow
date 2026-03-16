package agentflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	taskSourceLinear = "linear"
	taskSourceManual = "manual"
)

var linearIssueKeyPattern = regexp.MustCompile(`(?i)^[a-z][a-z0-9]*-\d+$`)

func canonicalLinearIssueKey(input string) string {
	return strings.ToUpper(strings.TrimSpace(input))
}

func looksLikeLinearIssueKey(input string) bool {
	return linearIssueKeyPattern.MatchString(strings.TrimSpace(input))
}

func linearTaskIdentity(issueID, issueKey string) string {
	if id := strings.TrimSpace(issueID); id != "" {
		return id
	}
	return canonicalLinearIssueKey(issueKey)
}

func linearTaskID(repoRoot, issueID, issueKey string) string {
	identity := linearTaskIdentity(issueID, issueKey)
	if identity == "" {
		return ""
	}
	return taskID(repoRoot, taskSourceLinear, identity)
}

func legacyLinearTaskID(repoRoot, issueKey string) string {
	key := canonicalLinearIssueKey(issueKey)
	if key == "" {
		return ""
	}
	return taskID(repoRoot, taskSourceLinear, key)
}

func buildLinearTaskRef(issue LinearIssue) (TaskRef, error) {
	key := canonicalLinearIssueKey(issue.Identifier)
	if key == "" {
		return TaskRef{}, fmt.Errorf("linear issue key must not be empty")
	}
	title := strings.TrimSpace(issue.Title)
	display := key
	if title != "" {
		display += " " + title
	}
	slugBase := key
	if title != "" {
		slugBase += " " + title
	}
	ref := TaskRef{
		Source: taskSourceLinear,
		Key:    key,
		Title:  display,
		Slug:   slugify(slugBase),
		ID:     strings.TrimSpace(issue.ID),
		URL:    strings.TrimSpace(issue.URL),
	}
	return ref, nil
}

func resolveLinearTask(repoRoot string, issue LinearIssue) (TaskRef, string, error) {
	ref, err := buildLinearTaskRef(issue)
	if err != nil {
		return TaskRef{}, "", err
	}
	return ref, linearTaskID(repoRoot, ref.ID, ref.Key), nil
}

func splitExplicitTaskSource(input string) (string, string, bool) {
	prefix, value, ok := strings.Cut(strings.TrimSpace(input), ":")
	if !ok {
		return "", "", false
	}
	switch strings.ToLower(strings.TrimSpace(prefix)) {
	case taskSourceLinear, taskSourceManual:
		return strings.ToLower(strings.TrimSpace(prefix)), strings.TrimSpace(value), true
	default:
		return "", "", false
	}
}

func linearTaskInputKey(input string) (string, bool) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", false
	}
	if source, explicitValue, ok := splitExplicitTaskSource(value); ok {
		if source != taskSourceLinear {
			return "", false
		}
		value = explicitValue
	}
	if !looksLikeLinearIssueKey(value) {
		return "", false
	}
	return canonicalLinearIssueKey(value), true
}

func (a *App) lookupTrackedTask(runtime RuntimeConfig, taskID string) (TaskRef, string, bool, error) {
	state, err := a.state.Load(runtime.RepoID, taskID)
	if err == nil {
		return state.TaskRef, state.TaskID, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return TaskRef{}, "", false, nil
	}
	return TaskRef{}, "", false, err
}

func (a *App) lookupTrackedLinearTask(runtime RuntimeConfig, issueKey, issueID string) (TaskState, bool, error) {
	candidates := make([]string, 0, 2)
	if taskID := linearTaskID(runtime.RepoRoot, issueID, issueKey); taskID != "" {
		candidates = append(candidates, taskID)
	}
	if taskID := legacyLinearTaskID(runtime.RepoRoot, issueKey); taskID != "" {
		candidates = append(candidates, taskID)
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		state, err := a.state.Load(runtime.RepoID, candidate)
		if err == nil {
			return state, true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return TaskState{}, false, err
		}
	}

	key := canonicalLinearIssueKey(issueKey)
	issueID = strings.TrimSpace(issueID)
	if key == "" && issueID == "" {
		return TaskState{}, false, nil
	}
	states, err := a.state.List(runtime.RepoID)
	if err != nil {
		return TaskState{}, false, err
	}
	var match *TaskState
	for idx := range states {
		state := &states[idx]
		if !isLinearTask(*state) {
			continue
		}
		if issueID != "" && strings.TrimSpace(state.TaskRef.ID) == issueID {
			if match != nil && match.TaskID != state.TaskID {
				return TaskState{}, false, fmt.Errorf("linear issue %q is tracked by multiple tasks", issueID)
			}
			match = state
			continue
		}
		if key != "" && strings.EqualFold(strings.TrimSpace(state.TaskRef.Key), key) {
			if match != nil && match.TaskID != state.TaskID {
				return TaskState{}, false, fmt.Errorf("linear issue %q is tracked by multiple tasks", key)
			}
			match = state
		}
	}
	if match == nil {
		return TaskState{}, false, nil
	}
	return *match, true, nil
}

func (a *App) lookupTrackedTaskByTitle(runtime RuntimeConfig, title string) (TaskRef, string, bool, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return TaskRef{}, "", false, nil
	}
	states, err := a.state.List(runtime.RepoID)
	if err != nil {
		return TaskRef{}, "", false, err
	}
	var match *TaskState
	for idx := range states {
		if !strings.EqualFold(strings.TrimSpace(states[idx].TaskRef.Title), title) {
			continue
		}
		if match != nil && match.TaskID != states[idx].TaskID {
			return TaskRef{}, "", false, fmt.Errorf("task %q is ambiguous; use manual:<task> or linear:<issue>", title)
		}
		match = &states[idx]
	}
	if match == nil {
		return TaskRef{}, "", false, nil
	}
	return match.TaskRef, match.TaskID, true, nil
}

func (a *App) lookupTrackedTaskByPath(runtime RuntimeConfig, path string) (TaskState, bool, error) {
	path = canonicalPath(path)
	if strings.TrimSpace(path) == "" {
		return TaskState{}, false, nil
	}
	states, err := a.state.List(runtime.RepoID)
	if err != nil {
		return TaskState{}, false, err
	}
	var match *TaskState
	for idx := range states {
		worktree := canonicalPath(states[idx].WorktreePath)
		if worktree == "" || !pathWithin(worktree, path) {
			continue
		}
		if match != nil && match.TaskID != states[idx].TaskID {
			return TaskState{}, false, fmt.Errorf("current directory %q matches multiple tracked tasks; pass an explicit task", path)
		}
		match = &states[idx]
	}
	if match == nil {
		return TaskState{}, false, nil
	}
	return *match, true, nil
}

func (a *App) trackedRepoRootForPath(path string) (string, bool, error) {
	path = canonicalPath(path)
	if strings.TrimSpace(path) == "" {
		return "", false, nil
	}
	states, err := a.state.List("")
	if err != nil {
		return "", false, err
	}
	matchRoot := ""
	for idx := range states {
		worktree := canonicalPath(states[idx].WorktreePath)
		if worktree == "" || !pathWithin(worktree, path) {
			continue
		}
		repoRoot := canonicalPath(states[idx].RepoRoot)
		if matchRoot != "" && matchRoot != repoRoot {
			return "", false, fmt.Errorf("current directory %q matches tracked tasks from multiple repos; pass --repo explicitly", path)
		}
		matchRoot = repoRoot
	}
	if matchRoot == "" {
		return "", false, nil
	}
	return matchRoot, true, nil
}

func (a *App) resolveTrackedTaskRef(runtime RuntimeConfig, input string) (TaskRef, string, bool, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		state, found, err := a.lookupTrackedTaskByPath(runtime, runtime.InvocationPath)
		if err != nil {
			return TaskRef{}, "", false, err
		}
		if !found {
			return TaskRef{}, "", false, nil
		}
		return state.TaskRef, state.TaskID, true, nil
	}

	if source, explicitValue, ok := splitExplicitTaskSource(value); ok {
		switch source {
		case taskSourceManual:
			ref, taskID, err := resolveManualTask(runtime.RepoRoot, explicitValue)
			if err != nil {
				return TaskRef{}, "", false, err
			}
			trackedRef, trackedTaskID, found, err := a.lookupTrackedTask(runtime, taskID)
			if found || err != nil {
				return trackedRef, trackedTaskID, found, err
			}
			return ref, taskID, false, nil
		case taskSourceLinear:
			key := canonicalLinearIssueKey(explicitValue)
			if key == "" {
				return TaskRef{}, "", false, fmt.Errorf("linear issue key must not be empty")
			}
			state, found, err := a.lookupTrackedLinearTask(runtime, key, "")
			if err != nil {
				return TaskRef{}, "", false, err
			}
			if !found {
				return TaskRef{Source: taskSourceLinear, Key: key}, legacyLinearTaskID(runtime.RepoRoot, key), false, nil
			}
			return state.TaskRef, state.TaskID, true, nil
		}
	}

	manualRef, manualID, err := resolveManualTask(runtime.RepoRoot, value)
	if err != nil {
		return TaskRef{}, "", false, err
	}
	if trackedRef, trackedTaskID, found, err := a.lookupTrackedTask(runtime, manualID); found || err != nil {
		return trackedRef, trackedTaskID, found, err
	}
	if issueKey, ok := linearTaskInputKey(value); ok {
		state, found, err := a.lookupTrackedLinearTask(runtime, issueKey, "")
		if err != nil {
			return TaskRef{}, "", false, err
		}
		if found {
			return state.TaskRef, state.TaskID, true, nil
		}
	}
	if trackedRef, trackedTaskID, found, err := a.lookupTrackedTaskByTitle(runtime, value); found || err != nil {
		return trackedRef, trackedTaskID, found, err
	}
	return manualRef, manualID, false, nil
}

func (a *App) resolveTaskRef(ctx context.Context, runtime RuntimeConfig, input string) (TaskRef, string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		if trackedRef, trackedTaskID, found, err := a.resolveTrackedTaskRef(runtime, value); found || err != nil {
			return trackedRef, trackedTaskID, err
		}
		return TaskRef{}, "", fmt.Errorf("task argument is required unless you run this command inside a tracked task worktree")
	}

	if trackedRef, trackedTaskID, found, err := a.resolveTrackedTaskRef(runtime, value); found || err != nil {
		return trackedRef, trackedTaskID, err
	}

	if source, explicitValue, ok := splitExplicitTaskSource(value); ok {
		switch source {
		case taskSourceManual:
			return resolveManualTask(runtime.RepoRoot, explicitValue)
		case taskSourceLinear:
			return a.resolveLinearTaskRef(ctx, runtime, explicitValue)
		}
	}

	manualRef, manualID, err := resolveManualTask(runtime.RepoRoot, value)
	if err != nil {
		return TaskRef{}, "", err
	}
	if _, ok := linearTaskInputKey(value); ok {
		if linearConfigured(runtime.EffectiveConfig.Linear) {
			return a.resolveLinearTaskRef(ctx, runtime, value)
		}
	}

	return manualRef, manualID, nil
}

func (a *App) resolveLinearTaskRef(ctx context.Context, runtime RuntimeConfig, input string) (TaskRef, string, error) {
	key := canonicalLinearIssueKey(input)
	if key == "" {
		return TaskRef{}, "", fmt.Errorf("linear issue key must not be empty")
	}
	if state, found, err := a.lookupTrackedLinearTask(runtime, key, ""); err == nil && found {
		return state.TaskRef, state.TaskID, nil
	} else if err != nil {
		return TaskRef{}, "", err
	}
	if !linearConfigured(runtime.EffectiveConfig.Linear) {
		return TaskRef{}, "", fmt.Errorf("linear is not configured in %s", runtime.ConfigPath)
	}
	apiKey, _, err := a.resolveLinearCredential(runtime.EffectiveConfig.Linear)
	if err != nil {
		return TaskRef{}, "", err
	}
	issue, err := a.linear.Issue(ctx, apiKey, key)
	if err != nil {
		return TaskRef{}, "", err
	}
	if issue == nil {
		return TaskRef{}, "", fmt.Errorf("linear issue %q not found", key)
	}
	if state, found, err := a.lookupTrackedLinearTask(runtime, issue.Identifier, issue.ID); err == nil && found {
		ref, mergeErr := buildLinearTaskRef(*issue)
		if mergeErr != nil {
			return TaskRef{}, "", mergeErr
		}
		if strings.TrimSpace(state.TaskRef.Slug) != "" {
			ref.Slug = state.TaskRef.Slug
		}
		return ref, state.TaskID, nil
	} else if err != nil {
		return TaskRef{}, "", err
	}
	return resolveLinearTask(runtime.RepoRoot, *issue)
}

func (a *App) loadTaskByInput(ctx context.Context, runtime RuntimeConfig, input string) (TaskState, error) {
	if strings.TrimSpace(input) == "" {
		state, found, err := a.lookupTrackedTaskByPath(runtime, runtime.InvocationPath)
		if err != nil {
			return TaskState{}, err
		}
		if !found {
			return TaskState{}, fmt.Errorf("task argument is required unless you run this command inside a tracked task worktree")
		}
		return state, nil
	}
	_, taskID, found, err := a.resolveTrackedTaskRef(runtime, input)
	if err != nil {
		return TaskState{}, err
	}
	if !found {
		issueKey, ok := linearTaskInputKey(input)
		if !ok || !runtime.Trusted || !linearConfigured(runtime.EffectiveConfig.Linear) {
			return TaskState{}, os.ErrNotExist
		}
		ref, resolvedTaskID, err := a.resolveLinearTaskRef(ctx, runtime, issueKey)
		if err != nil {
			return TaskState{}, err
		}
		state, err := a.state.Load(runtime.RepoID, resolvedTaskID)
		if err != nil {
			return TaskState{}, err
		}
		if ref.Source == taskSourceLinear {
			if strings.TrimSpace(ref.Slug) == "" {
				ref.Slug = state.TaskRef.Slug
			}
			state.TaskRef = ref
		}
		return state, nil
	}
	return a.state.Load(runtime.RepoID, taskID)
}

func isLinearTask(state TaskState) bool {
	return strings.EqualFold(state.TaskRef.Source, taskSourceLinear)
}

func taskLookupInput(state TaskState) string {
	if strings.TrimSpace(state.TaskRef.Source) == "" {
		return state.TaskRef.Title
	}
	return strings.ToLower(strings.TrimSpace(state.TaskRef.Source)) + ":" + state.TaskRef.Key
}

func pathWithin(root, path string) bool {
	root = canonicalPath(root)
	path = canonicalPath(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
