package claudeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const installedEntry = `{"type":"command","command":"resetwatch claude-statusline"}`

func testInstaller(t *testing.T) Installer {
	t.Helper()
	dir := t.TempDir()
	i := Installer{StateDir: filepath.Join(dir, "state"), ConfigDir: filepath.Join(dir, "claude")}
	for _, path := range []string{i.StateDir, i.ConfigDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return i
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// oldInstall reproduces what older ResetWatch versions left behind: their status line in the
// settings and a backup of the original entry (original "" means there was none).
func oldInstall(t *testing.T, i Installer, original, current string) {
	t.Helper()
	backup := map[string]any{"version": 1, "settings_path": i.settingsPath(), "had_entry": original != "",
		"installed": json.RawMessage(installedEntry)}
	if original != "" {
		backup["original"] = json.RawMessage(original)
	}
	data, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	write(t, i.backupPath(), string(data))
	write(t, i.settingsPath(), current)
}

func statusLine(t *testing.T, i Installer) (json.RawMessage, string) {
	t.Helper()
	doc, err := readDocument(i.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	return doc.fields["statusLine"], string(doc.fields["theme"])
}

func backupExists(i Installer) bool {
	_, err := os.Stat(i.backupPath())
	return err == nil
}

func TestRestorePutsBackOriginalAndKeepsOtherSettings(t *testing.T) {
	i := testInstaller(t)
	original := `{"type":"command","command":"original-status"}`
	oldInstall(t, i, original, `{"statusLine":`+installedEntry+`,"theme":"dark"}`)
	if err := i.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	line, theme := statusLine(t, i)
	if !matchesEntry(document{fields: map[string]json.RawMessage{"statusLine": line}}, true, json.RawMessage(original)) || theme != `"dark"` {
		t.Fatalf("original not restored or other settings changed: %s %s", line, theme)
	}
	if backupExists(i) {
		t.Fatal("backup should be removed after restoring")
	}
}

func TestRestoreRemovesEntryThatWasOriginallyAbsent(t *testing.T) {
	i := testInstaller(t)
	oldInstall(t, i, "", `{"statusLine":`+installedEntry+`,"theme":"dark"}`)
	if err := i.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if line, theme := statusLine(t, i); line != nil || theme != `"dark"` {
		t.Fatalf("status line should be removed and theme kept: %s %s", line, theme)
	}
}

func TestRestoreWithoutBackupChangesNothing(t *testing.T) {
	i := testInstaller(t)
	write(t, i.settingsPath(), `{"statusLine":{"type":"command","command":"mine"}}`)
	if err := i.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(i.settingsPath()); string(data) != `{"statusLine":{"type":"command","command":"mine"}}` {
		t.Fatalf("settings changed without a backup: %s", data)
	}
}

func TestRestoreKeepsAStatusLineTheUserChanged(t *testing.T) {
	i := testInstaller(t)
	edited := `{"statusLine":{"type":"command","command":"user-edit"}}`
	oldInstall(t, i, `{"type":"command","command":"original-status"}`, edited)
	if err := i.Restore(context.Background()); !errors.Is(err, ErrOwnership) {
		t.Fatalf("a user edit must be preserved with ErrOwnership, got %v", err)
	}
	if data, _ := os.ReadFile(i.settingsPath()); string(data) != edited || !backupExists(i) {
		t.Fatal("settings or backup changed after an ownership conflict")
	}
}

func TestRestoreWhenAlreadyOriginalOnlyDropsBackup(t *testing.T) {
	i := testInstaller(t)
	original := `{"type":"command","command":"original-status"}`
	oldInstall(t, i, original, `{"statusLine":`+original+`}`)
	if err := i.Restore(context.Background()); err != nil || backupExists(i) {
		t.Fatalf("already-restored settings should just drop the backup: %v", err)
	}
}

func TestRestoreAcceptsInterruptedReinstallEntry(t *testing.T) {
	i := testInstaller(t)
	previous := `{"type":"command","command":"resetwatch-old-launcher"}`
	backup := map[string]any{"version": 1, "settings_path": i.settingsPath(), "had_entry": false,
		"installed": json.RawMessage(installedEntry), "previous_installed": json.RawMessage(previous)}
	data, _ := json.Marshal(backup)
	write(t, i.backupPath(), string(data))
	write(t, i.settingsPath(), `{"statusLine":`+previous+`}`)
	if err := i.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if line, _ := statusLine(t, i); line != nil {
		t.Fatalf("previous launcher was not removed: %s", line)
	}
}

func TestRestoreRejectsBadBackupsAndSettings(t *testing.T) {
	cases := map[string]func(t *testing.T, i Installer){
		"invalid backup": func(t *testing.T, i Installer) {
			write(t, i.backupPath(), `not json`)
			write(t, i.settingsPath(), `{"statusLine":`+installedEntry+`}`)
		},
		"foreign settings path": func(t *testing.T, i Installer) {
			oldInstall(t, i, "", `{"statusLine":`+installedEntry+`}`)
			data, _ := json.Marshal(map[string]any{"version": 1, "settings_path": filepath.Join(t.TempDir(), "other.json"),
				"had_entry": false, "installed": json.RawMessage(installedEntry)})
			write(t, i.backupPath(), string(data))
		},
		"malformed settings": func(t *testing.T, i Installer) {
			oldInstall(t, i, "", `{"statusLine":`)
		},
	}
	for name, prepare := range cases {
		i := testInstaller(t)
		prepare(t, i)
		before, _ := os.ReadFile(i.settingsPath())
		if err := i.Restore(context.Background()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if after, _ := os.ReadFile(i.settingsPath()); string(after) != string(before) {
			t.Errorf("%s: settings changed after a rejected restore", name)
		}
	}
}

func TestRestoreRejectsRelativePathsAndCancelledContext(t *testing.T) {
	if err := (Installer{StateDir: "state", ConfigDir: "claude"}).Restore(context.Background()); err == nil {
		t.Fatal("relative paths must be rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := testInstaller(t).Restore(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context must stop restore, got %v", err)
	}
}
