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

// ConfigureSetup applies a nonempty explicit provider selection and the Gmail connection atomically.
func (d *DB) ConfigureSetup(ctx context.Context, config Config, now time.Time) error {
	if config.Providers != "codex" && config.Providers != "claude" && config.Providers != "both" {
		return errors.New("select codex, claude, or both")
	}
	if err := validateGmail(config.Gmail); err != nil {
		return err
	}
	return d.saveSetup(ctx, config, now)
}

func validateGmail(account GmailAccount) error {
	if !account.Connected() {
		return errors.New("connect Gmail first: run resetwatch gmail connect")
	}
	return notify.ValidateAddresses(account.Address, account.Address)
}

func cancelDisabled(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE notification_alerts SET state='cancelled',last_error=?,lease_until=0,updated_at=?
 WHERE (state IN ('pending','retry') OR (state='sending' AND lease_until<=?))
 AND NOT ((provider='codex' AND ?) OR (provider='claude' AND ?))`,
		providerDisabled, now.Unix(), now.Unix(), config.Selected("codex"), config.Selected("claude"))
	return err
}

func (d *DB) saveSetup(ctx context.Context, config Config, now time.Time) error {
	return d.updateSetup(ctx, func(Config) Config { return config }, now)
}

// commitSetup applies a configuration change, upgrading Resend-era state first when needed.
func (d *DB) commitSetup(ctx context.Context, tx *sql.Tx, old, config Config, now time.Time) error {
	if d.legacy {
		if err := upgradeLegacy(ctx, tx, &old, config, now); err != nil {
			return err
		}
	} else if config == old {
		return nil
	}
	if err := configureState(ctx, tx, old, config, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	d.legacy = false
	return nil
}

func configureState(ctx context.Context, tx *sql.Tx, old, config Config, now time.Time) error {
	if err := rejectActiveDelivery(ctx, tx, config, now); err != nil {
		return err
	}
	if err := cancelDisabled(ctx, tx, config, now); err != nil {
		return err
	}
	if old.Gmail.Subject != config.Gmail.Subject {
		if err := reconcileNotifications(ctx, tx, old, config, now); err != nil {
			return err
		}
	}
	return saveConfig(ctx, tx, config)
}
