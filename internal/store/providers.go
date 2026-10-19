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
	if err := notify.ValidateAddresses(config.To, config.From); err != nil {
		return err
	}
	return d.saveSetup(ctx, config, now)
}

func cancelDisabled(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE alerts SET state='cancelled',last_error=?,lease_until=0,updated_at=?
 WHERE (state IN ('pending','retry') OR (state='sending' AND lease_until<=?))
 AND NOT ((provider='codex' AND ?) OR (provider='claude' AND ?))`,
		providerDisabled, now.Unix(), now.Unix(), config.Selected("codex"), config.Selected("claude"))
	return err
}

func (d *DB) saveSetup(ctx context.Context, config Config, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := loadConfig(ctx, tx)
	if err != nil {
		return err
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
