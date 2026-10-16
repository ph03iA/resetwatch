package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/store"
)

func TestMain(tests *testing.M) {
	if os.Getenv("RESETWATCH_CONNECT_HELPER") == "1" && len(os.Args) == 2 && os.Args[1] == "app-server" {
		pauseConnectFixture()
		fmt.Fprintln(os.Stdout, `{"id":0,"result":{}}
{"id":1,"result":{"account":{"type":"chatgpt","email":"fixture@example.test"}}}
{"id":2,"result":{"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":4102444800}}}}}`)
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(tests.Run())
}

func pauseConnectFixture() {
	gate := os.Getenv("RESETWATCH_CONNECT_GATE")
	if gate == "" {
		return
	}
	if err := os.WriteFile(gate+".ready", nil, 0600); err != nil {
		os.Exit(41)
	}
	if err := waitConnectFile(gate + ".continue"); err != nil {
		os.Exit(42)
	}
}

func waitConnectFile(path string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("fake Codex gate timed out: %s", path)
}

func TestConnectPersistsAbsoluteBinary(t *testing.T) {
	binary := connectTestExecutable(t)
	t.Setenv("RESETWATCH_CONNECT_HELPER", "1")
	t.Chdir(filepath.Dir(binary))
	app, output := testApplication(t)
	relative := "." + string(filepath.Separator) + filepath.Base(binary)
	if err := app.run(context.Background(), []string{"connect", "codex", "--codex-bin", relative}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "next cycle") {
		t.Fatal("connect must explain live connection changes")
	}
	if got := connectSavedConfig(t, app).CodexBin; got != binary {
		t.Fatalf("saved executable = %q, want absolute path %q", got, binary)
	}
	t.Chdir(t.TempDir())
	if err := app.run(context.Background(), []string{"status", "--refresh", "--json"}); err != nil {
		t.Fatalf("refresh after changing directory: %v", err)
	}
}

func TestConnectPersistsPATHBinary(t *testing.T) {
	binary := connectTestExecutable(t)
	t.Setenv("RESETWATCH_CONNECT_HELPER", "1")
	t.Setenv("PATH", filepath.Dir(binary))
	t.Chdir(t.TempDir())
	app, _ := testApplication(t)
	if err := app.run(context.Background(), []string{"connect", "codex", "--codex-bin", filepath.Base(binary)}); err != nil {
		t.Fatal(err)
	}
	if got := connectSavedConfig(t, app).CodexBin; got != binary {
		t.Fatalf("saved executable = %q, want resolved path %q", got, binary)
	}
	t.Setenv("PATH", "")
	t.Chdir(t.TempDir())
	if err := app.run(context.Background(), []string{"status", "--refresh", "--json"}); err != nil {
		t.Fatalf("refresh after changing PATH and directory: %v", err)
	}
}

func TestConnectPreservesNotificationChangeDuringRead(t *testing.T) {
	binary := connectTestExecutable(t)
	gate := filepath.Join(t.TempDir(), "connect")
	t.Setenv("RESETWATCH_CONNECT_HELPER", "1")
	t.Setenv("RESETWATCH_CONNECT_GATE", gate)
	app, _ := testApplication(t)
	if err := app.run(context.Background(), []string{"notify", "--email", "old@example.test", "--from", "old-sender@example.test"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.run(ctx, []string{"connect", "codex", "--codex-bin", binary}) }()
	if err := waitConnectFile(gate + ".ready"); err != nil {
		t.Fatal(err)
	}
	notifyApp := &application{out: io.Discard, errOut: io.Discard, getenv: app.getenv}
	if err := notifyApp.run(ctx, []string{"notify", "--email", "new@example.test", "--from", "new-sender@example.test"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gate+".continue", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	config := connectSavedConfig(t, app)
	if config.To != "new@example.test" || config.From != "new-sender@example.test" {
		t.Fatalf("connect overwrote newer notification settings: %#v", config)
	}
}

func connectTestExecutable(t *testing.T) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), filepath.Base(binary))
	if err := os.Link(binary, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func connectSavedConfig(t *testing.T, app *application) store.Config {
	t.Helper()
	db, err := store.Open(app.getenv("RESETWATCH_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	config, err := db.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return config
}
