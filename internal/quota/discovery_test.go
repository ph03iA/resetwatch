package quota

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNpmCodexDiscovery(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows native executable lookup")
	}
	root := t.TempDir()
	path := filepath.Join(root, "npm", "node_modules", "@openai", "codex", "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "codex", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(binary, path); err != nil {
		t.Fatal(err)
	}
	found, err := npmCodexBinary(root)
	if err != nil || found != path {
		t.Fatalf("npm discovery = %q, %v; want %q", found, err, path)
	}
	// Automatic discovery must find the x64 package whatever this test binary's architecture is (386 included).
	t.Setenv("PATH", t.TempDir())
	t.Setenv("APPDATA", root)
	found, err = DiscoverBinary()
	if err != nil || found != path {
		t.Fatalf("automatic discovery = %q, %v; want %q", found, err, path)
	}
}

func TestNpmCodexDiscoveryRejectsMissingAndRelativeRoots(t *testing.T) {
	for _, root := range []string{"", "relative", t.TempDir()} {
		if _, err := npmCodexBinary(root); err == nil {
			t.Fatalf("unexpected binary in %q", root)
		}
	}
}
