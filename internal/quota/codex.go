package quota

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Codex struct {
	Binary  string
	Timeout time.Duration
}

func (provider Codex) Read(ctx context.Context) (Snapshot, error) {
	timeout := provider.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	binary, err := codexBinary(provider.Binary)
	if err != nil {
		return Snapshot{}, err
	}
	client, err := startCodex(ctx, binary)
	if err != nil {
		return Snapshot{}, err
	}
	defer client.close()
	return client.snapshot()
}

// ResolveBinary fixes explicit executable configuration to an absolute native path.
// An empty value preserves the default PATH lookup at read time.
func ResolveBinary(configured string) (string, error) {
	if configured == "" {
		return "", nil
	}
	path, err := codexBinary(configured)
	if err != nil {
		return "", err
	}
	path, err = exec.LookPath(path)
	if err != nil {
		return "", errors.New("Codex native executable not found; check --codex-bin")
	}
	path, err = nativeBinary(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func codexBinary(configured string) (string, error) {
	if configured != "" {
		return nativeBinary(configured)
	}
	name := "codex"
	if runtime.GOOS == "windows" {
		name = "codex.exe"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.New("Codex native executable not found; install Codex or set --codex-bin to its native executable")
	}
	return nativeBinary(path)
}

func nativeBinary(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat", ".ps1":
		return "", errors.New("Codex shell shims are unsupported; set --codex-bin to the native codex executable")
	}
	return path, nil
}

func (client *codexClient) snapshot() (Snapshot, error) {
	if err := client.initialize(); err != nil {
		return Snapshot{}, err
	}
	var account codexAccountResponse
	if err := client.call(1, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		return Snapshot{}, err
	}
	identity, err := accountIdentity(account)
	if err != nil {
		return Snapshot{}, err
	}
	var limits codexLimitsResponse
	if err := client.call(2, "account/rateLimits/read", nil, &limits); err != nil {
		return Snapshot{}, err
	}
	return decodeSnapshot(identity, time.Now().UTC(), limits)
}

func (client *codexClient) initialize() error {
	params := map[string]any{
		"clientInfo": map[string]string{"name": "resetwatch", "title": "ResetWatch", "version": "0.1.0"},
	}
	var result map[string]any
	if err := client.call(0, "initialize", params, &result); err != nil {
		return err
	}
	return client.send(rpcMessage{Method: "initialized", Params: map[string]any{}})
}
