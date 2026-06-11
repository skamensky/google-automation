package cli

import (
	"fmt"
	"os"

	"github.com/skamensky/google-automation/internal/googlesheets"
	"github.com/spf13/cobra"
)

func newSheetsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "sheets", Short: "Manage Google Sheets"}
	cmd.AddCommand(newSheetsCreateCommand(cfg))
	cmd.AddCommand(newSheetsGetCommand(cfg))
	cmd.AddCommand(newSheetsExportCommand(cfg))
	cmd.AddCommand(newSheetsSearchCommand(cfg))
	cmd.AddCommand(newSheetsBatchUpdateCommand(cfg))
	cmd.AddCommand(newSheetsValuesCommand(cfg))
	cmd.AddCommand(newSheetsTabsCommand(cfg))
	return cmd
}

func newSheetsCreateCommand(cfg *appConfig) *cobra.Command {
	var title string
	cmd := &cobra.Command{
		Use:   "create --title TITLE",
		Short: "Create a Google Sheet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			spreadsheet, err := client.Create(commandContext(cmd), title)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), spreadsheet)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "spreadsheet title")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newSheetsGetCommand(cfg *appConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "get SPREADSHEET_ID",
		Short: "Get a Google Sheet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			spreadsheet, err := client.Get(commandContext(cmd), args[0])
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), spreadsheet)
		},
	}
}

func newSheetsExportCommand(cfg *appConfig) *cobra.Command {
	var format string
	var output string
	cmd := &cobra.Command{
		Use:   "export SPREADSHEET_ID --format csv|xlsx --output PATH",
		Short: "Export a Google Sheet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			if err := client.Export(commandContext(cmd), args[0], format, output); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), output)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "xlsx", "export format: xlsx,csv,pdf")
	cmd.Flags().StringVar(&output, "output", "", "output file path")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newSheetsSearchCommand(cfg *appConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "search SPREADSHEET_ID QUERY",
		Short: "Search values in a Google Sheet",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			results, err := client.Search(commandContext(cmd), args[0], args[1])
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), results)
		},
	}
}

func newSheetsBatchUpdateCommand(cfg *appConfig) *cobra.Command {
	var requestFile string
	cmd := &cobra.Command{
		Use:   "batch-update SPREADSHEET_ID --request-file request.json",
		Short: "Run a raw Google Sheets batchUpdate request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := os.ReadFile(requestFile)
			if err != nil {
				return fmt.Errorf("read batch update JSON: %w", err)
			}
			req, err := googlesheets.ParseBatchUpdate(payload)
			if err != nil {
				return err
			}
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			resp, err := client.BatchUpdate(commandContext(cmd), args[0], req)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&requestFile, "request-file", "", "batchUpdate request JSON file")
	_ = cmd.MarkFlagRequired("request-file")
	return cmd
}

func newSheetsValuesCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "values", Short: "Manage Google Sheets values"}
	cmd.AddCommand(newSheetsValuesGetCommand(cfg))
	cmd.AddCommand(newSheetsValuesUpdateCommand(cfg))
	cmd.AddCommand(newSheetsValuesAppendCommand(cfg))
	cmd.AddCommand(newSheetsValuesClearCommand(cfg))
	return cmd
}

func newSheetsValuesGetCommand(cfg *appConfig) *cobra.Command {
	var rangeName string
	cmd := &cobra.Command{
		Use:   "get SPREADSHEET_ID --range RANGE",
		Short: "Get spreadsheet values",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			resp, err := client.ValuesGet(commandContext(cmd), args[0], rangeName)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&rangeName, "range", "", "A1 notation range")
	_ = cmd.MarkFlagRequired("range")
	return cmd
}

func newSheetsValuesUpdateCommand(cfg *appConfig) *cobra.Command {
	var rangeName string
	var valuesFile string
	cmd := &cobra.Command{
		Use:   "update SPREADSHEET_ID --range RANGE --values-file values.json",
		Short: "Update spreadsheet values",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			values, err := readSheetValues(valuesFile)
			if err != nil {
				return err
			}
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			resp, err := client.ValuesUpdate(commandContext(cmd), args[0], rangeName, values)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	addSheetsValuesWriteFlags(cmd, &rangeName, &valuesFile)
	return cmd
}

func newSheetsValuesAppendCommand(cfg *appConfig) *cobra.Command {
	var rangeName string
	var valuesFile string
	cmd := &cobra.Command{
		Use:   "append SPREADSHEET_ID --range RANGE --values-file values.json",
		Short: "Append spreadsheet values",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			values, err := readSheetValues(valuesFile)
			if err != nil {
				return err
			}
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			resp, err := client.ValuesAppend(commandContext(cmd), args[0], rangeName, values)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	addSheetsValuesWriteFlags(cmd, &rangeName, &valuesFile)
	return cmd
}

func newSheetsValuesClearCommand(cfg *appConfig) *cobra.Command {
	var rangeName string
	cmd := &cobra.Command{
		Use:   "clear SPREADSHEET_ID --range RANGE",
		Short: "Clear spreadsheet values",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			resp, err := client.ValuesClear(commandContext(cmd), args[0], rangeName)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&rangeName, "range", "", "A1 notation range")
	_ = cmd.MarkFlagRequired("range")
	return cmd
}

func newSheetsTabsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "tabs", Short: "Manage spreadsheet tabs"}
	cmd.AddCommand(newSheetsTabsListCommand(cfg))
	cmd.AddCommand(newSheetsTabsAddCommand(cfg))
	cmd.AddCommand(newSheetsTabsRenameCommand(cfg))
	cmd.AddCommand(newSheetsTabsDeleteCommand(cfg))
	return cmd
}

func newSheetsTabsListCommand(cfg *appConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "list SPREADSHEET_ID",
		Short: "List spreadsheet tabs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			tabs, err := client.TabsList(commandContext(cmd), args[0])
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), tabs)
		},
	}
}

func newSheetsTabsAddCommand(cfg *appConfig) *cobra.Command {
	var title string
	cmd := &cobra.Command{
		Use:   "add SPREADSHEET_ID --title TITLE",
		Short: "Add a spreadsheet tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			resp, err := client.TabsAdd(commandContext(cmd), args[0], title)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "tab title")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newSheetsTabsRenameCommand(cfg *appConfig) *cobra.Command {
	var sheetID int64
	var title string
	cmd := &cobra.Command{
		Use:   "rename SPREADSHEET_ID --sheet-id SHEET_ID --title TITLE",
		Short: "Rename a spreadsheet tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			resp, err := client.TabsRename(commandContext(cmd), args[0], sheetID, title)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().Int64Var(&sheetID, "sheet-id", 0, "sheet/tab ID")
	cmd.Flags().StringVar(&title, "title", "", "new tab title")
	_ = cmd.MarkFlagRequired("sheet-id")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newSheetsTabsDeleteCommand(cfg *appConfig) *cobra.Command {
	var sheetID int64
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete SPREADSHEET_ID --sheet-id SHEET_ID --yes",
		Short: "Delete a spreadsheet tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to delete without --yes")
			}
			client, err := googlesheets.NewClient(commandContext(cmd), cfg.sheetsConfig())
			if err != nil {
				return err
			}
			resp, err := client.TabsDelete(commandContext(cmd), args[0], sheetID)
			if err != nil {
				return err
			}
			return googlesheets.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().Int64Var(&sheetID, "sheet-id", 0, "sheet/tab ID")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm tab deletion")
	_ = cmd.MarkFlagRequired("sheet-id")
	return cmd
}

func addSheetsValuesWriteFlags(cmd *cobra.Command, rangeName, valuesFile *string) {
	cmd.Flags().StringVar(rangeName, "range", "", "A1 notation range")
	cmd.Flags().StringVar(valuesFile, "values-file", "", "values JSON file")
	_ = cmd.MarkFlagRequired("range")
	_ = cmd.MarkFlagRequired("values-file")
}

func readSheetValues(path string) ([][]interface{}, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read values JSON: %w", err)
	}
	return googlesheets.ParseValues(payload)
}
