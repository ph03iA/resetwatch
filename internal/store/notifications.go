package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ConfigureNotifications changes settings and outstanding reminders in one transaction.
// Existing completed reminders are never reconstructed from a saved quota observation.
func (d *DB) ConfigureNotifications(ctx context.Context, to, from string, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	if old.To == to && old.From == from {
		return nil
	}
	config := old
	config.To, config.From = to, from
	if err := reconcileNotifications(ctx, tx, old, config, now); err != nil {
		return err
	}
	if err := saveConfig(ctx, tx, config); err != nil {
		return err
	}
	return tx.Commit()
}

func reconcileNotifications(ctx context.Context, tx *sql.Tx, old, config Config, now time.Time) error {
	if err := rejectActiveDelivery(ctx, tx, config, now); err != nil {
		return err
	}
	if old.To == "" || old.From == "" {
		return scheduleSaved(ctx, tx, config, now)
	}
	return moveAlerts(ctx, tx, config, now)
}

func rejectActiveDelivery(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alerts WHERE state='sending' AND lease_until>?
 AND (recipient<>? OR sender<>?))`, now.Unix(), config.To, config.From).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return errors.New("an email delivery is in progress; retry notify after it finishes or its two-minute lease expires")
	}
	return nil
}

func scheduleSaved(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	for _, provider := range []string{"codex", "claude"} {
		snapshot, err := loadSnapshot(ctx, tx, provider)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := schedule(ctx, tx, snapshot, config, now); err != nil {
			return err
		}
	}
	return nil
}
