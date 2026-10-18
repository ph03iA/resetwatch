package quota

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// DiscoverBinary returns an absolute native executable, including Windows npm installs.
func DiscoverBinary() (string, error) {
	path, err := codexBinary("")
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

// npmCodexBinary checks every Windows package npm may have installed: a 32-bit ResetWatch
// build can still run the machine's 64-bit Codex, so the build architecture is not a guide.
func npmCodexBinary(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("Windows APPDATA directory is unavailable; use --codex-bin")
	}
	packageRoot := filepath.Join(root, "npm", "node_modules", "@openai", "codex")
	for _, target := range [][2]string{
		{"codex-win32-x64", "x86_64-pc-windows-msvc"},
		{"codex-win32-arm64", "aarch64-pc-windows-msvc"},
	} {
		if path, ok := npmVendorBinary(packageRoot, target[0], target[1]); ok {
			return path, nil
		}
	}
	return "", errors.New("Codex native executable not found in the Windows npm installation; install Codex or use --codex-bin")
}

func npmVendorBinary(packageRoot, packageName, triple string) (string, bool) {
	for _, vendor := range []string{
		filepath.Join(packageRoot, "node_modules", "@openai", packageName, "vendor", triple),
		filepath.Join(packageRoot, "vendor", triple),
	} {
		for _, subdir := range []string{"codex", "bin"} {
			if path, err := exec.LookPath(filepath.Join(vendor, subdir, "codex.exe")); err == nil {
				return path, true
			}
		}
	}
	return "", false
}

func defaultCodexBinary() (string, error) {
	name := "codex"
	if runtime.GOOS == "windows" {
		name = "codex.exe"
	}
	path, err := exec.LookPath(name)
	if err == nil {
		return nativeBinary(path)
	}
	if runtime.GOOS == "windows" {
		return npmCodexBinary(os.Getenv("APPDATA"))
	}
	return "", errors.New("Codex native executable not found; install Codex or use --codex-bin")
}
