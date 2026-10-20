package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"resetwatch/internal/quota"
)

func TestSetupRestoresDeselectedClaudeBeforeInitialCodexPersistenceFailure(t *testing.T) {
	app := connected(t).app
	settings := filepath.Join(app.getenv("CLAUDE_CONFIG_DIR"), "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	installOldClaudeStatusLine(t, app, []byte(`{"statusLine":{"type":"command","command":"original-display"},"theme":"dark"}`))
	binary := setupTestCodex(t, app)
	app.setupReadCodex = func(context.Context, string) (quota.Snapshot, error) {
		db, err := sql.Open("sqlite", filepath.Join(app.getenv("RESETWATCH_HOME"), "state.db"))
		if err != nil {
			return quota.Snapshot{}, err
		}
		defer db.Close()
		_, err = db.Exec(`CREATE TRIGGER fail_initial BEFORE INSERT ON snapshots BEGIN SELECT RAISE(FAIL,'synthetic initial write failure'); END`)
		return setupTestSnapshot("codex"), err
	}
	err := app.run(context.Background(), append(setupTestArguments("codex"), "--codex-bin", binary))
	if err == nil || !strings.Contains(err.Error(), "setup saved; initial codex reading") {
		t.Fatalf("expected saved setup with failed initial reading: %v", err)
	}
	assertSetupConfig(t, app, "codex")
	assertOriginalClaudeDisplay(t, settings)
}

func assertOriginalClaudeDisplay(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		StatusLine struct{ Command string }
		Theme      string
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.StatusLine.Command != "original-display" || document.Theme != "dark" {
		t.Fatal("saved selection failed to restore Claude before the independent reading error")
	}
}
