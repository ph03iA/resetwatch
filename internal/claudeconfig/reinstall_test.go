package claudeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

const originalHook = `{"type":"command","command":"user-hook","padding":2}`

func movedInstaller(t *testing.T) (Installer, json.RawMessage) {
	t.Helper()
	i := testInstaller(t)
	putSettings(t, i, `{"theme":"kept","statusLine":`+originalHook+`}`)
	mustInstall(t, i, true)
	previous := settings(t, i).fields["statusLine"]
	i.Executable = filepath.Join(filepath.Dir(i.Executable), "moved-resetwatch")
	return i, previous
}

func prepareReinstall(t *testing.T, i Installer) journal {
	t.Helper()
	doc := settings(t, i)
	installed, err := statusEntry(doc.fields["statusLine"], i.launcher())
	if err != nil {
		t.Fatal(err)
	}
	backup, err := i.installBackup(doc, installed, false)
	if err != nil {
		t.Fatal(err)
	}
	writeTestBackup(t, i, backup)
	return backup
}

func assertOriginalHook(t *testing.T, i Installer) {
	t.Helper()
	assertField(t, settings(t, i), "statusLine", originalHook)
	assertField(t, settings(t, i), "theme", `"kept"`)
}

func TestInterruptedReinstallCanRestorePreviousOwnedHook(t *testing.T) {
	i, previous := movedInstaller(t)
	backup := prepareReinstall(t, i)
	assertField(t, settings(t, i), "statusLine", string(previous))
	if len(backup.PreviousInstalled) == 0 {
		t.Fatal("reinstall journal omitted the still-installed launcher")
	}
	mustRestore(t, i)
	assertOriginalHook(t, i)
}

func TestInterruptedReinstallCanResumeWithoutLosingOriginal(t *testing.T) {
	i, _ := movedInstaller(t)
	prepareReinstall(t, i)
	// A third executable location must also recover an unfinished A-to-B update.
	i.Executable = filepath.Join(filepath.Dir(i.Executable), "third-resetwatch")
	mustInstall(t, i, false)
	backup, err := i.readBackup()
	if err != nil || len(backup.PreviousInstalled) != 0 {
		t.Fatal("completed reinstall retained pending launcher ownership")
	}
	mustRestore(t, i)
	assertOriginalHook(t, i)
}

func TestReinstallAfterSettingsWriteCanRestoreOrResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		i, _ := movedInstaller(t)
		backup := prepareReinstall(t, i)
		doc := settings(t, i)
		doc.fields["statusLine"] = backup.Installed
		if err := writeDocument(context.Background(), i.settingsPath(), doc); err != nil {
			t.Fatal(err)
		}
		if resume {
			mustInstall(t, i, false)
		}
		mustRestore(t, i)
		assertOriginalHook(t, i)
	}
}

func TestCompletedReinstallDoesNotOwnOldLauncher(t *testing.T) {
	i, previous := movedInstaller(t)
	mustInstall(t, i, false)
	doc := settings(t, i)
	doc.fields["statusLine"] = previous
	if err := writeDocument(context.Background(), i.settingsPath(), doc); err != nil {
		t.Fatal(err)
	}
	if err := i.Restore(context.Background()); !errors.Is(err, ErrOwnership) {
		t.Fatalf("finalized journal still owned a replaced launcher: %v", err)
	}
}

func TestInvalidPendingLauncherIsRejected(t *testing.T) {
	i, _ := movedInstaller(t)
	backup := prepareReinstall(t, i)
	backup.PreviousInstalled = json.RawMessage(`{"type":"command","command":""}`)
	writeTestBackup(t, i, backup)
	if err := i.Restore(context.Background()); err == nil {
		t.Fatal("accepted an invalid pending launcher")
	}
}
