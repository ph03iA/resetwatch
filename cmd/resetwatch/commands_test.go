package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func testApplication(t *testing.T) (*application, *bytes.Buffer) {
	t.Helper()
	output := &bytes.Buffer{}
	home := t.TempDir()
	app := &application{out: output, errOut: output, getenv: func(key string) string {
		if key == "RESETWATCH_HOME" {
			return home
		}
		return ""
	}}
	return app, output
}

func TestOfflineCommands(t *testing.T) {
	cases := []struct{ command, want string }{
		{"help", "resetwatch connect codex"},
		{"version", "0.1.0"},
		{"demo", "DEMO DATA"},
		{"history", "No alerts yet"},
		{"history --json", "[]"},
	}
	for _, test := range cases {
		t.Run(test.command, func(t *testing.T) {
			app, output := testApplication(t)
			if err := app.run(context.Background(), strings.Fields(test.command)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), test.want) {
				t.Fatalf("output %q does not contain %q", output.String(), test.want)
			}
		})
	}
}

func TestCLIRejectsInvalidInput(t *testing.T) {
	cases := [][]string{
		{"missing"}, {"version", "extra"}, {"demo", "extra"},
		{"connect", "claude"}, {"status"}, {"status", "extra"},
		{"notify", "--email", "bad", "--from", "bad"},
		{"notify", "--email", "a@example.com\r\nBcc: victim@example.com", "--from", "b@example.com"},
		{"history", "--limit", "0"}, {"history", "--limit", "1001"},
		{"history", "extra"}, {"watch"}, {"retry", "--id", "x"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, _ := testApplication(t)
			if err := app.run(context.Background(), args); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestConfigurePersistsWithoutSending(t *testing.T) {
	app, output := testApplication(t)
	args := []string{"notify", "--email", "recipient@example.com", "--from", "ResetWatch <alerts@example.com>"}
	if err := app.run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := app.run(context.Background(), []string{"watch", "--once"}); err == nil || !strings.Contains(err.Error(), "RESEND_API_KEY") {
		t.Fatalf("expected missing delivery key, got %v", err)
	}
}

func TestCountdown(t *testing.T) {
	now := time.Now()
	if got := countdown(now, now); !strings.Contains(got, "refresh needed") {
		t.Fatal(got)
	}
}
