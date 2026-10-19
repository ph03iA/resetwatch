package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func testApplication(t *testing.T) (*application, *bytes.Buffer) {
	t.Helper()
	output := &bytes.Buffer{}
	home, claudeDir := t.TempDir(), t.TempDir() // never touch the real ~/.claude
	// Tests never reach the real Claude Code; claudeLine and fakeClaude supply readings.
	app := &application{out: output, errOut: output, claude: fakeClaude{err: errors.New("no Claude in tests")}, getenv: func(key string) string {
		return map[string]string{"RESETWATCH_HOME": home, "CLAUDE_CONFIG_DIR": claudeDir}[key]
	}}
	return app, output
}

func TestOfflineCommands(t *testing.T) {
	cases := []struct{ command, want string }{
		{"help", "resetwatch setup"},
		{"version", "0.1.0"},
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
		{"missing"}, {"version", "extra"}, {"status"}, {"status", "extra"},
		{"history", "--limit", "0"}, {"history", "--limit", "1001"},
		{"history", "extra"}, {"watch"},
		{"connect", "codex"}, {"notify"}, {"retry"}, {"demo"}, // removed commands
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

func TestEmailSettingsRejectBadAddresses(t *testing.T) {
	app, _ := testApplication(t)
	for _, to := range []string{"bad", "a@example.com\r\nBcc: victim@example.com"} {
		if err := setEmail(app, to); err == nil {
			t.Errorf("email %q was accepted", to)
		}
	}
}

func TestWatchNeedsResendKey(t *testing.T) {
	t.Chdir(t.TempDir())
	app, output := testApplication(t)
	saveEmail(t, app, "recipient@example.com")
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
