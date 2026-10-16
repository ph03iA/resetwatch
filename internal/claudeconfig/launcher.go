package claudeconfig

import (
	"encoding/base64"
	"encoding/binary"
	"runtime"
	"strings"
	"unicode/utf16"
)

func (i Installer) launcher() string { return launcherForOS(i.Executable, i.StateDir, runtime.GOOS) }

func launcherForOS(executable, state, goos string) string {
	if goos != "windows" {
		return "RESETWATCH_HOME=" + shellQuote(state) + " " + shellQuote(executable) + " claude-statusline"
	}
	// Claude invokes either Git Bash or PowerShell; encoding avoids a second shell's quoting rules.
	script := "$ProgressPreference='SilentlyContinue';[Console]::InputEncoding=[Text.UTF8Encoding]::new($false);" +
		"$OutputEncoding=[Text.UTF8Encoding]::new($false);[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);" +
		"$env:RESETWATCH_HOME=" + powershellQuote(state) + ";[Console]::In.ReadToEnd() | & " +
		powershellQuote(executable) + " claude-statusline;exit $LASTEXITCODE"
	return "powershell.exe -NoProfile -NonInteractive -EncodedCommand " + encodePowerShell(script)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(data)
}
