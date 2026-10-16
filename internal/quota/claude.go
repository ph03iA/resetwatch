package quota

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// ErrNoClaudeLimits means the status line data had no plan usage windows:
// not a Pro/Max subscriber, or no API response in the session yet.
var ErrNoClaudeLimits = errors.New("Claude Code status line data has no plan usage limits")

// ClaudeAccountID stands in for an account identity.
// shortcut: the status line JSON has no account identity, so account switches go undetected; upgrade if Claude Code adds one.
const ClaudeAccountID = "claude-code"

const maxStatusLine = 1 << 20

type claudeWindow struct {
	UsedPercent *float64 `json:"used_percentage"`
	ResetsAt    *int64   `json:"resets_at"`
}

// DecodeClaudeStatusLine reads the JSON Claude Code sends to a status line command
// (documented fields rate_limits.five_hour and rate_limits.seven_day) and returns every reported window.
func DecodeClaudeStatusLine(r io.Reader, now time.Time) (Snapshot, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxStatusLine+1))
	if err != nil || len(data) > maxStatusLine {
		return Snapshot{}, errors.New("Claude Code status line data is unreadable or larger than 1 MiB")
	}
	var input struct {
		RateLimits struct {
			FiveHour *claudeWindow `json:"five_hour"`
			SevenDay *claudeWindow `json:"seven_day"`
		} `json:"rate_limits"`
	}
	// Windows PowerShell prefixes piped text with a UTF-8 byte-order mark.
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &input); err != nil {
		return Snapshot{}, errors.New("Claude Code status line data is not valid JSON")
	}
	snapshot := Snapshot{Provider: "claude", AccountID: ClaudeAccountID, ObservedAt: now.UTC(), Partial: true}
	for _, entry := range []struct {
		id       string
		duration time.Duration
		data     *claudeWindow
	}{{"claude:five_hour", 5 * time.Hour, input.RateLimits.FiveHour}, {"claude:seven_day", 7 * 24 * time.Hour, input.RateLimits.SevenDay}} {
		if window, ok := claudeWindowAt(entry.id, entry.duration, entry.data, now); ok {
			snapshot.Windows = append(snapshot.Windows, window)
		}
	}
	if len(snapshot.Windows) == 0 {
		return Snapshot{}, ErrNoClaudeLimits
	}
	return snapshot, ValidateSnapshot(snapshot)
}

// claudeWindowAt skips absent windows and ones whose reset has already passed in stale data.
func claudeWindowAt(id string, duration time.Duration, data *claudeWindow, now time.Time) (Window, bool) {
	if data == nil || data.UsedPercent == nil || data.ResetsAt == nil {
		return Window{}, false
	}
	resetsAt := time.Unix(*data.ResetsAt, 0).UTC()
	if !resetsAt.After(now) {
		return Window{}, false
	}
	return Window{ID: id, UsedPercent: *data.UsedPercent, ResetsAt: resetsAt, Duration: duration}, true
}
