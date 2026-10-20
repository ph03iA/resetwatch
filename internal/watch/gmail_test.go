package watch

import (
	"context"
	"errors"
	"testing"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/store"
)

func revoked(credential string) error {
	return &notify.AuthorizationError{Credential: credential, Reason: "Gmail no longer accepts the saved sign-in"}
}

// dueEngine has one reminder due now for the connected test account.
func dueEngine(t *testing.T) (*Engine, *fakeSender, *time.Time) {
	t.Helper()
	engine, _, sender, now := testEngine(t)
	ctx := context.Background()
	snapshot := watchSnapshot(now.Add(-time.Minute), *now, 100)
	must(t, engine.DB.RecordSnapshot(ctx, snapshot, snapshot.ObservedAt))
	engine.Provider = nil // no quota reads; these tests are about delivery
	return engine, sender, now
}

func gmailState(t *testing.T, engine *Engine) store.GmailAccount {
	t.Helper()
	config, err := engine.DB.Config(context.Background())
	must(t, err)
	return config.Gmail
}

func TestPausedDeliveryClaimsNothingButKeepsReading(t *testing.T) {
	engine, provider, sender, now := testEngine(t)
	ctx := context.Background()
	must(t, engine.DB.PauseGmail(ctx, "cred-person", "revoked"))
	mailerCalls := 0
	engine.Mailer = func(context.Context, store.Config) (notify.Sender, error) { mailerCalls++; return sender, nil }
	*now = now.Add(2 * time.Hour)
	must(t, engine.Step(ctx))
	if provider.reads != 1 || mailerCalls != 0 || len(sender.messages) != 0 {
		t.Fatalf("paused delivery must keep reading quota and send nothing: reads %d, mailer %d, sent %d", provider.reads, mailerCalls, len(sender.messages))
	}
	assertHistory(t, engine.DB, "pending", 0)
}

func TestSignInFailureDuringSendGivesTheAttemptBackAndPauses(t *testing.T) {
	engine, sender, _ := dueEngine(t)
	engine.Mailer = senderMailer(regressionSend(func(context.Context, notify.Message) error { return revoked("cred-person") }))
	must(t, engine.Step(context.Background()))
	if gmailState(t, engine).Ready() {
		t.Fatal("a rejected sign-in must pause delivery")
	}
	assertHistory(t, engine.DB, "retry", 0)
	if len(sender.messages) != 0 {
		t.Fatal("nothing was meant to reach the fake sender")
	}
}

func TestCredentialCheckBeforeClaimingPauses(t *testing.T) {
	engine, _, _ := dueEngine(t)
	engine.Mailer = func(context.Context, store.Config) (notify.Sender, error) { return nil, revoked("cred-person") }
	must(t, engine.Step(context.Background()))
	if gmailState(t, engine).Ready() {
		t.Fatal("a failed credential check must pause delivery")
	}
	assertHistory(t, engine.DB, "pending", 0)
}

func TestUnreachableGoogleDoesNotPause(t *testing.T) {
	engine, _, _ := dueEngine(t)
	outage := errors.New("dial tcp: no route to host")
	engine.Mailer = func(context.Context, store.Config) (notify.Sender, error) { return nil, outage }
	if err := engine.Step(context.Background()); !errors.Is(err, outage) {
		t.Fatalf("want the outage reported for this cycle, got %v", err)
	}
	if !gmailState(t, engine).Ready() {
		t.Fatal("a network outage must not pause delivery")
	}
	assertHistory(t, engine.DB, "pending", 0)
}

func TestStaleCredentialFailureDoesNotPauseANewerConnection(t *testing.T) {
	engine, _, _ := dueEngine(t)
	ctx := context.Background()
	renewed := testGmail("person")
	renewed.Credential = "cred-renewed"
	engine.Mailer = senderMailer(regressionSend(func(ctx context.Context, m notify.Message) error {
		must(t, engine.DB.ConnectGmail(ctx, renewed, time.Now()))
		return revoked("cred-person") // the old sign-in fails after the person already reconnected
	}))
	must(t, engine.Step(ctx))
	if account := gmailState(t, engine); !account.Ready() || account.Credential != "cred-renewed" {
		t.Fatalf("an old credential's failure paused the new connection: %+v", account)
	}
	assertHistory(t, engine.DB, "retry", 0)
}
