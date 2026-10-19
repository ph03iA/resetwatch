package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"resetwatch/internal/store"
)

// TestMain lets the test binary act as a fake Codex app-server when started by the tests.
func TestMain(tests *testing.M) {
	if os.Getenv("RESETWATCH_CONNECT_HELPER") == "1" && len(os.Args) == 2 && os.Args[1] == "app-server" {
		fmt.Fprintln(os.Stdout, `{"id":0,"result":{}}
{"id":1,"result":{"account":{"type":"chatgpt","email":"fixture@example.test"}}}
{"id":2,"result":{"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":4102444800}}}}}`)
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(tests.Run())
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
