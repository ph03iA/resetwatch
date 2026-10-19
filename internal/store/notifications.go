package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func reconcileNotifications(ctx context.Context, tx *sql.Tx, old, config Config, now time.Time) error {
	if old.To == "" || old.From == "" {
		return scheduleSaved(ctx, tx, old, config, now)
	}
	return moveAlerts(ctx, tx, config, now)
}

func rejectActiveDelivery(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alerts WHERE state='sending' AND lease_until>?
 AND (recipient<>? OR sender<>? OR NOT ((provider='codex' AND ?) OR (provider='claude' AND ?))))`,
		now.Unix(), config.To, config.From, config.Selected("codex"), config.Selected("claude")).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return errors.New("an email delivery is in progress; rerun setup after it finishes or its two-minute lease expires")
	}
	return nil
}

func scheduleSaved(ctx context.Context, tx *sql.Tx, old, config Config, now time.Time) error {
	for _, provider := range []string{"codex", "claude"} {
		if !old.Selected(provider) || !config.Selected(provider) {
			continue
		}
		snapshot, err := loadSnapshot(ctx, tx, provider)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := schedule(ctx, tx, snapshot, config, now, false); err != nil {
			return err
		}
	}
	return nil
}
