package cli

import (
	"fmt"
	"os"

	"github.com/skamensky/google-automation/internal/googledocs"
	"github.com/spf13/cobra"
)

func newDocsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Manage Google Docs",
	}
	cmd.AddCommand(newDocsCreateCommand(cfg))
	cmd.AddCommand(newDocsUploadCommand(cfg))
	cmd.AddCommand(newDocsGetCommand(cfg))
	cmd.AddCommand(newDocsExportCommand(cfg))
	cmd.AddCommand(newDocsAppendCommand(cfg))
	cmd.AddCommand(newDocsReplaceCommand(cfg))
	cmd.AddCommand(newDocsBatchUpdateCommand(cfg))
	return cmd
}

func newDocsCreateCommand(cfg *appConfig) *cobra.Command {
	var title string
	var text string
	var parent string
	cmd := &cobra.Command{
		Use:   "create --title TITLE",
		Short: "Create a Google Doc",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := googledocs.NewClient(commandContext(cmd), cfg.docsConfig())
			if err != nil {
				return err
			}
			doc, err := client.CreateWithOptions(commandContext(cmd), title, text, parent)
			if err != nil {
				return err
			}
			return googledocs.WriteJSON(cmd.OutOrStdout(), doc)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "document title")
	cmd.Flags().StringVar(&text, "text", "", "initial document text")
	cmd.Flags().StringVar(&parent, "parent", "root", "parent folder ID")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newDocsUploadCommand(cfg *appConfig) *cobra.Command {
	var title string
	var parent string
	cmd := &cobra.Command{
		Use:   "upload PATH",
		Short: "Upload and convert a local file to a Google Doc",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledocs.NewClient(commandContext(cmd), cfg.docsConfig())
			if err != nil {
				return err
			}
			file, err := client.Upload(commandContext(cmd), args[0], title, parent)
			if err != nil {
				return err
			}
			return googledocs.WriteJSON(cmd.OutOrStdout(), file)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "document title")
	cmd.Flags().StringVar(&parent, "parent", "root", "parent folder ID")
	return cmd
}

func newDocsGetCommand(cfg *appConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "get DOC_ID",
		Short: "Get a Google Doc",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledocs.NewClient(commandContext(cmd), cfg.docsConfig())
			if err != nil {
				return err
			}
			doc, err := client.Get(commandContext(cmd), args[0])
			if err != nil {
				return err
			}
			return googledocs.WriteJSON(cmd.OutOrStdout(), doc)
		},
	}
}

func newDocsExportCommand(cfg *appConfig) *cobra.Command {
	var format string
	var output string
	cmd := &cobra.Command{
		Use:   "export DOC_ID --format pdf|docx|txt|html --output PATH",
		Short: "Export a Google Doc",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledocs.NewClient(commandContext(cmd), cfg.docsConfig())
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
	cmd.Flags().StringVar(&format, "format", "pdf", "export format: pdf,docx,txt,html")
	cmd.Flags().StringVar(&output, "output", "", "output file path")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newDocsAppendCommand(cfg *appConfig) *cobra.Command {
	var text string
	cmd := &cobra.Command{
		Use:   "append DOC_ID --text TEXT",
		Short: "Append text to a Google Doc",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledocs.NewClient(commandContext(cmd), cfg.docsConfig())
			if err != nil {
				return err
			}
			resp, err := client.Append(commandContext(cmd), args[0], text)
			if err != nil {
				return err
			}
			return googledocs.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "text to append")
	_ = cmd.MarkFlagRequired("text")
	return cmd
}

func newDocsReplaceCommand(cfg *appConfig) *cobra.Command {
	var find string
	var replace string
	cmd := &cobra.Command{
		Use:   "replace DOC_ID --find OLD --replace NEW",
		Short: "Find and replace text in a Google Doc",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := googledocs.NewClient(commandContext(cmd), cfg.docsConfig())
			if err != nil {
				return err
			}
			resp, err := client.Replace(commandContext(cmd), args[0], find, replace)
			if err != nil {
				return err
			}
			return googledocs.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&find, "find", "", "text to find")
	cmd.Flags().StringVar(&replace, "replace", "", "replacement text")
	_ = cmd.MarkFlagRequired("find")
	_ = cmd.MarkFlagRequired("replace")
	return cmd
}

func newDocsBatchUpdateCommand(cfg *appConfig) *cobra.Command {
	var requestFile string
	cmd := &cobra.Command{
		Use:   "batch-update DOC_ID --request-file request.json",
		Short: "Run a raw Google Docs batchUpdate request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := os.ReadFile(requestFile)
			if err != nil {
				return fmt.Errorf("read batch update JSON: %w", err)
			}
			client, err := googledocs.NewClient(commandContext(cmd), cfg.docsConfig())
			if err != nil {
				return err
			}
			resp, err := client.BatchUpdate(commandContext(cmd), args[0], payload)
			if err != nil {
				return err
			}
			return googledocs.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&requestFile, "request-file", "", "batchUpdate request JSON file")
	_ = cmd.MarkFlagRequired("request-file")
	return cmd
}
