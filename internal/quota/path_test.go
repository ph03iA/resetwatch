package quota

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveBinaryRelativeAbsoluteAndPATH(t *testing.T) {
	binary := resolverTestExecutable(t)
	t.Setenv("PATH", filepath.Dir(binary))
	t.Chdir(filepath.Dir(binary))
	for _, configured := range []string{"." + string(filepath.Separator) + filepath.Base(binary), binary, filepath.Base(binary)} {
		resolved, err := ResolveBinary(configured)
		if err != nil {
			t.Fatal(err)
		}
		if resolved != binary {
			t.Fatalf("ResolveBinary(%q) = %q, want %q", configured, resolved, binary)
		}
	}
}

func TestResolveBinaryPreservesDefault(t *testing.T) {
	resolved, err := ResolveBinary("")
	if err != nil || resolved != "" {
		t.Fatalf("default path changed: %q, %v", resolved, err)
	}
}

func TestResolveBinaryRejectsShellShims(t *testing.T) {
	for _, path := range []string{"codex.cmd", "CODEX.BAT", "codex.ps1"} {
		if _, err := ResolveBinary(path); err == nil || !strings.Contains(err.Error(), "shell shims") {
			t.Errorf("expected shell-shim rejection before lookup for %q, got %v", path, err)
		}
	}
}

func TestResolveBinaryRejectsPATHShim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PATH extension lookup")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.cmd"), []byte("invalid native executable"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("PATHEXT", ".CMD")
	t.Chdir(t.TempDir())
	if _, err := ResolveBinary("fixture"); err == nil || !strings.Contains(err.Error(), "shell shims") {
		t.Fatalf("expected resolved shell-shim rejection, got %v", err)
	}
}

func TestResolveBinaryMissingNative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-codex-native.exe")
	if _, err := ResolveBinary(path); err == nil || !strings.Contains(err.Error(), "native executable not found") {
		t.Fatalf("expected clear missing executable error, got %v", err)
	}
}

func resolverTestExecutable(t *testing.T) string {
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
