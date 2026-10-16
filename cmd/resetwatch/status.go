package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

type statusOptions struct {
	provider           string
	refresh, json, all bool
}

func (a *application) status(ctx context.Context, args []string) error {
	options, err := a.statusFlags(args)
	if err != nil {
		return err
	}
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	if options.combined(config) {
		return a.statusAll(ctx, config, options.refresh, options.json)
	}
	snapshot, err := a.statusSnapshot(ctx, options.provider, options.refresh)
	if err != nil {
		return err
	}
	if options.json {
		return writeJSON(a.out, snapshot)
	}
	a.render(snapshot, false)
	return nil
}

func (a *application) statusFlags(args []string) (statusOptions, error) {
	var options statusOptions
	flags := a.flags("status")
	flags.BoolVar(&options.refresh, "refresh", false, "refresh selected Codex data; Claude uses callbacks")
	flags.BoolVar(&options.json, "json", false, "machine-readable output")
	flags.StringVar(&options.provider, "provider", "", "codex or claude")
	flags.BoolVar(&options.all, "all", false, "show all selected providers (JSON is keyed by provider)")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if err := noArguments(flags.Args()); err != nil {
		return options, err
	}
	if options.all && options.provider != "" {
		return options, errors.New("--all and --provider cannot be combined")
	}
	return options, nil
}

func (o statusOptions) combined(config store.Config) bool {
	if o.all {
		return true
	}
	return !o.json && o.provider == "" && config.Providers != ""
}

func (a *application) defaultStatusProvider(ctx context.Context) (string, error) {
	config, err := a.db.Config(ctx)
	if err != nil {
		return "", err
	}
	if config.Selected("codex") {
		_, err := a.db.Snapshot(ctx, "codex")
		if err == nil {
			return "codex", nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	if config.Selected("claude") {
		return "claude", nil
	}
	return "codex", nil
}

func (a *application) statusAll(ctx context.Context, config store.Config, refresh, asJSON bool) error {
	providers, err := a.statusProviders(ctx, config)
	if err != nil {
		return err
	}
	if err := a.refreshStatusProviders(ctx, providers, refresh); err != nil {
		return err
	}
	readings, err := a.statusReadings(ctx, providers)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(a.out, readings)
	}
	a.renderStatusReadings(providers, readings)
	return nil
}

func (a *application) statusProviders(ctx context.Context, config store.Config) ([]string, error) {
	providers := config.ProvidersList()
	if config.Providers == "" {
		var err error
		providers, err = a.connectedProviders(ctx)
		if err != nil {
			return nil, err
		}
	}
	if len(providers) == 0 {
		return nil, errors.New("no selected providers; run resetwatch setup")
	}
	return providers, nil
}

func (a *application) refreshStatusProviders(ctx context.Context, providers []string, refresh bool) error {
	if refresh && slices.Contains(providers, "codex") {
		return a.refresh(ctx)
	}
	return nil
}

func (a *application) statusReadings(ctx context.Context, providers []string) (map[string]*quota.Snapshot, error) {
	readings := make(map[string]*quota.Snapshot, len(providers))
	for _, provider := range providers {
		snapshot, err := a.db.Snapshot(ctx, provider)
		if errors.Is(err, sql.ErrNoRows) {
			readings[provider] = nil
			continue
		}
		if err != nil {
			return nil, err
		}
		readings[provider] = &snapshot
	}
	return readings, nil
}

func (a *application) renderStatusReadings(providers []string, readings map[string]*quota.Snapshot) {
	for _, provider := range providers {
		if readings[provider] == nil {
			fmt.Fprintln(a.out, provider+": awaiting first reading.")
			continue
		}
		a.render(*readings[provider], false)
	}
}

func (a *application) connectedProviders(ctx context.Context) ([]string, error) {
	var providers []string
	for _, provider := range []string{"codex", "claude"} {
		_, err := a.db.Snapshot(ctx, provider)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, nil
}
