package cli

import (
	"github.com/spf13/cobra"
)

func newGmailExportCommand(cfg *appConfig) *cobra.Command {
	var output string
	var workers int
	var spam bool
	c := &cobra.Command{Use: "export QUERY", Short: "Resume a complete search export of full messages to private local JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, ctx, e := newGmailClient(cmd, cfg)
		if e != nil {
			return e
		}
		return client.ExportMessages(ctx, args[0], output, workers, spam)
	}}
	c.Flags().StringVarP(&output, "output", "o", "", "Private output directory")
	c.MarkFlagRequired("output")
	c.Flags().IntVar(&workers, "workers", 8, "Parallel readers (1..16)")
	c.Flags().BoolVar(&spam, "include-spam-trash", false, "Include spam and trash")
	return c
}

func newGmailExportIDsCommand(cfg *appConfig) *cobra.Command {
	var output string
	var workers int
	c := &cobra.Command{Use: "export-ids INPUT_JSON", Short: "Resume full-message export for IDs found by previous searches", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, ctx, e := newGmailClient(cmd, cfg)
		if e != nil {
			return e
		}
		return client.ExportMessageIDs(ctx, args[0], output, workers)
	}}
	c.Flags().StringVarP(&output, "output", "o", "", "Private output directory")
	c.MarkFlagRequired("output")
	c.Flags().IntVar(&workers, "workers", 8, "Parallel readers (1..16)")
	return c
}
