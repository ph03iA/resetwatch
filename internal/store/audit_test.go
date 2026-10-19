package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
)

func claudeAuditSnapshot(observed, reset time.Time, used float64) quota.Snapshot {
	return quota.Snapshot{Provider: "claude", AccountID: "claude-account-hash", ObservedAt: observed, Partial: true,
		Windows: []quota.Window{{ID: "claude:five_hour", UsedPercent: used, ResetsAt: reset, Duration: 5 * time.Hour}}}
}

func TestRecordSnapshotRollsBackFailedSchedulingAndRetries(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	must(t, db.SaveConfig(ctx, testConfig()))
	now := storeTime()
	snapshot := claudeAuditSnapshot(now, now.Add(time.Hour), 100)
	_, err := db.db.Exec(`CREATE TRIGGER fail_alert BEFORE INSERT ON alerts BEGIN SELECT RAISE(FAIL,'synthetic scheduling failure'); END`)
	must(t, err)
	if err := db.RecordSnapshot(ctx, snapshot, now); err == nil {
		t.Fatal("expected synthetic scheduling failure")
	}
	if _, err := db.Snapshot(ctx, "claude"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("failed transaction left a saved snapshot: %v", err)
	}
	_, err = db.db.Exec("DROP TRIGGER fail_alert")
	must(t, err)
	must(t, db.RecordSnapshot(ctx, snapshot, now))
	must(t, db.RecordSnapshot(ctx, snapshot, now))
	assertStates(t, db, map[string]int{"pending": 1})
}

func TestConcurrentClaudeWindowsSurviveFirstEmailSetup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, other := openTestDB(t, dir), openTestDB(t, dir)
	now := storeTime()
	five := claudeAuditSnapshot(now, now.Add(time.Hour), 100)
	week := claudeAuditSnapshot(now, now.Add(7*24*time.Hour), 100)
	week.Windows[0].ID, week.Windows[0].Duration = "claude:seven_day", 7*24*time.Hour
	start, results := make(chan struct{}), make(chan error, 2)
	go func() { <-start; results <- db.RecordSnapshot(ctx, five, now) }()
	go func() { <-start; results <- other.RecordSnapshot(ctx, week, now) }()
	close(start)
	must(t, <-results)
	must(t, <-results)
	saved, err := db.Snapshot(ctx, "claude")
	must(t, err)
	if len(saved.Windows) != 2 {
		t.Fatalf("concurrent callbacks lost a window: %+v", saved)
	}
	must(t, db.ConfigureNotifications(ctx, testConfig().To, testConfig().From, now))
	assertStates(t, db, map[string]int{"pending": 2})
}

// Claude readings now come straight from Claude Code, so the latest one is authoritative:
// a lower percentage replaces the saved value, a same-reset drop does not release the reminder,
// and a later reset (a new window) makes it due at once.
func TestClaudeLatestReadingWinsAndNewWindowReleasesEarly(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	must(t, db.SaveConfig(ctx, testConfig()))
	now := storeTime()
	reset := now.Add(2 * time.Hour)
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, reset, 100), now))
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, reset, 50), now))
	saved, err := db.Snapshot(ctx, "claude")
	must(t, err)
	if saved.Windows[0].UsedPercent != 50 {
		t.Fatalf("latest reading did not replace the saved value: %+v", saved)
	}
	if _, err := db.Claim(ctx, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a same-reset drop released the reminder early: %v", err)
	}
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, reset.Add(time.Hour), 3), now))
	if _, err := db.Claim(ctx, now); err != nil {
		t.Fatalf("a new Claude window did not make the reminder due: %v", err)
	}
}

func TestNotificationChangeRecoversExpiredClaimWithoutRewritingOldPayload(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	config := testConfig()
	must(t, db.SaveConfig(ctx, config))
	now := storeTime()
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
	due := now.Add(2 * time.Hour)
	old, err := db.Claim(ctx, due)
	must(t, err)
	_, err = db.FreezeMessage(ctx, old, notify.Message{Key: old.ID, To: old.To, From: old.From})
	must(t, err)
	payload := storedPayload(t, db, old.ID)
	expired := due.Add(3 * time.Minute)
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(expired, expired.Add(5*time.Hour), 2), expired))
	must(t, db.ConfigureNotifications(ctx, "new@example.test", config.From, expired))
	moved, err := db.Claim(ctx, expired)
	must(t, err)
	if moved.To != "new@example.test" || moved.ID == old.ID || !moved.ResetAt.Equal(old.ResetAt) {
		t.Fatalf("expired claim was not transferred: %+v", moved)
	}
	if storedPayload(t, db, old.ID) != payload || storedPayload(t, db, moved.ID) != "" {
		t.Fatal("moving a claim rewrote or copied the old frozen payload")
	}
	if err := db.Finish(ctx, old, "sent", "", expired, expired); err == nil {
		t.Fatal("old claimed worker still owns the moved reminder")
	}
}

func TestReturningToCancelledDestinationPreservesItsFrozenRetry(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	config := testConfig()
	must(t, db.SaveConfig(ctx, config))
	now := storeTime()
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
	due := now.Add(2 * time.Hour)
	old, err := db.Claim(ctx, due)
	must(t, err)
	original := notify.Message{Key: old.ID, To: old.To, From: old.From, Confirmed: false}
	_, err = db.FreezeMessage(ctx, old, original)
	must(t, err)
	payload := storedPayload(t, db, old.ID)
	must(t, db.Finish(ctx, old, "retry", "synthetic transient failure", due, due))
	must(t, db.ConfigureNotifications(ctx, "new@example.test", config.From, due))
	must(t, db.ConfigureNotifications(ctx, config.To, config.From, due))
	resumed, err := db.Claim(ctx, due)
	must(t, err)
	if resumed.ID != old.ID || resumed.Attempts != 2 || !resumed.FirstAttempt.Equal(old.FirstAttempt) {
		t.Fatalf("returning to an existing key reset its retry budget: %+v", resumed)
	}
	if storedPayload(t, db, old.ID) != payload {
		t.Fatal("returning to a cancelled key changed its frozen payload")
	}
}

func TestFailedDestinationRollsBackNotificationChange(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	config := testConfig()
	must(t, db.SaveConfig(ctx, config))
	now := storeTime()
	snapshot := claudeAuditSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.RecordSnapshot(ctx, snapshot, now))
	destination := config
	destination.To = "failed@example.test"
	id := alertID(snapshot, snapshot.Windows[0], destination)
	_, err := db.db.Exec(`INSERT INTO alerts(id,provider,account_id,window_id,reset_at,recipient,sender,state,next_attempt,updated_at)
 VALUES(?,?,?,?,?,?,?,'failed',?,?)`, id, snapshot.Provider, snapshot.AccountID, snapshot.Windows[0].ID,
		snapshot.Windows[0].ResetsAt.Unix(), destination.To, destination.From, now.Unix(), now.Unix())
	must(t, err)
	if err := db.ConfigureNotifications(ctx, destination.To, destination.From, now); err == nil {
		t.Fatal("a failed destination was silently revived")
	}
	saved, err := db.Config(ctx)
	must(t, err)
	if saved != config {
		t.Fatalf("failed transfer changed configuration: %+v", saved)
	}
	assertStates(t, db, map[string]int{"pending": 1, "failed": 1})
}
