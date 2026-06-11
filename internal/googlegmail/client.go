package googlegmail

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/skamensky/google-automation/internal/cache"
	"github.com/skamensky/google-automation/internal/googleauth"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

type Config struct {
	CredentialsFile string
	TokenFile       string
	NoBrowser       bool
	Cache           cache.Config
}

type Client struct {
	service *gmail.Service
	cache   cache.Store
}

var sharedGmailThrottle apiThrottle

type apiThrottle struct {
	mu          sync.Mutex
	pausedUntil time.Time
}

type MessageListItem struct {
	ID       string `json:"id"`
	ThreadID string `json:"thread_id"`
}

type AttachmentInfo struct {
	MessageID    string `json:"message_id"`
	PartID       string `json:"part_id"`
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename"`
	MimeType     string `json:"mime_type"`
	Size         int64  `json:"size"`
}

type AddressInteraction struct {
	Email        string `json:"email"`
	Name         string `json:"name,omitempty"`
	FirstSeen    int64  `json:"first_seen_unix_ms"`
	LastSeen     int64  `json:"last_seen_unix_ms"`
	MessageCount int64  `json:"message_count"`
	Headers      string `json:"headers"`
}

type AddressExportOptions struct {
	AccountEmail string
	ForceRefresh bool
	Workers      int
}

type DraftOptions struct {
	To         []string
	Cc         []string
	Bcc        []string
	Subject    string
	Body       string
	ThreadID   string
	InReplyTo  string
	References string
}

type DraftInfo struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
	To        string `json:"to,omitempty"`
	Cc        string `json:"cc,omitempty"`
}

type ReplyDraftOptions struct {
	ThreadID        string
	Body            string
	ReplyAll        bool
	ReplaceExisting bool
	To              []string
	Cc              []string
	Bcc             []string
}

type ingestState struct {
	FullScanComplete     bool
	NextPageToken        string
	LastIncrementalAfter string
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	httpClient, err := googleauth.NewClient(ctx, googleauth.Config{
		CredentialsFile: cfg.CredentialsFile,
		TokenFile:       cfg.TokenFile,
		Scopes:          Scopes(),
		NoBrowser:       cfg.NoBrowser,
	})
	if err != nil {
		return nil, err
	}
	service, err := gmail.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("create gmail service: %w", err)
	}
	return &Client{service: service, cache: cache.NewStore(cfg.Cache)}, nil
}

func Scopes() []string {
	return []string{gmail.MailGoogleComScope}
}

func (c *Client) ListMessages(ctx context.Context, query string, labels []string, limit int64, includeSpamTrash bool) ([]MessageListItem, error) {
	if limit <= 0 {
		limit = 25
	}
	call := c.service.Users.Messages.List("me").MaxResults(limit).IncludeSpamTrash(includeSpamTrash).Context(ctx)
	if query != "" {
		call.Q(query)
	}
	for _, label := range labels {
		if strings.TrimSpace(label) != "" {
			call.LabelIds(strings.TrimSpace(label))
		}
	}
	resp, err := doWithRetry(ctx, func() (*gmail.ListMessagesResponse, error) {
		return call.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("list gmail messages: %w", err)
	}
	items := make([]MessageListItem, 0, len(resp.Messages))
	for _, message := range resp.Messages {
		items = append(items, MessageListItem{ID: message.Id, ThreadID: message.ThreadId})
	}
	return items, nil
}

func (c *Client) GetMessage(ctx context.Context, id, format string) (*gmail.Message, error) {
	if format == "" {
		format = "metadata"
	}
	call := c.service.Users.Messages.Get("me", id).Format(format).Context(ctx)
	if format == "metadata" {
		for _, header := range metadataHeaders() {
			call.MetadataHeaders(header)
		}
	}
	message, err := doWithRetry(ctx, func() (*gmail.Message, error) {
		return call.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("get gmail message %q: %w", id, err)
	}
	return message, nil
}

func (c *Client) GetThread(ctx context.Context, id, format string) (*gmail.Thread, error) {
	if format == "" {
		format = "metadata"
	}
	call := c.service.Users.Threads.Get("me", id).Format(format).Context(ctx)
	thread, err := doWithRetry(ctx, func() (*gmail.Thread, error) {
		return call.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("get gmail thread %q: %w", id, err)
	}
	return thread, nil
}

func (c *Client) ListAttachments(ctx context.Context, messageID string) ([]AttachmentInfo, error) {
	message, err := c.GetMessage(ctx, messageID, "full")
	if err != nil {
		return nil, err
	}
	var attachments []AttachmentInfo
	walkParts(messageID, message.Payload, &attachments)
	return attachments, nil
}

func (c *Client) DownloadAttachments(ctx context.Context, messageID, outputDir string) ([]string, error) {
	if outputDir == "" {
		outputDir = "."
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return nil, fmt.Errorf("create attachment output directory: %w", err)
	}
	attachments, err := c.ListAttachments(ctx, messageID)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, attachment := range attachments {
		call := c.service.Users.Messages.Attachments.Get("me", messageID, attachment.AttachmentID).Context(ctx)
		resp, err := doWithRetry(ctx, func() (*gmail.MessagePartBody, error) {
			return call.Do()
		})
		if err != nil {
			return nil, fmt.Errorf("download attachment %q: %w", attachment.AttachmentID, err)
		}
		data, err := base64.RawURLEncoding.DecodeString(resp.Data)
		if err != nil {
			return nil, fmt.Errorf("decode attachment %q: %w", attachment.AttachmentID, err)
		}
		name := safeFilename(attachment.Filename)
		if name == "" {
			name = attachment.AttachmentID
		}
		path := filepath.Join(outputDir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, fmt.Errorf("write attachment %q: %w", path, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (c *Client) CreateDraft(ctx context.Context, opts DraftOptions) (*DraftInfo, error) {
	raw, err := buildRawMessage(opts)
	if err != nil {
		return nil, err
	}
	message := &gmail.Message{Raw: raw}
	if strings.TrimSpace(opts.ThreadID) != "" {
		message.ThreadId = strings.TrimSpace(opts.ThreadID)
	}
	draft, err := doWithRetry(ctx, func() (*gmail.Draft, error) {
		return c.service.Users.Drafts.Create("me", &gmail.Draft{Message: message}).Context(ctx).Do()
	})
	if err != nil {
		return nil, fmt.Errorf("create gmail draft: %w", err)
	}
	info := &DraftInfo{ID: draft.Id}
	if draft.Message != nil {
		info.MessageID = draft.Message.Id
		info.ThreadID = draft.Message.ThreadId
	}
	return info, nil
}

func (c *Client) CreateOrReplaceThreadDraft(ctx context.Context, opts DraftOptions) (*DraftInfo, error) {
	threadID := strings.TrimSpace(opts.ThreadID)
	if threadID == "" {
		return nil, errors.New("thread id is required when replacing a thread draft")
	}
	drafts, err := c.draftsForThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if len(drafts) == 0 {
		return c.CreateDraft(ctx, opts)
	}
	for _, draft := range drafts[1:] {
		if err := c.DeleteDraft(ctx, draft.ID); err != nil {
			return nil, err
		}
	}
	return c.UpdateDraft(ctx, drafts[0].ID, opts)
}

func (c *Client) UpdateDraft(ctx context.Context, id string, opts DraftOptions) (*DraftInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("draft id is required")
	}
	raw, err := buildRawMessage(opts)
	if err != nil {
		return nil, err
	}
	message := &gmail.Message{Raw: raw}
	if strings.TrimSpace(opts.ThreadID) != "" {
		message.ThreadId = strings.TrimSpace(opts.ThreadID)
	}
	draft, err := doWithRetry(ctx, func() (*gmail.Draft, error) {
		return c.service.Users.Drafts.Update("me", id, &gmail.Draft{Message: message}).Context(ctx).Do()
	})
	if err != nil {
		return nil, fmt.Errorf("update gmail draft %q: %w", id, err)
	}
	return draftInfo(draft), nil
}

func (c *Client) DeleteDraft(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("draft id is required")
	}
	_, err := doWithRetry(ctx, func() (*emptyResponse, error) {
		err := c.service.Users.Drafts.Delete("me", id).Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		return &emptyResponse{}, nil
	})
	if err != nil {
		return fmt.Errorf("delete gmail draft %q: %w", id, err)
	}
	return nil
}

func (c *Client) GetDraft(ctx context.Context, id, format string) (*gmail.Draft, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("draft id is required")
	}
	if format == "" {
		format = "metadata"
	}
	call := c.service.Users.Drafts.Get("me", id).Format(format).Context(ctx)
	draft, err := doWithRetry(ctx, func() (*gmail.Draft, error) {
		return call.Do()
	})
	if err != nil {
		return nil, fmt.Errorf("get gmail draft %q: %w", id, err)
	}
	return draft, nil
}

func (c *Client) ListDrafts(ctx context.Context, maxResults int64) ([]DraftInfo, error) {
	if maxResults <= 0 {
		maxResults = 25
	}
	var out []DraftInfo
	call := c.service.Users.Drafts.List("me").MaxResults(maxResults).Context(ctx)
	for {
		resp, err := doWithRetry(ctx, func() (*gmail.ListDraftsResponse, error) {
			return call.Do()
		})
		if err != nil {
			return nil, fmt.Errorf("list gmail drafts: %w", err)
		}
		for _, draft := range resp.Drafts {
			info := *draftInfo(draft)
			if draft.Message != nil && info.Subject == "" && draft.Message.Id != "" {
				full, err := c.GetDraft(ctx, draft.Id, "metadata")
				if err == nil {
					info = *draftInfo(full)
				}
			}
			out = append(out, info)
			if int64(len(out)) >= maxResults {
				return out, nil
			}
		}
		if resp.NextPageToken == "" {
			return out, nil
		}
		call.PageToken(resp.NextPageToken)
	}
}

func (c *Client) CreateReplyDraft(ctx context.Context, opts ReplyDraftOptions) (*DraftInfo, error) {
	threadID := strings.TrimSpace(opts.ThreadID)
	if threadID == "" {
		return nil, errors.New("thread id is required")
	}
	thread, err := c.GetThread(ctx, threadID, "metadata")
	if err != nil {
		return nil, err
	}
	replyToMessage := latestNonDraftMessage(thread)
	if replyToMessage == nil {
		return nil, fmt.Errorf("thread %q has no non-draft messages to reply to", threadID)
	}
	draftOpts, err := c.replyDraftOptions(ctx, replyToMessage, opts)
	if err != nil {
		return nil, err
	}
	if opts.ReplaceExisting {
		drafts, err := c.draftsForThread(ctx, threadID)
		if err != nil {
			return nil, err
		}
		if len(drafts) > 0 {
			for _, draft := range drafts[1:] {
				if err := c.DeleteDraft(ctx, draft.ID); err != nil {
					return nil, err
				}
			}
			return c.UpdateDraft(ctx, drafts[0].ID, draftOpts)
		}
	}
	return c.CreateDraft(ctx, draftOpts)
}

func (c *Client) draftsForThread(ctx context.Context, threadID string) ([]DraftInfo, error) {
	var out []DraftInfo
	call := c.service.Users.Drafts.List("me").MaxResults(100).Context(ctx)
	for {
		resp, err := doWithRetry(ctx, func() (*gmail.ListDraftsResponse, error) {
			return call.Do()
		})
		if err != nil {
			return nil, fmt.Errorf("list gmail drafts: %w", err)
		}
		for _, draft := range resp.Drafts {
			if draft.Message != nil && draft.Message.ThreadId == threadID {
				out = append(out, *draftInfo(draft))
			}
		}
		if resp.NextPageToken == "" {
			return out, nil
		}
		call.PageToken(resp.NextPageToken)
	}
}

func (c *Client) replyDraftOptions(ctx context.Context, message *gmail.Message, opts ReplyDraftOptions) (DraftOptions, error) {
	headers := headerMap(message)
	profile, err := c.service.Users.GetProfile("me").Context(ctx).Do()
	if err != nil {
		return DraftOptions{}, fmt.Errorf("get gmail profile: %w", err)
	}
	self := strings.ToLower(strings.TrimSpace(profile.EmailAddress))

	to := opts.To
	cc := opts.Cc
	if len(to) == 0 {
		to = addressesForReply(headers)
	}
	if opts.ReplyAll && len(opts.Cc) == 0 {
		cc = replyAllCc(headers, self, to)
	}

	subject := headers["Subject"]
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		subject = "Re: " + subject
	}
	references := strings.TrimSpace(headers["References"])
	messageID := strings.TrimSpace(headers["Message-ID"])
	if messageID != "" && !strings.Contains(references, messageID) {
		if references != "" {
			references += " "
		}
		references += messageID
	}
	return DraftOptions{
		To:         to,
		Cc:         cc,
		Bcc:        opts.Bcc,
		Subject:    subject,
		Body:       opts.Body,
		ThreadID:   opts.ThreadID,
		InReplyTo:  messageID,
		References: references,
	}, nil
}

type emptyResponse struct{}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func WriteJSONFile(path string, value any) error {
	return writePrettyJSON(path, value)
}

func buildRawMessage(opts DraftOptions) (string, error) {
	to, err := normalizeAddressList(opts.To)
	if err != nil {
		return "", fmt.Errorf("to: %w", err)
	}
	if to == "" {
		return "", errors.New("to address is required")
	}
	cc, err := normalizeAddressList(opts.Cc)
	if err != nil {
		return "", fmt.Errorf("cc: %w", err)
	}
	bcc, err := normalizeAddressList(opts.Bcc)
	if err != nil {
		return "", fmt.Errorf("bcc: %w", err)
	}
	subject := strings.TrimSpace(opts.Subject)
	if subject == "" {
		return "", errors.New("subject is required")
	}

	var buf bytes.Buffer
	writeHeader := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		fmt.Fprintf(&buf, "%s: %s\r\n", name, strings.TrimSpace(value))
	}
	writeHeader("To", to)
	writeHeader("Cc", cc)
	writeHeader("Bcc", bcc)
	writeHeader("Subject", subject)
	writeHeader("In-Reply-To", opts.InReplyTo)
	writeHeader("References", opts.References)
	writeHeader("MIME-Version", "1.0")
	writeHeader("Content-Type", `text/plain; charset="UTF-8"`)
	writeHeader("Content-Transfer-Encoding", "8bit")
	buf.WriteString("\r\n")
	body := strings.ReplaceAll(opts.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	buf.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))

	return base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

func normalizeAddressList(values []string) (string, error) {
	var addresses []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			address, err := mail.ParseAddress(part)
			if err != nil {
				return "", err
			}
			addresses = append(addresses, address.String())
		}
	}
	return strings.Join(addresses, ", "), nil
}

func draftInfo(draft *gmail.Draft) *DraftInfo {
	if draft == nil {
		return &DraftInfo{}
	}
	info := &DraftInfo{ID: draft.Id}
	if draft.Message != nil {
		info.MessageID = draft.Message.Id
		info.ThreadID = draft.Message.ThreadId
		info.Snippet = draft.Message.Snippet
		headers := headerMap(draft.Message)
		info.Subject = headers["Subject"]
		info.To = headers["To"]
		info.Cc = headers["Cc"]
	}
	return info
}

func latestNonDraftMessage(thread *gmail.Thread) *gmail.Message {
	if thread == nil {
		return nil
	}
	var latest *gmail.Message
	for _, message := range thread.Messages {
		if hasLabel(message, "DRAFT") {
			continue
		}
		if latest == nil || message.InternalDate > latest.InternalDate {
			latest = message
		}
	}
	return latest
}

func hasLabel(message *gmail.Message, label string) bool {
	for _, value := range message.LabelIds {
		if value == label {
			return true
		}
	}
	return false
}

func addressesForReply(headers map[string]string) []string {
	if value := strings.TrimSpace(headers["Reply-To"]); value != "" {
		return addressesAsStrings(parseAddressList(value))
	}
	return addressesAsStrings(parseAddressList(headers["From"]))
}

func replyAllCc(headers map[string]string, self string, primary []string) []string {
	exclude := map[string]struct{}{}
	if self != "" {
		exclude[self] = struct{}{}
	}
	for _, value := range primary {
		for _, address := range parseAddressList(value) {
			exclude[strings.ToLower(address.Address)] = struct{}{}
		}
	}
	var out []string
	seen := make(map[string]struct{})
	for _, header := range []string{"To", "Cc"} {
		for _, address := range parseAddressList(headers[header]) {
			email := strings.ToLower(address.Address)
			if _, skip := exclude[email]; skip {
				continue
			}
			if _, ok := seen[email]; ok {
				continue
			}
			seen[email] = struct{}{}
			out = append(out, address.String())
		}
	}
	return out
}

func addressesAsStrings(addresses []*mail.Address) []string {
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address == nil {
			continue
		}
		out = append(out, address.String())
	}
	return out
}

func WriteMessageFile(path string, message *gmail.Message, format string) error {
	if format == "raw" {
		data, err := base64.RawURLEncoding.DecodeString(message.Raw)
		if err != nil {
			return fmt.Errorf("decode raw message: %w", err)
		}
		return os.WriteFile(path, data, 0o600)
	}
	return writePrettyJSON(path, message)
}

func WriteThreadFile(path string, thread *gmail.Thread) error {
	return writePrettyJSON(path, thread)
}

func (c *Client) ExportAddresses(ctx context.Context, opts AddressExportOptions) ([]AddressInteraction, error) {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.Workers > 32 {
		opts.Workers = 32
	}
	db, err := c.cache.Open(ctx)
	if err != nil {
		if errors.Is(err, cache.ErrDisabled) {
			return nil, errors.New("gmail address export requires cache; omit --no-cache")
		}
		return nil, err
	}
	defer db.Close()

	if err := c.ingestMetadata(ctx, db, opts); err != nil {
		return nil, err
	}
	return loadAddressInteractions(ctx, db, opts.AccountEmail)
}

func (c *Client) ingestMetadata(ctx context.Context, db *sql.DB, opts AddressExportOptions) error {
	if opts.ForceRefresh {
		if err := resetIngest(ctx, db, opts.AccountEmail); err != nil {
			return err
		}
	}
	state, err := loadIngestState(ctx, db, opts.AccountEmail)
	if err != nil {
		return err
	}

	var scanned int
	var pages int
	for {
		call := c.service.Users.Messages.List("me").MaxResults(500).Context(ctx)
		query := ""
		if state.FullScanComplete {
			query, err = incrementalQuery(ctx, db, opts.AccountEmail)
			if err != nil {
				return err
			}
			if query == "" {
				fmt.Fprintln(os.Stderr, "gmail metadata ingest: full scan complete and no incremental watermark found")
				return rebuildAddressInteractions(ctx, db, opts.AccountEmail)
			}
			call.Q(query)
		}
		if state.NextPageToken != "" {
			call.PageToken(state.NextPageToken)
		}
		resp, err := doWithRetry(ctx, func() (*gmail.ListMessagesResponse, error) {
			return call.Do()
		})
		if err != nil {
			return fmt.Errorf("list gmail metadata messages: %w", err)
		}
		pages++
		mode := "full"
		if state.FullScanComplete {
			mode = "incremental"
		}
		fmt.Fprintf(os.Stderr, "gmail metadata ingest: %s page %d, %d messages, workers=%d\n", mode, pages, len(resp.Messages), opts.Workers)
		messages, err := c.fetchMessageMetadataPage(ctx, resp.Messages, opts.Workers)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if message == nil {
				continue
			}
			if err := upsertMessageMetadata(ctx, db, opts.AccountEmail, message); err != nil {
				return err
			}
			scanned++
		}
		if scanned > 0 && scanned%250 == 0 {
			fmt.Fprintf(os.Stderr, "gmail metadata ingest: cached %d messages this run\n", scanned)
		}
		state.NextPageToken = resp.NextPageToken
		if resp.NextPageToken == "" {
			if state.FullScanComplete {
				if err := saveIngestState(ctx, db, opts.AccountEmail, state); err != nil {
					return err
				}
				break
			}
			state.FullScanComplete = true
		}
		if err := saveIngestState(ctx, db, opts.AccountEmail, state); err != nil {
			return err
		}
		if resp.NextPageToken == "" {
			break
		}
	}
	fmt.Fprintf(os.Stderr, "gmail metadata ingest: cached %d messages this run\n", scanned)
	return rebuildAddressInteractions(ctx, db, opts.AccountEmail)
}

func (c *Client) fetchMessageMetadataPage(ctx context.Context, items []*gmail.Message, workers int) ([]*gmail.Message, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]*gmail.Message, len(items))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				message, err := c.GetMessage(ctx, items[index].Id, "full")
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
					continue
				}
				results[index] = message
			}
		}()
	}

sendLoop:
	for index := 0; index < len(items); index++ {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- index:
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func loadIngestState(ctx context.Context, db *sql.DB, accountEmail string) (ingestState, error) {
	var state ingestState
	var fullScanComplete int
	err := db.QueryRowContext(ctx, `
		SELECT full_scan_complete, next_page_token, last_incremental_after
		FROM gmail_ingest_state
		WHERE account_email = ?
	`, accountEmail).Scan(&fullScanComplete, &state.NextPageToken, &state.LastIncrementalAfter)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state, nil
		}
		return state, fmt.Errorf("query gmail ingest state: %w", err)
	}
	state.FullScanComplete = fullScanComplete != 0
	return state, nil
}

func saveIngestState(ctx context.Context, db *sql.DB, accountEmail string, state ingestState) error {
	fullScanComplete := 0
	if state.FullScanComplete {
		fullScanComplete = 1
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO gmail_ingest_state(account_email, full_scan_complete, next_page_token, last_incremental_after, updated_at_unix_ms)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(account_email) DO UPDATE SET
			full_scan_complete = excluded.full_scan_complete,
			next_page_token = excluded.next_page_token,
			last_incremental_after = excluded.last_incremental_after,
			updated_at_unix_ms = excluded.updated_at_unix_ms
	`, accountEmail, fullScanComplete, state.NextPageToken, state.LastIncrementalAfter, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("save gmail ingest state: %w", err)
	}
	return nil
}

func resetIngest(ctx context.Context, db *sql.DB, accountEmail string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin gmail ingest reset: %w", err)
	}
	defer tx.Rollback()
	for _, query := range []string{
		`DELETE FROM gmail_messages WHERE account_email = ?`,
		`DELETE FROM gmail_threads WHERE account_email = ?`,
		`DELETE FROM gmail_attachments WHERE account_email = ?`,
		`DELETE FROM gmail_address_interactions WHERE account_email = ?`,
		`DELETE FROM gmail_ingest_state WHERE account_email = ?`,
	} {
		if _, err := tx.ExecContext(ctx, query, accountEmail); err != nil {
			return fmt.Errorf("reset gmail ingest cache: %w", err)
		}
	}
	return tx.Commit()
}

func incrementalQuery(ctx context.Context, db *sql.DB, accountEmail string) (string, error) {
	var lastSeen sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(internal_date_unix_ms) FROM gmail_messages WHERE account_email = ?`, accountEmail).Scan(&lastSeen); err != nil {
		return "", fmt.Errorf("query gmail metadata watermark: %w", err)
	}
	if !lastSeen.Valid {
		return "", nil
	}
	after := time.UnixMilli(lastSeen.Int64).Add(-24 * time.Hour).Format("2006/01/02")
	return "after:" + after, nil
}

func doWithRetry[T any](ctx context.Context, fn func() (*T, error)) (*T, error) {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		if err := sharedGmailThrottle.wait(ctx); err != nil {
			return nil, err
		}
		value, err := fn()
		if err == nil {
			return value, nil
		}
		lastErr = err
		if !retryableGoogleError(err) {
			return nil, err
		}
		delay := retryDelay(err, attempt)
		sharedGmailThrottle.pause(delay)
		fmt.Fprintf(os.Stderr, "gmail API throttled/transient error; retrying in %s: %v\n", delay, err)
	}
	return nil, lastErr
}

func (t *apiThrottle) wait(ctx context.Context) error {
	t.mu.Lock()
	until := t.pausedUntil
	t.mu.Unlock()

	delay := time.Until(until)
	if delay <= 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "gmail API throttle: waiting %s for shared quota backoff\n", delay.Round(time.Second))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (t *apiThrottle) pause(delay time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	until := time.Now().Add(delay)
	if until.After(t.pausedUntil) {
		t.pausedUntil = until
	}
}

func retryDelay(err error, attempt int) time.Duration {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && (apiErr.Code == 429 || apiErr.Code == 403) {
		for _, item := range apiErr.Errors {
			switch item.Reason {
			case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded":
				return 65 * time.Second
			}
		}
	}
	delay := time.Duration(1<<attempt) * time.Second
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func retryableGoogleError(err error) bool {
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Code == 429 || apiErr.Code >= 500 {
		return true
	}
	if apiErr.Code != 403 {
		return false
	}
	for _, item := range apiErr.Errors {
		switch item.Reason {
		case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "backendError":
			return true
		}
	}
	return false
}

func upsertMessageMetadata(ctx context.Context, db *sql.DB, accountEmail string, message *gmail.Message) error {
	headers := headerMap(message)
	sanitized := sanitizeMessage(message)
	raw, err := json.Marshal(sanitized)
	if err != nil {
		return fmt.Errorf("marshal gmail metadata: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin gmail metadata upsert: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO gmail_messages(
			account_email, message_id, thread_id, internal_date_unix_ms,
			from_header, to_header, cc_header, bcc_header, reply_to_header, sender_header,
			subject, snippet, raw_metadata_json, ingested_at_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_email, message_id) DO UPDATE SET
			thread_id = excluded.thread_id,
			internal_date_unix_ms = excluded.internal_date_unix_ms,
			from_header = excluded.from_header,
			to_header = excluded.to_header,
			cc_header = excluded.cc_header,
			bcc_header = excluded.bcc_header,
			reply_to_header = excluded.reply_to_header,
			sender_header = excluded.sender_header,
			subject = excluded.subject,
			snippet = excluded.snippet,
			raw_metadata_json = excluded.raw_metadata_json,
			ingested_at_unix_ms = excluded.ingested_at_unix_ms
	`, accountEmail, message.Id, message.ThreadId, message.InternalDate,
		headers["From"], headers["To"], headers["Cc"], headers["Bcc"], headers["Reply-To"], headers["Sender"],
		headers["Subject"], message.Snippet, raw, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("upsert gmail metadata: %w", err)
	}

	if err := upsertThreadMetadata(ctx, tx, accountEmail, message); err != nil {
		return err
	}
	if err := replaceAttachmentMetadata(ctx, tx, accountEmail, message); err != nil {
		return err
	}

	return tx.Commit()
}

func upsertThreadMetadata(ctx context.Context, tx *sql.Tx, accountEmail string, message *gmail.Message) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO gmail_threads(account_email, thread_id, message_count, latest_internal_date_unix_ms, snippet, ingested_at_unix_ms)
		VALUES (?, ?, 1, ?, ?, ?)
		ON CONFLICT(account_email, thread_id) DO UPDATE SET
			message_count = (
				SELECT COUNT(*) FROM gmail_messages
				WHERE account_email = excluded.account_email AND thread_id = excluded.thread_id
			),
			latest_internal_date_unix_ms = MAX(gmail_threads.latest_internal_date_unix_ms, excluded.latest_internal_date_unix_ms),
			snippet = CASE
				WHEN excluded.latest_internal_date_unix_ms >= gmail_threads.latest_internal_date_unix_ms THEN excluded.snippet
				ELSE gmail_threads.snippet
			END,
			ingested_at_unix_ms = excluded.ingested_at_unix_ms
	`, accountEmail, message.ThreadId, message.InternalDate, message.Snippet, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("upsert gmail thread metadata: %w", err)
	}
	return nil
}

func replaceAttachmentMetadata(ctx context.Context, tx *sql.Tx, accountEmail string, message *gmail.Message) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM gmail_attachments WHERE account_email = ? AND message_id = ?
	`, accountEmail, message.Id); err != nil {
		return fmt.Errorf("clear gmail attachment metadata: %w", err)
	}

	var attachments []AttachmentInfo
	walkParts(message.Id, message.Payload, &attachments)
	for _, attachment := range attachments {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO gmail_attachments(
				account_email, message_id, thread_id, part_id, attachment_id, filename, mime_type, size, ingested_at_unix_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, accountEmail, message.Id, message.ThreadId, attachment.PartID, attachment.AttachmentID, attachment.Filename, attachment.MimeType, attachment.Size, time.Now().UnixMilli())
		if err != nil {
			return fmt.Errorf("write gmail attachment metadata: %w", err)
		}
	}
	return nil
}

func rebuildAddressInteractions(ctx context.Context, db *sql.DB, accountEmail string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT internal_date_unix_ms, from_header, to_header, cc_header, bcc_header, reply_to_header, sender_header
		FROM gmail_messages
		WHERE account_email = ?
	`, accountEmail)
	if err != nil {
		return fmt.Errorf("query gmail cached metadata: %w", err)
	}
	defer rows.Close()

	type seen struct {
		name    string
		first   int64
		last    int64
		count   int64
		headers map[string]struct{}
	}
	addresses := make(map[string]*seen)
	for rows.Next() {
		var at int64
		values := make([]string, 6)
		if err := rows.Scan(&at, &values[0], &values[1], &values[2], &values[3], &values[4], &values[5]); err != nil {
			return fmt.Errorf("scan gmail metadata: %w", err)
		}
		for i, header := range []string{"from", "to", "cc", "bcc", "reply-to", "sender"} {
			for _, addr := range parseAddressList(values[i]) {
				entry := addresses[addr.Address]
				if entry == nil {
					entry = &seen{first: at, last: at, headers: make(map[string]struct{})}
					addresses[addr.Address] = entry
				}
				if entry.name == "" {
					entry.name = addr.Name
				}
				if at < entry.first {
					entry.first = at
				}
				if at > entry.last {
					entry.last = at
				}
				entry.count++
				entry.headers[header] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read gmail metadata rows: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin address rebuild: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM gmail_address_interactions WHERE account_email = ?`, accountEmail); err != nil {
		return fmt.Errorf("clear gmail address interactions: %w", err)
	}
	for email, item := range addresses {
		headers := sortedSet(item.headers)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO gmail_address_interactions(account_email, email, name, first_seen_unix_ms, last_seen_unix_ms, message_count, headers)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, accountEmail, email, item.name, item.first, item.last, item.count, strings.Join(headers, ",")); err != nil {
			return fmt.Errorf("write gmail address interaction: %w", err)
		}
	}
	return tx.Commit()
}

func loadAddressInteractions(ctx context.Context, db *sql.DB, accountEmail string) ([]AddressInteraction, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT email, name, first_seen_unix_ms, last_seen_unix_ms, message_count, headers
		FROM gmail_address_interactions
		WHERE account_email = ?
		ORDER BY last_seen_unix_ms DESC, email ASC
	`, accountEmail)
	if err != nil {
		return nil, fmt.Errorf("query gmail address interactions: %w", err)
	}
	defer rows.Close()
	var out []AddressInteraction
	for rows.Next() {
		var item AddressInteraction
		if err := rows.Scan(&item.Email, &item.Name, &item.FirstSeen, &item.LastSeen, &item.MessageCount, &item.Headers); err != nil {
			return nil, fmt.Errorf("scan gmail address interaction: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func headerMap(message *gmail.Message) map[string]string {
	headers := make(map[string]string)
	if message.Payload == nil {
		return headers
	}
	for _, header := range message.Payload.Headers {
		headers[header.Name] = header.Value
	}
	return headers
}

func sanitizeMessage(message *gmail.Message) *gmail.Message {
	if message == nil {
		return nil
	}
	clone := *message
	clone.Raw = ""
	clone.Payload = sanitizePart(message.Payload)
	return &clone
}

func sanitizePart(part *gmail.MessagePart) *gmail.MessagePart {
	if part == nil {
		return nil
	}
	clone := *part
	if part.Body != nil {
		body := *part.Body
		body.Data = ""
		clone.Body = &body
	}
	if len(part.Parts) > 0 {
		clone.Parts = make([]*gmail.MessagePart, 0, len(part.Parts))
		for _, child := range part.Parts {
			clone.Parts = append(clone.Parts, sanitizePart(child))
		}
	}
	return &clone
}

func metadataHeaders() []string {
	return []string{"From", "To", "Cc", "Bcc", "Reply-To", "Sender", "Subject", "Date", "Message-ID", "References"}
}

func walkParts(messageID string, part *gmail.MessagePart, attachments *[]AttachmentInfo) {
	if part == nil {
		return
	}
	if part.Body != nil && part.Body.AttachmentId != "" {
		*attachments = append(*attachments, AttachmentInfo{
			MessageID:    messageID,
			PartID:       part.PartId,
			AttachmentID: part.Body.AttachmentId,
			Filename:     part.Filename,
			MimeType:     part.MimeType,
			Size:         part.Body.Size,
		})
	}
	for _, child := range part.Parts {
		walkParts(messageID, child, attachments)
	}
}

func parseAddressList(value string) []*mail.Address {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	addresses, err := mail.ParseAddressList(value)
	if err != nil {
		address, err := mail.ParseAddress(value)
		if err != nil {
			return nil
		}
		return []*mail.Address{address}
	}
	for _, address := range addresses {
		address.Address = strings.ToLower(address.Address)
	}
	return addresses
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func safeFilename(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '_'
		default:
			return r
		}
	}, value)
	return value
}

func writePrettyJSON(path string, value any) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open output file %q: %w", path, err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write output file %q: %w", path, err)
	}
	return nil
}
