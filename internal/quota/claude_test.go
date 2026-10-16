package quota

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var claudeNow = time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)

func TestDecodeClaudeStatusLine(t *testing.T) {
	input := `{"session_id":"s1","rate_limits":{"five_hour":{"used_percentage":100,"resets_at":1893582000},` +
		`"seven_day":{"used_percentage":41.2,"resets_at":1894100000}}}`
	snapshot, err := DecodeClaudeStatusLine(strings.NewReader(input), claudeNow)
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{Provider: "claude", AccountID: ClaudeAccountID, ObservedAt: claudeNow, Partial: true, Windows: []Window{
		{ID: "claude:five_hour", UsedPercent: 100, ResetsAt: time.Unix(1893582000, 0).UTC(), Duration: 5 * time.Hour},
		{ID: "claude:seven_day", UsedPercent: 41.2, ResetsAt: time.Unix(1894100000, 0).UTC(), Duration: 7 * 24 * time.Hour},
	}}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot %#v", snapshot)
	}
}

func TestDecodeClaudeStatusLineAcceptsByteOrderMarkAndNoSession(t *testing.T) {
	for _, input := range []string{
		"\xef\xbb\xbf" + `{"session_id":"s1","rate_limits":{"five_hour":{"used_percentage":42,"resets_at":1893582000}}}` + "\r\n",
		`{"rate_limits":{"five_hour":{"used_percentage":42,"resets_at":1893582000}}}`,
	} {
		if _, err := DecodeClaudeStatusLine(strings.NewReader(input), claudeNow); err != nil {
			t.Fatalf("valid status line data was rejected: %v", err)
		}
	}
}

func TestDecodeClaudeStatusLineSkipsAbsentAndExpiredWindows(t *testing.T) {
	input := `{"session_id":"s1","rate_limits":{"five_hour":{"used_percentage":100,"resets_at":1893578400},"seven_day":{"used_percentage":12,"resets_at":1894100000}}}`
	snapshot, err := DecodeClaudeStatusLine(strings.NewReader(input), claudeNow)
	if err != nil || len(snapshot.Windows) != 1 || snapshot.Windows[0].ID != "claude:seven_day" {
		t.Fatalf("expired five-hour window should be skipped: %#v, %v", snapshot, err)
	}
}

func TestDecodeClaudeStatusLineRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"no rate limits":     `{"session_id":"s1","model":{"display_name":"Opus"}}`,
		"missing percentage": `{"session_id":"s1","rate_limits":{"five_hour":{"resets_at":1893582000}}}`,
		"not json":           `not json`,
		"percent over 100":   `{"session_id":"s1","rate_limits":{"five_hour":{"used_percentage":140,"resets_at":1893582000}}}`,
		"too large":          `{"pad":"` + strings.Repeat("x", maxStatusLine) + `"}`,
	}
	for name, input := range cases {
		if _, err := DecodeClaudeStatusLine(strings.NewReader(input), claudeNow); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := DecodeClaudeStatusLine(strings.NewReader(`{}`), claudeNow); !errors.Is(err, ErrNoClaudeLimits) {
		t.Errorf("missing limits should be ErrNoClaudeLimits, got %v", err)
	}
}
