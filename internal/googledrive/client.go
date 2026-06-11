package googledrive

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/skamensky/google-automation/internal/cache"
	"github.com/skamensky/google-automation/internal/googleauth"
	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

const (
	FolderMimeType        = "application/vnd.google-apps.folder"
	GoogleDocMimeType     = "application/vnd.google-apps.document"
	GoogleSheetMimeType   = "application/vnd.google-apps.spreadsheet"
	GoogleSlideMimeType   = "application/vnd.google-apps.presentation"
	GoogleDrawingMimeType = "application/vnd.google-apps.drawing"

	defaultFields = "id,name,mimeType,size,parents,webViewLink,webContentLink,owners(emailAddress,displayName),createdTime,modifiedTime,trashed,capabilities"
)

type Config struct {
	CredentialsFile string
	TokenFile       string
	NoBrowser       bool
	Cache           cache.Config
}

type Client struct {
	service *drive.Service
	cache   cache.Store
}

type FileList struct {
	Files         []*drive.File `json:"files"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

type SearchOptions struct {
	Query     string
	RawQuery  string
	Type      string
	Trashed   bool
	PageSize  int64
	PageToken string
}

type ListFolderOptions struct {
	Parent    string
	Type      string
	Trashed   bool
	PageSize  int64
	PageToken string
}

type CreateFolderOptions struct {
	Name   string
	Parent string
}

type UploadOptions struct {
	Path   string
	Name   string
	Parent string
}

type UpdateField struct {
	Field string
	Value string
}

type UpdateOptions struct {
	FileID string
	Fields []UpdateField
}

type MoveOptions struct {
	FileID   string
	Parent   string
	IsFolder bool
}

type TreeOptions struct {
	Parent  string
	Depth   int
	Type    string
	Trashed bool
}

type TreeNode struct {
	File     *drive.File `json:"file"`
	Children []*TreeNode `json:"children,omitempty"`
}

type DownloadZipOptions struct {
	Files         []string
	Folders       []string
	Output        string
	DocsFormat    string
	SheetsFormat  string
	SlidesFormat  string
	PreservePaths bool
	Flatten       bool
}

type PermissionCreateOptions struct {
	FileID string
	Email  string
	Role   string
}

func Scopes() []string {
	return []string{drive.DriveScope}
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

	service, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}

	return &Client{service: service, cache: cache.NewStore(cfg.Cache)}, nil
}

func (c *Client) Search(ctx context.Context, opts SearchOptions) (FileList, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 50
	}
	q, err := searchQuery(opts)
	if err != nil {
		return FileList{}, err
	}

	var cached FileList
	cacheKey := []string{"search", q, strconv.FormatInt(opts.PageSize, 10), opts.PageToken}
	if ok, err := c.cache.GetJSON(ctx, "drive", cacheKey, &cached); err != nil {
		return FileList{}, err
	} else if ok {
		return cached, nil
	}

	call := c.service.Files.List().
		Fields("nextPageToken, files(" + defaultFields + ")").
		PageSize(opts.PageSize).
		Q(q).
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(true).
		Context(ctx)
	if opts.PageToken != "" {
		call.PageToken(opts.PageToken)
	}
	resp, err := call.Do()
	if err != nil {
		return FileList{}, fmt.Errorf("search drive files: %w", err)
	}
	result := FileList{Files: resp.Files, NextPageToken: resp.NextPageToken}
	if err := c.cache.SetJSON(ctx, "drive", cacheKey, result); err != nil {
		return FileList{}, err
	}
	return result, nil
}

func (c *Client) ListFolder(ctx context.Context, opts ListFolderOptions) (FileList, error) {
	if opts.Parent == "" {
		opts.Parent = "root"
	}
	query := fmt.Sprintf("'%s' in parents", escapeQuery(opts.Parent))
	if !opts.Trashed {
		query += " and trashed=false"
	}
	typeQuery, err := typeFilter(opts.Type)
	if err != nil {
		return FileList{}, err
	}
	if typeQuery != "" {
		query += " and " + typeQuery
	}
	return c.Search(ctx, SearchOptions{
		RawQuery:  query,
		PageSize:  opts.PageSize,
		PageToken: opts.PageToken,
	})
}

func (c *Client) Get(ctx context.Context, id string) (*drive.File, error) {
	if id == "" {
		return nil, errors.New("file ID is required")
	}
	var cached drive.File
	if ok, err := c.cache.GetJSON(ctx, "drive", []string{"get", id}, &cached); err != nil {
		return nil, err
	} else if ok {
		return &cached, nil
	}
	file, err := c.service.Files.Get(id).
		Fields(defaultFields).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("get drive file %q: %w", id, err)
	}
	if err := c.cache.SetJSON(ctx, "drive", []string{"get", id}, file); err != nil {
		return nil, err
	}
	return file, nil
}

func (c *Client) CreateFolder(ctx context.Context, opts CreateFolderOptions) (*drive.File, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return nil, errors.New("folder name is required")
	}
	if opts.Parent == "" {
		opts.Parent = "root"
	}
	file := &drive.File{
		Name:     strings.TrimSpace(opts.Name),
		MimeType: FolderMimeType,
		Parents:  []string{opts.Parent},
	}
	created, err := c.service.Files.Create(file).
		Fields(defaultFields).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("create drive folder: %w", err)
	}
	return created, c.invalidate(ctx)
}

func (c *Client) CreateTextFile(ctx context.Context, name, parent, content string) (*drive.File, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("file name is required")
	}
	if parent == "" {
		parent = "root"
	}
	file := &drive.File{Name: strings.TrimSpace(name), Parents: []string{parent}}
	created, err := c.service.Files.Create(file).
		Media(strings.NewReader(content), googleapi.ContentType("text/plain")).
		Fields(defaultFields).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("create drive text file: %w", err)
	}
	return created, c.invalidate(ctx)
}

func (c *Client) Upload(ctx context.Context, opts UploadOptions) (*drive.File, error) {
	if opts.Path == "" {
		return nil, errors.New("path is required")
	}
	file, err := os.Open(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("open upload file: %w", err)
	}
	defer file.Close()
	if opts.Name == "" {
		opts.Name = filepath.Base(opts.Path)
	}
	if opts.Parent == "" {
		opts.Parent = "root"
	}
	contentType := mime.TypeByExtension(filepath.Ext(opts.Path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	metadata := &drive.File{Name: opts.Name, Parents: []string{opts.Parent}}
	created, err := c.service.Files.Create(metadata).
		Media(file, googleapi.ContentType(contentType)).
		Fields(defaultFields).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("upload drive file: %w", err)
	}
	return created, c.invalidate(ctx)
}

func (c *Client) Update(ctx context.Context, opts UpdateOptions) (*drive.File, error) {
	if opts.FileID == "" {
		return nil, errors.New("file ID is required")
	}
	if len(opts.Fields) == 0 {
		return nil, errors.New("at least one --field is required")
	}
	file := &drive.File{}
	for _, field := range opts.Fields {
		if err := applyUpdateField(file, field); err != nil {
			return nil, err
		}
	}
	updated, err := c.service.Files.Update(opts.FileID, file).
		Fields(defaultFields).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("update drive file %q: %w", opts.FileID, err)
	}
	return updated, c.invalidate(ctx)
}

func (c *Client) Move(ctx context.Context, opts MoveOptions) (*drive.File, error) {
	if opts.FileID == "" {
		return nil, errors.New("file ID is required")
	}
	if opts.Parent == "" {
		return nil, errors.New("parent folder ID is required")
	}
	file, err := c.Get(ctx, opts.FileID)
	if err != nil {
		return nil, err
	}
	if opts.IsFolder && file.MimeType != FolderMimeType {
		return nil, fmt.Errorf("%q is not a folder", opts.FileID)
	}
	previousParents := strings.Join(file.Parents, ",")
	moved, err := c.service.Files.Update(opts.FileID, &drive.File{}).
		AddParents(opts.Parent).
		RemoveParents(previousParents).
		Fields(defaultFields).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("move drive file %q: %w", opts.FileID, err)
	}
	return moved, c.invalidate(ctx)
}

func (c *Client) Trash(ctx context.Context, id string, isFolder bool) (*drive.File, error) {
	if isFolder {
		if err := c.ensureFolder(ctx, id); err != nil {
			return nil, err
		}
	}
	file := &drive.File{Trashed: true, ForceSendFields: []string{"Trashed"}}
	updated, err := c.service.Files.Update(id, file).
		Fields(defaultFields).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("trash drive file %q: %w", id, err)
	}
	return updated, c.invalidate(ctx)
}

func (c *Client) Delete(ctx context.Context, id string, isFolder bool) error {
	if isFolder {
		if err := c.ensureFolder(ctx, id); err != nil {
			return err
		}
	}
	if err := c.service.Files.Delete(id).
		SupportsAllDrives(true).
		Context(ctx).
		Do(); err != nil {
		return fmt.Errorf("delete drive file %q: %w", id, err)
	}
	return c.invalidate(ctx)
}

func (c *Client) Download(ctx context.Context, id, output string) error {
	if output == "" {
		return errors.New("--output is required")
	}
	file, err := c.Get(ctx, id)
	if err != nil {
		return err
	}
	if isGoogleNative(file.MimeType) {
		return fmt.Errorf("%q is a Google-native file; use docs export or zip export behavior", id)
	}
	resp, err := c.service.Files.Get(id).
		Download()
	if err != nil {
		return fmt.Errorf("download drive file %q: %w", id, err)
	}
	defer resp.Body.Close()
	return writeResponseToFile(resp, output)
}

func (c *Client) Export(ctx context.Context, id, mimeType, output string) error {
	if output == "" {
		return errors.New("--output is required")
	}
	resp, err := c.service.Files.Export(id, mimeType).Download()
	if err != nil {
		return fmt.Errorf("export drive file %q: %w", id, err)
	}
	defer resp.Body.Close()
	return writeResponseToFile(resp, output)
}

func (c *Client) Tree(ctx context.Context, opts TreeOptions) (*TreeNode, error) {
	if opts.Parent == "" {
		opts.Parent = "root"
	}
	if opts.Depth <= 0 {
		opts.Depth = 3
	}
	root, err := c.Get(ctx, opts.Parent)
	if err != nil {
		return nil, err
	}
	if root.MimeType != FolderMimeType && opts.Parent != "root" {
		return nil, fmt.Errorf("%q is not a folder", opts.Parent)
	}
	return c.tree(ctx, root, opts, 0)
}

func (c *Client) DownloadZip(ctx context.Context, opts DownloadZipOptions) error {
	if opts.Output == "" {
		return errors.New("--output is required")
	}
	if len(opts.Files) == 0 && len(opts.Folders) == 0 {
		return errors.New("at least one --file or --folder is required")
	}
	if !opts.Flatten {
		opts.PreservePaths = true
	}
	if opts.DocsFormat == "" {
		opts.DocsFormat = "docx"
	}
	if opts.SheetsFormat == "" {
		opts.SheetsFormat = "xlsx"
	}
	if opts.SlidesFormat == "" {
		opts.SlidesFormat = "pptx"
	}
	if err := os.MkdirAll(filepath.Dir(absOrClean(opts.Output)), 0o700); err != nil {
		return fmt.Errorf("create zip output directory: %w", err)
	}
	file, err := os.OpenFile(opts.Output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open zip output: %w", err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	defer zw.Close()

	used := make(map[string]int)
	for _, id := range opts.Files {
		driveFile, err := c.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := c.addFileToZip(ctx, zw, driveFile, safeFilename(driveFile.Name), opts, used); err != nil {
			return err
		}
	}
	for _, id := range opts.Folders {
		folder, err := c.Get(ctx, id)
		if err != nil {
			return err
		}
		if folder.MimeType != FolderMimeType {
			return fmt.Errorf("%q is not a folder", id)
		}
		base := safeFilename(folder.Name)
		if base == "" {
			base = folder.Id
		}
		if err := c.addFolderToZip(ctx, zw, folder, base, opts, used); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ListPermissions(ctx context.Context, fileID string) ([]*drive.Permission, error) {
	resp, err := c.service.Permissions.List(fileID).
		Fields("permissions(id,type,emailAddress,domain,role,displayName,deleted,expirationTime)").
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list permissions for %q: %w", fileID, err)
	}
	return resp.Permissions, nil
}

func (c *Client) CreatePermission(ctx context.Context, opts PermissionCreateOptions) (*drive.Permission, error) {
	if opts.FileID == "" {
		return nil, errors.New("file ID is required")
	}
	if opts.Email == "" {
		return nil, errors.New("email is required")
	}
	if opts.Role == "" {
		opts.Role = "reader"
	}
	perm := &drive.Permission{Type: "user", Role: opts.Role, EmailAddress: opts.Email}
	created, err := c.service.Permissions.Create(opts.FileID, perm).
		SendNotificationEmail(false).
		Fields("id,type,emailAddress,domain,role,displayName,deleted,expirationTime").
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("create permission for %q: %w", opts.FileID, err)
	}
	return created, nil
}

func (c *Client) DeletePermission(ctx context.Context, fileID, permissionID string) error {
	if fileID == "" || permissionID == "" {
		return errors.New("file ID and permission ID are required")
	}
	if err := c.service.Permissions.Delete(fileID, permissionID).
		SupportsAllDrives(true).
		Context(ctx).
		Do(); err != nil {
		return fmt.Errorf("delete permission %q on %q: %w", permissionID, fileID, err)
	}
	return nil
}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (c *Client) tree(ctx context.Context, file *drive.File, opts TreeOptions, depth int) (*TreeNode, error) {
	node := &TreeNode{File: file}
	if depth >= opts.Depth || file.MimeType != FolderMimeType {
		return node, nil
	}
	children, err := c.ListFolder(ctx, ListFolderOptions{
		Parent:   file.Id,
		Type:     opts.Type,
		Trashed:  opts.Trashed,
		PageSize: 1000,
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(children.Files, func(i, j int) bool {
		return strings.ToLower(children.Files[i].Name) < strings.ToLower(children.Files[j].Name)
	})
	for _, child := range children.Files {
		childNode, err := c.tree(ctx, child, opts, depth+1)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, childNode)
	}
	return node, nil
}

func (c *Client) addFolderToZip(ctx context.Context, zw *zip.Writer, folder *drive.File, path string, opts DownloadZipOptions, used map[string]int) error {
	children, err := c.ListFolder(ctx, ListFolderOptions{Parent: folder.Id, PageSize: 1000})
	if err != nil {
		return err
	}
	for _, child := range children.Files {
		childName := safeFilename(child.Name)
		if childName == "" {
			childName = child.Id
		}
		childPath := childName
		if !opts.Flatten {
			childPath = filepath.ToSlash(filepath.Join(path, childName))
		}
		if child.MimeType == FolderMimeType {
			if err := c.addFolderToZip(ctx, zw, child, childPath, opts, used); err != nil {
				return err
			}
			continue
		}
		if err := c.addFileToZip(ctx, zw, child, childPath, opts, used); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) addFileToZip(ctx context.Context, zw *zip.Writer, file *drive.File, path string, opts DownloadZipOptions, used map[string]int) error {
	name := path
	var reader io.ReadCloser
	var err error
	if isGoogleNative(file.MimeType) {
		format, mimeType, ext, err := nativeZipExport(file.MimeType, opts)
		if err != nil {
			return err
		}
		_ = format
		name = ensureExtension(name, ext)
		reader, err = c.exportReader(file.Id, mimeType)
	} else {
		if ext := extensionForNameOrMime(file.Name, file.MimeType); ext != "" {
			name = ensureExtension(name, ext)
		}
		reader, err = c.downloadReader(file.Id)
	}
	if err != nil {
		return err
	}
	defer reader.Close()

	name = disambiguateZipPath(filepath.ToSlash(name), used)
	writer, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create zip entry %q: %w", name, err)
	}
	if _, err := io.Copy(writer, reader); err != nil {
		return fmt.Errorf("write zip entry %q: %w", name, err)
	}
	return nil
}

func (c *Client) downloadReader(id string) (io.ReadCloser, error) {
	resp, err := c.service.Files.Get(id).Download()
	if err != nil {
		return nil, fmt.Errorf("download drive file %q: %w", id, err)
	}
	return resp.Body, nil
}

func (c *Client) exportReader(id, mimeType string) (io.ReadCloser, error) {
	resp, err := c.service.Files.Export(id, mimeType).Download()
	if err != nil {
		return nil, fmt.Errorf("export drive file %q: %w", id, err)
	}
	return resp.Body, nil
}

func (c *Client) ensureFolder(ctx context.Context, id string) error {
	file, err := c.Get(ctx, id)
	if err != nil {
		return err
	}
	if file.MimeType != FolderMimeType {
		return fmt.Errorf("%q is not a folder", id)
	}
	return nil
}

func (c *Client) invalidate(ctx context.Context) error {
	return c.cache.DeleteNamespace(ctx, "drive")
}

func searchQuery(opts SearchOptions) (string, error) {
	if opts.RawQuery != "" {
		return opts.RawQuery, nil
	}
	parts := make([]string, 0)
	if opts.Query != "" {
		parts = append(parts, fmt.Sprintf("name contains '%s'", escapeQuery(opts.Query)))
	}
	if !opts.Trashed {
		parts = append(parts, "trashed=false")
	}
	typeQuery, err := typeFilter(opts.Type)
	if err != nil {
		return "", err
	}
	if typeQuery != "" {
		parts = append(parts, typeQuery)
	}
	if len(parts) == 0 {
		return "trashed=false", nil
	}
	return strings.Join(parts, " and "), nil
}

func typeFilter(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "any":
		return "", nil
	case "folder":
		return fmt.Sprintf("mimeType='%s'", FolderMimeType), nil
	case "file":
		return fmt.Sprintf("mimeType!='%s'", FolderMimeType), nil
	case "google-doc":
		return fmt.Sprintf("mimeType='%s'", GoogleDocMimeType), nil
	case "google-sheet":
		return fmt.Sprintf("mimeType='%s'", GoogleSheetMimeType), nil
	case "google-slide":
		return fmt.Sprintf("mimeType='%s'", GoogleSlideMimeType), nil
	default:
		return "", fmt.Errorf("invalid type %q; valid values: any,file,folder,google-doc,google-sheet,google-slide", value)
	}
}

func escapeQuery(value string) string {
	return strings.ReplaceAll(value, "'", "\\'")
}

func applyUpdateField(file *drive.File, field UpdateField) error {
	switch strings.TrimSpace(field.Field) {
	case "name":
		file.Name = field.Value
	case "description":
		file.Description = field.Value
	case "starred":
		value, err := strconv.ParseBool(field.Value)
		if err != nil {
			return fmt.Errorf("invalid starred value %q: %w", field.Value, err)
		}
		file.Starred = value
		file.ForceSendFields = append(file.ForceSendFields, "Starred")
	default:
		return fmt.Errorf("unsupported update field %q; supported fields: description,name,starred", field.Field)
	}
	return nil
}

func isGoogleNative(mimeType string) bool {
	return strings.HasPrefix(mimeType, "application/vnd.google-apps.")
}

func nativeZipExport(mimeType string, opts DownloadZipOptions) (format, mimeValue, ext string, err error) {
	switch mimeType {
	case GoogleDocMimeType:
		return docExportFormat(opts.DocsFormat)
	case GoogleSheetMimeType:
		return sheetExportFormat(opts.SheetsFormat)
	case GoogleSlideMimeType:
		return slideExportFormat(opts.SlidesFormat)
	case GoogleDrawingMimeType:
		return "pdf", "application/pdf", ".pdf", nil
	default:
		return "", "", "", fmt.Errorf("unsupported Google-native zip export MIME type %q", mimeType)
	}
}

func docExportFormat(format string) (string, string, string, error) {
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
		return "", "", "", fmt.Errorf("invalid docs format %q; valid values: docx,pdf,txt,html", format)
	}
}

func sheetExportFormat(format string) (string, string, string, error) {
	switch format {
	case "", "xlsx":
		return "xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx", nil
	case "csv":
		return "csv", "text/csv", ".csv", nil
	default:
		return "", "", "", fmt.Errorf("invalid sheets format %q; valid values: xlsx,csv", format)
	}
}

func slideExportFormat(format string) (string, string, string, error) {
	switch format {
	case "", "pptx":
		return "pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx", nil
	case "pdf":
		return "pdf", "application/pdf", ".pdf", nil
	default:
		return "", "", "", fmt.Errorf("invalid slides format %q; valid values: pptx,pdf", format)
	}
}

func writeResponseToFile(resp *http.Response, output string) error {
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

func absOrClean(path string) string {
	if path == "" {
		return "."
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "." || name == ".." {
		return ""
	}
	return name
}

func ensureExtension(name, ext string) string {
	if ext == "" || strings.EqualFold(filepath.Ext(name), ext) {
		return name
	}
	return strings.TrimSuffix(name, filepath.Ext(name)) + ext
}

func extensionForNameOrMime(name, mimeType string) string {
	if ext := filepath.Ext(name); ext != "" {
		return ext
	}
	exts, err := mime.ExtensionsByType(mimeType)
	if err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func disambiguateZipPath(path string, used map[string]int) string {
	if used[path] == 0 {
		used[path] = 1
		return path
	}
	used[path]++
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return fmt.Sprintf("%s (%d)%s", base, used[path], ext)
}
