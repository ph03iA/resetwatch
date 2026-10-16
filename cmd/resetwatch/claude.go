package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"resetwatch/internal/quota"
)

var claudeWindowNames = map[string]string{"claude:five_hour": "5h", "claude:seven_day": "7d"}

// claudeStatusLine runs as the Claude Code status line command: it records new plan usage
// from the JSON on stdin, schedules reset reminders, and prints a short summary.
func (a *application) claudeStatusLine(ctx context.Context, args []string) error {
	if err := noArguments(args); err != nil {
		return err
	}
	now := time.Now().UTC()
	reading, session, err := quota.DecodeClaudeStatusLine(a.in, now)
	if errors.Is(err, quota.ErrNoClaudeLimits) {
		return nil // nothing to record yet; keep the status line empty
	}
	if err != nil {
		return err
	}
	fresh, err := a.db.NewReadings(ctx, session, reading.Windows, now)
	if err != nil {
		return err
	}
	if len(fresh) > 0 {
		if err := a.recordClaude(ctx, reading, fresh, now); err != nil {
			return err
		}
	}
	fmt.Fprintln(a.out, claudeSummary(reading))
	return nil
}

// recordClaude saves new Claude data and schedules reminders from it only. Windows missing from
// this reading keep their last saved values until they reset.
// shortcut: two status lines saving at the same instant can drop each other's display values (never
// reminders, which Schedule writes in its own transaction); the next reading restores them.
func (a *application) recordClaude(ctx context.Context, reading quota.Snapshot, fresh []quota.Window, now time.Time) error {
	saved, err := a.db.Snapshot(ctx, "claude")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	merged := reading
	merged.Windows = slices.Clone(fresh)
	for _, window := range saved.Windows {
		isFresh := slices.ContainsFunc(fresh, func(f quota.Window) bool { return f.ID == window.ID })
		if !isFresh && window.ResetsAt.After(now) {
			merged.Windows = append(merged.Windows, window)
		}
	}
	if err := a.db.SaveSnapshot(ctx, merged); err != nil {
		return err
	}
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	reading.Windows = fresh
	return a.db.Schedule(ctx, reading, config, now)
}

func claudeSummary(reading quota.Snapshot) string {
	parts := make([]string, 0, len(reading.Windows))
	for _, window := range reading.Windows {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", claudeWindowNames[window.ID], window.UsedPercent))
	}
	return "Claude " + strings.Join(parts, " · ")
}
