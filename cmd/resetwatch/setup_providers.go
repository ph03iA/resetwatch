package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"resetwatch/internal/claudeconfig"
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

func (a *application) prepareSetupProviders(ctx context.Context, s *setupState) error {
	var ready []string
	for _, provider := range (store.Config{Providers: s.options.providers}).ProvidersList() {
		ok, err := a.prepareSetupProvider(ctx, s, provider)
		if err != nil {
			return err
		}
		if ok {
			ready = append(ready, provider)
		}
	}
	if len(ready) == 0 {
		return errors.New("no provider was configured; previous setup remains active")
	}
	s.options.providers = strings.Join(ready, ",")
	if len(ready) == 2 {
		s.options.providers = "both"
	}
	return nil
}

func (a *application) prepareSetupProvider(ctx context.Context, s *setupState, provider string) (bool, error) {
	for {
		err := a.checkSetupProvider(ctx, s, provider)
		if err == nil {
			return true, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if !s.interactive {
			return false, fmt.Errorf("%s setup: %w", provider, err)
		}
		fmt.Fprintf(a.out, "%s: %v\n", provider, err)
		fmt.Fprintln(a.out, "For Codex, run codex login; for Claude, run claude and sign in with Pro or Max. Then retry. Skipping a provider cancels its unsent reminders when setup is saved.")
		choice, err := s.prompt.choose("Next action", []string{"Retry", "Skip this provider", "Cancel setup"}, 0)
		if err != nil {
			return false, err
		}
		if choice == 1 {
			return false, nil
		}
		if choice == 2 {
			return false, errSetupCancelled
		}
	}
}

func (a *application) checkSetupProvider(ctx context.Context, s *setupState, provider string) error {
	if provider == "codex" {
		binary, err := setupCodexBinary(s.options.binary)
		if err != nil {
			return err
		}
		reading, err := a.readSetupCodex(ctx, binary)
		if err != nil {
			return err
		}
		s.options.binary = binary
		s.readings = append(s.readings, reading)
		fmt.Fprintln(a.out, "Codex: signed in; quota reading OK.")
		return nil
	}
	reading, err := a.claudeReader().Read(ctx)
	if err != nil {
		return err
	}
	s.readings = append(s.readings, reading)
	fmt.Fprintln(a.out, "Claude: signed in; usage reading OK (no prompt sent).")
	return nil
}

func setupCodexBinary(configured string) (string, error) {
	if configured != "" {
		return quota.ResolveBinary(configured)
	}
	return quota.DiscoverBinary()
}

func (a *application) readSetupCodex(ctx context.Context, binary string) (quota.Snapshot, error) {
	if a.setupReadCodex != nil {
		return a.setupReadCodex(ctx, binary)
	}
	return (quota.Codex{Binary: binary, Timeout: 15 * time.Second, Version: version}).Read(ctx)
}

func (a *application) claudeInstaller() (claudeconfig.Installer, error) {
	state, err := stateDirectory(a.getenv("RESETWATCH_HOME"))
	if err != nil {
		return claudeconfig.Installer{}, err
	}
	configDir := a.getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return claudeconfig.Installer{}, err
		}
		configDir = filepath.Join(home, ".claude")
	}
	configDir, err = filepath.Abs(configDir)
	if err != nil {
		return claudeconfig.Installer{}, err
	}
	executable, err := os.Executable()
	return claudeconfig.Installer{Executable: executable, StateDir: state, ConfigDir: configDir}, err
}

func (a *application) activateSetup(ctx context.Context, s *setupState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.newKey != "" {
		if err := saveResendEnv(s.newKey); err != nil {
			return err
		}
		fmt.Fprintln(a.out, "API key saved privately in this folder's .env.")
	}
	config := store.Config{Providers: s.options.providers, To: s.options.email, From: s.options.from, CodexBin: s.options.binary}
	if err := a.db.ConfigureSetup(ctx, config, time.Now().UTC()); err != nil {
		return err
	}
	s.activated = true
	a.restoreClaudeStatusLine(ctx)
	for _, reading := range s.readings {
		if err := a.db.RecordSnapshot(ctx, reading, time.Now().UTC()); err != nil {
			return fmt.Errorf("setup saved; initial %s reading could not be recorded, run watch to retry: %w", reading.Provider, err)
		}
	}

	return nil
}

func (a *application) printSetupSummary(ctx context.Context, s *setupState) {
	fmt.Fprintln(a.out, "\nSetup saved. Providers:", s.options.providers)
	fmt.Fprintln(a.out, "Reminder email:", s.options.email)
	fmt.Fprintln(a.out, "Email time zone: Indian Standard Time (IST, UTC+05:30).")
	if s.emailTested {
		fmt.Fprintln(a.out, "Email test: accepted by Resend; check your inbox and spam folder.")
	}
	folder, _ := filepath.Abs(".")
	fmt.Fprintln(a.out, "Run the watcher from this folder so .env loads:", folder)
	fmt.Fprintln(a.out, "Start: resetwatch watch")
	fmt.Fprintln(a.out, "Provider changes apply on the next watcher cycle. Restart a running watcher after changing its API key.")
}
