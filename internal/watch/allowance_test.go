package watch

import (
	"testing"
	"time"

	"resetwatch/internal/quota"
)

func TestCodexConfirmationRequiresExplicitBackendPermission(t *testing.T) {
	now := watchTime()
	allowed, denied := true, false
	for _, test := range []struct {
		name       string
		permission *bool
		confirmed  bool
	}{
		{"allowed", &allowed, true},
		{"denied", &denied, false},
		{"unknown", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := watchSnapshot(now, now.Add(time.Hour), 10)
			snapshot.OrdinaryUsageAllowed = test.permission
			message := compose(composeAlert(now.Add(-time.Minute)), &snapshot, now)
			if message.Confirmed != test.confirmed {
				t.Fatalf("backend permission was ignored: %+v", message)
			}
		})
	}
}

// Claude readings are pulled from Claude Code, so fresh data below 100% confirms the reset; stale data does not.
func TestFreshClaudeReadingConfirmsReset(t *testing.T) {
	now := watchTime()
	alert := composeAlert(now.Add(-time.Minute))
	alert.Provider = "claude"
	for _, test := range []struct {
		observed time.Time
		want     bool
	}{{now, true}, {now.Add(-3 * time.Minute), false}} {
		snapshot := watchSnapshot(test.observed, now.Add(5*time.Hour), 10)
		snapshot.Provider = "claude"
		if got := compose(alert, &snapshot, now).Confirmed; got != test.want {
			t.Fatalf("reading observed %s: Confirmed=%v, want %v", now.Sub(test.observed), got, test.want)
		}
	}
}

// After a reset Claude leaves the window out until it is used again; a fresh reading without it confirms
// the reset and still names the window, but a weekly limit at 100% blocks the confirmation.
func TestMissingClaudeWindowAfterResetConfirms(t *testing.T) {
	now := watchTime()
	alert := composeAlert(now.Add(-time.Minute))
	alert.Provider, alert.Window = "claude", "claude:five_hour"
	for _, test := range []struct {
		weeklyUsed float64
		want       bool
	}{{34, true}, {100, false}} {
		snapshot := &quota.Snapshot{Provider: "claude", AccountID: "account-hash", ObservedAt: now,
			Windows: []quota.Window{{ID: "claude:seven_day", UsedPercent: test.weeklyUsed, Duration: 7 * 24 * time.Hour, ResetsAt: now.Add(48 * time.Hour)}}}
		m := compose(alert, snapshot, now)
		if m.Confirmed != test.want || m.Duration != 5*time.Hour {
			t.Fatalf("weekly %v%%: Confirmed=%v Duration=%v, want %v and 5h", test.weeklyUsed, m.Confirmed, m.Duration, test.want)
		}
	}
	codex := watchSnapshot(now, now.Add(time.Hour), 10)
	codex.Windows = nil
	if compose(composeAlert(now.Add(-time.Minute)), &codex, now).Confirmed {
		t.Fatal("a missing Codex window must not confirm the reset")
	}
}

// A window still at 100% must be named in the email even when the reset cannot be confirmed.
func TestStillExhaustedWindowWarnsWithoutConfirmation(t *testing.T) {
	reset := watchTime()
	now := reset.Add(time.Minute)
	no, yes := false, true
	reading := func(provider string, observed time.Time, allowed *bool, weeklyReset time.Time) *quota.Snapshot {
		return &quota.Snapshot{Provider: provider, AccountID: "account-hash", ObservedAt: observed, OrdinaryUsageAllowed: allowed,
			Windows: []quota.Window{
				{ID: provider + ":primary", UsedPercent: 0, Duration: 5 * time.Hour, ResetsAt: now.Add(5 * time.Hour)},
				{ID: provider + ":secondary", UsedPercent: 100, Duration: 7 * 24 * time.Hour, ResetsAt: weeklyReset},
			}}
	}
	week := now.Add(72 * time.Hour)
	cases := []struct {
		name     string
		provider string
		snapshot *quota.Snapshot
		warn     bool
	}{
		{"codex says not allowed", "codex", reading("codex", now, &no, week), true},
		{"codex permission unknown", "codex", reading("codex", now, nil, week), true},
		{"codex allowed", "codex", reading("codex", now, &yes, week), true},
		{"claude last known reading", "claude", reading("claude", now.Add(-30*time.Second), nil, week), true},
		{"reading from before the reset", "codex", reading("codex", reset.Add(-time.Minute), &no, week), false},
		{"exhausted window already reset", "codex", reading("codex", now, &no, reset.Add(-time.Second)), false},
	}
	for _, test := range cases {
		alert := composeAlert(reset)
		alert.Provider, alert.Window = test.provider, test.provider+":primary"
		m := compose(alert, test.snapshot, now)
		if m.OtherLimit != test.warn || m.Confirmed {
			t.Errorf("%s: OtherLimit=%v Confirmed=%v, want warning %v and no confirmation", test.name, m.OtherLimit, m.Confirmed, test.warn)
		}
	}
}
