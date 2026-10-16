package localfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteCreatesAndReplacesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.json")
	for _, content := range []string{"original", "replacement"} {
		if err := Write(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != content {
			t.Fatalf("replacement was not saved: %v", err)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("private file permissions were not preserved")
		}
	}
	assertNoTemporaryFiles(t, path)
}

func TestWriteRejectsDirectoryAndSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, []byte("invalid"), 0600); err == nil {
		t.Fatal("replaced a directory")
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink creation unavailable")
	}
	if err := Write(link, []byte("invalid"), 0600); err == nil {
		t.Fatal("accepted a symlink target")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatal("symlink rejection changed its destination")
	}
	assertNoTemporaryFiles(t, link)
}

func assertNoTemporaryFiles(t *testing.T, path string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files were left behind: %v", matches)
	}
}
