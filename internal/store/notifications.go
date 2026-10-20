package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// reconcileNotifications follows a Gmail account change. A first connection arms resets already saved;
// switching accounts moves unsent reminders; disconnecting cancels them.
func reconcileNotifications(ctx context.Context, tx *sql.Tx, old, config Config, now time.Time) error {
	if !config.Gmail.Connected() {
		return cancelUndelivered(ctx, tx, "Gmail disconnected", now)
	}
	if !old.Gmail.Connected() {
		return scheduleSaved(ctx, tx, old, config, now)
	}
	return moveAlerts(ctx, tx, config, now)
}

func cancelUndelivered(ctx context.Context, tx *sql.Tx, reason string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE notification_alerts SET state='cancelled',lease_until=0,last_error=?,updated_at=?
 WHERE state IN ('pending','retry') OR (state='sending' AND lease_until<=?)`, reason, now.Unix(), now.Unix())
	return err
}

func rejectActiveDelivery(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_alerts WHERE state='sending' AND lease_until>?
 AND (gmail_account<>? OR NOT ((provider='codex' AND ?) OR (provider='claude' AND ?))))`,
		now.Unix(), config.Gmail.Subject, config.Selected("codex"), config.Selected("claude")).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return errors.New("an email delivery is in progress; try again after it finishes or its two-minute lease expires")
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
