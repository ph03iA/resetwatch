package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func claimSelected(ctx context.Context, tx *sql.Tx, now time.Time) (Alert, error) {
	config, err := loadConfig(ctx, tx)
	if err != nil {
		return Alert{}, err
	}
	if err := cancelDisabled(ctx, tx, config, now); err != nil {
		return Alert{}, err
	}
	if err := failExhausted(ctx, tx, now); err != nil {
		return Alert{}, err
	}
	query := `UPDATE alerts SET state='sending',attempts=attempts+1,lease_until=?,updated_at=?,
 first_attempt=CASE WHEN first_attempt=0 THEN ? ELSE first_attempt END
 WHERE id=(SELECT id FROM alerts WHERE attempts<8 AND
 ((provider='codex' AND ?) OR (provider='claude' AND ?)) AND
 ((state IN ('pending','retry') AND next_attempt<=?) OR (state='sending' AND lease_until<=?))
 ORDER BY next_attempt,id LIMIT 1) RETURNING ` + alertColumns
	return scanAlert(tx.QueryRowContext(ctx, query, now.Add(2*time.Minute).Unix(), now.Unix(), now.Unix(),
		config.Selected("codex"), config.Selected("claude"), now.Unix(), now.Unix()))
}

func failExhausted(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE alerts SET state='failed',last_error='exhausted eight delivery attempts',lease_until=0,updated_at=?
 WHERE attempts>=8 AND ((state IN ('pending','retry') AND next_attempt<=?) OR (state='sending' AND lease_until<=?))`,
		now.Unix(), now.Unix(), now.Unix())
	return err
}

// ValidateDelivery checks current selection and lease immediately before an external send.
func (d *DB) ValidateDelivery(ctx context.Context, alert Alert, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	config, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	if !config.Selected(alert.Provider) {
		return errors.New(providerDisabled)
	}
	if alert.To != config.To || alert.From != config.From {
		return errors.New("notification settings changed")
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alerts
 WHERE id=? AND state='sending' AND attempts=? AND lease_until>?)`, alert.ID, alert.Attempts, now.Unix()).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return sql.ErrNoRows
	}
	return tx.Commit()
}
