package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/euan-cowie/agentflow/internal/agentflow"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	var repoPath string

	root := &cobra.Command{
		Use:   "agentflow",
		Short: "Spin up repo-driven agent workspaces",
	}

	root.PersistentFlags().StringVar(&repoPath, "repo", "", "Path inside the repo to operate on")

	appFor := func() *agentflow.App {
		app, err := agentflow.NewApp(os.Stdin, os.Stdout, os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return app
	}

	root.AddCommand(upCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(authCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(attachCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(statusCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(codexCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(syncCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(submitCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(landCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(verifyCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(reviewCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(downCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(listCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(issuesCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(gcCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(doctorCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(repairCommand(func() *agentflow.App { return appFor() }, &repoPath))
	root.AddCommand(configCommand(func() *agentflow.App { return appFor() }, &repoPath))

	root.SetContext(context.Background())
	return root
}
