package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"resetwatch/internal/gmailauth"
	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
)

type capturedTestSender struct {
	messages []notify.Message
	err      error
}

func (s *capturedTestSender) Send(_ context.Context, message notify.Message) error {
	s.messages = append(s.messages, message)
	return s.err
}

// testEmailApplication has Gmail connected and counts any quota read, which test-email must never make.
func testEmailApplication(t *testing.T) (*application, *capturedTestSender, *int) {
	t.Helper()
	app, _ := testApplication(t)
	useFakeGmail(app, "person")
	runOK(t, app, "gmail", "connect")
	reads := 0
	app.claude = countingClaude{reads: &reads}
	sender := &capturedTestSender{}
	app.testSender = sender
	return app, sender, &reads
}

type countingClaude struct{ reads *int }

func (c countingClaude) Read(context.Context) (quota.Snapshot, error) {
	*c.reads++
	return quota.Snapshot{}, errors.New("test-email must not read usage")
}

func TestEmailSendsOneLabeledTestToTheConnectedAccount(t *testing.T) {
	app, sender, reads := testEmailApplication(t)
	for range 2 {
		runOK(t, app, "test-email")
	}
	if len(sender.messages) != 2 || *reads != 0 {
		t.Fatalf("want one send per invocation and no quota reads: %d sends, %d reads", len(sender.messages), *reads)
	}
	message := sender.messages[0]
	if !message.Test || message.To != "person@example.test" || message.From != "person@example.test" || message.MIME == "" {
		t.Fatalf("unexpected test message: %#v", message)
	}
	if sender.messages[0].Key == sender.messages[1].Key {
		t.Fatal("separate invocations need distinct message keys")
	}
	if history := claudeAlerts(t, app); len(history) != 0 {
		t.Fatalf("test-email must not create reminders: %+v", history)
	}
}

func TestEmailNeedsGmailAndRejectsRemovedFlags(t *testing.T) {
	app, _ := testApplication(t)
	useFakeGmail(app, "person")
	if err := app.run(context.Background(), []string{"test-email"}); err == nil || !strings.Contains(err.Error(), "gmail connect") {
		t.Fatalf("test-email without Gmail must say how to connect, got %v", err)
	}
	connectedApp, sender, _ := testEmailApplication(t)
	for _, args := range [][]string{{"test-email", "--email", "a@example.test"}, {"test-email", "--from", "a@example.test"}, {"test-email", "extra"}} {
		if err := connectedApp.run(context.Background(), args); err == nil {
			t.Errorf("%v must fail", args)
		}
	}
	if len(sender.messages) != 0 {
		t.Fatal("rejected input sent an email")
	}
}

func TestEmailFailureIsNotRetried(t *testing.T) {
	app, sender, _ := testEmailApplication(t)
	sender.err = errors.New("simulated Gmail rejection")
	err := app.run(context.Background(), []string{"test-email"})
	if !errors.Is(err, sender.err) || len(sender.messages) != 1 {
		t.Fatalf("expected one failed attempt: %v", err)
	}
}

func TestEmailWithoutAGoogleClientExplainsTheBuild(t *testing.T) {
	app, _, _ := testEmailApplication(t)
	app.testSender = nil // use the real Gmail path, which needs the embedded Google client
	if err := app.run(context.Background(), []string{"test-email"}); !errors.Is(err, gmailauth.ErrNoClient) {
		t.Fatalf("a build without a Google client must say so, got %v", err)
	}
}
