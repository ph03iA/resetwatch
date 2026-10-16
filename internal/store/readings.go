package store

import (
	"context"
	"time"

	"resetwatch/internal/quota"
)

// NewReadings returns the windows whose values changed since this session last reported them.
// Status line callbacks repeat a session's last numbers on timers and UI events, so only a
// change (or a session's first report) is new quota data.
func (d *DB) NewReadings(ctx context.Context, session string, windows []quota.Window, now time.Time) ([]quota.Window, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM readings WHERE resets_at<?", now.Add(-24*time.Hour).Unix()); err != nil {
		return nil, err
	}
	var changed []quota.Window
	for _, window := range windows {
		result, err := tx.ExecContext(ctx, `INSERT INTO readings(session_id,window_id,used,resets_at) VALUES(?,?,?,?)
 ON CONFLICT(session_id,window_id) DO UPDATE SET used=excluded.used,resets_at=excluded.resets_at
 WHERE readings.used<>excluded.used OR readings.resets_at<>excluded.resets_at`,
			session, window.ID, window.UsedPercent, window.ResetsAt.Unix())
		if err != nil {
			return nil, err
		}
		if count, err := result.RowsAffected(); err == nil && count == 1 {
			changed = append(changed, window)
		}
	}
	return changed, tx.Commit()
}
