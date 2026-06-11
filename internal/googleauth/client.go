package googleauth

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Config struct {
	CredentialsFile string
	TokenFile       string
	Scopes          []string
	ForceRefresh    bool
	NoBrowser       bool
}

func Login(ctx context.Context, cfg Config) (*oauth2.Token, error) {
	cfg.ForceRefresh = true
	return Token(ctx, cfg)
}

func NewClient(ctx context.Context, cfg Config) (*http.Client, error) {
	token, oauthConfig, err := tokenAndConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return oauthConfig.Client(ctx, token), nil
}

func Token(ctx context.Context, cfg Config) (*oauth2.Token, error) {
	token, _, err := tokenAndConfig(ctx, cfg)
	return token, err
}

func ClientFromToken(ctx context.Context, cfg Config, token *oauth2.Token) (*http.Client, error) {
	credentials, err := os.ReadFile(cfg.CredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("read credentials file %q: %w", cfg.CredentialsFile, err)
	}

	oauthConfig, err := google.ConfigFromJSON(credentials, cfg.Scopes...)
	if err != nil {
		return nil, fmt.Errorf("parse OAuth credentials: %w", err)
	}

	return oauthConfig.Client(ctx, token), nil
}

func tokenAndConfig(ctx context.Context, cfg Config) (*oauth2.Token, *oauth2.Config, error) {
	if cfg.CredentialsFile == "" {
		return nil, nil, errors.New("credentials file is required")
	}
	if cfg.TokenFile == "" {
		return nil, nil, errors.New("token file is required")
	}

	credentials, err := os.ReadFile(cfg.CredentialsFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read credentials file %q: %w", cfg.CredentialsFile, err)
	}

	oauthConfig, err := google.ConfigFromJSON(credentials, cfg.Scopes...)
	if err != nil {
		return nil, nil, fmt.Errorf("parse OAuth credentials: %w", err)
	}

	token, err := tokenFromFile(cfg.TokenFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if cfg.ForceRefresh || needsBrowserToken(token) {
		token, err = tokenFromWeb(ctx, oauthConfig, cfg.NoBrowser)
		if err != nil {
			return nil, nil, err
		}
		if err := saveToken(cfg.TokenFile, token); err != nil {
			return nil, nil, err
		}
	}

	return token, oauthConfig, nil
}

func needsBrowserToken(token *oauth2.Token) bool {
	if token == nil {
		return true
	}
	return token.AccessToken == "" && token.RefreshToken == ""
}

func tokenFromFile(path string) (*oauth2.Token, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	token := &oauth2.Token{}
	if err := json.NewDecoder(file).Decode(token); err != nil {
		return nil, fmt.Errorf("decode token file %q: %w", path, err)
	}

	return token, nil
}

func tokenFromWeb(ctx context.Context, config *oauth2.Config, noBrowser bool) (*oauth2.Token, error) {
	if !noBrowser {
		token, err := tokenFromBrowser(ctx, config)
		if err == nil {
			return token, nil
		}

		fmt.Fprintf(os.Stderr, "Browser OAuth flow failed: %v\n", err)
	}

	authURL := config.AuthCodeURL(randomState(), oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Printf("Open this URL in your browser, approve access, then paste the authorization code:\n\n%s\n\nCode: ", authURL)

	code, err := readLine(ctx)
	if err != nil {
		return nil, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("authorization code is empty")
	}

	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	return token, nil
}

func tokenFromBrowser(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start local OAuth callback server: %w", err)
	}
	defer listener.Close()

	state := randomState()
	config.RedirectURL = fmt.Sprintf("http://localhost:%d/callback", listener.Addr().(*net.TCPAddr).Port)

	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
	}
	defer server.Shutdown(context.Background())

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid OAuth state", http.StatusBadRequest)
			errCh <- errors.New("invalid OAuth state returned by provider")
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			http.Error(w, "OAuth error: "+errMsg, http.StatusBadRequest)
			errCh <- fmt.Errorf("OAuth provider returned error: %s", errMsg)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			errCh <- errors.New("OAuth callback did not include an authorization code")
			return
		}

		io.WriteString(w, "Authorization complete. You can close this tab and return to the terminal.\n")
		codeCh <- code
	})
	server.Handler = mux

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Printf("Open this URL in your browser to authorize google-automation:\n\n%s\n\n", authURL)
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically: %v\n", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var code string
	select {
	case <-waitCtx.Done():
		return nil, waitCtx.Err()
	case err := <-errCh:
		return nil, err
	case code = <-codeCh:
	}

	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	return token, nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		if _, err := exec.LookPath("wslview"); err == nil {
			return exec.Command("wslview", url).Start()
		}
		if _, err := exec.LookPath("powershell.exe"); err == nil {
			return exec.Command("powershell.exe", "-NoProfile", "-Command", "Start-Process", url).Start()
		}
		return exec.Command("xdg-open", url).Start()
	}
}

func randomState() string {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func readLine(ctx context.Context) (string, error) {
	type result struct {
		value string
		err   error
	}

	ch := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		value, err := reader.ReadString('\n')
		ch <- result{value: value, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-ch:
		if result.err != nil {
			return "", fmt.Errorf("read authorization code: %w", result.err)
		}
		return result.value, nil
	}
}

func saveToken(path string, token *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open token file %q: %w", path, err)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(token); err != nil {
		return fmt.Errorf("write token file %q: %w", path, err)
	}

	return nil
}
