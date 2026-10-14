package cli

import (
	"github.com/skamensky/google-automation/internal/googlesheets"
	"github.com/spf13/cobra"
)

func newSheetsGridCommand(cfg *appConfig) *cobra.Command {
	var rangeName string
	cmd := &cobra.Command{Use: "grid SPREADSHEET_ID --range RANGE", Short: "Read a bounded typed grid snapshot with formulas", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
		if err != nil {
			return err
		}
		result, err := client.Grid(commandContext(cmd), args[0], rangeName)
		if err != nil {
			return err
		}
		return googlesheets.WriteJSON(cmd.OutOrStdout(), result)
	}}
	cmd.Flags().StringVar(&rangeName, "range", "", "bounded A1 range")
	_ = cmd.MarkFlagRequired("range")
	return cmd
}
