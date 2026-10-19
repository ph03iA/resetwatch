package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"resetwatch/internal/quota"
)

type stateQueries interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// RecordSnapshot commits a reading and its reminders together using current settings.
// Immediate transactions serialize separate CLI processes before they read shared state.
func (d *DB) RecordSnapshot(ctx context.Context, snapshot quota.Snapshot, now time.Time) error {
	if err := quota.ValidateSnapshot(snapshot); err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if snapshot.Partial {
		if snapshot, err = keepOmittedWindows(ctx, tx, snapshot, now); err != nil {
			return err
		}
	}
	if err := saveSnapshot(ctx, tx, snapshot); err != nil {
		return err
	}
	config, err := loadConfig(ctx, tx)
	if err != nil {
		return err
	}
	if err := schedule(ctx, tx, snapshot, config, now, true); err != nil {
		return err
	}
	return tx.Commit()
}

// keepOmittedWindows carries over saved windows a partial reading left out, until they reset.
// Windows present in the reading always replace their saved values.
func keepOmittedWindows(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, now time.Time) (quota.Snapshot, error) {
	saved, err := loadSnapshot(ctx, tx, snapshot.Provider)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && saved.AccountID != snapshot.AccountID) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	windows := slices.Clone(snapshot.Windows)
	for _, window := range saved.Windows {
		reported := slices.ContainsFunc(snapshot.Windows, func(w quota.Window) bool { return w.ID == window.ID })
		if !reported && window.ResetsAt.After(now) {
			windows = append(windows, window)
		}
	}
	snapshot.Windows = windows
	return snapshot, nil
}
