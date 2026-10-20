package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"resetwatch/internal/quota"
)

func TestClaimTerminalizesExhaustedAlertsAndClaimsNext(t *testing.T) {
	for _, state := range []string{"pending", "retry", "sending"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t, t.TempDir())
			now := storeTime()
			snapshot := testSnapshot(now.Add(-time.Minute), now, 100)
			exhausted := exhaustRegressionAlert(t, db, snapshot, now)
			if state != "sending" {
				must(t, db.Finish(ctx, exhausted, state, "synthetic prior failure", now, now))
			}
			second := snapshot.Windows[0]
			second.ID = "codex:secondary"
			snapshot.Windows = append(snapshot.Windows, second)
			must(t, db.Schedule(ctx, snapshot, testConfig(), now))
			claimed, err := db.Claim(ctx, now.Add(2*time.Minute))
			must(t, err)
			if claimed.Window != second.ID || claimed.Attempts != 1 {
				t.Fatalf("exhausted alert blocked the next eligible alert: %#v", claimed)
			}
			var terminal Alert
			must(t, scanRegressionAlert(db, exhausted.ID, &terminal))
			if terminal.State != "failed" || terminal.Attempts != 8 || terminal.LastError == "" {
				t.Fatalf("exhausted alert was not terminalized at eight attempts: %#v", terminal)
			}
			must(t, db.Finish(ctx, claimed, "sent", "", now, now))
			_, err = db.Claim(ctx, now.Add(3*time.Minute))
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("exhausted alert was claimable again: %v", err)
			}
		})
	}
}

func TestClaimPreservesTheEighthLiveLease(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	snapshot := testSnapshot(now.Add(-time.Minute), now, 100)
	exhausted := exhaustRegressionAlert(t, db, snapshot, now)
	_, err := db.Claim(ctx, now.Add(2*time.Minute-time.Second))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("live eighth lease was reclaimed: %v", err)
	}
	must(t, db.Finish(ctx, exhausted, "sent", "", now, now))
	assertStates(t, db, map[string]int{"sent": 1})
}

func exhaustRegressionAlert(t *testing.T, db *DB, snapshot quota.Snapshot, now time.Time) Alert {
	t.Helper()
	ctx := context.Background()
	must(t, db.Schedule(ctx, snapshot, testConfig(), now))
	var alert Alert
	for attempt := 1; attempt <= 8; attempt++ {
		var err error
		alert, err = db.Claim(ctx, now)
		must(t, err)
		if attempt < 8 {
			must(t, db.Finish(ctx, alert, "retry", "synthetic prior failure", now, now))
		}
	}
	return alert
}

func scanRegressionAlert(db *DB, id string, alert *Alert) error {
	loaded, err := scanAlert(db.db.QueryRowContext(context.Background(), "SELECT "+alertColumns+" FROM notification_alerts WHERE id=?", id))
	*alert = loaded
	return err
}
