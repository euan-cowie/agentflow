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
		Short: "Inspect global, repo, manifest, and effective config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			overview, err := app().ConfigOverview(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "global\t%s\texists=%t\trole=personal-defaults\n", overview.Global.Path, overview.Global.Exists)
			if overview.Repo != nil {
				fmt.Fprintf(os.Stdout, "repo\t%s\texists=%t\trole=repo-conventions\n", overview.Repo.Path, overview.Repo.Exists)
			}
			if overview.Manifest != nil {
				fmt.Fprintf(os.Stdout, "manifest\t%s\texists=%t\trole=executable-workflow\n", overview.Manifest.Path, overview.Manifest.Exists)
			}
			return nil
		},
	}
	cmd.AddCommand(configGlobalCommand(app))
	cmd.AddCommand(configRepoCommand(app, repoPath))
	cmd.AddCommand(configManifestCommand(app, repoPath))
	cmd.AddCommand(configEffectiveCommand(app, repoPath))
	return cmd
}

func configGlobalCommand(app func() *agentflow.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "global",
		Short: "Inspect or write global config",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the global config path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _, _, err := app().ShowGlobalConfig()
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the global config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, content, exists, err := app().ShowGlobalConfig()
			if err != nil {
				return err
			}
			if !exists {
				fmt.Fprintf(os.Stdout, "missing\t%s\nhint=run 'agentflow config global write'\n", path)
				return nil
			}
			fmt.Fprint(os.Stdout, content)
			return nil
		},
	})
	var force bool
	writeCmd := &cobra.Command{
		Use:   "write",
		Short: "Write the sample global config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := app().WriteGlobalConfig(force)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	}
	writeCmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")
	cmd.AddCommand(writeCmd)
	return cmd
}

func configRepoCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Inspect or write repo config",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the repo config path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _, _, err := app().ShowRepoConfig(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the repo config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, content, exists, err := app().ShowRepoConfig(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			if !exists {
				fmt.Fprintf(os.Stdout, "missing\t%s\nhint=run 'agentflow config repo write'\n", path)
				return nil
			}
			fmt.Fprint(os.Stdout, content)
			return nil
		},
	})
	var force bool
	writeCmd := &cobra.Command{
		Use:   "write",
		Short: "Write the sample repo config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := app().WriteRepoConfig(cmd.Context(), *repoPath, force)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	}
	writeCmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")
	cmd.AddCommand(writeCmd)
	return cmd
}

func configManifestCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Inspect or write repo manifest",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the repo manifest path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _, _, err := app().ShowManifest(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the repo manifest file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, content, exists, err := app().ShowManifest(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			if !exists {
				fmt.Fprintf(os.Stdout, "missing\t%s\nhint=run 'agentflow config manifest write'\n", path)
				return nil
			}
			fmt.Fprint(os.Stdout, content)
			return nil
		},
	})
	var force bool
	writeCmd := &cobra.Command{
		Use:   "write",
		Short: "Write the sample repo manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := app().WriteManifest(cmd.Context(), *repoPath, force)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	}
	writeCmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file")
	cmd.AddCommand(writeCmd)
	return cmd
}

func configEffectiveCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "effective",
		Short: "Inspect the merged effective config",
	}
	var format string
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Print the merged effective config for the current repo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := app().ShowEffectiveConfig(cmd.Context(), *repoPath, format)
			if err != nil {
				return err
			}
			fmt.Fprint(os.Stdout, content)
			return nil
		},
	}
	showCmd.Flags().StringVar(&format, "format", "toml", "Output format: toml or json")
	cmd.AddCommand(showCmd)
	return cmd
}

func printSummary(summary agentflow.TaskSummary) {
	fmt.Fprintf(os.Stdout, "task_id=%s\nstatus=%s\nrepo=%s\nworktree=%s\nbranch=%s\nsession=%s\nsurface=%s\n", summary.TaskID, summary.Status, summary.RepoRoot, summary.Worktree, summary.Branch, summary.Session, summary.Surface)
	if summary.ManifestDrift {
		fmt.Fprintln(os.Stdout, "manifest_drift=true")
	}
	if summary.LogPath != "" {
		fmt.Fprintf(os.Stdout, "log=%s\n", summary.LogPath)
	}
}
