package googleaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/skamensky/google-automation/internal/googleauth"
	"golang.org/x/oauth2"
	calendar "google.golang.org/api/calendar/v3"
	docs "google.golang.org/api/docs/v1"
	drive "google.golang.org/api/drive/v3"
	forms "google.golang.org/api/forms/v1"
	gmail "google.golang.org/api/gmail/v1"
	people "google.golang.org/api/people/v1"
	script "google.golang.org/api/script/v1"
	sheets "google.golang.org/api/sheets/v4"
	slides "google.golang.org/api/slides/v1"
	tasks "google.golang.org/api/tasks/v1"
)

const activeFileName = "active"

type Config struct {
	CredentialsFile string
	AccountsDir     string
	NoBrowser       bool
}

type Account struct {
	Email  string
	Active bool
	Path   string
}

type userInfo struct {
	Email string `json:"email"`
}

func Login(ctx context.Context, cfg Config) (Account, error) {
	if cfg.AccountsDir == "" {
		return Account{}, errors.New("accounts directory is required")
	}

	tmpTokenFile := filepath.Join(cfg.AccountsDir, ".login-token.json")
	token, err := googleauth.Login(ctx, googleauth.Config{
		CredentialsFile: cfg.CredentialsFile,
		TokenFile:       tmpTokenFile,
		Scopes:          Scopes(),
		NoBrowser:       cfg.NoBrowser,
	})
	if err != nil {
		return Account{}, err
	}
	defer os.Remove(tmpTokenFile)

	httpClient, err := googleauth.ClientFromToken(ctx, googleauth.Config{
		CredentialsFile: cfg.CredentialsFile,
		Scopes:          Scopes(),
	}, token)
	if err != nil {
		return Account{}, err
	}

	email, err := fetchEmail(ctx, httpClient)
	if err != nil {
		return Account{}, err
	}

	path := TokenPath(cfg.AccountsDir, email)
	if err := saveToken(path, token); err != nil {
		return Account{}, err
	}
	if err := Activate(cfg.AccountsDir, email); err != nil {
		return Account{}, err
	}

	return Account{Email: email, Active: true, Path: path}, nil
}

func List(accountsDir string) ([]Account, error) {
	activeEmail, err := ActiveEmail(accountsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	entries, err := os.ReadDir(accountsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read accounts directory: %w", err)
	}

	accounts := make([]Account, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		email := strings.TrimSuffix(entry.Name(), ".json")
		email = strings.ReplaceAll(email, "_at_", "@")
		email = strings.ReplaceAll(email, "_dot_", ".")
		path := filepath.Join(accountsDir, entry.Name())
		accounts = append(accounts, Account{
			Email:  email,
			Active: strings.EqualFold(email, activeEmail),
			Path:   path,
		})
	}

	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].Email < accounts[j].Email
	})
	return accounts, nil
}

func Activate(accountsDir, email string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return errors.New("email is required")
	}
	path := TokenPath(accountsDir, email)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("account %q is not logged in", email)
		}
		return fmt.Errorf("stat account token: %w", err)
	}

	if err := os.MkdirAll(accountsDir, 0o700); err != nil {
		return fmt.Errorf("create accounts directory: %w", err)
	}
	activePath := filepath.Join(accountsDir, activeFileName)
	if err := os.WriteFile(activePath, []byte(email+"\n"), 0o600); err != nil {
		return fmt.Errorf("write active account: %w", err)
	}
	return nil
}

func ActiveTokenPath(accountsDir string) (string, error) {
	email, err := ActiveEmail(accountsDir)
	if err != nil {
		return "", err
	}
	path := TokenPath(accountsDir, email)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("active account %q token is missing", email)
		}
		return "", fmt.Errorf("stat active account token: %w", err)
	}
	return path, nil
}

func ClearActive(accountsDir string) error {
	activePath := filepath.Join(accountsDir, activeFileName)
	if err := os.Remove(activePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove active account: %w", err)
	}
	return nil
}

func ActiveEmail(accountsDir string) (string, error) {
	activePath := filepath.Join(accountsDir, activeFileName)
	data, err := os.ReadFile(activePath)
	if err != nil {
		return "", err
	}
	email := strings.TrimSpace(string(data))
	if email == "" {
		return "", errors.New("active account is empty")
	}
	return email, nil
}

func TokenPath(accountsDir, email string) string {
	return filepath.Join(accountsDir, safeEmail(email)+".json")
}

func Scopes() []string {
	scopes := []string{
		calendar.CalendarScope,
		docs.DocumentsScope,
		drive.DriveScope,
		forms.FormsBodyScope,
		forms.FormsResponsesReadonlyScope,
		gmail.MailGoogleComScope,
		people.ContactsScope,
		script.ScriptDeploymentsScope,
		script.ScriptMetricsScope,
		script.ScriptProcessesScope,
		script.ScriptProjectsScope,
		sheets.SpreadsheetsScope,
		slides.PresentationsScope,
		tasks.TasksScope,
		people.UserinfoEmailScope,
		people.UserinfoProfileScope,
	}
	sort.Strings(scopes)
	return scopes
}

func fetchEmail(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", fmt.Errorf("create userinfo request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch userinfo: status %s", resp.Status)
	}

	var info userInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("decode userinfo: %w", err)
	}
	info.Email = strings.TrimSpace(strings.ToLower(info.Email))
	if info.Email == "" {
		return "", errors.New("Google userinfo did not return an email address")
	}
	return info.Email, nil
}

func saveToken(path string, token *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create accounts directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open account token: %w", err)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(token); err != nil {
		return fmt.Errorf("write account token: %w", err)
	}
	return nil
}

func safeEmail(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	email = strings.ReplaceAll(email, "@", "_at_")
	email = strings.ReplaceAll(email, ".", "_dot_")
	re := regexp.MustCompile(`[^a-z0-9_+\-]`)
	return re.ReplaceAllString(email, "_")
}
