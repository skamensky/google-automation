package cli

import (
	"fmt"
	"os"

	"github.com/skamensky/google-automation/internal/googleslides"
	"github.com/spf13/cobra"
)

func newSlidesCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{Use: "slides", Short: "Manage Google Slides"}
	cmd.AddCommand(newSlidesCreateCommand(cfg))
	cmd.AddCommand(newSlidesGetCommand(cfg))
	cmd.AddCommand(newSlidesExportCommand(cfg))
	cmd.AddCommand(newSlidesBatchUpdateCommand(cfg))
	return cmd
}

func newSlidesCreateCommand(cfg *appConfig) *cobra.Command {
	var title string
	cmd := &cobra.Command{
		Use:   "create --title TITLE",
		Short: "Create a Google Slides presentation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googleslides.NewClient(commandContext(cmd), cfg.slidesConfig())
			if err != nil {
				return err
			}
			presentation, err := client.Create(commandContext(cmd), title)
			if err != nil {
				return err
			}
			return googleslides.WriteJSON(cmd.OutOrStdout(), presentation)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "presentation title")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newSlidesGetCommand(cfg *appConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "get PRESENTATION_ID",
		Short: "Get a Google Slides presentation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googleslides.NewClient(commandContext(cmd), cfg.slidesConfig())
			if err != nil {
				return err
			}
			presentation, err := client.Get(commandContext(cmd), args[0])
			if err != nil {
				return err
			}
			return googleslides.WriteJSON(cmd.OutOrStdout(), presentation)
		},
	}
}

func newSlidesExportCommand(cfg *appConfig) *cobra.Command {
	var format string
	var output string
	cmd := &cobra.Command{
		Use:   "export PRESENTATION_ID --format pptx|pdf --output PATH",
		Short: "Export a Google Slides presentation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googleslides.NewClient(commandContext(cmd), cfg.slidesConfig())
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
	cmd.Flags().StringVar(&format, "format", "pptx", "export format: pptx,pdf")
	cmd.Flags().StringVar(&output, "output", "", "output file path")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newSlidesBatchUpdateCommand(cfg *appConfig) *cobra.Command {
	var requestFile string
	cmd := &cobra.Command{
		Use:   "batch-update PRESENTATION_ID --request-file request.json",
		Short: "Run a raw Google Slides batchUpdate request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := os.ReadFile(requestFile)
			if err != nil {
				return fmt.Errorf("read batch update JSON: %w", err)
			}
			req, err := googleslides.ParseBatchUpdate(payload)
			if err != nil {
				return err
			}
			client, err := googleslides.NewClient(commandContext(cmd), cfg.slidesConfig())
			if err != nil {
				return err
			}
			resp, err := client.BatchUpdate(commandContext(cmd), args[0], req)
			if err != nil {
				return err
			}
			return googleslides.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&requestFile, "request-file", "", "batchUpdate request JSON file")
	_ = cmd.MarkFlagRequired("request-file")
	return cmd
}
