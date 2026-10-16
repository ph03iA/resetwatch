package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"resetwatch/internal/store"
)

func TestResendAPIKeyEnvironmentWins(t *testing.T) {
	writeEnvFixture(t, "RESEND_API_KEY=\"unterminated_fixture_secret\n")
	app := &application{getenv: func(string) string { return "  re_environment_fixture  " }}
	key, err := app.resendAPIKey()
	if err != nil || key != "re_environment_fixture" {
		t.Fatal("process environment did not take precedence over malformed .env")
	}
}

func TestResendAPIKeyFileFormats(t *testing.T) {
	cases := []struct{ name, content, want string }{
		{"plain", "RESEND_API_KEY=re_fixture\n", "re_fixture"},
		{"spaces", " RESEND_API_KEY = re_fixture  \n", "re_fixture"},
		{"comments", "\n# comment\nRESEND_API_KEY=re_fixture# trailing comment\n", "re_fixture"},
		{"single quotes", "RESEND_API_KEY='re_fixture#literal' # comment\n", "re_fixture#literal"},
		{"double quotes", "RESEND_API_KEY=\"re_fixture\" # comment\n", "re_fixture"},
		{"BOM CRLF", "\ufeff# comment\r\nRESEND_API_KEY=\"re_fixture\"\r\n", "re_fixture"},
		{"literal values", "RESEND_API_KEY='${UNRELATED}\\literal=tail'\n", "${UNRELATED}\\literal=tail"},
		{"unrelated variables", "IGNORED=\"unterminated\nRESEND_API_KEY=re_fixture\n", "re_fixture"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			writeEnvFixture(t, test.content)
			app := &application{getenv: func(string) string { return " \t " }}
			key, err := app.resendAPIKey()
			if err != nil || key != test.want {
				t.Fatalf("fixture format was not loaded: %v", err)
			}
		})
	}
}

func TestResendAPIKeyMissing(t *testing.T) {
	for _, content := range []string{"", "# comment\n", "OTHER=re_fixture\n", "RESEND_API_KEY=\n", "RESEND_API_KEY='  '\n"} {
		writeEnvFixture(t, content)
		assertMissingResendKey(t)
	}
	t.Chdir(t.TempDir())
	assertMissingResendKey(t)
}

func assertMissingResendKey(t *testing.T) {
	t.Helper()
	app := &application{getenv: func(string) string { return "" }}
	_, err := app.resendAPIKey()
	if err == nil || !strings.Contains(err.Error(), "RESEND_API_KEY is missing") || !strings.Contains(err.Error(), ".env") {
		t.Fatalf("missing key did not explain both configuration options: %v", err)
	}
}

func TestResendEnvRejectsInvalidKeys(t *testing.T) {
	cases := []string{
		"RESEND_API_KEY",
		"RESEND_API_KEY=\"fixture_secret",
		"RESEND_API_KEY='fixture_secret\"",
		"RESEND_API_KEY=fixture_secret'",
		"RESEND_API_KEY=\"fixture_secret\" extra",
		"RESEND_API_KEY=\"fixture_secret\\\"tail\"",
		"RESEND_API_KEY=fixture_secret\nRESEND_API_KEY=fixture_secret\n",
		"RESEND_API_KEY=\nRESEND_API_KEY=fixture_secret\n",
	}
	for _, content := range cases {
		_, err := readResendEnv(strings.NewReader(content))
		if err == nil || strings.Contains(err.Error(), "fixture_secret") {
			t.Fatal("invalid .env key was accepted or exposed in the error")
		}
	}
}

func TestResendEnvBoundAndReadErrors(t *testing.T) {
	const prefix = "RESEND_API_KEY=re_fixture\n#"
	content := prefix + strings.Repeat("x", maxEnvSize-len(prefix))
	if _, err := readResendEnv(strings.NewReader(content)); err != nil {
		t.Fatalf("64 KiB fixture was rejected: %v", err)
	}
	if _, err := readResendEnv(strings.NewReader(content + "x")); err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatal("oversized .env was not rejected")
	}
	if _, err := readResendEnv(envFailureReader{}); err == nil || strings.Contains(err.Error(), "fixture_secret") {
		t.Fatal("reader failure was accepted or exposed in the error")
	}
}

type envFailureReader struct{}

func (envFailureReader) Read([]byte) (int, error) {
	return 0, errors.New("fixture_secret read failure")
}

func TestResendAPIKeyRejectsNonRegularFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".env", 0700); err != nil {
		t.Fatal(err)
	}
	app := &application{getenv: func(string) string { return "" }}
	if _, err := app.resendAPIKey(); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("non-regular .env was not rejected: %v", err)
	}
}

func TestResendEnvDoesNotExportUnrelatedVariables(t *testing.T) {
	const name = "RESETWATCH_DOTENV_UNRELATED_FIXTURE"
	t.Setenv(name, "process-fixture")
	writeEnvFixture(t, name+"=file-fixture\nRESEND_API_KEY=re_fixture\n")
	app := &application{getenv: func(string) string { return "" }}
	if _, err := app.resendAPIKey(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(name) != "process-fixture" {
		t.Fatal(".env changed the process environment")
	}
}

func TestEmailLoadsDotEnvWithoutState(t *testing.T) {
	writeEnvFixture(t, "RESEND_API_KEY=re_fixture_secret\n")
	app, output := testApplication(t)
	app.getenv = func(name string) string {
		if name != "RESEND_API_KEY" {
			t.Fatalf("test-email accessed unrelated environment variable %q", name)
		}
		return ""
	}
	sender := &capturedTestSender{}
	app.testSender = sender
	if err := app.run(context.Background(), []string{"test-email", "--email", "fixture@example.test"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 || app.db != nil {
		t.Fatal("expected one fake send without opening local state")
	}
	if strings.Contains(output.String(), "re_fixture_secret") {
		t.Fatal("test-email output exposed the key")
	}
}

func TestWatchLoadsDotEnvWithFakeCodex(t *testing.T) {
	writeEnvFixture(t, "RESEND_API_KEY=re_fixture\n")
	t.Setenv("RESETWATCH_CONNECT_HELPER", "1")
	app := envWatchApplication(t, connectTestExecutable(t))
	if err := app.watch(context.Background(), []string{"--once"}); err != nil {
		t.Fatalf("fake watch could not load .env: %v", err)
	}
}

func TestWatchRejectsDotEnvBeforeProviderRead(t *testing.T) {
	writeEnvFixture(t, "RESEND_API_KEY=fixture_secret\nRESEND_API_KEY=fixture_secret\n")
	app := envWatchApplication(t, filepath.Join(t.TempDir(), "missing-codex-native.exe"))
	if err := app.watch(context.Background(), []string{"--once"}); err == nil || !strings.Contains(err.Error(), "duplicate RESEND_API_KEY") {
		t.Fatalf("watch did not reject .env before reading quota: %v", err)
	}
}

func envWatchApplication(t *testing.T, binary string) *application {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.SaveConfig(context.Background(), store.Config{To: "fixture@example.test", From: "sender@example.test", CodexBin: binary}); err != nil {
		t.Fatal(err)
	}
	return &application{out: io.Discard, errOut: io.Discard, db: db, getenv: func(string) string { return "" }}
}

func writeEnvFixture(t *testing.T, content string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile(filepath.Join(".", ".env"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
