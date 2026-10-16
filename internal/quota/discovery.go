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

func npmCodexBinary(root, arch string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("Windows APPDATA directory is unavailable; use --codex-bin")
	}
	targets := map[string][2]string{
		"amd64": {"codex-win32-x64", "x86_64-pc-windows-msvc"},
		"arm64": {"codex-win32-arm64", "aarch64-pc-windows-msvc"},
	}
	target, ok := targets[arch]
	if !ok {
		return "", errors.New("unsupported Windows Codex architecture; use --codex-bin")
	}
	packageRoot := filepath.Join(root, "npm", "node_modules", "@openai", "codex")
	for _, vendor := range []string{
		filepath.Join(packageRoot, "node_modules", "@openai", target[0], "vendor", target[1]),
		filepath.Join(packageRoot, "vendor", target[1]),
	} {
		for _, subdir := range []string{"codex", "bin"} {
			candidate := filepath.Join(vendor, subdir, "codex.exe")
			if path, err := exec.LookPath(candidate); err == nil {
				return path, nil
			}
		}
	}
	return "", errors.New("Codex native executable not found in the Windows npm installation; install Codex or use --codex-bin")
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
		return npmCodexBinary(os.Getenv("APPDATA"), runtime.GOARCH)
	}
	return "", errors.New("Codex native executable not found; install Codex or use --codex-bin")
}
