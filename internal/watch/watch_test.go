package watch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

type fakeProvider struct {
	snapshot quota.Snapshot
	err      error
}

func (provider *fakeProvider) Read(context.Context) (quota.Snapshot, error) {
	return provider.snapshot, provider.err
}

type fakeSender struct {
	messages []notify.Message
	errors   []error
}

func (sender *fakeSender) Send(_ context.Context, message notify.Message) error {
	sender.messages = append(sender.messages, message)
	if len(sender.errors) == 0 {
		return nil
	}
	err := sender.errors[0]
	sender.errors = sender.errors[1:]
	return err
}

func TestComposeUsesFreshEvidenceAndReportsBlockedWindows(t *testing.T) {
	now := watchTime()
	alert := composeAlert(now.Add(-10 * time.Minute))
	tests := []struct {
		name      string
		edit      func(*quota.Snapshot)
		confirmed bool
		blocked   bool
	}{
		{"fresh allowance", func(*quota.Snapshot) {}, true, false},
		{"stale data", func(s *quota.Snapshot) { s.ObservedAt = now.Add(-3 * time.Minute) }, false, false},
		{"before reset", func(s *quota.Snapshot) { s.ObservedAt = alert.ResetAt.Add(-time.Second) }, false, false},
		{"future observation", func(s *quota.Snapshot) { s.ObservedAt = now.Add(2 * time.Minute) }, false, false},
		{"different account", func(s *quota.Snapshot) { s.AccountID = "different-account" }, false, false},
		{"different provider", func(s *quota.Snapshot) { s.Provider = "different-provider" }, false, false},
		{"missing target window", func(s *quota.Snapshot) { s.Windows[0].ID = "other:primary" }, false, false},
		{"target still exhausted", func(s *quota.Snapshot) { s.Windows[0].UsedPercent = 100 }, false, true},
		{"another window exhausted", func(s *quota.Snapshot) {
			s.Windows = append(s.Windows, quota.Window{ID: "codex:secondary", UsedPercent: 100,
				Duration: 7 * 24 * time.Hour, ResetsAt: now.Add(24 * time.Hour)})
		}, false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := watchSnapshot(now.Add(-time.Second), now.Add(time.Hour), 25)
			test.edit(&snapshot)
			message := compose(alert, &snapshot, now)
			if message.Confirmed != test.confirmed || message.OtherLimit != test.blocked {
				t.Fatalf("wrong evidence status: %#v", message)
			}
		})
	}
	if compose(alert, nil, now).Confirmed {
		t.Fatal("missing quota data was presented as confirmed availability")
	}
}

func TestStepSchedulesThenSendsConfirmedResetOnce(t *testing.T) {
	engine, provider, sender, now := testEngine(t)
	ctx := context.Background()
	must(t, engine.Step(ctx))
	if len(sender.messages) != 0 {
		t.Fatal("sent a notification before its reset time")
	}
	*now = now.Add(time.Hour)
	provider.snapshot = watchSnapshot(*now, now.Add(5*time.Hour), 20)
	must(t, engine.Step(ctx))
	if len(sender.messages) != 1 || !sender.messages[0].Confirmed {
		t.Fatalf("fresh reset was not delivered as confirmed: %#v", sender.messages)
	}
	if sender.messages[0].Duration != 5*time.Hour {
		t.Fatalf("window length for the email label = %v, want 5h", sender.messages[0].Duration)
	}
	must(t, engine.Step(ctx))
	if len(sender.messages) != 1 {
		t.Fatal("delivered an already completed reset more than once")
	}
	assertHistory(t, engine.DB, "sent", 1)
}

func TestStepSendsExpectedResetWhenProviderIsUnavailable(t *testing.T) {
	engine, provider, sender, now := testEngine(t)
	ctx := context.Background()
	must(t, engine.Step(ctx))
	*now = now.Add(time.Hour)
	provider.err = errors.New("synthetic quota outage")
	err := engine.Step(ctx)
	if !errors.Is(err, provider.err) {
		t.Fatalf("quota refresh failure was hidden: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0].Confirmed {
		t.Fatalf("scheduled reset must remain explicitly expected: %#v", sender.messages)
	}
	assertHistory(t, engine.DB, "sent", 1)
}

func TestTransientDeliveryRetriesTheFrozenMessage(t *testing.T) {
	engine, provider, sender, now := testEngine(t)
	ctx := context.Background()
	must(t, engine.Step(ctx))
	*now = now.Add(time.Hour)
	provider.err = errors.New("synthetic quota outage")
	sender.errors = []error{syntheticDeliveryError(t, http.StatusServiceUnavailable, "")}
	_ = engine.Step(ctx)
	assertHistory(t, engine.DB, "retry", 1)
	if len(sender.messages) != 1 || sender.messages[0].Confirmed {
		t.Fatal("first attempt did not capture expected reset status")
	}
	*now = now.Add(29 * time.Second)
	_ = engine.Step(ctx)
	if len(sender.messages) != 1 {
		t.Fatal("transient email failure retried before its backoff expired")
	}
	*now = now.Add(time.Second)
	provider.err = nil
	provider.snapshot = watchSnapshot(*now, now.Add(5*time.Hour), 10)
	must(t, engine.Step(ctx))
	if len(sender.messages) != 2 || !reflect.DeepEqual(sender.messages[0], sender.messages[1]) {
		t.Fatalf("retry reused an idempotency key with a changed message: %#v", sender.messages)
	}
	assertHistory(t, engine.DB, "sent", 2)
}

func TestPermanentDeliveryFailureDoesNotRetry(t *testing.T) {
	engine, provider, sender, now := testEngine(t)
	ctx := context.Background()
	must(t, engine.Step(ctx))
	*now = now.Add(time.Hour)
	provider.err = errors.New("synthetic quota outage")
	sender.errors = []error{syntheticDeliveryError(t, http.StatusUnauthorized, "")}
	_ = engine.Step(ctx)
	assertHistory(t, engine.DB, "failed", 1)
	*now = now.Add(time.Minute)
	_ = engine.Step(ctx)
	if len(sender.messages) != 1 {
		t.Fatal("permanent email failure retried automatically")
	}
}

func TestDueDeliveryCancelsChangedSettingsAndAccount(t *testing.T) {
	tests := []struct {
		name string
		edit func(*testing.T, *Engine, *fakeProvider)
	}{
		{"recipient", func(t *testing.T, e *Engine, p *fakeProvider) {
			config, err := e.DB.Config(context.Background())
			must(t, err)
			config.To = "other@example.test"
			must(t, e.DB.SaveConfig(context.Background(), config))
		}},
		{"sender", func(t *testing.T, e *Engine, p *fakeProvider) {
			config, err := e.DB.Config(context.Background())
			must(t, err)
			config.From = "other@example.test"
			must(t, e.DB.SaveConfig(context.Background(), config))
		}},
		{"account", func(t *testing.T, e *Engine, p *fakeProvider) {
			snapshot := p.snapshot
			snapshot.AccountID = "different-account"
			must(t, e.DB.SaveSnapshot(context.Background(), snapshot))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, provider, sender, now := testEngine(t)
			must(t, engine.Step(context.Background()))
			test.edit(t, engine, provider)
			*now = now.Add(time.Hour)
			provider.err = errors.New("synthetic quota outage")
			_ = engine.Step(context.Background())
			if len(sender.messages) != 0 {
				t.Fatal("sent an alert for superseded account or notification settings")
			}
			assertHistory(t, engine.DB, "cancelled", 1)
		})
	}
}

func TestDeliveryExpiryBoundaries(t *testing.T) {
	tests := []struct {
		name         string
		resetAge     time.Duration
		attemptAge   time.Duration
		wantMessages int
		wantState    string
	}{
		{"reset just under 24h", 24*time.Hour - time.Second, time.Minute, 1, "sent"},
		{"reset at 24h", 24 * time.Hour, time.Minute, 0, "cancelled"},
		{"retry just under 23h", 23 * time.Hour, 23*time.Hour - time.Second, 1, "sent"},
		{"retry at 23h", 23 * time.Hour, 23 * time.Hour, 0, "cancelled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, provider, sender, now := testEngine(t)
			ctx := context.Background()
			reset := now.Add(-test.resetAge)
			snapshot := watchSnapshot(reset.Add(-time.Hour), reset, 100)
			config, err := engine.DB.Config(ctx)
			must(t, err)
			must(t, engine.DB.SaveSnapshot(ctx, snapshot))
			must(t, engine.DB.Schedule(ctx, snapshot, config, reset.Add(-time.Hour)))
			alert, err := engine.DB.Claim(ctx, now.Add(-test.attemptAge))
			must(t, err)
			must(t, engine.DB.Finish(ctx, alert, "retry", "temporary", *now, *now))
			provider.err = errors.New("synthetic quota outage")
			_ = engine.Step(ctx)
			if len(sender.messages) != test.wantMessages {
				t.Fatalf("messages=%d, want %d", len(sender.messages), test.wantMessages)
			}
			assertHistory(t, engine.DB, test.wantState, 2)
		})
	}
}

func TestRetryDecisionHonorsProviderDelayAndStopsAtLimits(t *testing.T) {
	now := watchTime()
	transient := syntheticDeliveryError(t, http.StatusTooManyRequests, "120")
	alert := store.Alert{Attempts: 1, FirstAttempt: now}
	state, next := retryDecision(alert, transient, now)
	if state != "retry" || !next.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("provider Retry-After was ignored: %s %v", state, next)
	}
	alert.Attempts = 8
	state, _ = retryDecision(alert, transient, now)
	if state != "failed" {
		t.Fatal("automatic retry continued beyond eight attempts")
	}
	alert.Attempts = 1
	alert.FirstAttempt = now.Add(-23*time.Hour + time.Minute)
	state, _ = retryDecision(alert, transient, now)
	if state != "failed" {
		t.Fatal("scheduled retry would exceed the provider idempotency safety window")
	}
}

func testEngine(t *testing.T) (*Engine, *fakeProvider, *fakeSender, *time.Time) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	must(t, db.SaveConfig(context.Background(), store.Config{To: "person@example.test", From: "alerts@example.test"}))
	now := watchTime()
	provider := &fakeProvider{snapshot: watchSnapshot(now, now.Add(time.Hour), 100)}
	sender := &fakeSender{}
	engine := &Engine{DB: db, Provider: provider, Sender: sender, Now: func() time.Time { return now }}
	return engine, provider, sender, &now
}

func composeAlert(reset time.Time) store.Alert {
	return store.Alert{ID: strings.Repeat("a", 64), Provider: "codex", AccountID: "account-hash",
		Window: "codex:primary", ResetAt: reset, To: "person@example.test", From: "alerts@example.test"}
}

func watchSnapshot(observed, reset time.Time, used float64) quota.Snapshot {
	allowed := true
	return quota.Snapshot{Provider: "codex", AccountID: "account-hash", ObservedAt: observed,
		OrdinaryUsageAllowed: &allowed,
		Windows:              []quota.Window{{ID: "codex:primary", UsedPercent: used, Duration: 5 * time.Hour, ResetsAt: reset}}}
}

func watchTime() time.Time { return time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC) }

func assertHistory(t *testing.T, db *store.DB, state string, attempts int) {
	t.Helper()
	history, err := db.History(context.Background(), 100)
	must(t, err)
	if len(history) != 1 || history[0].State != state || history[0].Attempts != attempts {
		t.Fatalf("expected one %s alert with %d attempts, got %#v", state, attempts, history)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (transport roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func syntheticDeliveryError(t *testing.T, code int, retryAfter string) error {
	t.Helper()
	client := &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: code, Header: http.Header{"Retry-After": []string{retryAfter}},
			Body: io.NopCloser(strings.NewReader(`{}`)), Request: request}, nil
	})}
	service := notify.Resend{APIKey: "re_fake_test_key", Client: client}
	alert := composeAlert(watchTime())
	err := service.Send(context.Background(), compose(alert, nil, watchTime()))
	if err == nil {
		t.Fatal("synthetic email failure unexpectedly succeeded")
	}
	return err
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestStepEmailsEarlyResetOfExhaustedWindowOnly(t *testing.T) {
	engine, provider, sender, now := testEngine(t)
	ctx := context.Background()
	must(t, engine.Step(ctx))
	// Codex restores allowance well before the recorded reset and starts a new window.
	*now = now.Add(20 * time.Minute)
	provider.snapshot = watchSnapshot(*now, now.Add(5*time.Hour), 11)
	must(t, engine.Step(ctx))
	if len(sender.messages) != 1 || sender.messages[0].Test || !sender.messages[0].Confirmed {
		t.Fatalf("early reset of an exhausted window was not emailed as confirmed: %#v", sender.messages)
	}
	// The new window is used but never exhausted, so its reset must stay silent.
	*now = now.Add(6 * time.Hour)
	provider.snapshot = watchSnapshot(*now, now.Add(5*time.Hour), 40)
	must(t, engine.Step(ctx))
	if len(sender.messages) != 1 {
		t.Fatalf("emailed a reset of a window that was never exhausted: %d messages", len(sender.messages))
	}
	assertHistory(t, engine.DB, "sent", 1)
}

func TestStepWithoutPollingSendsClaudeResetFromSavedReadings(t *testing.T) {
	for _, fresh := range []bool{true, false} {
		engine, _, sender, now := testEngine(t)
		engine.Provider = nil
		ctx := context.Background()
		claude := quota.Snapshot{Provider: "claude", AccountID: quota.ClaudeAccountID, ObservedAt: *now,
			Windows: []quota.Window{{ID: "claude:five_hour", UsedPercent: 100, Duration: 5 * time.Hour, ResetsAt: now.Add(time.Hour)}}}
		must(t, engine.DB.SaveSnapshot(ctx, claude))
		config, err := engine.DB.Config(ctx)
		must(t, err)
		must(t, engine.DB.Schedule(ctx, claude, config, *now))
		*now = now.Add(time.Hour + time.Minute)
		if fresh {
			// Claude Code's status line reported again after the reset.
			claude.ObservedAt = *now
			claude.Windows[0].UsedPercent, claude.Windows[0].ResetsAt = 2, now.Add(5*time.Hour)
			must(t, engine.DB.SaveSnapshot(ctx, claude))
		}
		must(t, engine.Step(ctx))
		if len(sender.messages) != 1 || sender.messages[0].Provider != "claude" || sender.messages[0].Duration != 5*time.Hour {
			t.Fatalf("fresh=%v: Claude reset was not emailed with its window length: %#v", fresh, sender.messages)
		}
		if sender.messages[0].Confirmed {
			t.Fatalf("fresh=%v: status-line callbacks must never confirm Claude allowance", fresh)
		}
	}
}

func TestComposeFixesResetTimeInUserZone(t *testing.T) {
	originalZone := time.Local
	t.Cleanup(func() { time.Local = originalZone })
	time.Local = time.FixedZone("IST", 5*3600+30*60)
	message := compose(composeAlert(watchTime()), nil, watchTime())
	if message.LocalResetAt != "3:30 PM IST" {
		t.Fatalf("LocalResetAt = %q, want the reset in the user's zone", message.LocalResetAt)
	}
}
