package googledocs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/skamensky/google-automation/internal/cache"
	"github.com/skamensky/google-automation/internal/googleauth"
	docs "google.golang.org/api/docs/v1"
	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type Config struct {
	CredentialsFile string
	TokenFile       string
	NoBrowser       bool
	Cache           cache.Config
}

type Client struct {
	docs  *docs.Service
	drive *drive.Service
	cache cache.Store
}

func Scopes() []string {
	return []string{docs.DocumentsScope, drive.DriveScope}
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
	docsService, err := docs.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create docs service: %w", err)
	}
	driveService, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}
	return &Client{docs: docsService, drive: driveService, cache: cache.NewStore(cfg.Cache)}, nil
}

func (c *Client) Get(ctx context.Context, id string) (*docs.Document, error) {
	if id == "" {
		return nil, errors.New("document ID is required")
	}
	var cached docs.Document
	if ok, err := c.cache.GetJSON(ctx, "docs", []string{"get", id}, &cached); err != nil {
		return nil, err
	} else if ok {
		return &cached, nil
	}
	doc, err := c.docs.Documents.Get(id).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get document %q: %w", id, err)
	}
	if err := c.cache.SetJSON(ctx, "docs", []string{"get", id}, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (c *Client) Export(ctx context.Context, id, format, output string) error {
	if output == "" {
		return errors.New("--output is required")
	}
	_, mimeType, _, err := googledriveDocExportFormat(format)
	if err != nil {
		return err
	}
	resp, err := c.drive.Files.Export(id, mimeType).Download()
	if err != nil {
		return fmt.Errorf("export document %q: %w", id, err)
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(filepath.Dir(absOrClean(output)), 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open output file: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}

func (c *Client) Append(ctx context.Context, id, text string) (*docs.BatchUpdateDocumentResponse, error) {
	if text == "" {
		return nil, errors.New("text is required")
	}
	doc, err := c.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	index := int64(1)
	if doc.Body != nil && len(doc.Body.Content) > 0 {
		last := doc.Body.Content[len(doc.Body.Content)-1]
		if last.EndIndex > 1 {
			index = last.EndIndex - 1
		}
	}
	resp, err := c.docs.Documents.BatchUpdate(id, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			InsertText: &docs.InsertTextRequest{
				Location: &docs.Location{Index: index},
				Text:     text,
			},
		}},
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("append to document %q: %w", id, err)
	}
	return resp, c.invalidate(ctx)
}

func (c *Client) Replace(ctx context.Context, id, find, replace string) (*docs.BatchUpdateDocumentResponse, error) {
	if find == "" {
		return nil, errors.New("find text is required")
	}
	resp, err := c.docs.Documents.BatchUpdate(id, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			ReplaceAllText: &docs.ReplaceAllTextRequest{
				ContainsText: &docs.SubstringMatchCriteria{
					Text:      find,
					MatchCase: true,
				},
				ReplaceText: replace,
			},
		}},
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("replace in document %q: %w", id, err)
	}
	return resp, c.invalidate(ctx)
}

func (c *Client) BatchUpdate(ctx context.Context, id string, payload []byte) (*docs.BatchUpdateDocumentResponse, error) {
	if len(payload) == 0 {
		return nil, errors.New("batch update JSON is empty")
	}
	req, err := parseBatchUpdate(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.docs.Documents.BatchUpdate(id, req).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("batch update document %q: %w", id, err)
	}
	return resp, c.invalidate(ctx)
}

func (c *Client) Create(ctx context.Context, title string) (*docs.Document, error) {
	if title == "" {
		return nil, errors.New("title is required")
	}
	doc, err := c.docs.Documents.Create(&docs.Document{Title: title}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("create document: %w", err)
	}
	return doc, c.invalidate(ctx)
}

func (c *Client) CreateWithOptions(ctx context.Context, title, text, parent string) (*docs.Document, error) {
	doc, err := c.Create(ctx, title)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(parent) != "" && parent != "root" {
		if _, err := c.drive.Files.Update(doc.DocumentId, &drive.File{}).
			AddParents(parent).
			RemoveParents("root").
			Fields("id").
			SupportsAllDrives(true).
			Context(ctx).
			Do(); err != nil {
			return nil, fmt.Errorf("move created document to parent %q: %w", parent, err)
		}
	}
	if text != "" {
		if _, err := c.Append(ctx, doc.DocumentId, text); err != nil {
			return nil, err
		}
		doc, err = c.Get(ctx, doc.DocumentId)
		if err != nil {
			return nil, err
		}
	}
	return doc, c.invalidate(ctx)
}

func (c *Client) Upload(ctx context.Context, path, title, parent string) (*drive.File, error) {
	if path == "" {
		return nil, errors.New("path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open upload file: %w", err)
	}
	defer file.Close()
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if parent == "" {
		parent = "root"
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "text/plain"
	}
	metadata := &drive.File{
		Name:     title,
		MimeType: "application/vnd.google-apps.document",
		Parents:  []string{parent},
	}
	created, err := c.drive.Files.Create(metadata).
		Media(file, googleapi.ContentType(contentType)).
		Fields("id,name,mimeType,parents,webViewLink,createdTime,modifiedTime").
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("upload Google Doc: %w", err)
	}
	return created, c.invalidate(ctx)
}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func parseBatchUpdate(payload []byte) (*docs.BatchUpdateDocumentRequest, error) {
	var req docs.BatchUpdateDocumentRequest
	if err := json.Unmarshal(payload, &req); err == nil && len(req.Requests) > 0 {
		return &req, nil
	}
	var requests []*docs.Request
	if err := json.Unmarshal(payload, &requests); err != nil {
		return nil, fmt.Errorf("parse batch update JSON: %w", err)
	}
	if len(requests) == 0 {
		return nil, errors.New("batch update must contain at least one request")
	}
	return &docs.BatchUpdateDocumentRequest{Requests: requests}, nil
}

func (c *Client) invalidate(ctx context.Context) error {
	if err := c.cache.DeleteNamespace(ctx, "docs"); err != nil {
		return err
	}
	return c.cache.DeleteNamespace(ctx, "drive")
}

func googledriveDocExportFormat(format string) (string, string, string, error) {
	switch format {
	case "", "docx":
		return "docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx", nil
	case "pdf":
		return "pdf", "application/pdf", ".pdf", nil
	case "txt":
		return "txt", "text/plain", ".txt", nil
	case "html":
		return "html", "text/html", ".html", nil
	default:
		return "", "", "", fmt.Errorf("invalid docs format %q; valid values: pdf,docx,txt,html", format)
	}
}

func absOrClean(path string) string {
	if path == "" {
		return "."
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}
