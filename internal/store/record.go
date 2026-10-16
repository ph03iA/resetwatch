package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
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
	snapshot, err = recordingSnapshot(ctx, tx, snapshot, now)
	if err != nil {
		return err
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

func recordingSnapshot(ctx context.Context, tx *sql.Tx, snapshot quota.Snapshot, now time.Time) (quota.Snapshot, error) {
	if snapshot.Provider != "claude" {
		return snapshot, nil
	}
	saved, err := loadSnapshot(ctx, tx, "claude")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return quota.Snapshot{}, err
	}
	return mergeClaude(saved, snapshot, now), nil
}

// Claude windows are independent and session callbacks can arrive out of order.
func mergeClaude(saved, incoming quota.Snapshot, now time.Time) quota.Snapshot {
	windows := make(map[string]quota.Window)
	for _, window := range saved.Windows {
		if window.ResetsAt.After(now) {
			windows[window.ID] = window
		}
	}
	for _, window := range incoming.Windows {
		if newerClaudeWindow(windows[window.ID], window) {
			windows[window.ID] = window
		}
	}
	ids := make([]string, 0, len(windows))
	for id := range windows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	incoming.Partial = true
	incoming.Windows = make([]quota.Window, 0, len(ids))
	for _, id := range ids {
		incoming.Windows = append(incoming.Windows, windows[id])
	}
	return incoming
}

func newerClaudeWindow(saved, incoming quota.Window) bool {
	if incoming.ResetsAt.Before(saved.ResetsAt) {
		return false
	}
	if incoming.ResetsAt.Equal(saved.ResetsAt) {
		return incoming.UsedPercent > saved.UsedPercent
	}
	return true
}
