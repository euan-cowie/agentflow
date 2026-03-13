package cli

import (
	"fmt"
	"os"

	"github.com/euan-cowie/agentflow/internal/agentflow"
	"github.com/spf13/cobra"
)

func upCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	var surface string
	cmd := &cobra.Command{
		Use:   "up <task>",
		Short: "Create or resume a task worktree and tmux session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Up(cmd.Context(), agentflow.UpOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          args[0],
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
	cmd := &cobra.Command{
		Use:   "down <task>",
		Short: "Tear down a task worktree and session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := app().Down(cmd.Context(), agentflow.DownOptions{
				CommonOptions: agentflow.CommonOptions{RepoPath: *repoPath},
				Task:          args[0],
				DeleteBranch:  deleteBranch,
			})
			if err != nil {
				return err
			}
			printSummary(summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "Delete the branch after teardown if it is merged")
	return cmd
}

func listCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tracked tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			states, err := app().List(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			for _, state := range states {
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%s", state.TaskRef.Title, state.Status, state.Branch, state.WorktreePath, state.TmuxSession)
				if state.FailureReason != "" {
					fmt.Fprintf(os.Stdout, "\t%s", state.FailureReason)
				}
				fmt.Fprintln(os.Stdout)
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
	fmt.Fprintf(os.Stdout, "task_id=%s\nstatus=%s\nrepo=%s\nworktree=%s\nbranch=%s\nsession=%s\nsurface=%s\n", summary.TaskID, summary.Status, summary.RepoRoot, summary.Worktree, summary.Branch, summary.Session, summary.Surface)
	if summary.ConfigDrift {
		fmt.Fprintln(os.Stdout, "config_drift=true")
	}
	if summary.LogPath != "" {
		fmt.Fprintf(os.Stdout, "log=%s\n", summary.LogPath)
	}
}
