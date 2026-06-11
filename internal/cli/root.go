package cli

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/skamensky/google-automation/internal/cache"
	"github.com/skamensky/google-automation/internal/googleaccounts"
	"github.com/skamensky/google-automation/internal/googlecalendar"
	"github.com/skamensky/google-automation/internal/googlecontacts"
	"github.com/skamensky/google-automation/internal/googledocs"
	"github.com/skamensky/google-automation/internal/googledrive"
	"github.com/skamensky/google-automation/internal/googlegmail"
	"github.com/skamensky/google-automation/internal/googlesheets"
	"github.com/skamensky/google-automation/internal/googleslides"
	"github.com/skamensky/google-automation/internal/googletasks"
	"github.com/spf13/cobra"
)

type appConfig struct {
	credentialsFile string
	tokenFile       string
	tokenFileSet    bool
	accountsDir     string
	jsonOutput      bool
	noBrowser       bool
	noCache         bool
	cacheDir        string
	cacheTTL        time.Duration
}

func NewRootCommand() *cobra.Command {
	cfg := &appConfig{
		credentialsFile: defaultCredentialsFile(),
		tokenFile:       defaultTokenFile(),
		accountsDir:     defaultAccountsDir(),
		cacheDir:        defaultCacheDir(),
		cacheTTL:        2 * time.Minute,
	}

	cmd := &cobra.Command{
		Use:           "google-automation",
		Short:         "A personal automation CLI for Google services",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&cfg.credentialsFile, "credentials-file", cfg.credentialsFile, "OAuth client secret JSON file")
	cmd.PersistentFlags().StringVar(&cfg.tokenFile, "token-file", cfg.tokenFile, "OAuth token cache JSON file")
	cmd.PersistentFlags().StringVar(&cfg.accountsDir, "accounts-dir", cfg.accountsDir, "directory for named Google account tokens")
	cmd.PersistentFlags().BoolVar(&cfg.jsonOutput, "json", false, "print machine-readable JSON")
	cmd.PersistentFlags().BoolVar(&cfg.noBrowser, "no-browser", false, "print OAuth URL instead of opening a browser automatically")
	cmd.PersistentFlags().BoolVar(&cfg.noCache, "no-cache", false, "disable local disk API response caching")
	cmd.PersistentFlags().StringVar(&cfg.cacheDir, "cache-dir", cfg.cacheDir, "local disk cache directory")
	cmd.PersistentFlags().DurationVar(&cfg.cacheTTL, "cache-ttl", cfg.cacheTTL, "local disk cache expiration")
	cmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		tokenFlag := cmd.Flag("token-file")
		cfg.tokenFileSet = tokenFlag != nil && tokenFlag.Changed
		if cfg.tokenFileSet {
			return nil
		}
		activeToken, err := googleaccounts.ActiveTokenPath(cfg.accountsDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		cfg.tokenFile = activeToken
		return nil
	}

	cmd.AddCommand(newAuthCommand(cfg))
	cmd.AddCommand(newCalendarCommand(cfg))
	cmd.AddCommand(newContactsCommand(cfg))
	cmd.AddCommand(newDocsCommand(cfg))
	cmd.AddCommand(newDriveCommand(cfg))
	cmd.AddCommand(newGmailCommand(cfg))
	cmd.AddCommand(newSheetsCommand(cfg))
	cmd.AddCommand(newSlidesCommand(cfg))
	cmd.AddCommand(newTasksCommand(cfg))

	return cmd
}

func (cfg *appConfig) calendarConfig() googlecalendar.Config {
	return googlecalendar.Config{
		CredentialsFile: cfg.credentialsFile,
		TokenFile:       cfg.tokenFile,
		NoBrowser:       cfg.noBrowser,
		Cache: cache.Config{
			Dir:      cfg.cacheDir,
			TTL:      cfg.cacheTTL,
			Disabled: cfg.noCache,
		},
	}
}

func (cfg *appConfig) gmailConfig() googlegmail.Config {
	return googlegmail.Config{
		CredentialsFile: cfg.credentialsFile,
		TokenFile:       cfg.tokenFile,
		NoBrowser:       cfg.noBrowser,
		Cache: cache.Config{
			Dir:      cfg.cacheDir,
			TTL:      cfg.cacheTTL,
			Disabled: cfg.noCache,
		},
	}
}

func (cfg *appConfig) driveConfig() googledrive.Config {
	return googledrive.Config{
		CredentialsFile: cfg.credentialsFile,
		TokenFile:       cfg.tokenFile,
		NoBrowser:       cfg.noBrowser,
		Cache: cache.Config{
			Dir:      cfg.cacheDir,
			TTL:      cfg.cacheTTL,
			Disabled: cfg.noCache,
		},
	}
}

func (cfg *appConfig) docsConfig() googledocs.Config {
	return googledocs.Config{
		CredentialsFile: cfg.credentialsFile,
		TokenFile:       cfg.tokenFile,
		NoBrowser:       cfg.noBrowser,
		Cache: cache.Config{
			Dir:      cfg.cacheDir,
			TTL:      cfg.cacheTTL,
			Disabled: cfg.noCache,
		},
	}
}

func (cfg *appConfig) sheetsConfig() googlesheets.Config {
	return googlesheets.Config{
		CredentialsFile: cfg.credentialsFile,
		TokenFile:       cfg.tokenFile,
		NoBrowser:       cfg.noBrowser,
		Cache: cache.Config{
			Dir:      cfg.cacheDir,
			TTL:      cfg.cacheTTL,
			Disabled: cfg.noCache,
		},
	}
}

func (cfg *appConfig) slidesConfig() googleslides.Config {
	return googleslides.Config{
		CredentialsFile: cfg.credentialsFile,
		TokenFile:       cfg.tokenFile,
		NoBrowser:       cfg.noBrowser,
		Cache: cache.Config{
			Dir:      cfg.cacheDir,
			TTL:      cfg.cacheTTL,
			Disabled: cfg.noCache,
		},
	}
}

func (cfg *appConfig) tasksConfig() googletasks.Config {
	return googletasks.Config{
		CredentialsFile: cfg.credentialsFile,
		TokenFile:       cfg.tokenFile,
		NoBrowser:       cfg.noBrowser,
		Cache: cache.Config{
			Dir:      cfg.cacheDir,
			TTL:      cfg.cacheTTL,
			Disabled: cfg.noCache,
		},
	}
}

func (cfg *appConfig) accountsConfig() googleaccounts.Config {
	return googleaccounts.Config{
		CredentialsFile: cfg.credentialsFile,
		AccountsDir:     cfg.accountsDir,
		NoBrowser:       cfg.noBrowser,
	}
}

func (cfg *appConfig) contactsConfig() googlecontacts.Config {
	return googlecontacts.Config{
		CredentialsFile: cfg.credentialsFile,
		TokenFile:       cfg.tokenFile,
		NoBrowser:       cfg.noBrowser,
		Cache: cache.Config{
			Dir:      cfg.cacheDir,
			TTL:      cfg.cacheTTL,
			Disabled: cfg.noCache,
		},
	}
}

func defaultCredentialsFile() string {
	if value := os.Getenv("GOOGLE_AUTOMATION_CREDENTIALS_FILE"); value != "" {
		return value
	}
	return filepath.Join(defaultStateDir(), "client_secret.json")
}

func defaultTokenFile() string {
	if value := os.Getenv("GOOGLE_AUTOMATION_TOKEN_FILE"); value != "" {
		return value
	}
	return filepath.Join(defaultStateDir(), "token.json")
}

func defaultAccountsDir() string {
	if value := os.Getenv("GOOGLE_AUTOMATION_ACCOUNTS_DIR"); value != "" {
		return value
	}
	return filepath.Join(defaultStateDir(), "accounts")
}

func defaultCacheDir() string {
	if value := os.Getenv("GOOGLE_AUTOMATION_CACHE_DIR"); value != "" {
		return value
	}
	return filepath.Join(defaultStateDir(), "cache")
}

func defaultStateDir() string {
	if value := os.Getenv("GOOGLE_AUTOMATION_STATE_DIR"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".automation", "google")
	}
	return filepath.Join(home, ".automation", "google")
}
