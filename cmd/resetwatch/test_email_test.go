package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"resetwatch/internal/notify"
)

type capturedTestSender struct {
	messages []notify.Message
	err      error
}

func (s *capturedTestSender) Send(_ context.Context, message notify.Message) error {
	s.messages = append(s.messages, message)
	return s.err
}

func testEmailApplication(t *testing.T) (*application, *capturedTestSender) {
	t.Helper()
	app, _ := testApplication(t)
	app.getenv = func(key string) string {
		if key == "RESEND_API_KEY" {
			return "re_fake_test_key"
		}
		t.Fatalf("test-email unexpectedly accessed environment variable %q", key)
		return ""
	}
	sender := &capturedTestSender{}
	app.testSender = sender
	return app, sender
}

func TestEmailSendsExplicitTestWithoutState(t *testing.T) {
	app, sender := testEmailApplication(t)
	args := []string{"test-email", "--email", "recipient@example.com"}
	for range 2 {
		if err := app.run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	if len(sender.messages) != 2 || app.db != nil {
		t.Fatal("expected one send per invocation without opening the state database")
	}
	assertTestEmailFields(t, sender.messages[0])
	assertDistinctTestEmailKeys(t, sender.messages)
}

func assertTestEmailFields(t *testing.T, message notify.Message) {
	t.Helper()
	if !message.Test || message.To != "recipient@example.com" || message.From != "ResetWatch <onboarding@resend.dev>" {
		t.Fatalf("unexpected test message: %#v", message)
	}
}

func assertDistinctTestEmailKeys(t *testing.T, messages []notify.Message) {
	t.Helper()
	if messages[0].Key == "" || messages[0].Key == messages[1].Key {
		t.Fatal("separate explicit invocations require distinct message keys")
	}
}

func TestEmailValidationDoesNotSend(t *testing.T) {
	cases := [][]string{
		{"test-email"},
		{"test-email", "--email", "invalid"},
		{"test-email", "--email", "recipient@example.com", "--from", "invalid"},
		{"test-email", "--email", "recipient@example.com\r\nBcc: other@example.com"},
		{"test-email", "--email", "recipient@example.com", "extra"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, sender := testEmailApplication(t)
			if err := app.run(context.Background(), args); err == nil {
				t.Fatal("expected invalid input error")
			}
			if len(sender.messages) != 0 {
				t.Fatal("invalid input sent an email")
			}
		})
	}
}

func TestEmailRequiresAPIKey(t *testing.T) {
	t.Chdir(t.TempDir())
	app, sender := testEmailApplication(t)
	app.getenv = func(string) string { return "" }
	err := app.run(context.Background(), []string{"test-email", "--email", "recipient@example.com"})
	if err == nil || !strings.Contains(err.Error(), "RESEND_API_KEY") || len(sender.messages) != 0 {
		t.Fatalf("missing key was not rejected before sending: %v", err)
	}
}

func TestEmailFailureIsNotRetried(t *testing.T) {
	app, sender := testEmailApplication(t)
	sender.err = errors.New("simulated email service rejection")
	err := app.run(context.Background(), []string{"test-email", "--email", "recipient@example.com", "--from", "alerts@example.com"})
	if !errors.Is(err, sender.err) || len(sender.messages) != 1 {
		t.Fatalf("expected one failed attempt: %v", err)
	}
	if sender.messages[0].From != "alerts@example.com" {
		t.Fatal("explicit sender was ignored")
	}
}

func TestEmailMissingKeyDoesNotCreateState(t *testing.T) {
	t.Chdir(t.TempDir())
	app, _ := testApplication(t)
	dir := t.TempDir() + "/unused-state"
	app.getenv = func(key string) string {
		if key == "RESETWATCH_HOME" {
			return dir
		}
		return ""
	}
	if err := app.run(context.Background(), []string{"test-email", "--email", "recipient@example.com"}); err == nil {
		t.Fatal("expected missing API key")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("test-email touched local state: %v", err)
	}
}
