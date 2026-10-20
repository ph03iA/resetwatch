package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// legacySchema is the Resend-era version 1 layout, as older binaries created it.
const legacySchema = `
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE snapshots (provider TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE alerts (
 id TEXT PRIMARY KEY, provider TEXT NOT NULL, account_id TEXT NOT NULL,
 window_id TEXT NOT NULL, reset_at INTEGER NOT NULL, recipient TEXT NOT NULL,
 sender TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
 first_attempt INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL,
 lease_until INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
 updated_at INTEGER NOT NULL, payload TEXT NOT NULL DEFAULT '');
CREATE INDEX alerts_due ON alerts(state, next_attempt);
INSERT INTO settings VALUES('config','{"email":"old@example.test","from":"ResetWatch <onboarding@resend.dev>","providers":"both"}');
PRAGMA user_version = 1;`

type legacyRow struct {
	id, state         string
	attempts          int
	first, reset, due time.Time
	lease             time.Time
}

// legacyState writes a version 1 database and returns a raw connection to it, standing in for an old binary.
func legacyState(t *testing.T, dir string, rows ...legacyRow) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", filepath.Join(dir, "state.db"))
	must(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = raw.Exec(legacySchema)
	must(t, err)
	for _, row := range rows {
		first := int64(0)
		if !row.first.IsZero() {
			first = row.first.Unix()
		}
		_, err := raw.Exec(`INSERT INTO alerts(id,provider,account_id,window_id,reset_at,recipient,sender,state,attempts,first_attempt,next_attempt,lease_until,updated_at,payload)
 VALUES(?,'claude','claude-account','claude:five_hour',?,'old@example.test','ResetWatch <onboarding@resend.dev>',?,?,?,?,?,?,'{"Key":"legacy"}')`,
			row.id, row.reset.Unix(), row.state, row.attempts, first, row.due.Unix(), row.lease.Unix(), row.due.Unix())
		must(t, err)
	}
	return raw
}

func openLegacy(t *testing.T, dir string) *DB {
	t.Helper()
	db, err := Open(dir)
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	if !db.Legacy() {
		t.Fatal("a version 1 database must open as legacy")
	}
	return db
}

func TestLegacyStateWaitsForGmailAndKeepsHistoryReadable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := storeTime()
	legacyState(t, dir, legacyRow{id: "sent-1", state: "sent", attempts: 1, first: now, reset: now, due: now})
	db := openLegacy(t, dir)
	history, err := db.History(ctx, 10)
	if err != nil || len(history) != 1 || history[0].State != "sent" || history[0].To != "old@example.test" {
		t.Fatalf("legacy history must stay readable: %v %+v", err, history)
	}
	if _, err := db.Claim(ctx, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("nothing may be claimed before the Gmail upgrade, got %v", err)
	}
	if err := db.ConfigureSetup(ctx, Config{Providers: "both"}, now); err == nil {
		t.Fatal("setup without a Gmail connection must not upgrade legacy state")
	}
}

func TestGmailUpgradeTransfersUnsentRemindersOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := storeTime()
	reset := now.Add(time.Hour)
	raw := legacyState(t, dir,
		legacyRow{id: "pending", state: "pending", reset: reset, due: reset},
		legacyRow{id: "retry", state: "retry", attempts: 3, first: now.Add(-time.Hour), reset: now.Add(-30 * time.Minute), due: now.Add(time.Minute)},
		legacyRow{id: "expired-lease", state: "sending", attempts: 2, first: now.Add(-2 * time.Hour), reset: now.Add(-time.Hour), due: now, lease: now.Add(-time.Minute)},
		legacyRow{id: "exhausted", state: "retry", attempts: 8, first: now.Add(-time.Hour), reset: now.Add(-time.Hour), due: now},
		legacyRow{id: "too-old", state: "retry", attempts: 1, first: now.Add(-23 * time.Hour), reset: now.Add(-20 * time.Hour), due: now},
		legacyRow{id: "sent", state: "sent", attempts: 1, first: now.Add(-time.Hour), reset: now.Add(-time.Hour), due: now},
	)
	db := openLegacy(t, dir)
	must(t, db.ConnectGmail(ctx, gmailAccount("person"), now))
	if db.Legacy() {
		t.Fatal("connecting Gmail must finish the upgrade")
	}
	alerts := allAlerts(t, db)
	assertTransferred(t, alerts, 3)
	retried := findAlert(t, alerts, "google-person", "retry", 3)
	if !retried.FirstAttempt.Equal(now.Add(-time.Hour)) || !retried.NextAttempt.Equal(now.Add(time.Minute)) {
		t.Fatalf("attempts or retry deadlines were not preserved: %+v", retried)
	}
	assertLegacy(t, db, map[string]string{"pending": "cancelled", "retry": "cancelled", "expired-lease": "cancelled",
		"exhausted": "retry", "too-old": "retry", "sent": "sent"})
	var payload string
	must(t, db.db.QueryRow("SELECT payload FROM notification_alerts WHERE id='sent'").Scan(&payload))
	if payload != `{"Key":"legacy"}` {
		t.Fatal("legacy payloads must be kept unchanged")
	}
	if _, err := raw.Exec("UPDATE alerts SET state='sending' WHERE id='pending'"); err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("an old binary's claim must fail after the upgrade, got %v", err)
	}
}

func TestGmailUpgradeWaitsForAnOlderWatchersDelivery(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := storeTime()
	legacyState(t, dir, legacyRow{id: "live", state: "sending", attempts: 1, first: now, reset: now, due: now, lease: now.Add(time.Minute)})
	db := openLegacy(t, dir)
	if err := db.ConnectGmail(ctx, gmailAccount("person"), now); err == nil || !strings.Contains(err.Error(), "stop every running watcher") {
		t.Fatalf("an active legacy lease must block the upgrade, got %v", err)
	}
	if !db.Legacy() {
		t.Fatal("a rejected upgrade must leave the database at version 1")
	}
	reopened, err := Open(dir)
	must(t, err)
	defer reopened.Close()
	if !reopened.Legacy() {
		t.Fatal("a rejected upgrade must roll back completely")
	}
}

func TestReconnectingTheSameAccountKeepsReminders(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
	before := allAlerts(t, db)
	renewed := gmailAccount("person")
	renewed.Credential = "cred-renewed"
	must(t, db.ConnectGmail(ctx, renewed, now))
	after := allAlerts(t, db)
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].State != "pending" {
		t.Fatalf("reconnecting the same account must keep the reminder identity: %+v", after)
	}
	if config, _ := db.Config(ctx); config.Gmail.Credential != "cred-renewed" {
		t.Fatalf("the new credential reference was not saved: %+v", config.Gmail)
	}
}

func TestSwitchingAccountsAndBackPreservesTheReminder(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
	must(t, setGmail(ctx, db, "other", now))
	if pending := pendingFor(t, db, "google-other"); pending != 1 {
		t.Fatalf("the reminder must move to the new account, got %d", pending)
	}
	must(t, setGmail(ctx, db, "person", now))
	if pendingFor(t, db, "google-person") != 1 || pendingFor(t, db, "google-other") != 0 {
		t.Fatalf("switching back must resume the original reminder: %+v", allAlerts(t, db))
	}
}

func TestDisconnectCancelsUnsentRemindersAfterDelivery(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	due := now.Add(time.Hour)
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, due, 100), now))
	claimed, err := db.Claim(ctx, due)
	must(t, err)
	if err := db.DisconnectGmail(ctx, due); err == nil {
		t.Fatal("disconnecting during a live delivery must wait")
	}
	must(t, db.Finish(ctx, claimed, "retry", "temporary", due, due))
	must(t, db.DisconnectGmail(ctx, due))
	config, _ := db.Config(ctx)
	if config.Gmail.Connected() {
		t.Fatal("Gmail is still connected")
	}
	assertStates(t, db, map[string]int{"cancelled": 1})
	if _, err := db.Claim(ctx, due.Add(time.Hour)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("nothing may be claimed after disconnecting, got %v", err)
	}
}

func TestPauseAppliesOnlyToTheFailedCredential(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	must(t, db.PauseGmail(ctx, "cred-stale", "revoked"))
	if config, _ := db.Config(ctx); config.Gmail.Paused != "" {
		t.Fatal("a failure from an old credential paused the current connection")
	}
	must(t, db.PauseGmail(ctx, "cred-person", "Gmail no longer accepts the saved sign-in"))
	config, _ := db.Config(ctx)
	if config.Gmail.Ready() {
		t.Fatal("the failed credential must pause delivery")
	}
	renewed := gmailAccount("person")
	renewed.Credential = "cred-renewed"
	must(t, db.ConnectGmail(ctx, renewed, storeTime()))
	if config, _ := db.Config(ctx); !config.Gmail.Ready() {
		t.Fatal("reconnecting must clear the pause")
	}
}

func TestReleaseReturnsTheAttempt(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	due := now.Add(time.Hour)
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, due, 100), now))
	claimed, err := db.Claim(ctx, due)
	must(t, err)
	must(t, db.ReleaseDelivery(ctx, claimed, due))
	alert := allAlerts(t, db)[0]
	if alert.State != "retry" || alert.Attempts != 0 || !alert.FirstAttempt.IsZero() {
		t.Fatalf("a released claim must not count as an attempt: %+v", alert)
	}
}

// assertTransferred checks that want reminders now belong to the test Gmail account and are addressed to it.
func assertTransferred(t *testing.T, alerts []Alert, want int) {
	t.Helper()
	moved := 0
	for _, alert := range alerts {
		if alert.Gmail != "google-person" {
			continue
		}
		moved++
		if alert.To != "person@example.test" || alert.From != "person@example.test" {
			t.Fatalf("transferred reminder is not addressed to the Gmail account: %+v", alert)
		}
	}
	if moved != want {
		t.Fatalf("want %d reminders transferred, got %d: %+v", want, moved, alerts)
	}
}

func allAlerts(t *testing.T, db *DB) []Alert {
	t.Helper()
	alerts, err := db.History(context.Background(), 100)
	must(t, err)
	return alerts
}

func findAlert(t *testing.T, alerts []Alert, gmail, state string, attempts int) Alert {
	t.Helper()
	for _, alert := range alerts {
		if alert.Gmail == gmail && alert.State == state && alert.Attempts == attempts {
			return alert
		}
	}
	t.Fatalf("no %s reminder with %d attempts for %s: %+v", state, attempts, gmail, alerts)
	return Alert{}
}

func pendingFor(t *testing.T, db *DB, gmail string) int {
	t.Helper()
	count := 0
	for _, alert := range allAlerts(t, db) {
		if alert.Gmail == gmail && alert.State == "pending" {
			count++
		}
	}
	return count
}

func assertLegacy(t *testing.T, db *DB, want map[string]string) {
	t.Helper()
	for id, state := range want {
		var got string
		must(t, db.db.QueryRow("SELECT state FROM notification_alerts WHERE id=?", id).Scan(&got))
		if got != state {
			t.Errorf("legacy reminder %s: state %s, want %s", id, got, state)
		}
	}
}
