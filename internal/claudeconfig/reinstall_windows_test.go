//go:build windows

package claudeconfig

import (
	"context"
	"testing"

	"golang.org/x/sys/windows"
)

func lockSettings(t *testing.T, i Installer) func() {
	t.Helper()
	name, err := windows.UTF16PtrFromString(i.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return func() { windows.CloseHandle(handle) }
}

func TestLockedReinstallKeepsOriginalBackupAndCanRecover(t *testing.T) {
	for _, resume := range []bool{false, true} {
		i, previous := movedInstaller(t)
		unlock := lockSettings(t, i)
		err := i.Install(context.Background(), false)
		unlock()
		if err == nil {
			t.Fatal("expected locked settings to prevent launcher replacement")
		}
		assertField(t, settings(t, i), "statusLine", string(previous))
		if resume {
			mustInstall(t, i, false)
		}
		mustRestore(t, i)
		assertOriginalHook(t, i)
	}
}
