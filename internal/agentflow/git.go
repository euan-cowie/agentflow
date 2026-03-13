package agentflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type GitOps struct {
	exec Executor
}

func NewGitOps(exec Executor) GitOps {
	return GitOps{exec: exec}
}

func (g GitOps) RepoRoot(ctx context.Context, dir string) (string, error) {
	result, err := g.exec.Run(ctx, dir, nil, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (g GitOps) WorktreeList(ctx context.Context, repoRoot string) ([]WorktreeInfo, error) {
	result, err := g.exec.Run(ctx, repoRoot, nil, "git", "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	chunks := strings.Split(result.Stdout, "\x00")
	infos := make([]WorktreeInfo, 0)
	var current *WorktreeInfo
	for _, chunk := range chunks {
		if chunk == "" {
			continue
		}
		switch {
		case strings.HasPrefix(chunk, "worktree "):
			if current != nil {
				infos = append(infos, *current)
			}
			current = &WorktreeInfo{Path: strings.TrimPrefix(chunk, "worktree ")}
		case strings.HasPrefix(chunk, "HEAD "):
			if current != nil {
				current.Head = strings.TrimPrefix(chunk, "HEAD ")
			}
		case strings.HasPrefix(chunk, "branch "):
			if current != nil {
				current.BranchRef = strings.TrimPrefix(chunk, "branch ")
			}
		case chunk == "locked":
			if current != nil {
				current.Locked = true
			}
		case strings.HasPrefix(chunk, "prunable"):
			if current != nil {
				current.Prunable = true
			}
		}
	}
	if current != nil {
		infos = append(infos, *current)
	}
	return infos, nil
}

func (g GitOps) CreateWorktree(ctx context.Context, repoRoot, branch, path, baseBranch string) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	_, err := g.exec.Run(ctx, repoRoot, nil, "git", "rev-parse", "--verify", "refs/heads/"+branch)
	if err == nil {
		_, err = g.exec.Run(ctx, repoRoot, nil, "git", "worktree", "add", path, branch)
		return err
	}
	_, err = g.exec.Run(ctx, repoRoot, nil, "git", "worktree", "add", "-b", branch, path, baseBranch)
	return err
}

func (g GitOps) RefExists(ctx context.Context, repoRoot, ref string) bool {
	if strings.TrimSpace(ref) == "" {
		return false
	}
	_, err := g.exec.Run(ctx, repoRoot, nil, "git", "rev-parse", "--verify", ref)
	return err == nil
}

func (g GitOps) HasCommit(ctx context.Context, repoRoot string) bool {
	_, err := g.exec.Run(ctx, repoRoot, nil, "git", "rev-parse", "--verify", "HEAD")
	return err == nil
}

func (g GitOps) CurrentBranch(ctx context.Context, repoRoot string) (string, error) {
	result, err := g.exec.Run(ctx, repoRoot, nil, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (g GitOps) ResolveBaseRef(ctx context.Context, repoRoot, configured string) (string, bool, error) {
	configured = strings.TrimSpace(configured)
	if g.RefExists(ctx, repoRoot, configured) {
		return configured, false, nil
	}
	if !g.HasCommit(ctx, repoRoot) {
		branch, err := g.CurrentBranch(ctx, repoRoot)
		if err == nil && branch != "" {
			return "", false, fmt.Errorf("repo has no commits yet on %q; make the initial commit before running agentflow up", branch)
		}
		return "", false, errors.New("repo has no commits yet; make the initial commit before running agentflow up")
	}

	current, err := g.CurrentBranch(ctx, repoRoot)
	if err == nil && current != "" && g.RefExists(ctx, repoRoot, current) {
		return current, current != configured, nil
	}
	return "", false, fmt.Errorf("configured base branch %q does not exist; set repo.base_branch to a valid ref", configured)
}

func (g GitOps) RemoveWorktree(ctx context.Context, repoRoot, path string) error {
	_, err := g.exec.Run(ctx, repoRoot, nil, "git", "worktree", "remove", path)
	return err
}

func (g GitOps) RepairWorktree(ctx context.Context, repoRoot, path string) error {
	_, err := g.exec.Run(ctx, repoRoot, nil, "git", "worktree", "repair", path)
	return err
}

func (g GitOps) PruneWorktrees(ctx context.Context, repoRoot string) error {
	_, err := g.exec.Run(ctx, repoRoot, nil, "git", "worktree", "prune")
	return err
}

func (g GitOps) DeleteBranch(ctx context.Context, repoRoot, branch string) error {
	_, err := g.exec.Run(ctx, repoRoot, nil, "git", "branch", "-d", branch)
	return err
}

func (g GitOps) IsBranchMerged(ctx context.Context, repoRoot, baseBranch, branch string) (bool, error) {
	result, err := g.exec.Run(ctx, repoRoot, nil, "git", "branch", "--merged", baseBranch, "--list", branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.Stdout) != "", nil
}

func (g GitOps) IsDirty(ctx context.Context, worktreePath string) (bool, error) {
	result, err := g.exec.Run(ctx, worktreePath, nil, "git", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.Stdout) != "", nil
}

func (g GitOps) IsDirtyIgnoring(ctx context.Context, worktreePath string, ignorePaths []string) (bool, error) {
	args := []string{"status", "--porcelain", "--untracked-files=all", "--", "."}
	for _, path := range uniqueStrings(ignorePaths) {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		args = append(args, ":(exclude)"+path)
	}
	result, err := g.exec.Run(ctx, worktreePath, nil, "git", args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.Stdout) != "", nil
}

func (g GitOps) ValidateTaskWorktree(ctx context.Context, state TaskState) error {
	infos, err := g.WorktreeList(ctx, state.RepoRoot)
	if err != nil {
		return err
	}
	branchRef := "refs/heads/" + state.Branch
	expectedPath := canonicalPath(state.WorktreePath)
	for _, info := range infos {
		if canonicalPath(info.Path) == expectedPath && info.BranchRef == branchRef {
			if _, err := os.Stat(info.Path); err != nil {
				return fmt.Errorf("worktree path missing on disk: %s", info.Path)
			}
			return nil
		}
	}
	return errors.New("saved worktree is not present in git worktree metadata")
}

func (g GitOps) BranchCheckedOutElsewhere(ctx context.Context, repoRoot, branch, expectedPath string) (bool, error) {
	infos, err := g.WorktreeList(ctx, repoRoot)
	if err != nil {
		return false, err
	}
	branchRef := "refs/heads/" + branch
	expectedPath = canonicalPath(expectedPath)
	for _, info := range infos {
		if info.BranchRef == branchRef && canonicalPath(info.Path) != expectedPath {
			return true, nil
		}
	}
	return false, nil
}
