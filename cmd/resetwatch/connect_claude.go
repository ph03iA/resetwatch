package main

import (
	"context"
	"fmt"
)

func (a *application) connectClaude(ctx context.Context, args []string) error {
	flags := a.flags("connect claude")
	replace := flags.Bool("replace-statusline", false, "replace an existing Claude status line after saving its original entry")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noArguments(flags.Args()); err != nil {
		return err
	}
	installer, err := a.claudeInstaller()
	if err != nil {
		return err
	}
	if err := installer.Install(ctx, *replace); err != nil {
		return fmt.Errorf("connect Claude: %w; use --replace-statusline only if you want to replace your current display", err)
	}
	if err := a.db.EnableProvider(ctx, "claude"); err != nil {
		return fmt.Errorf("Claude status line installed, but provider could not be enabled; rerun connect claude: %w", err)
	}
	fmt.Fprintln(a.out, "Claude status line installed; previous entry backed up. Use Claude Code to record your first reading.")
	fmt.Fprintln(a.out, "Provider changes apply on the next watcher cycle. Project or managed Claude settings may override this user setting.")
	return nil
}
