package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/watch"
)

// savedSnapshot hands an already-read snapshot to the watcher's save-and-schedule step.
type savedSnapshot struct{ snapshot quota.Snapshot }

func (s savedSnapshot) Read(context.Context) (quota.Snapshot, error) { return s.snapshot, nil }

var claudeWindowNames = map[string]string{"claude:five_hour": "5h", "claude:seven_day": "7d"}

// claudeStatusLine runs as the Claude Code status line command: it records plan usage
// from the JSON on stdin, schedules reset reminders, and prints a short summary.
func (a *application) claudeStatusLine(ctx context.Context, args []string) error {
	if err := noArguments(args); err != nil {
		return err
	}
	snapshot, err := quota.DecodeClaudeStatusLine(a.in, time.Now())
	if errors.Is(err, quota.ErrNoClaudeLimits) {
		return nil // nothing to record yet; keep the status line empty
	}
	if err != nil {
		return err
	}
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	engine := watch.Engine{DB: a.db, Provider: savedSnapshot{snapshot}}
	if _, err := engine.Observe(ctx, config); err != nil {
		return err
	}
	parts := make([]string, 0, len(snapshot.Windows))
	for _, window := range snapshot.Windows {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", claudeWindowNames[window.ID], window.UsedPercent))
	}
	fmt.Fprintln(a.out, "Claude "+strings.Join(parts, " · "))
	return nil
}
