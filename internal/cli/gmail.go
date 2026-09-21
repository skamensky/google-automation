package cli

import (
	"context"
	"encoding/csv"
	"fmt"
	"mime"
	"os"
	"path/filepath"

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
	cmd.AddCommand(newGmailSendCommand(cfg))
	cmd.AddCommand(newGmailDraftsCommand(cfg))

	return cmd
}

func newGmailSendCommand(cfg *appConfig) *cobra.Command {
	opts := draftFlags{}
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a Gmail message immediately",
		RunE: func(cmd *cobra.Command, _ []string) error {
			messageOpts, err := opts.toDraftOptions()
			if err != nil {
				return err
			}
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			sent, err := client.SendMessage(ctx, messageOpts)
			if err != nil {
				return err
			}
			if cfg.jsonOutput {
				return googlegmail.WriteJSON(cmd.OutOrStdout(), sent)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", sent.ID, sent.ThreadID)
			return nil
		},
	}
	opts.addFlags(cmd)
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
		Short: "Manage Gmail drafts",
	}

	cmd.AddCommand(newGmailDraftsListCommand(cfg))
	cmd.AddCommand(newGmailDraftsGetCommand(cfg))
	cmd.AddCommand(newGmailDraftsDeleteCommand(cfg))
	cmd.AddCommand(newGmailDraftsCreateCommand(cfg))
	cmd.AddCommand(newGmailDraftsUpdateCommand(cfg))
	cmd.AddCommand(newGmailDraftsReplyCommand(cfg))

	return cmd
}

func newGmailDraftsListCommand(cfg *appConfig) *cobra.Command {
	var limit int64
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Gmail drafts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			drafts, err := client.ListDrafts(ctx, limit)
			if err != nil {
				return err
			}
			if cfg.jsonOutput {
				return googlegmail.WriteJSON(cmd.OutOrStdout(), drafts)
			}
			for _, draft := range drafts {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", draft.ID, draft.MessageID, draft.ThreadID, draft.Subject)
			}
			return nil
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 25, "maximum drafts to return")
	return cmd
}

func newGmailDraftsGetCommand(cfg *appConfig) *cobra.Command {
	var format string
	var output string
	cmd := &cobra.Command{
		Use:   "get DRAFT_ID",
		Short: "Get a Gmail draft",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			draft, err := client.GetDraft(ctx, args[0], format)
			if err != nil {
				return err
			}
			if output != "" {
				return googlegmail.WriteJSONFile(output, draft)
			}
			return googlegmail.WriteJSON(cmd.OutOrStdout(), draft)
		},
	}
	cmd.Flags().StringVar(&format, "format", "metadata", "draft format: metadata, full, minimal, raw")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write draft JSON to file")
	return cmd
}

func newGmailDraftsDeleteCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete DRAFT_ID",
		Short: "Delete a Gmail draft",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			if err := client.DeleteDraft(ctx, args[0]); err != nil {
				return err
			}
			if cfg.jsonOutput {
				return googlegmail.WriteJSON(cmd.OutOrStdout(), map[string]string{"deleted": args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted\t%s\n", args[0])
			return nil
		},
	}
	return cmd
}

func newGmailDraftsCreateCommand(cfg *appConfig) *cobra.Command {
	opts := draftFlags{}
	var replaceThreadDraft bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Gmail draft",
		RunE: func(cmd *cobra.Command, _ []string) error {
			draftOpts, err := opts.toDraftOptions()
			if err != nil {
				return err
			}
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			var draft *googlegmail.DraftInfo
			if replaceThreadDraft {
				draft, err = client.CreateOrReplaceThreadDraft(ctx, draftOpts)
			} else {
				draft, err = client.CreateDraft(ctx, draftOpts)
			}
			if err != nil {
				return err
			}
			return writeDraftInfo(cmd, cfg, draft)
		},
	}
	opts.addFlags(cmd)
	cmd.Flags().BoolVar(&replaceThreadDraft, "replace-thread-draft", false, "update the first existing draft in --thread-id and delete duplicate drafts")
	return cmd
}

func newGmailDraftsUpdateCommand(cfg *appConfig) *cobra.Command {
	opts := draftFlags{}
	cmd := &cobra.Command{
		Use:   "update DRAFT_ID",
		Short: "Update a Gmail draft",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			draftOpts, err := opts.toDraftOptions()
			if err != nil {
				return err
			}
			client, ctx, err := newGmailClient(cmd, cfg)
			if err != nil {
				return err
			}
			draft, err := client.UpdateDraft(ctx, args[0], draftOpts)
			if err != nil {
				return err
			}
			return writeDraftInfo(cmd, cfg, draft)
		},
	}
	opts.addFlags(cmd)
	return cmd
}

func newGmailDraftsReplyCommand(cfg *appConfig) *cobra.Command {
	var bodyFile string
	var replyAll bool
	var replaceExisting bool
	var to []string
	var cc []string
	var bcc []string
	cmd := &cobra.Command{
		Use:   "reply THREAD_ID",
		Short: "Create a reply draft for the latest non-draft message in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			draft, err := client.CreateReplyDraft(ctx, googlegmail.ReplyDraftOptions{
				ThreadID:        args[0],
				Body:            string(body),
				ReplyAll:        replyAll,
				ReplaceExisting: replaceExisting,
				To:              to,
				Cc:              cc,
				Bcc:             bcc,
			})
			if err != nil {
				return err
			}
			return writeDraftInfo(cmd, cfg, draft)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "plain text draft body file")
	cmd.Flags().BoolVar(&replyAll, "reply-all", false, "include original To/Cc recipients, excluding the active account and primary reply recipients")
	cmd.Flags().BoolVar(&replaceExisting, "replace-existing", false, "update the first existing draft in this thread and delete duplicate drafts")
	cmd.Flags().StringSliceVar(&to, "to", nil, "override recipient address, repeatable or comma-separated")
	cmd.Flags().StringSliceVar(&cc, "cc", nil, "override cc address, repeatable or comma-separated")
	cmd.Flags().StringSliceVar(&bcc, "bcc", nil, "bcc address, repeatable or comma-separated")
	return cmd
}

type draftFlags struct {
	to          []string
	cc          []string
	bcc         []string
	subject     string
	bodyFile    string
	html        bool
	threadID    string
	inReplyTo   string
	references  string
	attachments []string
}

func (f *draftFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&f.to, "to", nil, "recipient address, repeatable or comma-separated")
	cmd.Flags().StringSliceVar(&f.cc, "cc", nil, "cc address, repeatable or comma-separated")
	cmd.Flags().StringSliceVar(&f.bcc, "bcc", nil, "bcc address, repeatable or comma-separated")
	cmd.Flags().StringVar(&f.subject, "subject", "", "draft subject")
	cmd.Flags().StringVar(&f.bodyFile, "body-file", "", "message body file")
	cmd.Flags().BoolVar(&f.html, "html", false, "treat --body-file as HTML")
	cmd.Flags().StringVar(&f.threadID, "thread-id", "", "Gmail thread ID for reply drafts")
	cmd.Flags().StringVar(&f.inReplyTo, "in-reply-to", "", "Message-ID value for In-Reply-To")
	cmd.Flags().StringVar(&f.references, "references", "", "References header value")
	cmd.Flags().StringSliceVar(&f.attachments, "attachment", nil, "file to attach, repeatable or comma-separated")
}

func (f draftFlags) toDraftOptions() (googlegmail.DraftOptions, error) {
	if f.bodyFile == "" {
		return googlegmail.DraftOptions{}, fmt.Errorf("--body-file is required")
	}
	body, err := os.ReadFile(f.bodyFile)
	if err != nil {
		return googlegmail.DraftOptions{}, fmt.Errorf("read body file: %w", err)
	}
	attachments := make([]googlegmail.Attachment, 0, len(f.attachments))
	for _, path := range f.attachments {
		data, err := os.ReadFile(path)
		if err != nil {
			return googlegmail.DraftOptions{}, fmt.Errorf("read attachment %q: %w", path, err)
		}
		mimeType := mime.TypeByExtension(filepath.Ext(path))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		attachments = append(attachments, googlegmail.Attachment{
			Filename: filepath.Base(path),
			MIMEType: mimeType,
			Data:     data,
		})
	}
	return googlegmail.DraftOptions{
		To:          f.to,
		Cc:          f.cc,
		Bcc:         f.bcc,
		Subject:     f.subject,
		Body:        string(body),
		HTML:        f.html,
		ThreadID:    f.threadID,
		InReplyTo:   f.inReplyTo,
		References:  f.references,
		Attachments: attachments,
	}, nil
}

func writeDraftInfo(cmd *cobra.Command, cfg *appConfig, draft *googlegmail.DraftInfo) error {
	if cfg.jsonOutput {
		return googlegmail.WriteJSON(cmd.OutOrStdout(), draft)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", draft.ID, draft.MessageID, draft.ThreadID, draft.Subject)
	return nil
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
