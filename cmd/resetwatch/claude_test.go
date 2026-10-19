package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

// fakeClaude stands in for Claude Code's usage request.
type fakeClaude struct {
	snapshot quota.Snapshot
	err      error
}

func (f fakeClaude) Read(context.Context) (quota.Snapshot, error) { return f.snapshot, f.err }

func claudeReading(windows map[string][2]int64) quota.Snapshot {
	durations := map[string]time.Duration{"five_hour": 5 * time.Hour, "seven_day": 7 * 24 * time.Hour}
	snapshot := quota.Snapshot{Provider: "claude", AccountID: "claude-account-hash", ObservedAt: time.Now().UTC(), Partial: true}
	for _, name := range []string{"five_hour", "seven_day"} {
		if w, ok := windows[name]; ok {
			snapshot.Windows = append(snapshot.Windows, quota.Window{ID: "claude:" + name, UsedPercent: float64(w[0]),
				ResetsAt: time.Unix(w[1], 0).UTC(), Duration: durations[name]})
		}
	}
	return snapshot
}

// claudeLine records one Claude usage reading through status --refresh; windows map
// "five_hour"/"seven_day" to {percent, reset}. The session argument is kept for older test names.
func claudeLine(t *testing.T, app *application, _ string, windows map[string][2]int64) {
	t.Helper()
	app.claude = fakeClaude{snapshot: claudeReading(windows)}
	runOK(t, app, "status", "--provider", "claude", "--refresh", "--json")
}

func claudeAlerts(t *testing.T, app *application) []store.Alert {
	t.Helper()
	output := &strings.Builder{}
	app.out = output
	if err := app.run(context.Background(), []string{"history", "--json"}); err != nil {
		t.Fatal(err)
	}
	var alerts []store.Alert
	if err := json.Unmarshal([]byte(output.String()), &alerts); err != nil {
		t.Fatal(err)
	}
	return alerts
}

func runOK(t *testing.T, app *application, args ...string) {
	t.Helper()
	app.out = &strings.Builder{}
	if err := app.run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
}

func onlyPending(t *testing.T, alerts []store.Alert, window string, reset int64) store.Alert {
	t.Helper()
	var pending []store.Alert
	for _, alert := range alerts {
		if alert.State == "pending" {
			pending = append(pending, alert)
		}
	}
	if len(pending) != 1 || pending[0].Window != window || pending[0].ResetAt.Unix() != reset {
		t.Fatalf("want one pending %s alert at %d, got %#v", window, reset, alerts)
	}
	return pending[0]
}

func TestClaudeReadingSchedulesReminder(t *testing.T) {
	app, _ := testApplication(t)
	saveEmail(t, app, "person@example.test")
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {100, reset}})
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
}

// A lower percentage with the same reset time does not prove a new window began.
func TestClaudeLowerPercentageDoesNotReleaseReminder(t *testing.T) {
	app, _ := testApplication(t)
	saveEmail(t, app, "person@example.test")
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {100, reset}})
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {50, reset}})
	if alert := onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset); !alert.NextAttempt.After(time.Now()) {
		t.Fatal("a lower percentage released the reminder early")
	}
}

// Fresh Claude data showing a later reset means the window already reset, so the reminder is due now.
func TestClaudeNewWindowReleasesReminderEarly(t *testing.T) {
	app, _ := testApplication(t)
	saveEmail(t, app, "person@example.test")
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {100, reset}})
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {3, reset + 3600}})
	for _, alert := range claudeAlerts(t, app) {
		if alert.State == "pending" && alert.NextAttempt.After(time.Now()) {
			t.Fatalf("an early Claude reset should make the reminder due now: %#v", alert)
		}
	}
}

// Claude may omit a window; that means unknown, so its reminder stays.
func TestClaudePartialReadingKeepsReminder(t *testing.T) {
	app, _ := testApplication(t)
	saveEmail(t, app, "person@example.test")
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {100, reset}, "seven_day": {40, reset + 86400}})
	claudeLine(t, app, "", map[string][2]int64{"seven_day": {41, reset + 86400}})
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
}

func TestEmailSetupSchedulesAndMovesClaudeReminders(t *testing.T) {
	app, _ := testApplication(t)
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {100, reset}})
	if len(claudeAlerts(t, app)) != 0 {
		t.Fatal("no reminder can exist before email setup")
	}
	saveEmail(t, app, "first@example.test")
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
	claudeLine(t, app, "", map[string][2]int64{"seven_day": {10, reset + 86400}})
	saveEmail(t, app, "second@example.test")
	if alert := onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset); alert.To != "second@example.test" {
		t.Fatalf("reminder was not moved to the new address: %#v", alert)
	}
	saveEmail(t, app, "second@example.test")
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset) // same settings: no duplicate
}

// status works for Claude-only users, keeps the single-object JSON shape, and can refresh Claude.
func TestStatusShowsClaudeWithoutCodex(t *testing.T) {
	app, _ := testApplication(t)
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {20, time.Now().Add(time.Hour).Unix()}})
	runOK(t, app, "status", "--json")
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(app.out.(*strings.Builder).String()), &snapshot); err != nil || snapshot["provider"] != "claude" {
		t.Fatalf("status --json should print one Claude object: %v %v", snapshot, err)
	}
	for _, args := range [][]string{{"status", "--provider", "gemini"}, {"status", "--provider", "codex"}} {
		app.out = &strings.Builder{}
		if err := app.run(context.Background(), args); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
}

// Status lines installed by older versions keep calling claude-statusline; it must stay silent.
func TestRetiredClaudeStatusLineIsSilent(t *testing.T) {
	app, output := testApplication(t)
	for _, input := range []string{`{"session_id":"s1","rate_limits":{"five_hour":{"used_percentage":100,"resets_at":1}}}`, `not json`} {
		app.in = strings.NewReader(input)
		if err := app.run(context.Background(), []string{"claude-statusline"}); err != nil || output.Len() != 0 {
			t.Fatalf("retired status line must print nothing: err %v, output %q", err, output.String())
		}
	}
}

// One provider failing to refresh must not hide the other's saved reading.
func TestStatusRefreshShowsReadingsWhenOneProviderFails(t *testing.T) {
	app, _ := testApplication(t)
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {30, time.Now().Add(time.Hour).Unix()}})
	config := store.Config{Providers: "both", To: "p@example.test", From: "a@example.test", CodexBin: `Z:\missing\codex.exe`}
	if err := auditStateDB(t, app).ConfigureSetup(context.Background(), config, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	app.out = &strings.Builder{}
	err := app.run(context.Background(), []string{"status", "--refresh"})
	out := app.out.(*strings.Builder).String()
	if err == nil || !strings.Contains(err.Error(), "codex refresh failed") || !strings.Contains(out, "claude:five_hour") {
		t.Fatalf("want the Codex error and the Claude reading; err %v, output %q", err, out)
	}
}
