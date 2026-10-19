package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
	"resetwatch/internal/watch"
)

// claudeStatusLine is a silent no-op so Claude Code status lines installed by older versions show
// nothing instead of an error; connect claude and setup restore the original status line.
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

func (a *application) connectClaude(ctx context.Context, args []string) error {
	if err := noArguments(args); err != nil {
		return err
	}
	engine := watch.Engine{DB: a.db, Provider: a.claudeReader()}
	if _, err := engine.Observe(ctx); err != nil {
		return fmt.Errorf("connect Claude: %w", err)
	}
	if err := a.db.EnableProvider(ctx, "claude"); err != nil {
		return fmt.Errorf("Claude reading saved, but provider could not be enabled; rerun connect claude: %w", err)
	}
	fmt.Fprintln(a.out, "Connected to Claude. Usage is read from Claude Code without sending any prompt.")
	a.restoreClaudeStatusLine(ctx)
	fmt.Fprintln(a.out, "Provider changes apply on the next watcher cycle.")
	return nil
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
