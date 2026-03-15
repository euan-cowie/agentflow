package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/euan-cowie/agentflow/internal/agentflow"
	"github.com/spf13/cobra"
)

func upCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var surface string
	cmd := &cobra.Command{
		Use:   "up [task]",
		Short: "Create or resume a task worktree and tmux session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task := ""
			if len(args) == 1 {
				task = args[0]
			} else {
				issues, err := app().LinearIssues(cmd.Context(), agentflow.CommonOptions{RepoPath: *repoPath})
				if err != nil {
					return err
				}
				task, err = pickLinearIssue(issues)
				if errors.Is(err, errLinearIssueSelectionCancelled) {
					return nil
				}
				if err != nil {
					return err
				}
			}
			summary, err := app().Up(cmd.Context(), agentflow.UpOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          task,
				Surface:       surface,
			})
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
	cmd.Flags().StringVar(&surface, "surface", "", "Override the task surface")
	return cmd
}

func attachCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "attach <task>",
		Short: "Attach to the tmux session for a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Attach(cmd.Context(), agentflow.CommonOptions{RepoPath: *repoPath}, args[0])
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
}

func statusCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status [task]",
		Short: "Show local and delivery status for one task or all tasks",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task := ""
			if len(args) == 1 {
				task = args[0]
			}
			statuses, err := app().Status(cmd.Context(), agentflow.CommonOptions{RepoPath: *repoPath}, task)
			if err != nil {
				return err
			}
			printStatuses(statuses)
			return nil
		},
	}
}

func codexCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "codex <task>",
		Short: "Jump to the primary Codex window for a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Codex(cmd.Context(), agentflow.CommonOptions{RepoPath: *repoPath}, args[0])
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
}

func syncCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var all bool
	var push bool
	cmd := &cobra.Command{
		Use:   "sync <task>",
		Short: "Sync one or more task branches with the repo base branch",
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) != 0 {
					return fmt.Errorf("sync --all does not take a task argument")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			task := ""
			if len(args) == 1 {
				task = args[0]
			}
			summaries, err := app().Sync(cmd.Context(), agentflow.SyncOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          task,
				All:           all,
				Push:          push,
			})
			if err != nil {
				return err
			}
			printSummaries(summaries)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Sync every tracked task in the repo")
	cmd.Flags().BoolVar(&push, "push", false, "Push the branch after syncing")
	return cmd
}

func submitCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var draft bool
	var ready bool
	cmd := &cobra.Command{
		Use:   "submit <task>",
		Short: "Push a task branch and create or reuse a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Submit(cmd.Context(), agentflow.SubmitOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          args[0],
				Draft:         draft,
				Ready:         ready,
			})
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&draft, "draft", false, "Create or keep the pull request as draft")
	cmd.Flags().BoolVar(&ready, "ready", false, "Submit the pull request as ready for review")
	return cmd
}

func landCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var watch bool
	cmd := &cobra.Command{
		Use:   "land <task>",
		Short: "Run preflight checks and enable merge for a task pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Land(cmd.Context(), agentflow.LandOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          args[0],
				Watch:         watch,
			})
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "Wait until the pull request merges or closes")
	return cmd
}

func gcCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "gc [task]",
		Short: "Clean up merged task worktrees and sessions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task := ""
			if len(args) == 1 {
				task = args[0]
			}
			summaries, err := app().GC(cmd.Context(), agentflow.GCOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          task,
			})
			if err != nil {
				return err
			}
			printSummaries(summaries)
			return nil
		},
	}
}

func verifyCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var surface string
	var foreground bool
	cmd := &cobra.Command{
		Use:   "verify <task>",
		Short: "Run the configured verify command for a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Verify(cmd.Context(), agentflow.VerifyOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          args[0],
				Surface:       surface,
				Foreground:    foreground,
			}, "verify")
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
	cmd.Flags().StringVar(&surface, "surface", "", "Override the verify surface")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run in the foreground instead of the verify tmux window")
	return cmd
}

func reviewCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var foreground bool
	cmd := &cobra.Command{
		Use:   "review <task>",
		Short: "Run the configured review command for a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Review(cmd.Context(), agentflow.VerifyOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          args[0],
				Foreground:    foreground,
			})
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run in the foreground instead of the verify tmux window")
	return cmd
}

func downCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var deleteBranch bool
	var force bool
	cmd := &cobra.Command{
		Use:   "down <task>",
		Short: "Tear down a tracked task worktree and session",
		Long: "Tear down a tracked task worktree and session.\n\n" +
			"<task> can be a Linear issue key like TGG-132, an explicit source ref like linear:TGG-132 or manual:fix auth flow, or the exact tracked task title.",
		Example: "  agentflow down TGG-132\n" +
			"  agentflow down linear:TGG-132\n" +
			"  agentflow down \"fix auth flow\"",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Down(cmd.Context(), agentflow.DownOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          args[0],
				DeleteBranch:  deleteBranch,
				Force:         force,
			})
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "Delete the branch after teardown if it is merged")
	cmd.Flags().BoolVar(&force, "force", false, "Discard dirty worktree changes after an explicit confirmation prompt")
	return cmd
}

func listCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tracked tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			states, err := app().List(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			printTaskStates(states, verbose)
			return nil
		},
	}
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show branch, worktree, session, and the full saved failure reason")
	return cmd
}

func issuesCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issues",
		Short: "Inspect available tracker issues",
	}
	cmd.AddCommand(issuesListCommand(app, repoPath))
	return cmd
}

func issuesListCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available issues from the configured tracker",
		RunE: func(cmd *cobra.Command, args []string) error {
			issues, err := app().LinearIssues(cmd.Context(), agentflow.CommonOptions{RepoPath: *repoPath})
			if err != nil {
				return err
			}
			for _, issue := range issues {
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", issue.Identifier, issue.State.Name, issue.Title)
			}
			return nil
		},
	}
}

func doctorCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check required binaries and repo configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks, err := app().Doctor(cmd.Context(), agentflow.DoctorOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
			})
			if err != nil {
				return err
			}
			failed := false
			for _, check := range checks {
				status := "ok"
				if !check.OK {
					status = "fail"
					failed = true
				}
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", status, check.Name, check.Details)
			}
			if failed {
				return fmt.Errorf("doctor found failing checks")
			}
			return nil
		},
	}
}

func repairCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "repair <task>",
		Short: "Repair drift between task state, worktree metadata, and tmux",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Repair(cmd.Context(), agentflow.CommonOptions{RepoPath: *repoPath}, args[0])
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
}

func configCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect or write repo config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			overview, err := app().ConfigOverview(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			status := "missing"
			if overview.Repo.Exists {
				status = "exists"
			}
			fmt.Fprintf(os.Stdout, "Repo config: %s (%s)\n", overview.Repo.Path, status)
			fmt.Fprintln(os.Stdout, "Effective config: derived from repo config + tool-owned defaults")
			return nil
		},
	}
	cmd.AddCommand(configPathCommand(app, repoPath))
	cmd.AddCommand(configShowCommand(app, repoPath))
	cmd.AddCommand(configWriteCommand(app, repoPath))
	cmd.AddCommand(configEffectiveCommand(app, repoPath))
	return cmd
}

func configPathCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the repo config path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _, _, err := app().ShowConfig(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	}
}

func configShowCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the repo config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, content, exists, err := app().ShowConfig(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			if !exists {
				fmt.Fprintf(os.Stdout, "missing\t%s\nhint=run 'agentflow config write'\n", path)
				return nil
			}
			fmt.Fprint(os.Stdout, content)
			return nil
		},
	}
}

func configWriteCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write the sample repo config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := app().WriteConfig(cmd.Context(), *repoPath, force)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")
	return cmd
}

func configEffectiveCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "effective",
		Short: "Inspect the merged effective config",
		Args:  cobra.NoArgs,
	}
	var format string
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		content, err := app().ShowEffectiveConfig(cmd.Context(), *repoPath, format)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stdout, content)
		return nil
	}
	cmd.Flags().StringVar(&format, "format", "toml", "Output format: toml or json")
	return cmd
}

func printSummary(summary agentflow.TaskSummary) {
	fmt.Fprintf(os.Stdout, "task_id=%s\ntask=%s\nstatus=%s\nrepo=%s\nworktree=%s\nbranch=%s\nsession=%s\nsurface=%s\n", summary.TaskID, summary.TaskTitle, summary.Status, summary.RepoRoot, summary.Worktree, summary.Branch, summary.Session, summary.Surface)
	if summary.ConfigDrift {
		fmt.Fprintln(os.Stdout, "config_drift=true")
	}
	if summary.LogPath != "" {
		fmt.Fprintf(os.Stdout, "log=%s\n", summary.LogPath)
	}
	if summary.Issue != "" {
		fmt.Fprintf(os.Stdout, "issue=%s\n", summary.Issue)
	}
	if summary.IssueURL != "" {
		fmt.Fprintf(os.Stdout, "issue_url=%s\n", summary.IssueURL)
	}
	if summary.IssueState != "" {
		fmt.Fprintf(os.Stdout, "issue_state=%s\n", summary.IssueState)
	}
	if summary.Delivery.State != "" {
		fmt.Fprintf(os.Stdout, "delivery_state=%s\n", summary.Delivery.State)
	}
	if summary.Delivery.PullRequestNumber != 0 {
		fmt.Fprintf(os.Stdout, "pr=%d\n", summary.Delivery.PullRequestNumber)
	}
	if summary.Delivery.PullRequestURL != "" {
		fmt.Fprintf(os.Stdout, "pr_url=%s\n", summary.Delivery.PullRequestURL)
	}
	if summary.Ahead != 0 || summary.Behind != 0 {
		fmt.Fprintf(os.Stdout, "ahead=%d\nbehind=%d\n", summary.Ahead, summary.Behind)
	}
	if summary.ChecksState != "" {
		fmt.Fprintf(os.Stdout, "checks=%s\n", summary.ChecksState)
	}
	if summary.MergeState != "" {
		fmt.Fprintf(os.Stdout, "merge_state=%s\n", summary.MergeState)
	}
}

func printSummaries(summaries []agentflow.TaskSummary) {
	for _, summary := range summaries {
		printSummary(summary)
		fmt.Fprintln(os.Stdout)
	}
}

func printStatuses(statuses []agentflow.TaskStatus) {
	if len(statuses) == 1 {
		status := statuses[0]
		fmt.Fprintf(os.Stdout, "task_id=%s\ntask=%s\nstatus=%s\nrepo=%s\nworktree=%s\nbranch=%s\nsession=%s\nsurface=%s\n", status.TaskID, status.TaskTitle, status.Status, status.RepoRoot, status.Worktree, status.Branch, status.Session, status.Surface)
		if status.ConfigDrift {
			fmt.Fprintln(os.Stdout, "config_drift=true")
		}
		if status.FailureReason != "" {
			fmt.Fprintf(os.Stdout, "failure=%s\n", status.FailureReason)
		}
		if status.Issue != "" {
			fmt.Fprintf(os.Stdout, "issue=%s\n", status.Issue)
		}
		if status.IssueURL != "" {
			fmt.Fprintf(os.Stdout, "issue_url=%s\n", status.IssueURL)
		}
		if status.IssueState != "" {
			fmt.Fprintf(os.Stdout, "issue_state=%s\n", status.IssueState)
		}
		fmt.Fprintf(os.Stdout, "delivery_state=%s\n", status.Delivery.State)
		fmt.Fprintf(os.Stdout, "dirty=%t\nahead=%d\nbehind=%d\n", status.Dirty, status.Ahead, status.Behind)
		if status.Delivery.PullRequestNumber != 0 {
			fmt.Fprintf(os.Stdout, "pr=%d\n", status.Delivery.PullRequestNumber)
		}
		if status.Delivery.PullRequestURL != "" {
			fmt.Fprintf(os.Stdout, "pr_url=%s\n", status.Delivery.PullRequestURL)
		}
		if status.ChecksState != "" {
			fmt.Fprintf(os.Stdout, "checks=%s\n", status.ChecksState)
		}
		if status.MergeState != "" {
			fmt.Fprintf(os.Stdout, "merge_state=%s\n", status.MergeState)
		}
		return
	}

	for _, status := range statuses {
		fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\tdirty=%t\tahead=%d\tbehind=%d", status.TaskTitle, status.Status, status.Delivery.State, status.Branch, status.Dirty, status.Ahead, status.Behind)
		if status.Delivery.PullRequestNumber != 0 {
			fmt.Fprintf(os.Stdout, "\tpr=%d", status.Delivery.PullRequestNumber)
		}
		if status.ChecksState != "" {
			fmt.Fprintf(os.Stdout, "\tchecks=%s", status.ChecksState)
		}
		if status.MergeState != "" {
			fmt.Fprintf(os.Stdout, "\tmerge=%s", status.MergeState)
		}
		if status.FailureReason != "" {
			fmt.Fprintf(os.Stdout, "\tfailure=%s", status.FailureReason)
		}
		fmt.Fprintln(os.Stdout)
	}
}

func printTaskStates(states []agentflow.TaskState, verbose bool) {
	for _, state := range states {
		if verbose {
			fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%s", taskListIdentifier(state), state.Status, state.Branch, state.WorktreePath, state.TmuxSession)
			if state.FailureReason != "" {
				fmt.Fprintf(os.Stdout, "\t%s", state.FailureReason)
			}
			fmt.Fprintln(os.Stdout)
			continue
		}

		fmt.Fprintf(os.Stdout, "%s\t%s", taskListIdentifier(state), state.Status)
		if title := taskListTitle(state); title != "" {
			fmt.Fprintf(os.Stdout, "\t%s", title)
		}
		if state.FailureReason != "" {
			fmt.Fprintf(os.Stdout, "\terror=%s", summarizeFailureReason(state.FailureReason))
		}
		fmt.Fprintln(os.Stdout)
	}
}

func taskListIdentifier(state agentflow.TaskState) string {
	if strings.EqualFold(strings.TrimSpace(state.TaskRef.Source), "linear") && strings.TrimSpace(state.TaskRef.Key) != "" {
		return strings.TrimSpace(state.TaskRef.Key)
	}
	if title := strings.TrimSpace(state.TaskRef.Title); title != "" {
		return title
	}
	if key := strings.TrimSpace(state.TaskRef.Key); key != "" {
		return key
	}
	return state.TaskID
}

func taskListTitle(state agentflow.TaskState) string {
	title := strings.TrimSpace(state.TaskRef.Title)
	if title == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(state.TaskRef.Source), "linear") {
		key := strings.TrimSpace(state.TaskRef.Key)
		if key != "" {
			trimmed := strings.TrimSpace(strings.TrimPrefix(title, key))
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func summarizeFailureReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	if idx := strings.Index(reason, "{"); idx >= 0 {
		var payload struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal([]byte(reason[idx:]), &payload); err == nil {
			if len(payload.Errors) > 0 && strings.TrimSpace(payload.Errors[0].Message) != "" {
				reason = strings.TrimSpace(payload.Errors[0].Message)
			}
		}
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if len(reason) > 120 {
		return reason[:117] + "..."
	}
	return reason
}
