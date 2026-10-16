package claudeconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestMain(tests *testing.M) {
	if os.Getenv("RESETWATCH_LAUNCHER_HELPER") == "1" && len(os.Args) == 2 && os.Args[1] == "claude-statusline" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(41)
		}
		result := map[string]string{"input": string(data), "home": os.Getenv("RESETWATCH_HOME")}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			os.Exit(42)
		}
		os.Exit(0)
	}
	os.Exit(tests.Run())
}

func TestWindowsLauncherUsesEncodedUTF8ForwardingAndLiteralPaths(t *testing.T) {
	executable, state := "C:/A B/O'Brien & $/東京/resetwatch.exe", "C:/State O'Brien & $/東京"
	command := launcherForOS(executable, state, "windows")
	const prefix = "powershell.exe -NoProfile -NonInteractive -EncodedCommand "
	if !strings.HasPrefix(command, prefix) {
		t.Fatal("Windows launcher must not depend on the outer shell's path quoting")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, prefix))
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[index*2:])
	}
	script := string(utf16.Decode(units))
	for _, want := range []string{powershellQuote(executable), powershellQuote(state), "[Console]::InputEncoding", "$OutputEncoding", "[Console]::OutputEncoding", "[Console]::In.ReadToEnd() | &"} {
		if !strings.Contains(script, want) {
			t.Fatalf("encoded launcher lost required forwarding or literal path: %s", want)
		}
	}
}

func TestLauncherPreservesJSONAndStateThroughSupportedShells(t *testing.T) {
	t.Setenv("RESETWATCH_LAUNCHER_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Executable O'Brien & $ 東京")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(dir, filepath.Base(executable))
	if err := os.Link(executable, linked); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "State O'Brien & $ 東京")
	command := launcherForOS(linked, state, runtime.GOOS)
	for _, shell := range launcherShells(t, command) {
		t.Run(shell.Path, func(t *testing.T) { assertForwarding(t, shell, state) })
	}
}

func launcherShells(t *testing.T, command string) []*exec.Cmd {
	t.Helper()
	if runtime.GOOS != "windows" {
		return []*exec.Cmd{exec.Command("sh", "-c", command)}
	}
	args := strings.Fields(command)
	commands := []*exec.Cmd{exec.Command(args[0], args[1:]...)}
	bash := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
	if info, err := os.Stat(bash); err == nil && info.Mode().IsRegular() {
		commands = append(commands, exec.Command(bash, "--noprofile", "--norc", "-c", command))
	}
	return commands
}

func assertForwarding(t *testing.T, command *exec.Cmd, state string) {
	t.Helper()
	input := []byte(`{"rate_limits":{"five_hour":{"used_percentage":100,"resets_at":4102444800}},"workspace":{"current_dir":"東京"}}`)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command = exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatal(fmt.Errorf("synthetic status line launcher failed: %w (%s)", err, stderr.String()))
	}
	var result map[string]string
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result["home"] != state || !bytes.Equal(bytes.TrimSpace([]byte(result["input"])), input) {
		t.Fatal("launcher changed UTF-8 quota JSON or the literal state directory")
	}
}
