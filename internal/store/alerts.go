package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"resetwatch/internal/quota"
)

func alertID(snapshot quota.Snapshot, window quota.Window, config Config) string {
	return alertKey(snapshot.Provider, snapshot.AccountID, window.ID, window.ResetsAt.Unix(), config)
}

func alertKey(provider, accountID, windowID string, resetAt int64, config Config) string {
	data, _ := json.Marshal([]any{provider, accountID, windowID, resetAt, config.To, config.From})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (d *DB) Schedule(ctx context.Context, snapshot quota.Snapshot, config Config, now time.Time) error {
	if config.To == "" || config.From == "" {
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
	if err := schedule(ctx, tx, snapshot, config, now); err != nil {
		return err
	}
	return tx.Commit()
}

func schedule(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, config Config, now time.Time) error {
	if config.To == "" || config.From == "" {
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
		if err := scheduleWindow(ctx, tx, snapshot, window, config, now); err != nil {
			return err
		}
	}
	return nil
}

func cancelReplaced(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, config Config, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE alerts SET state='cancelled',updated_at=? WHERE provider=?
 AND state IN ('pending','retry') AND (account_id<>? OR recipient<>? OR sender<>?)`,
		now.Unix(), snapshot.Provider, snapshot.AccountID, config.To, config.From)
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
	_, err = tx.ExecContext(ctx, `UPDATE alerts SET state='cancelled',updated_at=?
 WHERE provider=? AND account_id=? AND reset_at>? AND state IN ('pending','retry')
 AND window_id NOT IN (SELECT value FROM json_each(?))`, now.Unix(), snapshot.Provider, snapshot.AccountID, now.Unix(), string(data))
	return err
}

func scheduleWindow(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, window quota.Window, config Config, now time.Time) error {
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
	_, err := tx.ExecContext(ctx, `INSERT INTO alerts
 (id,provider,account_id,window_id,reset_at,recipient,sender,next_attempt,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state='pending',updated_at=excluded.updated_at
 WHERE alerts.state='cancelled' AND alerts.reset_at>?`, id, snapshot.Provider, snapshot.AccountID, window.ID,
		window.ResetsAt.Unix(), config.To, config.From, window.ResetsAt.Unix(), now.Unix(), now.Unix())
	return err
}

// releaseEarlyReset makes an exhausted window's future alert due now when the
// provider reports allowance back in a new window before the recorded reset time.
// Only a later reset time proves a new window began; a lower percentage alone may be an old reading.
func releaseEarlyReset(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, window quota.Window, now time.Time) error {
	if snapshot.Provider == "claude" {
		return nil // a status-line callback cannot prove fresh allowance
	}
	resetAt := min(snapshot.ObservedAt.Unix(), now.Unix())
	_, err := tx.ExecContext(ctx, `UPDATE alerts SET reset_at=?,next_attempt=?,updated_at=?
 WHERE provider=? AND account_id=? AND window_id=? AND reset_at>? AND reset_at<? AND state IN ('pending','retry')`,
		resetAt, now.Unix(), now.Unix(), snapshot.Provider, snapshot.AccountID, window.ID, now.Unix(), window.ResetsAt.Unix())
	return err
}

func cancelSuperseded(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, window quota.Window, id string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE alerts SET state='cancelled',updated_at=?
 WHERE provider=? AND account_id=? AND window_id=? AND id<>? AND reset_at>?
 AND state IN ('pending','retry')`, now.Unix(), snapshot.Provider, snapshot.AccountID, window.ID, id, now.Unix())
	return err
}

const alertColumns = `id,provider,account_id,window_id,reset_at,recipient,sender,state,attempts,first_attempt,next_attempt,last_error`

type scanner interface{ Scan(...any) error }

func scanAlert(row scanner) (Alert, error) {
	var alert Alert
	var reset, first, next int64
	err := row.Scan(&alert.ID, &alert.Provider, &alert.AccountID, &alert.Window, &reset,
		&alert.To, &alert.From, &alert.State, &alert.Attempts, &first, &next, &alert.LastError)
	alert.ResetAt = time.Unix(reset, 0).UTC()
	alert.NextAttempt = time.Unix(next, 0).UTC()
	if first != 0 {
		alert.FirstAttempt = time.Unix(first, 0).UTC()
	}
	return alert, err
}

func (d *DB) Claim(ctx context.Context, now time.Time) (Alert, error) {
	_, err := d.db.ExecContext(ctx, `UPDATE alerts SET state='failed',last_error='exhausted eight delivery attempts',lease_until=0,updated_at=?
 WHERE attempts>=8 AND ((state IN ('pending','retry') AND next_attempt<=?) OR (state='sending' AND lease_until<=?))`,
		now.Unix(), now.Unix(), now.Unix())
	if err != nil {
		return Alert{}, err
	}
	query := `UPDATE alerts SET state='sending',attempts=attempts+1,lease_until=?,updated_at=?,
 first_attempt=CASE WHEN first_attempt=0 THEN ? ELSE first_attempt END
 WHERE id=(SELECT id FROM alerts WHERE attempts<8 AND
 ((state IN ('pending','retry') AND next_attempt<=?) OR (state='sending' AND lease_until<=?))
 ORDER BY next_attempt,id LIMIT 1) RETURNING ` + alertColumns
	return scanAlert(d.db.QueryRowContext(ctx, query, now.Add(2*time.Minute).Unix(), now.Unix(), now.Unix(), now.Unix(), now.Unix()))
}

func (d *DB) Finish(ctx context.Context, alert Alert, state, reason string, next time.Time, now time.Time) error {
	result, err := d.db.ExecContext(ctx, `UPDATE alerts SET state=?,last_error=?,next_attempt=?,lease_until=0,updated_at=?
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
	rows, err := d.db.QueryContext(ctx, "SELECT "+alertColumns+" FROM alerts ORDER BY updated_at DESC,id LIMIT ?", limit)
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

func (d *DB) Retry(ctx context.Context, id string, now time.Time) error {
	result, err := d.db.ExecContext(ctx, `UPDATE alerts SET state='retry',next_attempt=?,updated_at=?,last_error=''
 WHERE id=? AND state='failed' AND attempts<8 AND first_attempt>? AND reset_at>?`,
		now.Unix(), now.Unix(), id, now.Add(-23*time.Hour).Unix(), now.Add(-24*time.Hour).Unix())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return errors.New("alert is not failed, not found, exhausted eight attempts, or outside its safe retry window")
	}
	return err
}

// moveAlerts readdresses undelivered reminders after the email settings change. Each gets a new
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
	id := alertKey(alert.Provider, alert.AccountID, alert.Window, alert.ResetAt.Unix(), config)
	_, err := tx.ExecContext(ctx, `INSERT INTO alerts (id,provider,account_id,window_id,reset_at,recipient,sender,next_attempt,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state='pending',next_attempt=excluded.next_attempt,
 lease_until=0,last_error='',updated_at=excluded.updated_at
 WHERE alerts.state='cancelled' AND alerts.attempts<8 AND (alerts.first_attempt=0 OR alerts.first_attempt>?) AND alerts.reset_at>?`,
		id, alert.Provider, alert.AccountID, alert.Window, alert.ResetAt.Unix(), config.To, config.From,
		alert.NextAttempt.Unix(), now.Unix(), now.Add(-23*time.Hour).Unix(), now.Add(-24*time.Hour).Unix())
	if err != nil {
		return err
	}
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM alerts WHERE id=?", id).Scan(&state); err != nil {
		return err
	}
	if state == "failed" || state == "cancelled" {
		return errors.New("the destination reminder cannot be resumed; notification settings were not changed")
	}
	_, err = tx.ExecContext(ctx, `UPDATE alerts SET state='cancelled',lease_until=0,last_error='notification settings changed',updated_at=?
 WHERE id=? AND (state IN ('pending','retry') OR (state='sending' AND lease_until<=?))`, now.Unix(), alert.ID, now.Unix())
	return err
}

func alertsToMove(ctx context.Context, tx *sql.Tx, config Config, now time.Time) ([]Alert, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+alertColumns+` FROM alerts
 WHERE (state IN ('pending','retry') OR (state='sending' AND lease_until<=?)) AND (recipient<>? OR sender<>?)
 AND attempts<8 AND (first_attempt=0 OR first_attempt>?) AND reset_at>?`, now.Unix(), config.To, config.From,
		now.Add(-23*time.Hour).Unix(), now.Add(-24*time.Hour).Unix())
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
