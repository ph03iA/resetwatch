package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"resetwatch/internal/quota"

	_ "modernc.org/sqlite"
)

// DB is the state database. legacy marks a version 1 database from the Resend era: its reminders stay
// readable but nothing is delivered until connecting Gmail upgrades it.
type DB struct {
	db     *sql.DB
	legacy bool
}

// ErrUpgradeRequired means the state predates Gmail delivery and must be upgraded by connecting Gmail.
var ErrUpgradeRequired = errors.New("ResetWatch now sends reminders with your Gmail account; stop any older watcher, then run resetwatch setup to connect Gmail and carry your pending reminders over")

type Config struct {
	CodexBin  string       `json:"codex_bin,omitempty"`
	Providers string       `json:"providers,omitempty"`
	Gmail     GmailAccount `json:"gmail"`
	// LegacyTo and LegacyFrom are the Resend-era recipient and sender, kept only in state that predates Gmail.
	LegacyTo   string `json:"email,omitempty"`
	LegacyFrom string `json:"from,omitempty"`
}

// GmailAccount is the connected Google account. Reminders go from it to itself.
type GmailAccount struct {
	Address    string `json:"address,omitempty"`
	Subject    string `json:"subject,omitempty"`    // Google's stable account ID
	Credential string `json:"credential,omitempty"` // names the refresh token in the system credential store
	Paused     string `json:"paused,omitempty"`     // why delivery waits for reconnection
}

func (g GmailAccount) Connected() bool {
	return g.Subject != "" && g.Address != "" && g.Credential != ""
}

// Ready reports whether reminders can be delivered now.
func (g GmailAccount) Ready() bool { return g.Connected() && g.Paused == "" }

type Alert struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	AccountID    string    `json:"-"`
	Window       string    `json:"window"`
	ResetAt      time.Time `json:"reset_at"`
	To           string    `json:"email"`
	From         string    `json:"from"`
	Gmail        string    `json:"-"` // Google account ID the reminder is bound to; empty for Resend-era reminders
	State        string    `json:"state"`
	Attempts     int       `json:"attempts"`
	FirstAttempt time.Time `json:"first_attempt,omitempty"`
	NextAttempt  time.Time `json:"next_attempt"`
	LastError    string    `json:"last_error,omitempty"`
}

// Version 2 renamed the reminder table to notification_alerts and bound each reminder to a Gmail account.
// The new name keeps an older binary that is still running from claiming Gmail reminders.
const schema = `
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS snapshots (provider TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS notification_alerts (
 id TEXT PRIMARY KEY, provider TEXT NOT NULL, account_id TEXT NOT NULL,
 window_id TEXT NOT NULL, reset_at INTEGER NOT NULL, recipient TEXT NOT NULL,
 sender TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
 first_attempt INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL,
 lease_until INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
 updated_at INTEGER NOT NULL, payload TEXT NOT NULL DEFAULT '', gmail_account TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS notification_alerts_due ON notification_alerts(state, next_attempt);
PRAGMA user_version = 2;`

func Open(dir string) (*DB, error) {
	if err := prepareDirectory(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "state.db")
	if err := prepareFile(path); err != nil {
		return nil, err
	}
	fileURL := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}
	q := url.Values{"_pragma": {"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)"}, "_txlock": {"immediate"}}
	fileURL.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", fileURL.String())
	if err != nil {
		return nil, fmt.Errorf("open state database: %w", err)
	}
	db.SetMaxOpenConns(1)
	return initialize(db)
}

func prepareDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("state directory must be a real directory")
	}
	return os.Chmod(dir, 0700)
}

func prepareFile(path string) error {
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		return errors.New("state database must be a regular file")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("create state database: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func initialize(db *sql.DB) (*DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version > 2 {
		db.Close()
		return nil, errors.New("state database is from a newer ResetWatch; upgrade this binary")
	}
	if version == 1 {
		return &DB{db: db, legacy: true}, nil // upgraded only by connecting Gmail
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize state database: %w", err)
	}
	return &DB{db: db}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// Legacy reports whether the state still awaits the Gmail upgrade.
func (d *DB) Legacy() bool { return d.legacy }

func (d *DB) Config(ctx context.Context) (Config, error) {
	return loadConfig(ctx, d.db)
}

func loadConfig(ctx context.Context, db stateQueries) (Config, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='config'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	err = json.Unmarshal([]byte(value), &config)
	return config, err
}

func (d *DB) SaveConfig(ctx context.Context, config Config) error {
	return saveConfig(ctx, d.db, config)
}

func saveConfig(ctx context.Context, db stateQueries, config Config) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('config',?)
 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(data))
	return err
}

func (d *DB) Snapshot(ctx context.Context, provider string) (quota.Snapshot, error) {
	return loadSnapshot(ctx, d.db, provider)
}

func loadSnapshot(ctx context.Context, db stateQueries, provider string) (quota.Snapshot, error) {
	var data string
	err := db.QueryRowContext(ctx, "SELECT data FROM snapshots WHERE provider=?", provider).Scan(&data)
	if err != nil {
		return quota.Snapshot{}, err
	}
	var snapshot quota.Snapshot
	err = json.Unmarshal([]byte(data), &snapshot)
	return snapshot, err
}

func (d *DB) SaveSnapshot(ctx context.Context, snapshot quota.Snapshot) error {
	return saveSnapshot(ctx, d.db, snapshot)
}

func saveSnapshot(ctx context.Context, db stateQueries, snapshot quota.Snapshot) error {
	if err := quota.ValidateSnapshot(snapshot); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO snapshots(provider,data) VALUES(?,?)
 ON CONFLICT(provider) DO UPDATE SET data=excluded.data`, snapshot.Provider, string(data))
	return err
}
