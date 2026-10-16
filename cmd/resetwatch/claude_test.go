package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/store"
)

// claudeLine feeds one status line update; windows are "five_hour"/"seven_day" -> {percent, reset}.
func claudeLine(t *testing.T, app *application, session string, windows map[string][2]int64) string {
	t.Helper()
	parts := make([]string, 0, len(windows))
	for name, w := range windows {
		parts = append(parts, fmt.Sprintf(`%q:{"used_percentage":%d,"resets_at":%d}`, name, w[0], w[1]))
	}
	app.in = strings.NewReader(fmt.Sprintf(`{"session_id":%q,"rate_limits":{%s}}`, session, strings.Join(parts, ",")))
	output := &strings.Builder{}
	app.out = output
	if err := app.run(context.Background(), []string{"claude-statusline"}); err != nil {
		t.Fatal(err)
	}
	return output.String()
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

func TestClaudeStatusLineRecordsLimitAndSchedulesReminder(t *testing.T) {
	app, _ := testApplication(t)
	runOK(t, app, "notify", "--email", "person@example.test", "--from", "alerts@example.test")
	reset := time.Now().Add(2 * time.Hour).Unix()
	got := claudeLine(t, app, "s1", map[string][2]int64{"five_hour": {100, reset}})
	if got != "Claude 5h 100%\n" {
		t.Fatalf("status line output = %q", got)
	}
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
}

// Reviewer bug 1: an old session repeating 50% must not release the reminder early.
func TestClaudeOldReadingsDoNotReleaseReminder(t *testing.T) {
	app, _ := testApplication(t)
	runOK(t, app, "notify", "--email", "person@example.test", "--from", "alerts@example.test")
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "old", map[string][2]int64{"five_hour": {50, reset}})
	claudeLine(t, app, "active", map[string][2]int64{"five_hour": {100, reset}})
	claudeLine(t, app, "old", map[string][2]int64{"five_hour": {50, reset}})        // timer repeat of old data
	claudeLine(t, app, "never-seen", map[string][2]int64{"five_hour": {50, reset}}) // first report, same window
	alert := onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
	if !alert.NextAttempt.After(time.Now()) {
		t.Fatal("old readings released the reminder early")
	}
}

func TestClaudeNewWindowKeepsRecordedResetTime(t *testing.T) {
	app, _ := testApplication(t)
	runOK(t, app, "notify", "--email", "person@example.test", "--from", "alerts@example.test")
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "s1", map[string][2]int64{"five_hour": {100, reset}})
	claudeLine(t, app, "s1", map[string][2]int64{"five_hour": {3, reset + 3600}})
	alert := onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
	if alert.NextAttempt.Unix() != reset {
		t.Fatalf("a cached Claude reading released the reminder before its recorded reset: %#v", alert)
	}
}

// Reviewer bug 2: a reading without the five-hour window must keep its reminder.
func TestClaudePartialReadingKeepsReminder(t *testing.T) {
	app, _ := testApplication(t)
	runOK(t, app, "notify", "--email", "person@example.test", "--from", "alerts@example.test")
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "s1", map[string][2]int64{"five_hour": {100, reset}, "seven_day": {40, reset + 86400}})
	claudeLine(t, app, "s1", map[string][2]int64{"seven_day": {41, reset + 86400}})
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
	runOK(t, app, "status", "--provider", "claude", "--json")
	if out := app.out.(*strings.Builder).String(); !strings.Contains(out, "claude:five_hour") {
		t.Fatalf("saved reading lost the omitted window: %s", out)
	}
}

// Reviewer bug 3: email setup after a Claude 100% reading, then an address change after a partial reading.
func TestNotifySchedulesAndMovesClaudeReminders(t *testing.T) {
	app, _ := testApplication(t)
	reset := time.Now().Add(2 * time.Hour).Unix()
	claudeLine(t, app, "s1", map[string][2]int64{"five_hour": {100, reset}})
	if len(claudeAlerts(t, app)) != 0 {
		t.Fatal("no reminder can exist before email setup")
	}
	runOK(t, app, "notify", "--email", "first@example.test", "--from", "alerts@example.test")
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset)
	claudeLine(t, app, "s1", map[string][2]int64{"seven_day": {10, reset + 86400}})
	runOK(t, app, "notify", "--email", "second@example.test", "--from", "alerts@example.test")
	if alert := onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset); alert.To != "second@example.test" {
		t.Fatalf("reminder was not moved to the new address: %#v", alert)
	}
	runOK(t, app, "notify", "--email", "second@example.test", "--from", "alerts@example.test")
	onlyPending(t, claudeAlerts(t, app), "claude:five_hour", reset) // same settings: no duplicate
}

// Reviewer bug 4: status works for Claude-only users and keeps the single-object JSON shape.
func TestStatusShowsClaudeWithoutCodex(t *testing.T) {
	app, _ := testApplication(t)
	claudeLine(t, app, "s1", map[string][2]int64{"five_hour": {20, time.Now().Add(time.Hour).Unix()}})
	runOK(t, app, "status", "--json")
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(app.out.(*strings.Builder).String()), &snapshot); err != nil || snapshot["provider"] != "claude" {
		t.Fatalf("status --json should print one Claude object: %v %v", snapshot, err)
	}
	for _, args := range [][]string{{"status", "--provider", "claude", "--refresh"}, {"status", "--provider", "gemini"}, {"status", "--provider", "codex"}} {
		app.out = &strings.Builder{}
		if err := app.run(context.Background(), args); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
}

func TestClaudeStatusLineStaysQuietWithoutPlanLimits(t *testing.T) {
	app, output := testApplication(t)
	app.in = strings.NewReader(`{"session_id":"s1","model":{"display_name":"Opus"}}`)
	if err := app.run(context.Background(), []string{"claude-statusline"}); err != nil || output.Len() != 0 {
		t.Fatalf("API-key sessions and fresh sessions must print nothing: err %v, output %q", err, output.String())
	}
	app.in = strings.NewReader(`not json`)
	if err := app.run(context.Background(), []string{"claude-statusline"}); err == nil {
		t.Fatal("malformed status line data must be an error")
	}
}

// An old session repeating a lower weekly number must not overwrite newer data used to confirm resets.
func TestClaudeOldSessionDoesNotOverwriteNewerReading(t *testing.T) {
	app, _ := testApplication(t)
	week := time.Now().Add(72 * time.Hour).Unix()
	claudeLine(t, app, "old", map[string][2]int64{"seven_day": {90, week}})
	claudeLine(t, app, "active", map[string][2]int64{"seven_day": {100, week}})
	claudeLine(t, app, "old", map[string][2]int64{"seven_day": {90, week}})
	runOK(t, app, "status", "--provider", "claude", "--json")
	if out := app.out.(*strings.Builder).String(); !strings.Contains(out, `"used_percent": 100`) {
		t.Fatalf("old session overwrote the newest weekly reading: %s", out)
	}
}
