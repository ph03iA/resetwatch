package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

func setupTestApplication(t *testing.T, key string) (*application, *bytes.Buffer, *capturedTestSender) {
	t.Helper()
	t.Chdir(t.TempDir())
	output := &bytes.Buffer{}
	state, claude := filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "claude")
	sender := &capturedTestSender{}
	app := &application{in: strings.NewReader(""), out: output, errOut: output, testSender: sender,
		setupTerminal: func() bool { return false }, claude: fakeClaude{snapshot: setupTestSnapshot("claude")},
		getenv: func(name string) string {
			return map[string]string{"RESETWATCH_HOME": state, "CLAUDE_CONFIG_DIR": claude, "RESEND_API_KEY": key}[name]
		},
	}
	return app, output, sender
}

func setupTestArguments(provider string) []string {
	return []string{"setup", "--providers", provider, "--email", "fixture@example.test", "--from", "Fixture <sender@example.test>", "--non-interactive"}
}

func setupTestCodex(t *testing.T, app *application) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	app.setupReadCodex = func(context.Context, string) (quota.Snapshot, error) { return setupTestSnapshot("codex"), nil }
	return binary
}

func setupTestSnapshot(provider string) quota.Snapshot {
	now := time.Now().UTC()
	return quota.Snapshot{Provider: provider, AccountID: "fixture-account", ObservedAt: now,
		Windows: []quota.Window{{ID: "primary", UsedPercent: 25, ResetsAt: now.Add(time.Hour), Duration: 5 * time.Hour}}}
}

func TestSetupInteractiveClaudeSavesKeyOnce(t *testing.T) {
	originalZone := time.Local
	t.Cleanup(func() { time.Local = originalZone })
	time.Local = time.FixedZone("IST", 5*3600+30*60)
	app, output, sender := setupTestApplication(t, "")
	app.setupTerminal = func() bool { return true }
	app.in = strings.NewReader("2\nfixture@example.test\n\nn\n")
	secretReads := 0
	app.setupSecret = func() (string, error) { secretReads++; return "re_fixture_secret", nil }
	if err := app.run(context.Background(), []string{"setup"}); err != nil {
		t.Fatal(err)
	}
	assertSetupConfig(t, app, "claude")
	assertSetupKeySaved(t)
	if secretReads != 1 {
		t.Fatal("hidden key was not requested exactly once")
	}
	if len(sender.messages) != 0 || strings.Contains(output.String(), "re_fixture_secret") {
		t.Fatal("setup sent an unrequested email or exposed its key")
	}
	if !strings.Contains(output.String(), "Claude: signed in; usage reading OK") || !strings.Contains(output.String(), "IST, UTC+05:30") {
		t.Fatal("setup did not truthfully describe Claude readiness and email timezone")
	}
	app.in = strings.NewReader("\n\n\nn\n")
	if err := app.run(context.Background(), []string{"setup"}); err != nil {
		t.Fatal(err)
	}
	if secretReads != 1 {
		t.Fatal("rerunning setup requested the saved key again")
	}
}

func assertSetupKeySaved(t *testing.T) {
	t.Helper()
	key, err := loadResendEnv()
	if err != nil || key != "re_fixture_secret" {
		t.Fatalf("hidden key was not saved: %v", err)
	}
}

func TestSetupNonInteractiveBothPersistsReadings(t *testing.T) {
	app, _, sender := setupTestApplication(t, "re_fixture_secret")
	binary := setupTestCodex(t, app)
	args := append(setupTestArguments("both"), "--codex-bin", binary)
	if err := app.run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	assertSetupConfig(t, app, "both")
	reading := readSetupSnapshot(t, app, "codex")
	if reading.AccountID != "fixture-account" || len(sender.messages) != 0 {
		t.Fatal("setup did not save Codex's initial reading or sent an unrequested email")
	}
	if readSetupSnapshot(t, app, "claude").Provider != "claude" {
		t.Fatal("setup did not save Claude's initial reading")
	}
	if _, err := os.Stat(filepath.Join(app.getenv("CLAUDE_CONFIG_DIR"), "settings.json")); !os.IsNotExist(err) {
		t.Fatal("setup must not write Claude settings")
	}
}

func TestSetupProviderFailureRetryAndSkip(t *testing.T) {
	cases := []struct {
		name, input, providers string
		wantReads              int
	}{
		{"retry", "1\nn\n", "both", 2},
		{"skip", "2\nn\n", "claude", 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			app, _, _ := setupTestApplication(t, "re_fixture")
			app.setupTerminal = func() bool { return true }
			app.in = strings.NewReader(test.input)
			binary := setupTestCodex(t, app)
			reads := 0
			app.setupReadCodex = func(context.Context, string) (quota.Snapshot, error) {
				reads++
				if reads == 1 {
					return quota.Snapshot{}, errors.New("fixture authentication failed")
				}
				return setupTestSnapshot("codex"), nil
			}
			args := []string{"setup", "--providers", "both", "--email", "fixture@example.test", "--from", "sender@example.test", "--codex-bin", binary}
			if err := app.run(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			assertSetupConfig(t, app, test.providers)
			if reads != test.wantReads {
				t.Fatalf("provider reads = %d; want %d", reads, test.wantReads)
			}
		})
	}
}

func TestSetupProviderFailureCancelOrSkipOnlyProvider(t *testing.T) {
	for _, input := range []string{"3\n", "2\n"} {
		app, _, sender := setupTestApplication(t, "re_fixture")
		app.setupTerminal = func() bool { return true }
		app.in = strings.NewReader(input)
		binary := setupTestCodex(t, app)
		app.setupReadCodex = func(context.Context, string) (quota.Snapshot, error) {
			return quota.Snapshot{}, errors.New("fixture unavailable")
		}
		args := []string{"setup", "--providers", "codex", "--email", "fixture@example.test", "--from", "sender@example.test", "--codex-bin", binary}
		if err := app.run(context.Background(), args); err == nil {
			t.Fatal("canceled/empty provider setup succeeded")
		}
		assertSetupInactive(t, app)
		if len(sender.messages) != 0 {
			t.Fatal("canceled setup sent email")
		}
	}
}

// installOldClaudeStatusLine reproduces what older versions did: replace the user's status line.
func installOldClaudeStatusLine(t *testing.T, app *application, original []byte) {
	t.Helper()
	writeSetupSettings(t, app, original)
	installer, err := app.claudeInstaller()
	if err != nil {
		t.Fatal(err)
	}
	if err := installer.Install(context.Background(), true); err != nil {
		t.Fatal(err)
	}
}

func assertOriginalClaudeStatusLine(t *testing.T, app *application) {
	t.Helper()
	settings := readSetupSettings(t, app)
	var status struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(settings["statusLine"], &status); err != nil || status.Command != "original-status" {
		t.Fatalf("previous Claude status line was not restored: %s", settings["statusLine"])
	}
	if string(settings["theme"]) != `"fixture"` {
		t.Fatal("restoring changed unrelated Claude settings")
	}
}

func TestSetupRestoresStatusLineFromOlderVersions(t *testing.T) {
	app, _, _ := setupTestApplication(t, "re_fixture")
	installOldClaudeStatusLine(t, app, []byte(`{"statusLine":{"type":"command","command":"original-status"},"theme":"fixture"}`))
	if err := app.run(context.Background(), setupTestArguments("claude")); err != nil {
		t.Fatal(err)
	}
	assertSetupConfig(t, app, "claude")
	assertOriginalClaudeStatusLine(t, app)
}

func TestConnectClaudeRestoresStatusLineFromOlderVersions(t *testing.T) {
	app, _, _ := setupTestApplication(t, "re_fixture")
	installOldClaudeStatusLine(t, app, []byte(`{"statusLine":{"type":"command","command":"original-status"},"theme":"fixture"}`))
	if err := app.run(context.Background(), []string{"connect", "claude"}); err != nil {
		t.Fatal(err)
	}
	assertOriginalClaudeStatusLine(t, app)
}

func TestSetupOptionalEmailAndFailureRetainsConfiguration(t *testing.T) {
	cases := []struct {
		name          string
		request, fail bool
		sends         int
	}{
		{"default no email", false, false, 0},
		{"explicit test", true, false, 1},
		{"failed test", true, true, 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			app, _, sender := setupTestApplication(t, "re_fixture_secret")
			args := setupTestArguments("claude")
			if test.request {
				args = append(args, "--test-email")
			}
			if test.fail {
				sender.err = errors.New("fixture email rejection")
			}
			err := app.run(context.Background(), args)
			if (err != nil) != test.fail {
				t.Fatalf("test-email result mismatch: %v", err)
			}
			assertSetupConfig(t, app, "claude")
			if len(sender.messages) != test.sends {
				t.Fatalf("email attempts = %d; want %d", len(sender.messages), test.sends)
			}
		})
	}
}

func TestSetupInteractiveEmailFailureCanBeDeclined(t *testing.T) {
	app, _, sender := setupTestApplication(t, "re_fixture")
	app.setupTerminal = func() bool { return true }
	app.in = strings.NewReader("y\nn\n")
	sender.err = errors.New("fixture email rejection")
	args := []string{"setup", "--providers", "claude", "--email", "fixture@example.test", "--from", "sender@example.test"}
	if err := app.run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	assertSetupConfig(t, app, "claude")
	if len(sender.messages) != 1 {
		t.Fatal("interactive email confirmation did not send exactly one attempt")
	}
}

func TestSetupNonInteractiveMissingKeyNeverInstalls(t *testing.T) {
	app, _, _ := setupTestApplication(t, "")
	if err := app.run(context.Background(), setupTestArguments("claude")); err == nil || !strings.Contains(err.Error(), "RESEND_API_KEY") {
		t.Fatalf("missing noninteractive key was not rejected: %v", err)
	}
	assertSetupInactive(t, app)
	path := filepath.Join(app.getenv("CLAUDE_CONFIG_DIR"), "settings.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing key changed Claude settings")
	}
}

func TestSetupEOFAndControlCancelBeforeChanges(t *testing.T) {
	for _, input := range []string{"", "2\n", "2\nfixture@example.test\n", "\x03"} {
		app, _, sender := setupTestApplication(t, "re_fixture")
		app.setupTerminal = func() bool { return true }
		app.in = strings.NewReader(input)
		if err := app.run(context.Background(), []string{"setup"}); !errors.Is(err, errSetupCancelled) {
			t.Fatalf("incomplete setup was accepted: %v", err)
		}
		assertSetupInactive(t, app)
		if len(sender.messages) != 0 {
			t.Fatal("canceled setup sent email")
		}
	}
}

func TestSetupInvalidExplicitEmailReturnsWithoutRetry(t *testing.T) {
	app, _, _ := setupTestApplication(t, "re_fixture")
	app.setupTerminal = func() bool { return true }
	args := []string{"setup", "--providers", "claude", "--email", "invalid", "--from", "sender@example.test"}
	if err := app.run(context.Background(), args); err == nil || errors.Is(err, errSetupCancelled) {
		t.Fatalf("invalid explicit email did not fail validation directly: %v", err)
	}
	assertSetupInactive(t, app)
}

func TestSetupClaudeReadFailureLeavesConfigInactive(t *testing.T) {
	app, output, _ := setupTestApplication(t, "re_fixture_secret")
	app.claude = fakeClaude{err: quota.ErrNoClaudeLimits}
	if err := app.run(context.Background(), setupTestArguments("claude")); err == nil {
		t.Fatal("setup accepted Claude without plan limits")
	}
	assertSetupInactive(t, app)
	if strings.Contains(output.String(), "re_fixture_secret") {
		t.Fatal("Claude failure exposed the key")
	}
}

func assertSetupConfig(t *testing.T, app *application, provider string) {
	t.Helper()
	config := connectSavedConfig(t, app)
	if config.Providers != provider || config.To != "fixture@example.test" {
		t.Fatalf("setup configuration = %#v", config)
	}
}

func assertSetupInactive(t *testing.T, app *application) {
	t.Helper()
	config := connectSavedConfig(t, app)
	if config.Providers != "" || config.To != "" {
		t.Fatalf("failed setup activated configuration: %#v", config)
	}
}

func readSetupSnapshot(t *testing.T, app *application, provider string) quota.Snapshot {
	t.Helper()
	db, err := store.Open(app.getenv("RESETWATCH_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	snapshot, err := db.Snapshot(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func writeSetupSettings(t *testing.T, app *application, content []byte) {
	t.Helper()
	dir := app.getenv("CLAUDE_CONFIG_DIR")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), content, 0600); err != nil {
		t.Fatal(err)
	}
}

func setupSettingsBytes(t *testing.T, app *application) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(app.getenv("CLAUDE_CONFIG_DIR"), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func readSetupSettings(t *testing.T, app *application) map[string]json.RawMessage {
	t.Helper()
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(setupSettingsBytes(t, app), &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestZoneLabelShowsComputerZone(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for zone, want := range map[*time.Location]string{
		time.FixedZone("IST", 5*3600+30*60): "IST, UTC+05:30",
		time.FixedZone("PST", -8*3600):      "PST, UTC-08:00",
		time.UTC:                            "UTC, UTC+00:00",
	} {
		if got := zoneLabel(at.In(zone)); got != want {
			t.Errorf("zoneLabel = %q, want %q", got, want)
		}
	}
}
