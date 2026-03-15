package cli

import (
	"fmt"
	"os"

	"github.com/euan-cowie/agentflow/internal/agentflow"
	"github.com/spf13/cobra"
)

func authCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage stored credentials",
	}
	cmd.AddCommand(linearAuthCommand(app, repoPath))
	return cmd
}

func linearAuthCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "linear",
		Short: "Manage Linear credentials",
	}
	cmd.AddCommand(linearAuthLoginCommand(app))
	cmd.AddCommand(linearAuthLogoutCommand(app))
	cmd.AddCommand(linearAuthStatusCommand(app, repoPath))
	cmd.AddCommand(linearAuthListCommand(app))
	return cmd
}

func linearAuthLoginCommand(app func() *agentflow.App) *cobra.Command {
	var profile string
	var token string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Validate and store a Linear API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := app().SaveLinearCredential(cmd.Context(), profile, token)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "provider=%s\nsource=%s\nstatus=stored\n", status.Provider, status.Source)
			if status.Profile != "" {
				fmt.Fprintf(os.Stdout, "profile=%s\n", status.Profile)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Credential profile name to store")
	cmd.Flags().StringVar(&token, "token", "", "Linear API key to store")
	return cmd
}

func linearAuthLogoutCommand(app func() *agentflow.App) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Delete the stored Linear API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app().DeleteLinearCredential(profile); err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "provider=linear")
			if profile != "" {
				fmt.Fprintf(os.Stdout, "profile=%s\n", profile)
			}
			fmt.Fprintln(os.Stdout, "status=deleted")
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Credential profile name to delete")
	return cmd
}

func linearAuthStatusCommand(app func() *agentflow.App, repoPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show how the Linear API key resolves",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := app().LinearCredentialStatus(cmd.Context(), *repoPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "provider=%s\nsource=%s\navailable=%t\n", status.Provider, status.Source, status.Available)
			if status.Profile != "" {
				fmt.Fprintf(os.Stdout, "profile=%s\n", status.Profile)
			}
			if !status.UpdatedAt.IsZero() {
				fmt.Fprintf(os.Stdout, "updated_at=%s\n", status.UpdatedAt.Format("2006-01-02T15:04:05Z"))
			}
			return nil
		},
	}
}

func linearAuthListCommand(app func() *agentflow.App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored Linear credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			credentials, err := app().ListLinearCredentials()
			if err != nil {
				return err
			}
			if len(credentials) == 0 {
				fmt.Fprintln(os.Stdout, "provider=linear\nstored=false")
				return nil
			}
			for idx, credential := range credentials {
				if idx > 0 {
					fmt.Fprintln(os.Stdout)
				}
				fmt.Fprintf(os.Stdout, "provider=%s\n", credential.Provider)
				if credential.Legacy {
					fmt.Fprintln(os.Stdout, "scope=legacy")
				} else {
					fmt.Fprintln(os.Stdout, "scope=profile")
					fmt.Fprintf(os.Stdout, "profile=%s\n", credential.Profile)
				}
				if !credential.UpdatedAt.IsZero() {
					fmt.Fprintf(os.Stdout, "updated_at=%s\n", credential.UpdatedAt.Format("2006-01-02T15:04:05Z"))
				}
			}
			return nil
		},
	}
}
