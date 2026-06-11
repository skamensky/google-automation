package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	Dir      string
	TTL      time.Duration
	Disabled bool
}

type SQLStore interface {
	Open(context.Context) (*sql.DB, error)
}

type JSONStore interface {
	GetJSON(context.Context, string, []string, any) (bool, error)
	SetJSON(context.Context, string, []string, any) error
	DeleteNamespace(context.Context, string) error
}

type Store struct {
	cfg Config
}

func NewStore(cfg Config) Store {
	return Store{cfg: cfg}
}

func (s Store) Open(ctx context.Context) (*sql.DB, error) {
	if s.cfg.Disabled {
		return nil, ErrDisabled
	}
	if s.cfg.Dir == "" {
		return nil, errors.New("cache directory is required")
	}
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	dbPath := filepath.Join(s.cfg.Dir, "cache.sqlite")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open cache database: %w", err)
	}
	if err := os.Chmod(dbPath, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		db.Close()
		return nil, fmt.Errorf("set cache database permissions: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure cache database: %w", err)
	}
	if err := s.migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s Store) GetJSON(ctx context.Context, namespace string, keyParts []string, target any) (bool, error) {
	if s.cfg.Disabled {
		return false, nil
	}

	db, err := s.Open(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()

	var payload []byte
	var expiresAt sql.NullInt64
	err = db.QueryRowContext(ctx, `
		SELECT payload, expires_at_unix_ms
		FROM cache_entries
		WHERE namespace = ? AND cache_key = ?
	`, namespace, cacheKey(keyParts)).Scan(&payload, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query cache entry: %w", err)
	}
	if expiresAt.Valid && time.Now().UnixMilli() > expiresAt.Int64 {
		return false, nil
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return false, fmt.Errorf("decode cached value: %w", err)
	}
	return true, nil
}

func (s Store) SetJSON(ctx context.Context, namespace string, keyParts []string, value any) error {
	if s.cfg.Disabled {
		return nil
	}

	db, err := s.Open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode cached value: %w", err)
	}

	var expiresAt any
	if s.cfg.TTL > 0 {
		expiresAt = time.Now().Add(s.cfg.TTL).UnixMilli()
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO cache_entries(namespace, cache_key, payload, expires_at_unix_ms, updated_at_unix_ms)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(namespace, cache_key) DO UPDATE SET
			payload = excluded.payload,
			expires_at_unix_ms = excluded.expires_at_unix_ms,
			updated_at_unix_ms = excluded.updated_at_unix_ms
	`, namespace, cacheKey(keyParts), payload, expiresAt, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("write cache entry: %w", err)
	}
	return nil
}

func (s Store) DeleteNamespace(ctx context.Context, namespace string) error {
	if s.cfg.Disabled {
		return nil
	}

	db, err := s.Open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `DELETE FROM cache_entries WHERE namespace = ?`, namespace); err != nil {
		return fmt.Errorf("delete cache namespace %q: %w", namespace, err)
	}
	return nil
}

func (s Store) migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cache_entries (
			namespace TEXT NOT NULL,
			cache_key TEXT NOT NULL,
			payload BLOB NOT NULL,
			expires_at_unix_ms INTEGER,
			updated_at_unix_ms INTEGER NOT NULL,
			PRIMARY KEY(namespace, cache_key)
		);

		CREATE TABLE IF NOT EXISTS gmail_messages (
			account_email TEXT NOT NULL,
			message_id TEXT NOT NULL,
			thread_id TEXT NOT NULL,
			internal_date_unix_ms INTEGER NOT NULL,
			from_header TEXT NOT NULL DEFAULT '',
			to_header TEXT NOT NULL DEFAULT '',
			cc_header TEXT NOT NULL DEFAULT '',
			bcc_header TEXT NOT NULL DEFAULT '',
			reply_to_header TEXT NOT NULL DEFAULT '',
			sender_header TEXT NOT NULL DEFAULT '',
			subject TEXT NOT NULL DEFAULT '',
			snippet TEXT NOT NULL DEFAULT '',
			raw_metadata_json BLOB NOT NULL,
			ingested_at_unix_ms INTEGER NOT NULL,
			PRIMARY KEY(account_email, message_id)
		);

		CREATE INDEX IF NOT EXISTS idx_gmail_messages_account_date
			ON gmail_messages(account_email, internal_date_unix_ms);

		CREATE TABLE IF NOT EXISTS gmail_threads (
			account_email TEXT NOT NULL,
			thread_id TEXT NOT NULL,
			message_count INTEGER NOT NULL,
			latest_internal_date_unix_ms INTEGER NOT NULL,
			snippet TEXT NOT NULL DEFAULT '',
			ingested_at_unix_ms INTEGER NOT NULL,
			PRIMARY KEY(account_email, thread_id)
		);

		CREATE INDEX IF NOT EXISTS idx_gmail_threads_account_latest
			ON gmail_threads(account_email, latest_internal_date_unix_ms);

		CREATE TABLE IF NOT EXISTS gmail_attachments (
			account_email TEXT NOT NULL,
			message_id TEXT NOT NULL,
			thread_id TEXT NOT NULL,
			part_id TEXT NOT NULL,
			attachment_id TEXT NOT NULL,
			filename TEXT NOT NULL DEFAULT '',
			mime_type TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			ingested_at_unix_ms INTEGER NOT NULL,
			PRIMARY KEY(account_email, message_id, part_id, attachment_id)
		);

		CREATE INDEX IF NOT EXISTS idx_gmail_attachments_account_message
			ON gmail_attachments(account_email, message_id);

		CREATE TABLE IF NOT EXISTS gmail_address_interactions (
			account_email TEXT NOT NULL,
			email TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			first_seen_unix_ms INTEGER NOT NULL,
			last_seen_unix_ms INTEGER NOT NULL,
			message_count INTEGER NOT NULL,
			headers TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(account_email, email)
		);

		CREATE TABLE IF NOT EXISTS gmail_ingest_state (
			account_email TEXT PRIMARY KEY,
			full_scan_complete INTEGER NOT NULL DEFAULT 0,
			next_page_token TEXT NOT NULL DEFAULT '',
			last_incremental_after TEXT NOT NULL DEFAULT '',
			updated_at_unix_ms INTEGER NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("migrate cache database: %w", err)
	}
	return nil
}

var ErrDisabled = errors.New("cache disabled")

func cacheKey(parts []string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
