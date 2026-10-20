package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ConnectGmail switches delivery to account, keeping the provider selection. Reconnecting the same Google
// account keeps reminder identities; another account receives the unsent reminders.
func (d *DB) ConnectGmail(ctx context.Context, account GmailAccount, now time.Time) error {
	if err := validateGmail(account); err != nil {
		return err
	}
	return d.updateSetup(ctx, func(config Config) Config {
		config.Gmail = account
		config.LegacyTo, config.LegacyFrom = "", ""
		return config
	}, now)
}

// DisconnectGmail stops delivery and cancels unsent reminders. It commits before the caller deletes the
// credential, so a crash in between leaves delivery off rather than pointing at a missing secret.
func (d *DB) DisconnectGmail(ctx context.Context, now time.Time) error {
	return d.updateSetup(ctx, func(config Config) Config {
		config.Gmail = GmailAccount{}
		return config
	}, now)
}

// PauseGmail stops delivery until reconnection, but only while credential is still the configured one:
// a failure from an old sign-in must never pause a newer connection.
func (d *DB) PauseGmail(ctx context.Context, credential, reason string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	config, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	if !config.Gmail.Connected() || config.Gmail.Credential != credential || config.Gmail.Paused != "" {
		return nil
	}
	config.Gmail.Paused = reason
	if err := saveConfig(ctx, tx, config); err != nil {
		return err
	}
	return tx.Commit()
}

// ReleaseDelivery gives back a claimed attempt that never reached Gmail because the sign-in failed,
// so a paused account cannot exhaust the queue's eight-attempt budgets.
func (d *DB) ReleaseDelivery(ctx context.Context, alert Alert, now time.Time) error {
	_, err := d.db.ExecContext(ctx, `UPDATE notification_alerts SET state='retry',attempts=attempts-1,
 first_attempt=CASE WHEN attempts=1 THEN 0 ELSE first_attempt END,lease_until=0,next_attempt=?,updated_at=?
 WHERE id=? AND state='sending' AND attempts=?`, now.Unix(), now.Unix(), alert.ID, alert.Attempts)
	return err
}

// updateSetup reads the configuration inside the write transaction, so concurrent changes are not lost.
func (d *DB) updateSetup(ctx context.Context, change func(Config) Config, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	return d.commitSetup(ctx, tx, old, change(old), now)
}

// upgradeLegacy turns Resend-era state into Gmail state inside the caller's transaction: the reminder table
// is renamed, eligible unsent reminders are re-created for the Gmail account with their reset times, retry
// deadlines and attempts, and the originals are cancelled with their frozen payloads untouched.
func upgradeLegacy(ctx context.Context, tx *sql.Tx, old *Config, config Config, now time.Time) error {
	if !config.Gmail.Connected() {
		return ErrUpgradeRequired
	}
	var active bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM alerts WHERE state='sending' AND lease_until>?)", now.Unix()).Scan(&active); err != nil {
		return err
	}
	if active {
		return errors.New("an older ResetWatch is delivering an email; stop every running watcher, then run setup again")
	}
	_, err := tx.ExecContext(ctx, `ALTER TABLE alerts RENAME TO notification_alerts;
 ALTER TABLE notification_alerts ADD COLUMN gmail_account TEXT NOT NULL DEFAULT '';
 DROP INDEX IF EXISTS alerts_due;
 CREATE INDEX notification_alerts_due ON notification_alerts(state, next_attempt);
 PRAGMA user_version = 2;`)
	if err != nil {
		return err
	}
	if err := transferLegacy(ctx, tx, config, now); err != nil {
		return err
	}
	old.Gmail = config.Gmail // transferred, so the account change must not rebuild reminders from saved readings
	return nil
}

func transferLegacy(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	rows, err := tx.QueryContext(ctx, "SELECT "+alertColumns+` FROM notification_alerts WHERE gmail_account=''
 AND (state IN ('pending','retry') OR (state='sending' AND lease_until<=?)) AND attempts<8
 AND (first_attempt=0 OR first_attempt>?) AND reset_at>?`, now.Unix(), now.Add(-23*time.Hour).Unix(), now.Add(-24*time.Hour).Unix())
	if err != nil {
		return err
	}
	var legacy []Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, alert)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, alert := range legacy {
		if err := transferAlert(ctx, tx, alert, config, now); err != nil {
			return err
		}
	}
	return nil
}

func transferAlert(ctx context.Context, tx *sql.Tx, alert Alert, config Config, now time.Time) error {
	state := "retry"
	if alert.State == "pending" {
		state = "pending"
	}
	first := int64(0)
	if !alert.FirstAttempt.IsZero() {
		first = alert.FirstAttempt.Unix()
	}
	id := alertKey(alert.Provider, alert.AccountID, alert.Window, alert.ResetAt.Unix(), config)
	_, err := tx.ExecContext(ctx, `INSERT INTO notification_alerts
 (id,provider,account_id,window_id,reset_at,recipient,sender,gmail_account,state,attempts,first_attempt,next_attempt,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, id, alert.Provider, alert.AccountID, alert.Window, alert.ResetAt.Unix(),
		config.Gmail.Address, config.Gmail.Address, config.Gmail.Subject, state, alert.Attempts, first, alert.NextAttempt.Unix(), now.Unix())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE notification_alerts SET state='cancelled',lease_until=0,last_error='moved to Gmail during upgrade',updated_at=?
 WHERE id=?`, now.Unix(), alert.ID)
	return err
}
