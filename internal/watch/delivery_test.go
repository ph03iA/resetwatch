package watch

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/store"
)

func TestDeliveryRejectsStaleClaimBeforeSending(t *testing.T) {
	engine, _, sender, now := testEngine(t)
	ctx := context.Background()
	first := claimDeliveryAlert(t, engine, *now)
	*now = now.Add(2 * time.Minute)
	current, err := engine.DB.Claim(ctx, *now)
	must(t, err)
	var logs []string
	engine.Log = func(message string) { logs = append(logs, message) }
	err = engine.deliver(ctx, first, nil)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale claim did not return the freeze error: %v", err)
	}
	if len(sender.messages) != 0 || len(logs) != 0 {
		t.Fatalf("stale claim sent %d messages and logged %v", len(sender.messages), logs)
	}
	assertRegressionAlert(t, engine.DB, current.ID, "sending", 2)
}

func TestDeliveryReturnsLeaseLossBeforeLoggingOutcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sendErr error
	}{
		{"accepted", nil},
		{"rejected", errors.New("synthetic permanent email failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _, sender, now := testEngine(t)
			ctx := context.Background()
			first := claimDeliveryAlert(t, engine, *now)
			var current store.Alert
			engine.Sender = regressionSend(func(ctx context.Context, message notify.Message) error {
				sender.messages = append(sender.messages, message)
				*now = now.Add(2 * time.Minute)
				var err error
				current, err = engine.DB.Claim(ctx, *now)
				must(t, err)
				return tc.sendErr
			})
			var logs []string
			engine.Log = func(message string) { logs = append(logs, message) }
			err := engine.deliver(ctx, first, nil)
			if err == nil || !strings.Contains(err.Error(), "lease was lost") {
				t.Fatalf("delivery did not return the completion error: %v", err)
			}
			if len(sender.messages) != 1 || sender.messages[0].Key != first.ID {
				t.Fatalf("unexpected delivery attempts: %#v", sender.messages)
			}
			if len(logs) != 0 {
				t.Fatalf("logged an outcome without recording it: %v", logs)
			}
			assertRegressionAlert(t, engine.DB, current.ID, "sending", 2)
		})
	}
}

func claimDeliveryAlert(t *testing.T, engine *Engine, now time.Time) store.Alert {
	t.Helper()
	ctx := context.Background()
	config, err := engine.DB.Config(ctx)
	must(t, err)
	snapshot := watchSnapshot(now.Add(-time.Minute), now, 100)
	must(t, engine.DB.SaveSnapshot(ctx, snapshot))
	must(t, engine.DB.Schedule(ctx, snapshot, config, now))
	alert, err := engine.DB.Claim(ctx, now)
	must(t, err)
	return alert
}
