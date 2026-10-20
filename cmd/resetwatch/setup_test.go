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

	"resetwatch/internal/gmailauth"
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

type setupFixture struct {
	app         *application
	output      *bytes.Buffer
	sender      *capturedTestSender
	credentials *memoryCredentials
	google      *fakeGoogle
}

// setupTestApplication is a fresh install with fake Claude, Gmail and credential store; nothing is connected.
func setupTestApplication(t *testing.T) setupFixture {
	t.Helper()
	t.Chdir(t.TempDir())
	output := &bytes.Buffer{}
	state, claude := filepath.Join(t.TempDir(), "state"), filepath.Join(t.TempDir(), "claude")
	sender := &capturedTestSender{}
	app := &application{in: strings.NewReader(""), out: output, errOut: output, testSender: sender,
		setupTerminal: func() bool { return false }, claude: fakeClaude{snapshot: setupTestSnapshot("claude")},
		getenv: func(name string) string {
			return map[string]string{"RESETWATCH_HOME": state, "CLAUDE_CONFIG_DIR": claude}[name]
		},
	}
	credentials, google := useFakeGmail(app, "person")
	return setupFixture{app: app, output: output, sender: sender, credentials: credentials, google: google}
}

// connected is a fixture whose Gmail account is already connected, as noninteractive setup requires.
func connected(t *testing.T) setupFixture {
	t.Helper()
	f := setupTestApplication(t)
	runOK(t, f.app, "gmail", "connect")
	f.google.signIns = 0
	return f
}

func setupTestArguments(provider string) []string {
	return []string{"setup", "--providers", provider, "--non-interactive"}
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

func TestSetupInteractiveConnectsGmailOnce(t *testing.T) {
	originalZone := time.Local
	t.Cleanup(func() { time.Local = originalZone })
	time.Local = time.FixedZone("IST", 5*3600+30*60)
	f := setupTestApplication(t)
	f.app.setupTerminal = func() bool { return true }
	f.app.in = strings.NewReader("2\nn\n")
	if err := f.app.run(context.Background(), []string{"setup"}); err != nil {
		t.Fatal(err)
	}
	assertSetupConfig(t, f.app, "claude")
	first := savedGmail(t, f.app)
	if f.google.signIns != 1 || f.credentials.secrets[first.Credential] != "refresh-person" {
		t.Fatalf("setup must sign in once and save the credential: %d sign-ins, %v", f.google.signIns, f.credentials.secrets)
	}
	assertFirstSetupOutput(t, f)
	f.app.in = strings.NewReader("\n\nn\n") // keep providers, keep the connected account, no test
	if err := f.app.run(context.Background(), []string{"setup"}); err != nil {
		t.Fatal(err)
	}
	if f.google.signIns != 1 || savedGmail(t, f.app) != first {
		t.Fatal("rerunning setup and keeping the account must not sign in again")
	}
}

func assertFirstSetupOutput(t *testing.T, f setupFixture) {
	t.Helper()
	out := f.output.String()
	if len(f.sender.messages) != 0 || strings.Contains(out, "refresh-person") {
		t.Fatal("setup sent an unrequested email or printed the refresh token")
	}
	for _, want := range []string{"Claude: signed in; usage reading OK", "IST, UTC+05:30", "person@example.test"} {
		if !strings.Contains(out, want) {
			t.Fatalf("setup output lacks %q: %s", want, out)
		}
	}
}

func TestSetupInteractiveCanSwitchGmailAccount(t *testing.T) {
	f := connected(t)
	old := savedGmail(t, f.app)
	f.app.setupTerminal = func() bool { return true }
	f.google.name = "other"
	f.app.in = strings.NewReader("2\nn\n") // connect a different account, no test
	if err := f.app.run(context.Background(), []string{"setup", "--providers", "claude"}); err != nil {
		t.Fatal(err)
	}
	if account := savedGmail(t, f.app); account.Address != "other@example.test" || f.credentials.secrets[old.Credential] != "" {
		t.Fatalf("switching must activate the new account and delete the old credential: %+v %v", account, f.credentials.secrets)
	}
}

func TestSetupFailedSignInKeepsTheExistingConnection(t *testing.T) {
	f := connected(t)
	before := savedGmail(t, f.app)
	f.app.setupTerminal = func() bool { return true }
	f.google.err = gmailauth.ErrDenied
	f.app.in = strings.NewReader("2\n")
	if err := f.app.run(context.Background(), []string{"setup", "--providers", "claude"}); !errors.Is(err, gmailauth.ErrDenied) {
		t.Fatalf("want the sign-in failure, got %v", err)
	}
	if savedGmail(t, f.app) != before || len(f.credentials.secrets) != 1 {
		t.Fatal("a failed sign-in changed the working connection")
	}
}

func TestSetupActivationFailureRemovesTheStagedCredential(t *testing.T) {
	f := connected(t)
	runOK(t, f.app, "setup", "--providers", "claude", "--non-interactive")
	reset := time.Now().Add(time.Minute).Unix()
	claudeLine(t, f.app, "", map[string][2]int64{"five_hour": {100, reset}})
	if _, err := auditStateDB(t, f.app).Claim(context.Background(), time.Now().Add(2*time.Minute).UTC()); err != nil {
		t.Fatal(err) // a live delivery lease blocks account changes
	}
	f.app.setupTerminal = func() bool { return true }
	f.google.name = "other"
	f.app.in = strings.NewReader("2\n")
	if err := f.app.run(context.Background(), []string{"setup", "--providers", "claude"}); err == nil || !strings.Contains(err.Error(), "delivery is in progress") {
		t.Fatalf("want the delivery-in-progress refusal, got %v", err)
	}
	if savedGmail(t, f.app).Address != "person@example.test" || len(f.credentials.secrets) != 1 {
		t.Fatalf("the staged credential must be removed and the old account kept: %v", f.credentials.secrets)
	}
}

func TestSetupNonInteractiveNeedsAWorkingGmailConnection(t *testing.T) {
	f := setupTestApplication(t)
	if err := f.app.run(context.Background(), setupTestArguments("claude")); err == nil || !strings.Contains(err.Error(), "gmail connect") {
		t.Fatalf("noninteractive setup without Gmail must explain gmail connect, got %v", err)
	}
	if f.google.signIns != 0 {
		t.Fatal("noninteractive setup must never open a sign-in")
	}
	assertSetupInactive(t, f.app)
	g := connected(t)
	if err := auditStateDB(t, g.app).PauseGmail(context.Background(), savedGmail(t, g.app).Credential, "revoked"); err != nil {
		t.Fatal(err)
	}
	if err := g.app.run(context.Background(), setupTestArguments("claude")); err == nil {
		t.Fatal("a paused connection is not usable for noninteractive setup")
	}
}

func TestSetupRejectsRemovedAddressFlags(t *testing.T) {
	f := connected(t)
	for _, flag := range []string{"--email", "--from"} {
		err := f.app.run(context.Background(), append(setupTestArguments("claude"), flag, "someone@example.test"))
		if !errors.Is(err, errAddressFlags) {
			t.Fatalf("%s must explain the Gmail migration, got %v", flag, err)
		}
	}
}

func TestSetupNonInteractiveBothPersistsReadings(t *testing.T) {
	f := connected(t)
	binary := setupTestCodex(t, f.app)
	if err := f.app.run(context.Background(), append(setupTestArguments("both"), "--codex-bin", binary)); err != nil {
		t.Fatal(err)
	}
	assertSetupConfig(t, f.app, "both")
	if readSetupSnapshot(t, f.app, "codex").AccountID != "fixture-account" || len(f.sender.messages) != 0 {
		t.Fatal("setup did not save Codex's initial reading or sent an unrequested email")
	}
	if readSetupSnapshot(t, f.app, "claude").Provider != "claude" {
		t.Fatal("setup did not save Claude's initial reading")
	}
	if _, err := os.Stat(filepath.Join(f.app.getenv("CLAUDE_CONFIG_DIR"), "settings.json")); !os.IsNotExist(err) {
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
			f := setupTestApplication(t)
			f.app.setupTerminal = func() bool { return true }
			f.app.in = strings.NewReader(test.input)
			binary := setupTestCodex(t, f.app)
			reads := 0
			f.app.setupReadCodex = func(context.Context, string) (quota.Snapshot, error) {
				reads++
				if reads == 1 {
					return quota.Snapshot{}, errors.New("fixture authentication failed")
				}
				return setupTestSnapshot("codex"), nil
			}
			if err := f.app.run(context.Background(), []string{"setup", "--providers", "both", "--codex-bin", binary}); err != nil {
				t.Fatal(err)
			}
			assertSetupConfig(t, f.app, test.providers)
			if reads != test.wantReads {
				t.Fatalf("provider reads = %d; want %d", reads, test.wantReads)
			}
		})
	}
}

func TestSetupProviderFailureCancelOrSkipOnlyProvider(t *testing.T) {
	for _, input := range []string{"3\n", "2\n"} {
		f := setupTestApplication(t)
		f.app.setupTerminal = func() bool { return true }
		f.app.in = strings.NewReader(input)
		binary := setupTestCodex(t, f.app)
		f.app.setupReadCodex = func(context.Context, string) (quota.Snapshot, error) {
			return quota.Snapshot{}, errors.New("fixture unavailable")
		}
		if err := f.app.run(context.Background(), []string{"setup", "--providers", "codex", "--codex-bin", binary}); err == nil {
			t.Fatal("canceled/empty provider setup succeeded")
		}
		assertSetupInactive(t, f.app)
		if len(f.sender.messages) != 0 || f.google.signIns != 0 {
			t.Fatal("a canceled setup sent email or signed in to Google")
		}
	}
}

// installOldClaudeStatusLine reproduces what older versions left behind: ResetWatch's status line
// in the settings and a backup of the user's original entry.
func installOldClaudeStatusLine(t *testing.T, app *application, original []byte) {
	t.Helper()
	installer, err := app.claudeInstaller()
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(original, &settings); err != nil {
		t.Fatal(err)
	}
	installed := json.RawMessage(`{"type":"command","command":"resetwatch claude-statusline"}`)
	backup, err := json.Marshal(map[string]any{"version": 1, "settings_path": filepath.Join(installer.ConfigDir, "settings.json"),
		"had_entry": settings["statusLine"] != nil, "original": settings["statusLine"], "installed": installed})
	if err != nil {
		t.Fatal(err)
	}
	settings["statusLine"] = installed
	current, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeSetupSettings(t, app, current)
	if err := os.MkdirAll(installer.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installer.StateDir, "claude-statusline.json"), backup, 0600); err != nil {
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
	f := connected(t)
	installOldClaudeStatusLine(t, f.app, []byte(`{"statusLine":{"type":"command","command":"original-status"},"theme":"fixture"}`))
	if err := f.app.run(context.Background(), setupTestArguments("claude")); err != nil {
		t.Fatal(err)
	}
	assertSetupConfig(t, f.app, "claude")
	assertOriginalClaudeStatusLine(t, f.app)
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
			f := connected(t)
			args := setupTestArguments("claude")
			if test.request {
				args = append(args, "--test-email")
			}
			if test.fail {
				f.sender.err = errors.New("fixture email rejection")
			}
			err := f.app.run(context.Background(), args)
			if (err != nil) != test.fail {
				t.Fatalf("test-email result mismatch: %v", err)
			}
			assertSetupConfig(t, f.app, "claude")
			if len(f.sender.messages) != test.sends {
				t.Fatalf("email attempts = %d; want %d", len(f.sender.messages), test.sends)
			}
		})
	}
}

func TestSetupInteractiveEmailFailureCanBeDeclined(t *testing.T) {
	f := setupTestApplication(t)
	f.app.setupTerminal = func() bool { return true }
	f.app.in = strings.NewReader("y\nn\n")
	f.sender.err = errors.New("fixture email rejection")
	if err := f.app.run(context.Background(), []string{"setup", "--providers", "claude"}); err != nil {
		t.Fatal(err)
	}
	assertSetupConfig(t, f.app, "claude")
	if len(f.sender.messages) != 1 || f.sender.messages[0].To != "person@example.test" {
		t.Fatal("the interactive test must send exactly one attempt to the Gmail account")
	}
}

func TestSetupEOFAndControlCancelBeforeChanges(t *testing.T) {
	for _, input := range []string{"", "\x03"} {
		f := setupTestApplication(t)
		f.app.setupTerminal = func() bool { return true }
		f.app.in = strings.NewReader(input)
		if err := f.app.run(context.Background(), []string{"setup"}); !errors.Is(err, errSetupCancelled) {
			t.Fatalf("incomplete setup was accepted: %v", err)
		}
		assertSetupInactive(t, f.app)
		if len(f.sender.messages) != 0 || f.google.signIns != 0 {
			t.Fatal("a canceled setup sent email or signed in")
		}
	}
	f := connected(t)
	f.app.setupTerminal = func() bool { return true }
	f.app.in = strings.NewReader("3\n") // cancel at the Gmail account choice
	if err := f.app.run(context.Background(), []string{"setup", "--providers", "claude"}); !errors.Is(err, errSetupCancelled) {
		t.Fatalf("cancelling at the Gmail choice must cancel setup, got %v", err)
	}
	if connectSavedConfig(t, f.app).Providers != "" {
		t.Fatal("a canceled setup saved providers")
	}
}

func TestSetupClaudeReadFailureLeavesConfigInactive(t *testing.T) {
	f := setupTestApplication(t)
	f.app.claude = fakeClaude{err: quota.ErrNoClaudeLimits}
	if err := f.app.run(context.Background(), setupTestArguments("claude")); err == nil {
		t.Fatal("setup accepted Claude without plan limits")
	}
	assertSetupInactive(t, f.app)
}

func assertSetupConfig(t *testing.T, app *application, provider string) {
	t.Helper()
	config := connectSavedConfig(t, app)
	if config.Providers != provider || config.Gmail.Address != "person@example.test" {
		t.Fatalf("setup configuration = %#v", config)
	}
}

func assertSetupInactive(t *testing.T, app *application) {
	t.Helper()
	config := connectSavedConfig(t, app)
	if config.Providers != "" || config.Gmail.Connected() {
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
