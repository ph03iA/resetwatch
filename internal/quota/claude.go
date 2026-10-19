package quota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNoClaudeLimits means Claude Code reported no plan limits: an API-key, Bedrock or Vertex
// session, or a claude.ai account without a Pro or Max plan.
var ErrNoClaudeLimits = errors.New("Claude Code reports no plan usage limits; sign in to Claude Code with a Pro or Max plan")

// Claude reads plan usage the way the Claude Agent SDK does: it starts Claude Code with no prompt,
// so no model request is made, and sends the get_usage control request behind /usage.
// shortcut: get_usage is marked experimental by Anthropic and may change; AGENTS.md records this exception.
type Claude struct {
	Binary  string // empty: look up claude on PATH
	Timeout time.Duration
}

func (provider Claude) Read(ctx context.Context) (Snapshot, error) {
	timeout := provider.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	binary, err := claudeBinary(provider.Binary)
	if err != nil {
		return Snapshot{}, err
	}
	session, err := startClaude(ctx, binary)
	if err != nil {
		return Snapshot{}, err
	}
	defer session.close()
	var init claudeInitResponse
	if err := session.request("initialize", map[string]any{"subtype": "initialize"}, &init); err != nil {
		return Snapshot{}, err
	}
	var usage claudeUsageResponse
	// skip_behaviors avoids scanning every local transcript; only the rate limits are needed.
	if err := session.request("usage", map[string]any{"subtype": "get_usage", "skip_behaviors": true}, &usage); err != nil {
		return Snapshot{}, err
	}
	return decodeClaudeUsage(init, usage, time.Now().UTC())
}

func claudeBinary(configured string) (string, error) {
	path := configured
	if path == "" {
		found, err := exec.LookPath("claude")
		if err != nil {
			return "", errors.New("Claude Code not found on PATH; install Claude Code and sign in with a Pro or Max plan")
		}
		path = found
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat", ".ps1":
		return "", errors.New("Claude Code shell shims are unsupported; install the native Claude Code build")
	}
	return path, nil
}

type claudeInitResponse struct {
	Account *struct {
		Email *string `json:"email"`
	} `json:"account"`
}

type claudeUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

type claudeUsageResponse struct {
	RateLimitsAvailable bool `json:"rate_limits_available"`
	RateLimits          *struct {
		FiveHour *claudeUsageWindow `json:"five_hour"`
		SevenDay *claudeUsageWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

func decodeClaudeUsage(init claudeInitResponse, usage claudeUsageResponse, now time.Time) (Snapshot, error) {
	if init.Account == nil || init.Account.Email == nil || strings.TrimSpace(*init.Account.Email) == "" {
		return Snapshot{}, errors.New("Claude Code is not signed in to a claude.ai account; run claude and sign in")
	}
	if !usage.RateLimitsAvailable || usage.RateLimits == nil {
		return Snapshot{}, ErrNoClaudeLimits
	}
	email := strings.ToLower(strings.TrimSpace(*init.Account.Email))
	digest := sha256.Sum256([]byte("claude:" + email))
	// Partial: Claude may omit a window, which means unknown rather than removed.
	snapshot := Snapshot{Provider: "claude", AccountID: hex.EncodeToString(digest[:]), ObservedAt: now, Partial: true}
	for _, entry := range []struct {
		id       string
		duration time.Duration
		data     *claudeUsageWindow
	}{{"claude:five_hour", 5 * time.Hour, usage.RateLimits.FiveHour}, {"claude:seven_day", 7 * 24 * time.Hour, usage.RateLimits.SevenDay}} {
		if window, ok := claudeWindowAt(entry.id, entry.duration, entry.data, now); ok {
			snapshot.Windows = append(snapshot.Windows, window)
		}
	}
	if len(snapshot.Windows) == 0 {
		return Snapshot{}, ErrNoClaudeLimits
	}
	return snapshot, ValidateSnapshot(snapshot)
}

// claudeWindowAt skips absent or already-reset windows. Claude recomputes resets_at with
// sub-second drift between calls, so it is rounded to the minute to keep one reminder per window.
func claudeWindowAt(id string, duration time.Duration, data *claudeUsageWindow, now time.Time) (Window, bool) {
	if data == nil || data.Utilization == nil || data.ResetsAt == nil {
		return Window{}, false
	}
	resetsAt, err := time.Parse(time.RFC3339Nano, *data.ResetsAt)
	if err != nil {
		return Window{}, false
	}
	resetsAt = resetsAt.UTC().Round(time.Minute)
	if !resetsAt.After(now) {
		return Window{}, false
	}
	return Window{ID: id, UsedPercent: *data.Utilization, ResetsAt: resetsAt, Duration: duration}, true
}
