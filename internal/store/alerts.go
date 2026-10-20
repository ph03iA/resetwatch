package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"resetwatch/internal/quota"
)

func alertID(snapshot quota.Snapshot, window quota.Window, config Config) string {
	return alertKey(snapshot.Provider, snapshot.AccountID, window.ID, window.ResetsAt.Unix(), config)
}

func alertKey(provider, accountID, windowID string, resetAt int64, config Config) string {
	data, _ := json.Marshal([]any{provider, accountID, windowID, resetAt, "gmail", config.Gmail.Subject})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (d *DB) Schedule(ctx context.Context, snapshot quota.Snapshot, config Config, now time.Time) error {
	if !config.Gmail.Connected() {
		return nil
	}
	if err := quota.ValidateSnapshot(snapshot); err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	config.Providers = current.Providers
	if err := schedule(ctx, tx, snapshot, config, now, false); err != nil {
		return err
	}
	return tx.Commit()
}

func schedule(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, config Config, now time.Time, newReading bool) error {
	if !config.Gmail.Connected() || !config.Selected(snapshot.Provider) {
		return nil
	}
	if err := quota.ValidateSnapshot(snapshot); err != nil {
		return err
	}
	if err := cancelReplaced(ctx, tx, snapshot, config, now); err != nil {
		return err
	}
	if err := cancelMissing(ctx, tx, snapshot, now); err != nil {
		return err
	}
	for _, window := range snapshot.Windows {
		if err := scheduleWindow(ctx, tx, snapshot, window, config, now, newReading); err != nil {
			return err
		}
	}
	return nil
}

func cancelReplaced(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, config Config, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE notification_alerts SET state='cancelled',updated_at=? WHERE provider=?
 AND state IN ('pending','retry') AND (account_id<>? OR gmail_account<>?)`,
		now.Unix(), snapshot.Provider, snapshot.AccountID, config.Gmail.Subject)
	return err
}

func cancelMissing(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, now time.Time) error {
	if snapshot.Partial {
		return nil // a window missing from a partial reading is unknown, not removed
	}
	ids := make([]string, 0, len(snapshot.Windows))
	for _, window := range snapshot.Windows {
		ids = append(ids, window.ID)
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE notification_alerts SET state='cancelled',updated_at=?
 WHERE provider=? AND account_id=? AND reset_at>? AND state IN ('pending','retry')
 AND window_id NOT IN (SELECT value FROM json_each(?))`, now.Unix(), snapshot.Provider, snapshot.AccountID, now.Unix(), string(data))
	return err
}

func scheduleWindow(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, window quota.Window, config Config, now time.Time, newReading bool) error {
	if window.UsedPercent < 100 {
		return releaseEarlyReset(ctx, tx, snapshot, window, now)
	}
	id := alertID(snapshot, window, config)
	if err := cancelSuperseded(ctx, tx, snapshot, window, id, now); err != nil {
		return err
	}
	if window.ResetsAt.Before(now.Add(-24 * time.Hour)) {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO notification_alerts
 (id,provider,account_id,window_id,reset_at,recipient,sender,gmail_account,next_attempt,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state='pending',last_error='',updated_at=excluded.updated_at
 WHERE notification_alerts.state='cancelled' AND notification_alerts.reset_at>? AND notification_alerts.attempts<8
 AND (notification_alerts.first_attempt=0 OR notification_alerts.first_attempt>?) AND (notification_alerts.last_error<>? OR ?)`, id, snapshot.Provider, snapshot.AccountID, window.ID,
		window.ResetsAt.Unix(), config.Gmail.Address, config.Gmail.Address, config.Gmail.Subject, window.ResetsAt.Unix(), now.Unix(), now.Unix(),
		now.Add(-23*time.Hour).Unix(), providerDisabled, newReading)
	return err
}

// releaseEarlyReset makes an exhausted window's future alert due now when the
// provider reports allowance back in a new window before the recorded reset time.
// Only a later reset time proves a new window began; a lower percentage alone may be an old reading.
func releaseEarlyReset(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, window quota.Window, now time.Time) error {
	resetAt := min(snapshot.ObservedAt.Unix(), now.Unix())
	_, err := tx.ExecContext(ctx, `UPDATE notification_alerts SET reset_at=?,next_attempt=?,updated_at=?
 WHERE provider=? AND account_id=? AND window_id=? AND reset_at>? AND reset_at<? AND state IN ('pending','retry')`,
		resetAt, now.Unix(), now.Unix(), snapshot.Provider, snapshot.AccountID, window.ID, now.Unix(), window.ResetsAt.Unix())
	return err
}

func cancelSuperseded(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, window quota.Window, id string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE notification_alerts SET state='cancelled',updated_at=?
 WHERE provider=? AND account_id=? AND window_id=? AND id<>? AND reset_at>?
 AND state IN ('pending','retry')`, now.Unix(), snapshot.Provider, snapshot.AccountID, window.ID, id, now.Unix())
	return err
}

const alertColumns = `id,provider,account_id,window_id,reset_at,recipient,sender,gmail_account,state,attempts,first_attempt,next_attempt,last_error`

type scanner interface{ Scan(...any) error }

func scanAlert(row scanner) (Alert, error) {
	var alert Alert
	var reset, first, next int64
	err := row.Scan(&alert.ID, &alert.Provider, &alert.AccountID, &alert.Window, &reset,
		&alert.To, &alert.From, &alert.Gmail, &alert.State, &alert.Attempts, &first, &next, &alert.LastError)
	alert.ResetAt = time.Unix(reset, 0).UTC()
	alert.NextAttempt = time.Unix(next, 0).UTC()
	if first != 0 {
		alert.FirstAttempt = time.Unix(first, 0).UTC()
	}
	return alert, err
}

func (d *DB) Claim(ctx context.Context, now time.Time) (Alert, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Alert{}, err
	}
	defer tx.Rollback()
	alert, claimErr := claimSelected(ctx, tx, now)
	if claimErr != nil && !errors.Is(claimErr, sql.ErrNoRows) {
		return Alert{}, claimErr
	}
	if err := tx.Commit(); err != nil {
		return Alert{}, err
	}
	return alert, claimErr
}

func (d *DB) Finish(ctx context.Context, alert Alert, state, reason string, next time.Time, now time.Time) error {
	result, err := d.db.ExecContext(ctx, `UPDATE notification_alerts SET state=?,last_error=?,next_attempt=?,lease_until=0,updated_at=?
 WHERE id=? AND state='sending' AND attempts=?`, state, reason, next.Unix(), now.Unix(), alert.ID, alert.Attempts)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return errors.New("alert delivery lease was lost")
	}
	return err
}

func (d *DB) History(ctx context.Context, limit int) ([]Alert, error) {
	query := "SELECT " + alertColumns + " FROM notification_alerts ORDER BY updated_at DESC,id LIMIT ?"
	if d.legacy {
		query = "SELECT " + strings.Replace(alertColumns, "gmail_account", "''", 1) + " FROM alerts ORDER BY updated_at DESC,id LIMIT ?"
	}
	rows, err := d.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	alerts := make([]Alert, 0)
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

// moveAlerts readdresses undelivered reminders after the Gmail account changes. Each gets a new
// delivery key and an empty payload; the old entry is cancelled with its frozen message untouched.
// Reset and due times carry over, so an already-due reminder stays due.
func moveAlerts(ctx context.Context, tx *sql.Tx, config Config, now time.Time) error {
	moving, err := alertsToMove(ctx, tx, config, now)
	if err != nil {
		return err
	}
	for _, alert := range moving {
		if err := moveAlert(ctx, tx, alert, config, now); err != nil {
			return err
		}
	}
	return nil
}

func moveAlert(ctx context.Context, tx *sql.Tx, alert Alert, config Config, now time.Time) error {
	if !config.Selected(alert.Provider) {
		return nil
	}
	id := alertKey(alert.Provider, alert.AccountID, alert.Window, alert.ResetAt.Unix(), config)
	_, err := tx.ExecContext(ctx, `INSERT INTO notification_alerts (id,provider,account_id,window_id,reset_at,recipient,sender,gmail_account,next_attempt,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state='pending',next_attempt=excluded.next_attempt,
 lease_until=0,last_error='',updated_at=excluded.updated_at
 WHERE notification_alerts.state='cancelled' AND notification_alerts.last_error<>? AND notification_alerts.attempts<8 AND (notification_alerts.first_attempt=0 OR notification_alerts.first_attempt>?) AND notification_alerts.reset_at>?`,
		id, alert.Provider, alert.AccountID, alert.Window, alert.ResetAt.Unix(), config.Gmail.Address, config.Gmail.Address, config.Gmail.Subject,
		alert.NextAttempt.Unix(), now.Unix(), providerDisabled, now.Add(-23*time.Hour).Unix(), now.Add(-24*time.Hour).Unix())
	if err != nil {
		return err
	}
	var state, reason string
	if err := tx.QueryRowContext(ctx, "SELECT state,last_error FROM notification_alerts WHERE id=?", id).Scan(&state, &reason); err != nil {
		return err
	}
	if state == "cancelled" && reason == providerDisabled {
		return cancelMovedAlert(ctx, tx, alert, now)
	}
	if state == "failed" || state == "cancelled" {
		return errors.New("a reminder for this Gmail account cannot be resumed; the Gmail connection was not changed")
	}
	return cancelMovedAlert(ctx, tx, alert, now)
}

func cancelMovedAlert(ctx context.Context, tx *sql.Tx, alert Alert, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE notification_alerts SET state='cancelled',lease_until=0,last_error='Gmail account changed',updated_at=?
 WHERE id=? AND (state IN ('pending','retry') OR (state='sending' AND lease_until<=?))`, now.Unix(), alert.ID, now.Unix())
	return err
}

func alertsToMove(ctx context.Context, tx *sql.Tx, config Config, now time.Time) ([]Alert, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+alertColumns+` FROM notification_alerts
 WHERE (state IN ('pending','retry') OR (state='sending' AND lease_until<=?)) AND gmail_account<>?
 AND attempts<8 AND (first_attempt=0 OR first_attempt>?) AND reset_at>?
 AND ((provider='codex' AND ?) OR (provider='claude' AND ?))`, now.Unix(), config.Gmail.Subject,
		now.Add(-23*time.Hour).Unix(), now.Add(-24*time.Hour).Unix(), config.Selected("codex"), config.Selected("claude"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var alerts []Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}
