package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

// claudeStatusLine is a silent no-op so Claude Code status lines installed by older versions show
// nothing instead of an error; setup restores the original status line.
// shortcut: keeps a retired command alive; remove once older installs have had time to reconnect.
func (a *application) claudeStatusLine(args []string) error {
	_, _ = io.Copy(io.Discard, a.in)
	return nil
}

// claudeReader returns the Claude usage reader; tests replace it.
func (a *application) claudeReader() quota.Provider {
	if a.claude != nil {
		return a.claude
	}
	return quota.Claude{Timeout: 20 * time.Second}
}

// restoreClaudeStatusLine puts back the status line older versions replaced; it does nothing otherwise.
func (a *application) restoreClaudeStatusLine(ctx context.Context) {
	installer, err := a.claudeInstaller()
	if err == nil {
		err = installer.Restore(ctx)
	}
	if err != nil {
		fmt.Fprintln(a.errOut, "Could not restore your previous Claude status line:", err)
	}
}

// claudeProvider polls Claude when selected; legacy configurations poll only once Claude was connected.
func (a *application) claudeProvider(ctx context.Context, config store.Config) quota.Provider {
	if !config.Selected("claude") {
		return nil
	}
	if config.Providers == "" {
		if _, err := a.db.Snapshot(ctx, "claude"); err != nil {
			return nil
		}
	}
	return a.claudeReader()
}
