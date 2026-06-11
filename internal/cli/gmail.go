package cli

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"

	"github.com/skamensky/google-automation/internal/googleaccounts"
	"github.com/skamensky/google-automation/internal/googlegmail"
	"github.com/spf13/cobra"
)

func newGmailCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gmail",
		Short: "Search, inspect, and export Gmail data",
	}

	cmd.AddCommand(newGmailSearchCommand(cfg))
	cmd.AddCommand(newGmailListCommand(cfg))
	cmd.AddCommand(newGmailGetMessageCommand(cfg))
	cmd.AddCommand(newGmailGetThreadCommand(cfg))
	cmd.AddCommand(newGmailAttachmentsCommand(cfg))
	cmd.AddCommand(newGmailAddressesCommand(cfg))
	cmd.AddCommand(newGmailDraftsCommand(cfg))

	return cmd
}

func newGmailSearchCommand(cfg *appConfig) *cobra.Command {
	var limit int64
	var includeSpamTrash bool

	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search Gmail messages using Gmail search syntax",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			items, err := client.ListMessages(ctx, args[0], nil, limit, includeSpamTrash)
			if err != nil {
				return err
			}
			return writeMessageList(cmd, cfg, items)
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 25, "maximum messages to return")
	cmd.Flags().BoolVar(&includeSpamTrash, "include-spam-trash", false, "include spam and trash")
	return cmd
}

func newGmailListCommand(cfg *appConfig) *cobra.Command {
	var labels []string
	var limit int64
	var includeSpamTrash bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Gmail messages",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			items, err := client.ListMessages(ctx, "", labels, limit, includeSpamTrash)
			if err != nil {
				return err
			}
			return writeMessageList(cmd, cfg, items)
		},
	}
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label ID to require, repeatable")
	cmd.Flags().Int64Var(&limit, "limit", 25, "maximum messages to return")
	cmd.Flags().BoolVar(&includeSpamTrash, "include-spam-trash", false, "include spam and trash")
	return cmd
}

func newGmailGetMessageCommand(cfg *appConfig) *cobra.Command {
	var format string
	var output string

	cmd := &cobra.Command{
		Use:   "get-message MESSAGE_ID",
		Short: "Get a Gmail message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			message, err := client.GetMessage(ctx, args[0], format)
			if err != nil {
				return err
			}
			if output != "" {
				return googlegmail.WriteMessageFile(output, message, format)
			}
			return googlegmail.WriteJSON(cmd.OutOrStdout(), message)
		},
	}
	cmd.Flags().StringVar(&format, "format", "metadata", "message format: metadata, full, minimal, raw")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write message to file")
	return cmd
}

func newGmailGetThreadCommand(cfg *appConfig) *cobra.Command {
	var format string
	var output string

	cmd := &cobra.Command{
		Use:   "get-thread THREAD_ID",
		Short: "Get a Gmail thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			thread, err := client.GetThread(ctx, args[0], format)
			if err != nil {
				return err
			}
			if output != "" {
				return googlegmail.WriteThreadFile(output, thread)
			}
			return googlegmail.WriteJSON(cmd.OutOrStdout(), thread)
		},
	}
	cmd.Flags().StringVar(&format, "format", "metadata", "thread format: metadata, full, minimal")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write thread JSON to file")
	return cmd
}

func newGmailAttachmentsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attachments",
		Short: "List and download Gmail attachments",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list MESSAGE_ID",
		Short: "List message attachments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			attachments, err := client.ListAttachments(ctx, args[0])
			if err != nil {
				return err
			}
			if cfg.jsonOutput {
				return googlegmail.WriteJSON(cmd.OutOrStdout(), attachments)
			}
			for _, attachment := range attachments {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%d\n", attachment.AttachmentID, attachment.Filename, attachment.MimeType, attachment.Size)
			}
			return nil
		},
	})

	var outputDir string
	download := &cobra.Command{
		Use:   "download MESSAGE_ID",
		Short: "Download message attachments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			paths, err := client.DownloadAttachments(ctx, args[0], outputDir)
			if err != nil {
				return err
			}
			for _, path := range paths {
				fmt.Fprintln(cmd.OutOrStdout(), path)
			}
			return nil
		},
	}
	download.Flags().StringVarP(&outputDir, "output", "o", ".", "directory for downloaded attachments")
	cmd.AddCommand(download)

	return cmd
}

func newGmailAddressesCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addresses",
		Short: "Export Gmail address interactions",
	}

	var output string
	var refresh bool
	var workers int
	export := &cobra.Command{
		Use:   "export",
		Short: "Export all email addresses seen in cached Gmail metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			accountEmail, err := googleaccounts.ActiveEmail(cfg.accountsDir)
			if err != nil {
				return err
			}
			addresses, err := client.ExportAddresses(ctx, googlegmail.AddressExportOptions{
				AccountEmail: accountEmail,
				ForceRefresh: refresh,
				Workers:      workers,
			})
			if err != nil {
				return err
			}
			if cfg.jsonOutput {
				if output != "" {
					file, err := os.OpenFile(output, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
					if err != nil {
						return fmt.Errorf("open output file: %w", err)
					}
					defer file.Close()
					return googlegmail.WriteJSON(file, addresses)
				}
				return googlegmail.WriteJSON(cmd.OutOrStdout(), addresses)
			}
			return writeAddressesCSV(cmd, output, addresses)
		},
	}
	export.Flags().StringVarP(&output, "output", "o", "", "write CSV or JSON output to file")
	export.Flags().BoolVar(&refresh, "refresh", false, "ignore metadata watermark and rescan mailbox")
	export.Flags().IntVar(&workers, "workers", 4, "parallel Gmail metadata fetch workers")
	cmd.AddCommand(export)
	return cmd
}

func newGmailDraftsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drafts",
		Short: "Create Gmail drafts",
	}

	var to []string
	var cc []string
	var bcc []string
	var subject string
	var bodyFile string
	var threadID string
	var inReplyTo string
	var references string

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a Gmail draft",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if bodyFile == "" {
				return fmt.Errorf("--body-file is required")
			}
			body, err := os.ReadFile(bodyFile)
			if err != nil {
				return fmt.Errorf("read body file: %w", err)
			}
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			draft, err := client.CreateDraft(ctx, googlegmail.DraftOptions{
				To:         to,
				Cc:         cc,
				Bcc:        bcc,
				Subject:    subject,
				Body:       string(body),
				ThreadID:   threadID,
				InReplyTo:  inReplyTo,
				References: references,
			})
			if err != nil {
				return err
			}
			if cfg.jsonOutput {
				return googlegmail.WriteJSON(cmd.OutOrStdout(), draft)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", draft.ID, draft.MessageID, draft.ThreadID)
			return nil
		},
	}
	create.Flags().StringSliceVar(&to, "to", nil, "recipient address, repeatable or comma-separated")
	create.Flags().StringSliceVar(&cc, "cc", nil, "cc address, repeatable or comma-separated")
	create.Flags().StringSliceVar(&bcc, "bcc", nil, "bcc address, repeatable or comma-separated")
	create.Flags().StringVar(&subject, "subject", "", "draft subject")
	create.Flags().StringVar(&bodyFile, "body-file", "", "plain text draft body file")
	create.Flags().StringVar(&threadID, "thread-id", "", "Gmail thread ID for reply drafts")
	create.Flags().StringVar(&inReplyTo, "in-reply-to", "", "Message-ID value for In-Reply-To")
	create.Flags().StringVar(&references, "references", "", "References header value")
	cmd.AddCommand(create)

	return cmd
}

func newGmailClient(cmd *cobra.Command, cfg *appConfig) (*googlegmail.Client, context.Context, error) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := googlegmail.NewClient(ctx, cfg.gmailConfig())
	return client, ctx, err
}

func writeMessageList(cmd *cobra.Command, cfg *appConfig, items []googlegmail.MessageListItem) error {
	if cfg.jsonOutput {
		return googlegmail.WriteJSON(cmd.OutOrStdout(), items)
	}
	for _, item := range items {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", item.ID, item.ThreadID)
	}
	return nil
}

func writeAddressesCSV(cmd *cobra.Command, output string, addresses []googlegmail.AddressInteraction) error {
	w := cmd.OutOrStdout()
	var file *os.File
	if output != "" {
		var err error
		file, err = os.OpenFile(output, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("open output file: %w", err)
		}
		defer file.Close()
		w = file
	}
	writer := csv.NewWriter(w)
	defer writer.Flush()
	if err := writer.Write([]string{"email", "name", "first_seen_unix_ms", "last_seen_unix_ms", "message_count", "headers"}); err != nil {
		return err
	}
	for _, address := range addresses {
		if err := writer.Write([]string{
			address.Email,
			address.Name,
			fmt.Sprintf("%d", address.FirstSeen),
			fmt.Sprintf("%d", address.LastSeen),
			fmt.Sprintf("%d", address.MessageCount),
			address.Headers,
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}
