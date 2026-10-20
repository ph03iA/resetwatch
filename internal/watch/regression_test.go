package watch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

type regressionRead func(context.Context) (quota.Snapshot, error)

func (read regressionRead) Read(ctx context.Context) (quota.Snapshot, error) { return read(ctx) }

type regressionSend func(context.Context, notify.Message) error

func (send regressionSend) Send(ctx context.Context, message notify.Message) error {
	return send(ctx, message)
}

type settingsChange struct {
	name string
	edit func(*store.Config)
}

func notificationChanges() []settingsChange {
	return []settingsChange{
		{"gmail account", func(config *store.Config) { config.Gmail = testGmail("replacement") }},
	}
}

func TestObserveSchedulesWithSettingsChangedDuringRead(t *testing.T) {
	for _, change := range notificationChanges() {
		t.Run(change.name, func(t *testing.T) {
			engine, provider, _, now := testEngine(t)
			ctx := context.Background()
			original, err := engine.DB.Config(ctx)
			must(t, err)
			updated := original
			change.edit(&updated)
			engine.Provider = regressionRead(func(ctx context.Context) (quota.Snapshot, error) {
				must(t, engine.DB.SaveConfig(ctx, updated))
				return provider.snapshot, nil
			})
			_, err = engine.Observe(ctx)
			must(t, err)
			history, err := engine.DB.History(ctx, 10)
			must(t, err)
			if len(history) != 1 || history[0].To != updated.Gmail.Address || history[0].From != updated.Gmail.Address {
				t.Fatalf("scheduled stale notification settings at %s: %#v", *now, history)
			}
		})
	}
}

func TestStepCancelsExpiredLeaseAfterSettingsChangeDuringRead(t *testing.T) {
	for _, change := range notificationChanges() {
		t.Run(change.name, func(t *testing.T) {
			engine, _, sender, now := testEngine(t)
			ctx := context.Background()
			original, err := engine.DB.Config(ctx)
			must(t, err)
			snapshot := watchSnapshot(now.Add(-4*time.Minute), now.Add(-3*time.Minute), 100)
			must(t, engine.DB.SaveSnapshot(ctx, snapshot))
			must(t, engine.DB.Schedule(ctx, snapshot, original, snapshot.ObservedAt))
			old, err := engine.DB.Claim(ctx, now.Add(-2*time.Minute-time.Second))
			must(t, err)
			updated := original
			change.edit(&updated)
			engine.Provider = regressionRead(func(ctx context.Context) (quota.Snapshot, error) {
				must(t, engine.DB.SaveConfig(ctx, updated))
				must(t, engine.DB.Schedule(ctx, snapshot, updated, *now))
				return watchSnapshot(*now, now.Add(time.Hour), 0), nil
			})
			must(t, engine.Step(ctx))
			for _, message := range sender.messages {
				if message.To != updated.Gmail.Address || message.From != updated.Gmail.Address {
					t.Fatalf("sent using superseded settings: %#v", message)
				}
			}
			assertRegressionAlert(t, engine.DB, old.ID, "cancelled", 2)
		})
	}
}

func TestStepReloadsSettingsBetweenDeliveries(t *testing.T) {
	for _, change := range notificationChanges() {
		t.Run(change.name, func(t *testing.T) {
			engine, provider, sender, now := testEngine(t)
			ctx := context.Background()
			config, err := engine.DB.Config(ctx)
			must(t, err)
			snapshot := watchSnapshot(now.Add(-time.Minute), *now, 100)
			second := snapshot.Windows[0]
			second.ID = "codex:secondary"
			snapshot.Windows = append(snapshot.Windows, second)
			must(t, engine.DB.SaveSnapshot(ctx, snapshot))
			must(t, engine.DB.Schedule(ctx, snapshot, config, *now))
			provider.err = errors.New("synthetic quota outage")
			change.edit(&config)
			engine.Mailer = senderMailer(regressionSend(func(ctx context.Context, message notify.Message) error {
				sender.messages = append(sender.messages, message)
				return engine.DB.SaveConfig(ctx, config)
			}))
			if err := engine.Step(ctx); !errors.Is(err, provider.err) {
				t.Fatalf("unexpected cycle error: %v", err)
			}
			if len(sender.messages) != 1 {
				t.Fatalf("sent %d messages despite settings changing after the first", len(sender.messages))
			}
			history, err := engine.DB.History(ctx, 10)
			must(t, err)
			states := map[string]int{}
			for _, alert := range history {
				states[alert.State]++
			}
			if states["sent"] != 1 || states["cancelled"] != 1 {
				t.Fatalf("unexpected final states: %v", states)
			}
		})
	}
}

func TestStepDoesNotSendAnExhaustedExpiredLease(t *testing.T) {
	engine, provider, sender, now := testEngine(t)
	ctx := context.Background()
	snapshot := watchSnapshot(now.Add(-time.Minute), *now, 100)
	config, err := engine.DB.Config(ctx)
	must(t, err)
	must(t, engine.DB.SaveSnapshot(ctx, snapshot))
	must(t, engine.DB.Schedule(ctx, snapshot, config, *now))
	var alert store.Alert
	for attempt := 1; attempt <= 8; attempt++ {
		alert, err = engine.DB.Claim(ctx, *now)
		must(t, err)
		if attempt < 8 {
			must(t, engine.DB.Finish(ctx, alert, "retry", "synthetic prior failure", *now, *now))
		}
	}
	*now = now.Add(3 * time.Minute)
	provider.err = errors.New("synthetic quota outage")
	if err := engine.Step(ctx); !errors.Is(err, provider.err) {
		t.Fatalf("unexpected cycle error: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatal("sent an exhausted alert after recovering its expired lease")
	}
	assertRegressionAlert(t, engine.DB, alert.ID, "failed", 8)
}

func TestDeliveryFailsClosedWhenSettingsCannotBeRead(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(dir)
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	config := store.Config{Gmail: testGmail("person")}
	must(t, db.SaveConfig(ctx, config))
	now := watchTime()
	snapshot := watchSnapshot(now.Add(-time.Minute), now, 100)
	must(t, db.SaveSnapshot(ctx, snapshot))
	must(t, db.Schedule(ctx, snapshot, config, now))
	alert, err := db.Claim(ctx, now)
	must(t, err)
	writer, err := sql.Open("sqlite", filepath.Join(dir, "state.db"))
	must(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	_, err = writer.ExecContext(ctx, "UPDATE settings SET value=? WHERE key='config'", "invalid JSON")
	must(t, err)
	sender := &fakeSender{}
	engine := Engine{DB: db, Mailer: senderMailer(sender), Now: func() time.Time { return now }}
	err = engine.deliver(ctx, alert, sender)
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) {
		t.Fatalf("settings read error was hidden: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatal("sent despite notification settings being unreadable")
	}
	assertRegressionAlert(t, db, alert.ID, "sending", 1)
}

func assertRegressionAlert(t *testing.T, db *store.DB, id, state string, attempts int) {
	t.Helper()
	history, err := db.History(context.Background(), 100)
	must(t, err)
	for _, alert := range history {
		if alert.ID != id {
			continue
		}
		if alert.State != state || alert.Attempts != attempts {
			t.Fatalf("alert state=%s attempts=%d, want state=%s attempts=%d", alert.State, alert.Attempts, state, attempts)
		}
		return
	}
	t.Fatalf("alert %s was not found", id)
}
