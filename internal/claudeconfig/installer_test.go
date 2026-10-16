package claudeconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"resetwatch/internal/localfile"
)

func testInstaller(t *testing.T) Installer {
	t.Helper()
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Installer{Executable: executable, StateDir: filepath.Join(dir, "state"), ConfigDir: filepath.Join(dir, "claude")}
}

func putSettings(t *testing.T, i Installer, value string) {
	t.Helper()
	if err := os.MkdirAll(i.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(i.settingsPath(), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func mustInstall(t *testing.T, i Installer, replace bool) {
	t.Helper()
	if err := i.Install(context.Background(), replace); err != nil {
		t.Fatal(err)
	}
}

func mustRestore(t *testing.T, i Installer) {
	t.Helper()
	if err := i.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func settings(t *testing.T, i Installer) document {
	t.Helper()
	doc, err := readDocument(i.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestInstallAndRestorePreserveUnrelatedSettingsAndOriginalHook(t *testing.T) {
	i := testInstaller(t)
	putSettings(t, i, `{"model":"original-model","permissions":{"allow":["Read(foo)"]},"statusLine":{"type":"command","command":"old-hook","padding":2}}`)
	if err := i.Install(context.Background(), false); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing hook did not require replacement approval: %v", err)
	}
	if _, err := os.Stat(i.backupPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("declining replacement created a backup")
	}
	mustInstall(t, i, true)
	backup, err := os.ReadFile(i.backupPath())
	if err != nil {
		t.Fatal(err)
	}
	mustInstall(t, i, false)
	again, err := os.ReadFile(i.backupPath())
	if err != nil || !bytes.Equal(backup, again) {
		t.Fatal("rerunning setup overwrote the original backup")
	}
	doc := settings(t, i)
	doc.fields["model"] = json.RawMessage(`"new-model"`)
	if err := writeDocument(context.Background(), i.settingsPath(), doc); err != nil {
		t.Fatal(err)
	}
	mustRestore(t, i)
	assertField(t, settings(t, i), "model", `"new-model"`)
	assertField(t, settings(t, i), "permissions", `{"allow":["Read(foo)"]}`)
	assertField(t, settings(t, i), "statusLine", `{"type":"command","command":"old-hook","padding":2}`)
}

func TestRestoreRemovesOnlyAnOriginallyAbsentEntry(t *testing.T) {
	i := testInstaller(t)
	mustInstall(t, i, false)
	doc := settings(t, i)
	doc.fields["theme"] = json.RawMessage(`"new-theme"`)
	if err := writeDocument(context.Background(), i.settingsPath(), doc); err != nil {
		t.Fatal(err)
	}
	mustRestore(t, i)
	doc = settings(t, i)
	if _, present := doc.fields["statusLine"]; present {
		t.Fatal("restoration retained the newly installed hook")
	}
	assertField(t, doc, "theme", `"new-theme"`)
	mustRestore(t, i)
}

func TestExternalHookEditIsPreservedUntilExplicitReplacement(t *testing.T) {
	i := testInstaller(t)
	putSettings(t, i, `{"statusLine":{"type":"command","command":"first-hook"}}`)
	mustInstall(t, i, true)
	putSettings(t, i, `{"statusLine":{"type":"command","command":"external-hook"},"theme":"user-theme"}`)
	before, err := os.ReadFile(i.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Restore(context.Background()); !errors.Is(err, ErrOwnership) {
		t.Fatalf("undo did not detect an external edit: %v", err)
	}
	if err := i.Install(context.Background(), false); !errors.Is(err, ErrConflict) {
		t.Fatalf("external hook did not require explicit replacement: %v", err)
	}
	after, err := os.ReadFile(i.settingsPath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("an ownership conflict changed the user's settings")
	}
	mustInstall(t, i, true)
	mustRestore(t, i)
	assertField(t, settings(t, i), "statusLine", `{"type":"command","command":"external-hook"}`)
}

func TestInterruptedInstallResumesFromItsOriginalBackup(t *testing.T) {
	i := testInstaller(t)
	putSettings(t, i, `{"statusLine":null,"model":"kept"}`)
	doc := settings(t, i)
	installed, err := statusEntry(doc.fields["statusLine"], i.launcher())
	if err != nil {
		t.Fatal(err)
	}
	backup, err := i.installBackup(doc, installed, true)
	if err != nil {
		t.Fatal(err)
	}
	writeTestBackup(t, i, backup)
	mustInstall(t, i, false)
	mustRestore(t, i)
	assertField(t, settings(t, i), "statusLine", `null`)
	assertField(t, settings(t, i), "model", `"kept"`)
}

func TestChangedSettingsAreNotOverwritten(t *testing.T) {
	i := testInstaller(t)
	putSettings(t, i, `{"theme":"first"}`)
	doc := settings(t, i)
	doc.fields["statusLine"] = json.RawMessage(`{"type":"command","command":"new"}`)
	putSettings(t, i, `{"theme":"external"}`)
	if err := writeDocument(context.Background(), i.settingsPath(), doc); err == nil {
		t.Fatal("overwrote settings changed after the initial read")
	}
	assertField(t, settings(t, i), "theme", `"external"`)
}

func TestInstallerRejectsMalformedAndNonregularSettings(t *testing.T) {
	for _, value := range []string{"invalid fixture secret", "null", "[]", strings.Repeat("x", maxSettings+1)} {
		i := testInstaller(t)
		putSettings(t, i, value)
		err := i.Install(context.Background(), true)
		if err == nil || strings.Contains(err.Error(), "fixture secret") {
			t.Fatal("invalid settings were accepted or exposed")
		}
	}
	i := testInstaller(t)
	if err := os.MkdirAll(i.settingsPath(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := i.Install(context.Background(), true); err == nil {
		t.Fatal("accepted a directory as settings.json")
	}
}

func TestInstallerRejectsSymlinkAndCancelledContext(t *testing.T) {
	i := testInstaller(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := i.Install(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled setup was not stopped")
	}
	putSettings(t, i, `{"model":"kept"}`)
	target := filepath.Join(i.ConfigDir, "actual.json")
	if err := os.Rename(i.settingsPath(), target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, i.settingsPath()); err != nil {
		t.Skip("symlink creation unavailable")
	}
	if err := i.Install(context.Background(), true); err == nil {
		t.Fatal("accepted a symlink as settings.json")
	}
}

func TestBackupCannotTargetAnotherConfigurationDirectory(t *testing.T) {
	i := testInstaller(t)
	mustInstall(t, i, false)
	i.ConfigDir = filepath.Join(filepath.Dir(i.ConfigDir), "another-profile")
	if err := i.Restore(context.Background()); err == nil {
		t.Fatal("restored a backup into another Claude profile")
	}
}

func TestInvalidBackupNeverChangesSettings(t *testing.T) {
	for _, invalid := range []string{"fixture secret", `{"version":2}`, "null entry"} {
		i := testInstaller(t)
		putSettings(t, i, `{"theme":"kept"}`)
		if invalid == "null entry" {
			data, err := json.Marshal(journal{Version: 1, SettingsPath: i.settingsPath(), Installed: json.RawMessage(`null`)})
			if err != nil {
				t.Fatal(err)
			}
			invalid = string(data)
		}
		if err := os.MkdirAll(i.StateDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(i.backupPath(), []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		err := i.Install(context.Background(), true)
		if err == nil || strings.Contains(err.Error(), "fixture secret") {
			t.Fatal("invalid backup was accepted or exposed")
		}
		assertField(t, settings(t, i), "theme", `"kept"`)
	}
}

func writeTestBackup(t *testing.T, i Installer, backup journal) {
	t.Helper()
	if err := os.MkdirAll(i.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := localfile.Write(i.backupPath(), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func assertField(t *testing.T, doc document, name, value string) {
	t.Helper()
	expected := document{fields: map[string]json.RawMessage{name: json.RawMessage(value)}}
	current := document{fields: map[string]json.RawMessage{"statusLine": doc.fields[name]}}
	if !matchesEntry(current, true, expected.fields[name]) {
		t.Fatalf("field %s did not retain its expected value", name)
	}
}
