package watch

import (
	"context"
	"testing"
	"time"

	"resetwatch/internal/notify"
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

func TestClaudeCallbackCannotConfirmAllowance(t *testing.T) {
	now := watchTime()
	snapshot := watchSnapshot(now, now.Add(time.Hour), 10)
	snapshot.Provider = "claude"
	alert := composeAlert(now.Add(-time.Minute))
	alert.Provider = "claude"
	if compose(alert, &snapshot, now).Confirmed {
		t.Fatal("receipt of a Claude callback was treated as verified fresh allowance")
	}
}

func TestOldFrozenClaudeConfirmationIsRetiredWithoutSending(t *testing.T) {
	engine, _, sender, now := testEngine(t)
	engine.Provider = nil
	ctx := context.Background()
	snapshot := watchSnapshot(*now, now.Add(time.Hour), 100)
	snapshot.Provider, snapshot.AccountID = "claude", quota.ClaudeAccountID
	must(t, engine.DB.RecordSnapshot(ctx, snapshot, *now))
	*now = now.Add(time.Hour)
	alert, err := engine.DB.Claim(ctx, *now)
	must(t, err)
	_, err = engine.DB.FreezeMessage(ctx, alert, notify.Message{Key: alert.ID, To: alert.To, From: alert.From,
		Provider: "claude", Window: alert.Window, ResetAt: alert.ResetAt, Confirmed: true})
	must(t, err)
	must(t, engine.DB.Finish(ctx, alert, "retry", "synthetic interruption", *now, *now))
	must(t, engine.Step(ctx))
	if len(sender.messages) != 0 {
		t.Fatal("a pre-fix confirmed Claude payload bypassed the current expected-only policy")
	}
	assertHistory(t, engine.DB, "cancelled", 2)
}
