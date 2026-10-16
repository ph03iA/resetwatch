package main

import (
	"context"
	"errors"
	"fmt"
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
	reading, err := quota.DecodeClaudeStatusLine(a.in, now)
	if errors.Is(err, quota.ErrNoClaudeLimits) {
		return nil // nothing to record yet; keep the status line empty
	}
	if err != nil {
		return err
	}
	if err := a.db.RecordSnapshot(ctx, reading, now); err != nil {
		return err
	}
	fmt.Fprintln(a.out, claudeSummary(reading))
	return nil
}

func claudeSummary(reading quota.Snapshot) string {
	parts := make([]string, 0, len(reading.Windows))
	for _, window := range reading.Windows {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", claudeWindowNames[window.ID], window.UsedPercent))
	}
	return "Claude " + strings.Join(parts, " · ")
}
