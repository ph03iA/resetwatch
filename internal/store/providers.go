package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"resetwatch/internal/notify"
)

const providerDisabled = "provider disabled"

// Selected preserves monitoring of both providers for configurations predating setup.
func (c Config) Selected(provider string) bool {
	switch provider {
	case "codex":
		return c.Providers == "" || c.Providers == "codex" || c.Providers == "both"
	case "claude":
		return c.Providers == "" || c.Providers == "claude" || c.Providers == "both"
	}
	return false
}

func (c Config) ProvidersList() []string {
	var providers []string
	for _, provider := range []string{"codex", "claude"} {
		if c.Selected(provider) {
			providers = append(providers, provider)
		}
	}
	return providers
}

// ConfigureSetup applies a nonempty explicit provider selection and notification settings atomically.
func (d *DB) ConfigureSetup(ctx context.Context, config Config, now time.Time) error {
	if config.Providers != "codex" && config.Providers != "claude" && config.Providers != "both" {
		return errors.New("select codex, claude, or both")
	}
	return d.configureNotifications(ctx, &config, config.To, config.From, now)
}

func (d *DB) EnableProvider(ctx context.Context, provider string) error {
	if !(Config{}).Selected(provider) {
		return errors.New("provider must be codex or claude")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	config, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	config.Providers = config.withProvider(provider)
	if err := saveConfig(ctx, tx, config); err != nil {
		return err
	}
	return tx.Commit()
}

func (c Config) withProvider(provider string) string {
	if c.Selected(provider) {
		return c.Providers
	}
	return "both"
}

func cancelDisabled(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE alerts SET state='cancelled',last_error=?,lease_until=0,updated_at=?
 WHERE (state IN ('pending','retry') OR (state='sending' AND lease_until<=?))
 AND NOT ((provider='codex' AND ?) OR (provider='claude' AND ?))`,
		providerDisabled, now.Unix(), now.Unix(), config.Selected("codex"), config.Selected("claude"))
	return err
}

func (d *DB) configureNotifications(ctx context.Context, setup *Config, to, from string, now time.Time) error {
	if err := notify.ValidateAddresses(to, from); err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	config := old
	config.To, config.From = to, from
	if setup != nil {
		config.Providers, config.CodexBin = setup.Providers, setup.CodexBin
	}
	if config == old {
		return nil
	}
	if err := configureState(ctx, tx, old, config, now); err != nil {
		return err
	}
	return tx.Commit()
}

func configureState(ctx context.Context, tx *sql.Tx, old, config Config, now time.Time) error {
	if err := rejectActiveDelivery(ctx, tx, config, now); err != nil {
		return err
	}
	if err := cancelDisabled(ctx, tx, config, now); err != nil {
		return err
	}
	if old.To != config.To || old.From != config.From {
		if err := reconcileNotifications(ctx, tx, old, config, now); err != nil {
			return err
		}
	}
	return saveConfig(ctx, tx, config)
}
