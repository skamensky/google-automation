package googleslides

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/skamensky/google-automation/internal/cache"
	"github.com/skamensky/google-automation/internal/googleauth"
	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	slides "google.golang.org/api/slides/v1"
)

type Config struct {
	CredentialsFile string
	TokenFile       string
	NoBrowser       bool
	Cache           cache.Config
}

type Client struct {
	slides *slides.Service
	drive  *drive.Service
	cache  cache.Store
}

func Scopes() []string {
	return []string{slides.PresentationsScope, drive.DriveScope}
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
	slidesService, err := slides.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create slides service: %w", err)
	}
	driveService, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}
	return &Client{slides: slidesService, drive: driveService, cache: cache.NewStore(cfg.Cache)}, nil
}

func (c *Client) Create(ctx context.Context, title string) (*slides.Presentation, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("title is required")
	}
	presentation, err := c.slides.Presentations.Create(&slides.Presentation{Title: title}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("create presentation: %w", err)
	}
	return presentation, c.invalidate(ctx)
}

func (c *Client) Get(ctx context.Context, id string) (*slides.Presentation, error) {
	if id == "" {
		return nil, errors.New("presentation ID is required")
	}
	var cached slides.Presentation
	if ok, err := c.cache.GetJSON(ctx, "slides", []string{"get", id}, &cached); err != nil {
		return nil, err
	} else if ok {
		return &cached, nil
	}
	presentation, err := c.slides.Presentations.Get(id).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get presentation %q: %w", id, err)
	}
	if err := c.cache.SetJSON(ctx, "slides", []string{"get", id}, presentation); err != nil {
		return nil, err
	}
	return presentation, nil
}

func (c *Client) Export(ctx context.Context, id, format, output string) error {
	if output == "" {
		return errors.New("--output is required")
	}
	mimeType, err := exportMime(format)
	if err != nil {
		return err
	}
	resp, err := c.drive.Files.Export(id, mimeType).Download()
	if err != nil {
		return fmt.Errorf("export presentation %q: %w", id, err)
	}
	defer resp.Body.Close()
	return writeResponse(output, resp.Body)
}

func (c *Client) BatchUpdate(ctx context.Context, id string, req *slides.BatchUpdatePresentationRequest) (*slides.BatchUpdatePresentationResponse, error) {
	if req == nil || len(req.Requests) == 0 {
		return nil, errors.New("batch update must contain at least one request")
	}
	resp, err := c.slides.Presentations.BatchUpdate(id, req).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("batch update presentation %q: %w", id, err)
	}
	return resp, c.invalidate(ctx)
}

func ParseBatchUpdate(payload []byte) (*slides.BatchUpdatePresentationRequest, error) {
	var req slides.BatchUpdatePresentationRequest
	if err := json.Unmarshal(payload, &req); err == nil && len(req.Requests) > 0 {
		return &req, nil
	}
	var requests []*slides.Request
	if err := json.Unmarshal(payload, &requests); err != nil {
		return nil, fmt.Errorf("parse batch update JSON: %w", err)
	}
	if len(requests) == 0 {
		return nil, errors.New("batch update must contain at least one request")
	}
	return &slides.BatchUpdatePresentationRequest{Requests: requests}, nil
}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (c *Client) invalidate(ctx context.Context) error {
	if err := c.cache.DeleteNamespace(ctx, "slides"); err != nil {
		return err
	}
	return c.cache.DeleteNamespace(ctx, "drive")
}

func exportMime(format string) (string, error) {
	switch format {
	case "", "pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation", nil
	case "pdf":
		return "application/pdf", nil
	default:
		return "", fmt.Errorf("invalid slides export format %q; valid values: pptx,pdf", format)
	}
}

func writeResponse(output string, body io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(absOrClean(output)), 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open output file: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, body); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
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
