package googlesheets

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
	sheets "google.golang.org/api/sheets/v4"
)

type Config struct {
	CredentialsFile string
	TokenFile       string
	NoBrowser       bool
	Cache           cache.Config
}

type Client struct {
	sheets *sheets.Service
	drive  *drive.Service
	cache  cache.Store
}

func Scopes() []string {
	return []string{sheets.SpreadsheetsScope, drive.DriveScope}
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
	sheetsService, err := sheets.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create sheets service: %w", err)
	}
	driveService, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}
	return &Client{sheets: sheetsService, drive: driveService, cache: cache.NewStore(cfg.Cache)}, nil
}

func (c *Client) Create(ctx context.Context, title string) (*sheets.Spreadsheet, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("title is required")
	}
	spreadsheet, err := c.sheets.Spreadsheets.Create(&sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{Title: title},
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("create spreadsheet: %w", err)
	}
	return spreadsheet, c.invalidate(ctx)
}

func (c *Client) Get(ctx context.Context, id string) (*sheets.Spreadsheet, error) {
	if id == "" {
		return nil, errors.New("spreadsheet ID is required")
	}
	var cached sheets.Spreadsheet
	if ok, err := c.cache.GetJSON(ctx, "sheets", []string{"get", id}, &cached); err != nil {
		return nil, err
	} else if ok {
		return &cached, nil
	}
	spreadsheet, err := c.sheets.Spreadsheets.Get(id).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get spreadsheet %q: %w", id, err)
	}
	if err := c.cache.SetJSON(ctx, "sheets", []string{"get", id}, spreadsheet); err != nil {
		return nil, err
	}
	return spreadsheet, nil
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
		return fmt.Errorf("export spreadsheet %q: %w", id, err)
	}
	defer resp.Body.Close()
	return writeResponse(output, resp.Body)
}

func (c *Client) ValuesGet(ctx context.Context, id, rangeName string) (*sheets.ValueRange, error) {
	if rangeName == "" {
		return nil, errors.New("range is required")
	}
	resp, err := c.sheets.Spreadsheets.Values.Get(id, rangeName).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get spreadsheet values: %w", err)
	}
	return resp, nil
}

func (c *Client) ValuesUpdate(ctx context.Context, id, rangeName string, values [][]interface{}) (*sheets.UpdateValuesResponse, error) {
	if rangeName == "" {
		return nil, errors.New("range is required")
	}
	resp, err := c.sheets.Spreadsheets.Values.Update(id, rangeName, &sheets.ValueRange{Values: values}).
		ValueInputOption("USER_ENTERED").
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("update spreadsheet values: %w", err)
	}
	return resp, c.invalidate(ctx)
}

func (c *Client) ValuesAppend(ctx context.Context, id, rangeName string, values [][]interface{}) (*sheets.AppendValuesResponse, error) {
	if rangeName == "" {
		return nil, errors.New("range is required")
	}
	resp, err := c.sheets.Spreadsheets.Values.Append(id, rangeName, &sheets.ValueRange{Values: values}).
		ValueInputOption("USER_ENTERED").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("append spreadsheet values: %w", err)
	}
	return resp, c.invalidate(ctx)
}

func (c *Client) ValuesClear(ctx context.Context, id, rangeName string) (*sheets.ClearValuesResponse, error) {
	if rangeName == "" {
		return nil, errors.New("range is required")
	}
	resp, err := c.sheets.Spreadsheets.Values.Clear(id, rangeName, &sheets.ClearValuesRequest{}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("clear spreadsheet values: %w", err)
	}
	return resp, c.invalidate(ctx)
}

func (c *Client) TabsList(ctx context.Context, id string) ([]*sheets.Sheet, error) {
	spreadsheet, err := c.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return spreadsheet.Sheets, nil
}

func (c *Client) TabsAdd(ctx context.Context, id, title string) (*sheets.BatchUpdateSpreadsheetResponse, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("title is required")
	}
	return c.BatchUpdate(ctx, id, &sheets.BatchUpdateSpreadsheetRequest{Requests: []*sheets.Request{{
		AddSheet: &sheets.AddSheetRequest{Properties: &sheets.SheetProperties{Title: title}},
	}}})
}

func (c *Client) TabsRename(ctx context.Context, id string, sheetID int64, title string) (*sheets.BatchUpdateSpreadsheetResponse, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("title is required")
	}
	return c.BatchUpdate(ctx, id, &sheets.BatchUpdateSpreadsheetRequest{Requests: []*sheets.Request{{
		UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{SheetId: sheetID, Title: title},
			Fields:     "title",
		},
	}}})
}

func (c *Client) TabsDelete(ctx context.Context, id string, sheetID int64) (*sheets.BatchUpdateSpreadsheetResponse, error) {
	return c.BatchUpdate(ctx, id, &sheets.BatchUpdateSpreadsheetRequest{Requests: []*sheets.Request{{
		DeleteSheet: &sheets.DeleteSheetRequest{SheetId: sheetID},
	}}})
}

func (c *Client) Search(ctx context.Context, id, query string) ([]map[string]interface{}, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query is required")
	}
	spreadsheet, err := c.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var results []map[string]interface{}
	for _, sheet := range spreadsheet.Sheets {
		if sheet == nil || sheet.Properties == nil {
			continue
		}
		title := sheet.Properties.Title
		values, err := c.ValuesGet(ctx, id, title)
		if err != nil {
			continue
		}
		for r, row := range values.Values {
			for col, value := range row {
				text := fmt.Sprint(value)
				if strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
					results = append(results, map[string]interface{}{
						"sheet":  title,
						"row":    r + 1,
						"column": col + 1,
						"value":  text,
					})
				}
			}
		}
	}
	return results, nil
}

func (c *Client) BatchUpdate(ctx context.Context, id string, req *sheets.BatchUpdateSpreadsheetRequest) (*sheets.BatchUpdateSpreadsheetResponse, error) {
	if req == nil || len(req.Requests) == 0 {
		return nil, errors.New("batch update must contain at least one request")
	}
	resp, err := c.sheets.Spreadsheets.BatchUpdate(id, req).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("batch update spreadsheet %q: %w", id, err)
	}
	return resp, c.invalidate(ctx)
}

func ParseBatchUpdate(payload []byte) (*sheets.BatchUpdateSpreadsheetRequest, error) {
	var req sheets.BatchUpdateSpreadsheetRequest
	if err := json.Unmarshal(payload, &req); err == nil && len(req.Requests) > 0 {
		return &req, nil
	}
	var requests []*sheets.Request
	if err := json.Unmarshal(payload, &requests); err != nil {
		return nil, fmt.Errorf("parse batch update JSON: %w", err)
	}
	if len(requests) == 0 {
		return nil, errors.New("batch update must contain at least one request")
	}
	return &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}, nil
}

func ParseValues(payload []byte) ([][]interface{}, error) {
	var direct [][]interface{}
	if err := json.Unmarshal(payload, &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		Values [][]interface{} `json:"values"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, fmt.Errorf("parse values JSON: %w", err)
	}
	return wrapped.Values, nil
}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (c *Client) invalidate(ctx context.Context) error {
	if err := c.cache.DeleteNamespace(ctx, "sheets"); err != nil {
		return err
	}
	return c.cache.DeleteNamespace(ctx, "drive")
}

func exportMime(format string) (string, error) {
	switch format {
	case "", "xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
	case "csv":
		return "text/csv", nil
	case "pdf":
		return "application/pdf", nil
	default:
		return "", fmt.Errorf("invalid sheets export format %q; valid values: xlsx,csv,pdf", format)
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
