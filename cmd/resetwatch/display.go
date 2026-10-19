package main

import (
	"fmt"
	"strings"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

func (a *application) render(snapshot quota.Snapshot) {
	fmt.Fprintln(a.out, "\n  RESETWATCH / QUOTA MONITOR")
	fmt.Fprintln(a.out, "  ──────────────────────────────────────────────────────")
	fmt.Fprintf(a.out, "  %-22s %-20s %s\n", "WINDOW", "USED", "NEXT RESET")
	for _, window := range snapshot.Windows {
		a.renderWindow(window)
	}
	fmt.Fprintln(a.out, "  ──────────────────────────────────────────────────────")
	fmt.Fprintln(a.out, "  Provider:", snapshot.Provider)
	fmt.Fprintln(a.out, "  Last observed:", snapshot.ObservedAt.Local().Format(time.RFC1123))
	fmt.Fprintln(a.out, "  Saved observation; run status --refresh for current limits.\n ")
}

func (a *application) renderWindow(window quota.Window) {
	filled := min(max(int(window.UsedPercent/10), 0), 10)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
	fmt.Fprintf(a.out, "  %-22s %s %5.1f%%   %s\n", window.ID, bar, window.UsedPercent, countdown(window.ResetsAt, time.Now()))
}

func countdown(reset, now time.Time) string {
	if !reset.After(now) {
		return "expected reset passed; refresh needed"
	}
	return reset.Sub(now).Round(time.Second).String()
}

func (a *application) renderHistory(alerts []store.Alert) {
	if len(alerts) == 0 {
		fmt.Fprintln(a.out, "No alerts yet. Alerts are armed when an exhausted quota window is observed.")
		return
	}
	fmt.Fprintf(a.out, "%-12s %-10s %-22s %-8s %s\n", "ID", "STATE", "WINDOW", "ATTEMPTS", "RESET (LOCAL)")
	for _, alert := range alerts {
		fmt.Fprintf(a.out, "%-12s %-10s %-22s %-8d %s\n", alert.ID[:12], alert.State, alert.Window, alert.Attempts, alert.ResetAt.Local().Format(time.RFC3339))
		if alert.LastError != "" {
			fmt.Fprintln(a.out, "  "+alert.LastError)
		}
	}
}
