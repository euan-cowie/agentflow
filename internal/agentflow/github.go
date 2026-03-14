package agentflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type GitHubOps struct {
	exec Executor
}

type PullRequest struct {
	Number           int                    `json:"number"`
	URL              string                 `json:"url"`
	State            string                 `json:"state"`
	IsDraft          bool                   `json:"isDraft"`
	BaseRefName      string                 `json:"baseRefName"`
	HeadRefName      string                 `json:"headRefName"`
	HeadRefOID       string                 `json:"headRefOid"`
	HeadRepository   *GitHubRepository      `json:"headRepository"`
	HeadRepoOwner    *GitHubRepositoryOwner `json:"headRepositoryOwner"`
	MergeStateStatus string                 `json:"mergeStateStatus"`
	MergedAt         *time.Time             `json:"mergedAt"`
}

type GitHubRepository struct {
	Name string `json:"name"`
}

type GitHubRepositoryOwner struct {
	Login string `json:"login"`
}

type PullRequestCheck struct {
	State string `json:"state"`
}

func NewGitHubOps(exec Executor) GitHubOps {
	return GitHubOps{exec: exec}
}

func (g GitHubOps) AuthStatus(ctx context.Context, cwd string) error {
	_, err := g.exec.Run(ctx, cwd, nil, "gh", "auth", "status")
	return err
}

func (pr PullRequest) HeadRepositoryIdentity() string {
	if pr.HeadRepoOwner == nil || pr.HeadRepository == nil {
		return ""
	}
	owner := strings.TrimSpace(pr.HeadRepoOwner.Login)
	repo := strings.TrimSpace(pr.HeadRepository.Name)
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

func (g GitHubOps) FindPullRequest(ctx context.Context, cwd, branch, headRepoIdentity string) (*PullRequest, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, nil
	}
	result, err := g.exec.Run(ctx, cwd, nil, "gh", "pr", "list", "--head", branch, "--state", "all", "--json", "number,url,state,isDraft,baseRefName,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeStateStatus,mergedAt")
	if err != nil {
		return nil, err
	}
	var prs []PullRequest
	if err := json.Unmarshal([]byte(result.Stdout), &prs); err != nil {
		return nil, err
	}
	var fallback *PullRequest
	for _, pr := range prs {
		if headRepoIdentity != "" && !strings.EqualFold(pr.HeadRepositoryIdentity(), headRepoIdentity) {
			continue
		}
		if strings.EqualFold(pr.State, "OPEN") {
			match := pr
			return &match, nil
		}
		if fallback == nil || strings.EqualFold(pr.State, "MERGED") {
			match := pr
			fallback = &match
		}
	}
	return fallback, nil
}

func (g GitHubOps) ViewPullRequest(ctx context.Context, cwd, selector string) (*PullRequest, error) {
	result, err := g.exec.Run(ctx, cwd, nil, "gh", "pr", "view", selector, "--json", "number,url,state,isDraft,baseRefName,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeStateStatus,mergedAt")
	if err != nil {
		return nil, err
	}
	var pr PullRequest
	if err := json.Unmarshal([]byte(result.Stdout), &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func (g GitHubOps) CreatePullRequest(ctx context.Context, cwd, base string, draft bool, labels, reviewers []string) error {
	args := []string{"pr", "create", "--fill", "--base", base}
	if draft {
		args = append(args, "--draft")
	}
	for _, label := range labels {
		if strings.TrimSpace(label) != "" {
			args = append(args, "--label", label)
		}
	}
	for _, reviewer := range reviewers {
		if strings.TrimSpace(reviewer) != "" {
			args = append(args, "--reviewer", reviewer)
		}
	}
	_, err := g.exec.Run(ctx, cwd, nil, "gh", args...)
	return err
}

func (g GitHubOps) ReadyPullRequest(ctx context.Context, cwd, selector string) error {
	_, err := g.exec.Run(ctx, cwd, nil, "gh", "pr", "ready", selector)
	return err
}

func (g GitHubOps) MergePullRequest(ctx context.Context, cwd, selector, headSHA, mergeMethod string, auto bool) error {
	args := []string{"pr", "merge", selector, "--match-head-commit", headSHA}
	if auto {
		args = append(args, "--auto")
	}
	switch mergeMethod {
	case "", "auto":
		args = append(args, "--merge")
	case "squash":
		args = append(args, "--squash")
	case "merge":
		args = append(args, "--merge")
	case "rebase":
		args = append(args, "--rebase")
	default:
		return fmt.Errorf("unsupported GitHub merge method %q", mergeMethod)
	}
	_, err := g.exec.Run(ctx, cwd, nil, "gh", args...)
	return err
}

func (g GitHubOps) RequiredChecksState(ctx context.Context, cwd, selector string) (string, error) {
	result, err := g.exec.Run(ctx, cwd, nil, "gh", "pr", "checks", selector, "--required", "--json", "state")
	if err != nil {
		return "", err
	}
	var checks []PullRequestCheck
	if err := json.Unmarshal([]byte(result.Stdout), &checks); err != nil {
		return "", err
	}
	return summarizeCheckStates(checks), nil
}

func summarizeCheckStates(checks []PullRequestCheck) string {
	if len(checks) == 0 {
		return ""
	}
	hasPending := false
	for _, check := range checks {
		switch strings.ToUpper(strings.TrimSpace(check.State)) {
		case "SUCCESS", "SKIPPED", "NEUTRAL":
		case "PENDING", "QUEUED", "IN_PROGRESS", "WAITING":
			hasPending = true
		default:
			return "failing"
		}
	}
	if hasPending {
		return "pending"
	}
	return "passing"
}
