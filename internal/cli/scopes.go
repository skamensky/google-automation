package cli

import (
	"fmt"

	"github.com/skamensky/google-automation/internal/googleaccounts"
	"github.com/spf13/cobra"
)

func newAuthScopesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "scopes",
		Short: "Print OAuth scopes requested by login",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, scope := range googleaccounts.Scopes() {
				fmt.Fprintln(cmd.OutOrStdout(), scope)
			}
			return nil
		},
	}
}
