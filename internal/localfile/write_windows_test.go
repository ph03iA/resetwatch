//go:build windows

package localfile

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestFailedReplacementKeepsOriginalAndCleansTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := Write(path, []byte("replacement"), 0600); err == nil {
		t.Fatal("expected the existing handle to deny replacement")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatal("failed replacement damaged the original")
	}
	assertNoTemporaryFiles(t, path)
}
