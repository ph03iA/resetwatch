package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
)

func TestStatePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openTestDB(t, dir)
	now := storeTime()
	config := testConfig()
	snapshot := testSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.SaveConfig(ctx, config))
	must(t, db.SaveSnapshot(ctx, snapshot))
	must(t, db.Schedule(ctx, snapshot, config, now))
	must(t, db.Close())
	db = openTestDB(t, dir)
	loadedConfig, err := db.Config(ctx)
	must(t, err)
	loadedSnapshot, err := db.Snapshot(ctx, "codex")
	must(t, err)
	if !reflect.DeepEqual(loadedConfig, config) || !reflect.DeepEqual(loadedSnapshot, snapshot) {
		t.Fatal("configuration or snapshot changed after reopening the database")
	}
	alert, err := db.Claim(ctx, now.Add(time.Hour))
	must(t, err)
	if alert.Attempts != 1 || alert.To != config.To || alert.AccountID != snapshot.AccountID {
		t.Fatalf("persisted alert was not claimable: %#v", alert)
	}
	must(t, db.Finish(ctx, alert, "sent", "", now, now))
	must(t, db.Close())
	db = openTestDB(t, dir)
	assertStates(t, db, map[string]int{"sent": 1})
}

func TestScheduleDeduplicatesAndDoesNotResendFinishedAlerts(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	snapshot := testSnapshot(now, now.Add(time.Hour), 100)
	for range 3 {
		must(t, db.Schedule(ctx, snapshot, testConfig(), now))
	}
	assertStates(t, db, map[string]int{"pending": 1})
	alert, err := db.Claim(ctx, now.Add(time.Hour))
	must(t, err)
	must(t, db.Finish(ctx, alert, "sent", "", now, now))
	must(t, db.Schedule(ctx, snapshot, testConfig(), now))
	assertStates(t, db, map[string]int{"sent": 1})
}

func TestScheduleMovedResetOrEarlyRelease(t *testing.T) {
	cases := []struct {
		name string
		used float64
	}{{"still exhausted", 100}, {"allowance restored", 25}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t, t.TempDir())
			now := storeTime()
			initial := testSnapshot(now, now.Add(time.Hour), 100)
			must(t, db.Schedule(ctx, initial, testConfig(), now))
			updated := testSnapshot(now.Add(time.Minute), now.Add(2*time.Hour), test.used)
			must(t, db.Schedule(ctx, updated, testConfig(), now.Add(time.Minute)))
			want := map[string]int{"pending": 1}
			if test.used == 100 {
				want["cancelled"] = 1
			}
			assertStates(t, db, want)
		})
	}
}

func TestCancelledFutureResetCanBeRearmedWithoutDuplicates(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	snapshot := testSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.Schedule(ctx, snapshot, testConfig(), now))
	withoutPrimary := snapshot
	withoutPrimary.Windows = []quota.Window{{ID: "codex:secondary", UsedPercent: 50, Duration: 7 * 24 * time.Hour, ResetsAt: now.Add(7 * 24 * time.Hour)}}
	must(t, db.Schedule(ctx, withoutPrimary, testConfig(), now.Add(time.Minute)))
	assertStates(t, db, map[string]int{"cancelled": 1})
	must(t, db.Schedule(ctx, snapshot, testConfig(), now.Add(2*time.Minute)))
	assertStates(t, db, map[string]int{"pending": 1})
	alert, err := db.Claim(ctx, now.Add(time.Hour))
	must(t, err)
	must(t, db.Finish(ctx, alert, "failed", "permanent", now, now))
	must(t, db.Schedule(ctx, snapshot, testConfig(), now))
	assertStates(t, db, map[string]int{"failed": 1})
}

func TestScheduleReleasesEarlyResetButKeepsDueReset(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	initial := testSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.Schedule(ctx, initial, testConfig(), now))
	// Provider restores allowance 30 minutes early and starts a new window.
	early := testSnapshot(now.Add(30*time.Minute), now.Add(5*time.Hour+30*time.Minute), 4)
	must(t, db.Schedule(ctx, early, testConfig(), now.Add(30*time.Minute)))
	alert, err := db.Claim(ctx, now.Add(30*time.Minute))
	must(t, err)
	if !alert.ResetAt.Equal(early.ObservedAt) {
		t.Fatalf("early reset time = %s, want observation time %s", alert.ResetAt, early.ObservedAt)
	}
	must(t, db.Finish(ctx, alert, "sent", "", now, now))
	must(t, db.Schedule(ctx, early, testConfig(), now.Add(31*time.Minute)))
	assertStates(t, db, map[string]int{"sent": 1})

	db = openTestDB(t, t.TempDir())
	must(t, db.Schedule(ctx, initial, testConfig(), now))
	available := testSnapshot(now.Add(time.Hour), now.Add(6*time.Hour), 10)
	must(t, db.Schedule(ctx, available, testConfig(), now.Add(time.Hour)))
	assertStates(t, db, map[string]int{"pending": 1})
}

func TestScheduleCancelsRemovedFutureWindowAndKeepsReportedWindow(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	snapshot := testSnapshot(now, now.Add(time.Hour), 100)
	snapshot.Windows = append(snapshot.Windows, quota.Window{ID: "codex:secondary", UsedPercent: 100,
		Duration: 7 * 24 * time.Hour, ResetsAt: now.Add(7 * 24 * time.Hour)})
	must(t, db.Schedule(ctx, snapshot, testConfig(), now))
	snapshot.Windows = snapshot.Windows[1:]
	must(t, db.Schedule(ctx, snapshot, testConfig(), now.Add(time.Minute)))
	assertStates(t, db, map[string]int{"cancelled": 1, "pending": 1})
	history, err := db.History(ctx, 10)
	must(t, err)
	states := make(map[string]string)
	for _, alert := range history {
		states[alert.Window] = alert.State
	}
	want := map[string]string{"codex:primary": "cancelled", "codex:secondary": "pending"}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("removed window cancellation changed the retained window: %v", states)
	}
}

func TestScheduleCancelsOldAccountRecipientAndSender(t *testing.T) {
	tests := []struct {
		name string
		edit func(*quota.Snapshot, *Config)
	}{
		{"account", func(s *quota.Snapshot, c *Config) { s.AccountID = "different-account-hash" }},
		{"recipient", func(s *quota.Snapshot, c *Config) { c.To = "different@example.test" }},
		{"sender", func(s *quota.Snapshot, c *Config) { c.From = "other@example.test" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t, t.TempDir())
			now := storeTime()
			snapshot, config := testSnapshot(now, now.Add(time.Hour), 100), testConfig()
			must(t, db.Schedule(ctx, snapshot, config, now))
			test.edit(&snapshot, &config)
			must(t, db.Schedule(ctx, snapshot, config, now))
			assertStates(t, db, map[string]int{"cancelled": 1, "pending": 1})
		})
	}
}

func TestTwoDatabaseInstancesClaimAtomically(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first, second := openTestDB(t, dir), openTestDB(t, dir)
	now := storeTime()
	must(t, first.Schedule(ctx, testSnapshot(now, now.Add(time.Hour), 100), testConfig(), now))
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, db := range []*DB{first, second} {
		go func(db *DB) {
			<-start
			_, err := db.Claim(ctx, now.Add(time.Hour))
			results <- err
		}(db)
	}
	close(start)
	claimed, empty := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			claimed++
		case errors.Is(err, sql.ErrNoRows):
			empty++
		default:
			t.Fatalf("competing database claim failed: %v", err)
		}
	}
	if claimed != 1 || empty != 1 {
		t.Fatalf("claims were not exclusive: claimed=%d empty=%d", claimed, empty)
	}
	assertStates(t, first, map[string]int{"sending": 1})
}

func TestLeaseRecoversAndRejectsStaleCompletion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openTestDB(t, dir)
	now := storeTime()
	due := now.Add(time.Hour)
	must(t, db.Schedule(ctx, testSnapshot(now, due, 100), testConfig(), now))
	first, err := db.Claim(ctx, due)
	must(t, err)
	must(t, db.Close())
	db = openTestDB(t, dir)
	_, err = db.Claim(ctx, due.Add(2*time.Minute-time.Second))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("live lease was stolen: %v", err)
	}
	second, err := db.Claim(ctx, due.Add(2*time.Minute))
	must(t, err)
	if second.ID != first.ID || second.Attempts != 2 || !second.FirstAttempt.Equal(first.FirstAttempt) {
		t.Fatalf("recovered lease lost delivery identity: first=%#v second=%#v", first, second)
	}
	err = db.Finish(ctx, first, "sent", "", due, due)
	if err == nil || !strings.Contains(err.Error(), "lease was lost") {
		t.Fatalf("stale completion unexpectedly succeeded: %v", err)
	}
	_, err = db.FreezeMessage(ctx, first, notify.Message{Key: first.ID})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale claimant changed the frozen message: %v", err)
	}
	must(t, db.Finish(ctx, second, "sent", "", due, due))
}

func TestFreezeMessagePreservesExactRetryPayloadAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := openTestDB(t, dir)
	now := storeTime()
	due := now.Add(time.Hour)
	must(t, db.Schedule(ctx, testSnapshot(now, due, 100), testConfig(), now))
	alert, err := db.Claim(ctx, due)
	must(t, err)
	initial := notify.Message{Key: alert.ID, To: alert.To, From: alert.From, Provider: alert.Provider,
		Window: alert.Window, ResetAt: alert.ResetAt, OtherLimit: true}
	frozen, err := db.FreezeMessage(ctx, alert, initial)
	must(t, err)
	firstPayload := storedPayload(t, db, alert.ID)
	must(t, db.Finish(ctx, alert, "retry", "temporary", due.Add(time.Minute), due))
	must(t, db.Close())
	db = openTestDB(t, dir)
	alert, err = db.Claim(ctx, due.Add(time.Minute))
	must(t, err)
	changed := initial
	changed.Confirmed, changed.OtherLimit = true, false
	retried, err := db.FreezeMessage(ctx, alert, changed)
	must(t, err)
	if !reflect.DeepEqual(frozen, retried) || firstPayload != storedPayload(t, db, alert.ID) {
		t.Fatal("retry changed the persisted email body while reusing its idempotency key")
	}
}

func TestExplicitRetryPreservesPayloadAndRejectsFinishedAlerts(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	due := now.Add(time.Hour)
	must(t, db.Schedule(ctx, testSnapshot(now, due, 100), testConfig(), now))
	alert, err := db.Claim(ctx, due)
	must(t, err)
	initial := notify.Message{Key: alert.ID, To: alert.To, From: alert.From, Provider: alert.Provider,
		Window: alert.Window, ResetAt: alert.ResetAt}
	frozen, err := db.FreezeMessage(ctx, alert, initial)
	must(t, err)
	must(t, db.Finish(ctx, alert, "failed", "permanent", due, due))
	must(t, db.Retry(ctx, alert.ID, due.Add(time.Minute)))
	claimed, err := db.Claim(ctx, due.Add(time.Minute))
	must(t, err)
	changed := initial
	changed.Confirmed = true
	retried, err := db.FreezeMessage(ctx, claimed, changed)
	must(t, err)
	if claimed.ID != alert.ID || claimed.Attempts != 2 || !reflect.DeepEqual(frozen, retried) {
		t.Fatal("explicit retry lost the delivery identity or original payload")
	}
	must(t, db.Finish(ctx, claimed, "sent", "", due, due))
	if err := db.Retry(ctx, claimed.ID, due.Add(2*time.Minute)); err == nil {
		t.Fatal("explicit retry requeued an already sent alert")
	}
	if err := db.Retry(ctx, "missing-id", due); err == nil {
		t.Fatal("explicit retry accepted an unknown alert")
	}
}

func TestExplicitRetryRejectsExpiredDeliveryWindows(t *testing.T) {
	tests := []struct {
		name    string
		firstAt time.Duration
		retryAt time.Duration
	}{
		{"23 hour idempotency window", 0, 23 * time.Hour},
		{"24 hour reset window", 23 * time.Hour, 24 * time.Hour},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t, t.TempDir())
			now := storeTime()
			due := now.Add(time.Hour)
			must(t, db.Schedule(ctx, testSnapshot(now, due, 100), testConfig(), now))
			alert, err := db.Claim(ctx, due.Add(test.firstAt))
			must(t, err)
			must(t, db.Finish(ctx, alert, "failed", "permanent", due, due))
			if err := db.Retry(ctx, alert.ID, due.Add(test.retryAt)); err == nil {
				t.Fatal("explicit retry exceeded its safe delivery window")
			}
			assertStates(t, db, map[string]int{"failed": 1})
		})
	}
}

func TestExplicitRetryRejectsEightAttemptAlert(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	due := now.Add(time.Hour)
	must(t, db.Schedule(ctx, testSnapshot(now, due, 100), testConfig(), now))
	var alert Alert
	for attempt := 1; attempt <= 8; attempt++ {
		var err error
		alert, err = db.Claim(ctx, due)
		must(t, err)
		must(t, db.Finish(ctx, alert, "failed", "permanent", due, due))
		if attempt < 8 {
			must(t, db.Retry(ctx, alert.ID, due))
		}
	}
	if alert.Attempts != 8 {
		t.Fatalf("expected eight persisted attempts, got %d", alert.Attempts)
	}
	if err := db.Retry(ctx, alert.ID, due); err == nil {
		t.Fatal("explicit retry exceeded eight delivery attempts")
	}
	assertStates(t, db, map[string]int{"failed": 1})
}

func openTestDB(t *testing.T, dir string) *DB {
	t.Helper()
	db, err := Open(dir)
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testConfig() Config {
	return Config{To: "person@example.test", From: "ResetWatch <alerts@example.test>", CodexBin: "codex-native"}
}

func storeTime() time.Time { return time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC) }

func testSnapshot(observed, reset time.Time, used float64) quota.Snapshot {
	return quota.Snapshot{Provider: "codex", AccountID: "account-hash", ObservedAt: observed,
		Windows: []quota.Window{{ID: "codex:primary", UsedPercent: used, Duration: 5 * time.Hour, ResetsAt: reset}}}
}

func assertStates(t *testing.T, db *DB, want map[string]int) {
	t.Helper()
	alerts, err := db.History(context.Background(), 100)
	must(t, err)
	got := make(map[string]int)
	for _, alert := range alerts {
		got[alert.State]++
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("alert states = %v, want %v; history=%#v", got, want, alerts)
	}
}

func storedPayload(t *testing.T, db *DB, id string) string {
	t.Helper()
	var payload string
	must(t, db.db.QueryRow("SELECT payload FROM alerts WHERE id=?", id).Scan(&payload))
	return payload
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddressChangeKeepsDueReminderDueAndFrozenPayload(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	must(t, db.SaveConfig(ctx, testConfig()))
	must(t, db.Schedule(ctx, testSnapshot(now, now.Add(time.Hour), 100), testConfig(), now))
	due := now.Add(2 * time.Hour) // reset passed, not delivered yet
	old, err := db.Claim(ctx, due)
	must(t, err)
	_, err = db.FreezeMessage(ctx, old, notify.Message{Key: old.ID, To: old.To, From: old.From})
	must(t, err)
	must(t, db.Finish(ctx, old, "retry", "temporary", due, due))
	moved := testConfig()
	moved.To = "new@example.test"
	must(t, db.ConfigureNotifications(ctx, moved.To, moved.From, due))
	must(t, db.ConfigureNotifications(ctx, moved.To, moved.From, due)) // same settings again: nothing more to move
	if config, err := db.Config(ctx); err != nil || config.To != moved.To {
		t.Fatalf("settings were not saved with the moved reminder: %#v %v", config, err)
	}
	alert, err := db.Claim(ctx, due)
	must(t, err)
	if alert.To != moved.To || alert.ID == old.ID || alert.Attempts != 1 || !alert.ResetAt.Equal(old.ResetAt) {
		t.Fatalf("due reminder was not moved intact: %#v", alert)
	}
	var state, payload string
	must(t, db.db.QueryRowContext(ctx, "SELECT state,payload FROM alerts WHERE id=?", old.ID).Scan(&state, &payload))
	if state != "cancelled" || payload == "" {
		t.Fatalf("old reminder must be cancelled with its frozen payload kept: %s %q", state, payload)
	}
	assertStates(t, db, map[string]int{"cancelled": 1, "sending": 1})
}
