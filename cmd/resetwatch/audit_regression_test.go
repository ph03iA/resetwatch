package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

func TestNotifyFirstConfigurationAfterPartialClaudeReading(t *testing.T) {
	app, _ := testApplication(t)
	reset := time.Now().Add(time.Hour).Unix()
	claudeLine(t, app, "session", map[string][2]int64{"five_hour": {100, reset}})
	claudeLine(t, app, "session", map[string][2]int64{"seven_day": {40, reset + 86400}})
	runOK(t, app, "notify", "--email", "first@example.test", "--from", "alerts@example.test")
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
}

func TestNotifyAddressRoundTripKeepsDueClaudeReminder(t *testing.T) {
	app, _ := testApplication(t)
	runOK(t, app, "notify", "--email", "first@example.test", "--from", "alerts@example.test")
	now := time.Now().UTC()
	db := auditStateDB(t, app)
	seedDueClaudeReminder(t, db, now)
	runOK(t, app, "notify", "--email", "second@example.test", "--from", "alerts@example.test")
	runOK(t, app, "notify", "--email", "first@example.test", "--from", "alerts@example.test")
	alert := onlyPending(t, claudeAlerts(t, app), "claude:five_hour", now.Add(-time.Hour).Unix())
	if alert.To != "first@example.test" || alert.NextAttempt.After(now) {
		t.Fatalf("returning to the first address lost the due reminder: %#v", alert)
	}
}

func TestNotifyAddressChangeDoesNotResendCompletedClaudeReset(t *testing.T) {
	app, _ := testApplication(t)
	runOK(t, app, "notify", "--email", "first@example.test", "--from", "alerts@example.test")
	now := time.Now().UTC()
	db := auditStateDB(t, app)
	seedDueClaudeReminder(t, db, now)
	alert, err := db.Claim(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Finish(context.Background(), alert, "sent", "", now, now); err != nil {
		t.Fatal(err)
	}
	runOK(t, app, "notify", "--email", "second@example.test", "--from", "alerts@example.test")
	alerts := claudeAlerts(t, app)
	if len(alerts) != 1 || alerts[0].State != "sent" {
		t.Fatalf("changing email rearmed a completed reset: %#v", alerts)
	}
}

func TestNotifyAddressChangeDuringDeliveryLeavesConfigurationIntact(t *testing.T) {
	app, _ := testApplication(t)
	runOK(t, app, "notify", "--email", "first@example.test", "--from", "alerts@example.test")
	now := time.Now().UTC()
	db := auditStateDB(t, app)
	seedDueClaudeReminder(t, db, now)
	if _, err := db.Claim(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if err := app.run(context.Background(), []string{"notify", "--email", "second@example.test", "--from", "alerts@example.test"}); err == nil {
		t.Fatal("changing addresses during a live delivery must wait for completion")
	}
	config, err := db.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.To != "first@example.test" {
		t.Fatal("a rejected configuration change still replaced the recipient")
	}
	runOK(t, app, "notify", "--email", "first@example.test", "--from", "alerts@example.test")
	alerts := claudeAlerts(t, app)
	if len(alerts) != 1 || alerts[0].State != "sending" {
		t.Fatalf("unchanged settings disturbed the live delivery: %#v", alerts)
	}
}

func TestClaudeReadingRecoversAfterSchedulingFailure(t *testing.T) {
	app, _ := testApplication(t)
	runOK(t, app, "notify", "--email", "person@example.test", "--from", "alerts@example.test")
	writer, err := sql.Open("sqlite", filepath.Join(app.getenv("RESETWATCH_HOME"), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	_, err = writer.Exec(`CREATE TRIGGER reject_test_alert BEFORE INSERT ON alerts BEGIN SELECT RAISE(FAIL, 'synthetic scheduling failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add(time.Hour).Unix()
	app.claude = fakeClaude{snapshot: claudeReading(map[string][2]int64{"five_hour": {100, reset}})}
	if err := app.run(context.Background(), []string{"status", "--provider", "claude", "--refresh"}); err == nil {
		t.Fatal("expected the synthetic scheduling failure")
	}
	if _, err := writer.Exec("DROP TRIGGER reject_test_alert"); err != nil {
		t.Fatal(err)
	}
	runOK(t, app, "status", "--provider", "claude", "--refresh")
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
}

func TestStatusKeepsCodexJSONObjectAndSelectsClaude(t *testing.T) {
	app, _ := testApplication(t)
	now := time.Now().UTC()
	claudeLine(t, app, "session", map[string][2]int64{"five_hour": {40, now.Add(time.Hour).Unix()}})
	db := auditStateDB(t, app)
	codex := quota.Snapshot{Provider: "codex", AccountID: "fixture-account", ObservedAt: now,
		Windows: []quota.Window{{ID: "codex:primary", UsedPercent: 20, ResetsAt: now.Add(time.Hour), Duration: 5 * time.Hour}}}
	if err := db.SaveSnapshot(context.Background(), codex); err != nil {
		t.Fatal(err)
	}
	assertStatusProvider(t, app, "codex", "status", "--json")
	assertStatusProvider(t, app, "claude", "status", "--provider", "claude", "--json")
}

func auditStateDB(t *testing.T, app *application) *store.DB {
	t.Helper()
	db, err := store.Open(app.getenv("RESETWATCH_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedDueClaudeReminder(t *testing.T, db *store.DB, now time.Time) {
	t.Helper()
	ctx := context.Background()
	snapshot := quota.Snapshot{Provider: "claude", AccountID: "claude-account-hash", ObservedAt: now.Add(-2 * time.Hour), Partial: true,
		Windows: []quota.Window{{ID: "claude:five_hour", UsedPercent: 100, ResetsAt: now.Add(-time.Hour), Duration: 5 * time.Hour}}}
	if err := db.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	config, err := db.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Schedule(ctx, snapshot, config, now); err != nil {
		t.Fatal(err)
	}
}

func assertStatusProvider(t *testing.T, app *application, provider string, args ...string) {
	t.Helper()
	runOK(t, app, args...)
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(app.out.(*strings.Builder).String()), &snapshot); err != nil {
		t.Fatalf("status must retain its single JSON object: %v", err)
	}
	if snapshot["provider"] != provider {
		t.Fatalf("status selected %v, want %s", snapshot["provider"], provider)
	}
}
