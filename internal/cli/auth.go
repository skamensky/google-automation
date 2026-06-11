package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/skamensky/google-automation/internal/googleaccounts"
	"github.com/spf13/cobra"
)

func newAuthCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Google OAuth authorization",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Authorize and save a Google account token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			account, err := googleaccounts.Login(ctx, cfg.accountsConfig())
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Logged in %s\n", account.Email)
			fmt.Fprintf(cmd.OutOrStdout(), "Active account set to %s\n", account.Email)
			fmt.Fprintf(cmd.OutOrStdout(), "OAuth token saved to %s\n", account.Path)
			return nil
		},
	})
	cmd.AddCommand(newAuthScopesCommand())

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List saved Google account logins",
		RunE: func(cmd *cobra.Command, _ []string) error {
			accounts, err := googleaccounts.List(cfg.accountsDir)
			if err != nil {
				return err
			}
			for _, account := range accounts {
				marker := " "
				if account.Active {
					marker = "*"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", marker, account.Email, account.Path)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "activate EMAIL",
		Short: "Make a saved Google account active",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := googleaccounts.Activate(cfg.accountsDir, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Active account set to %s\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "logout [EMAIL]",
		Short: "Remove a saved Google account token",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenFile := cfg.tokenFile
			label := tokenFile
			if len(args) == 1 {
				tokenFile = googleaccounts.TokenPath(cfg.accountsDir, args[0])
				label = args[0]
			}
			if err := os.Remove(tokenFile); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove token file: %w", err)
			}
			if len(args) == 1 {
				activeEmail, err := googleaccounts.ActiveEmail(cfg.accountsDir)
				if err == nil && strings.EqualFold(activeEmail, args[0]) {
					if err := googleaccounts.ClearActive(cfg.accountsDir); err != nil {
						return err
					}
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OAuth token removed for %s\n", label)
			return nil
		},
	})

	return cmd
}
