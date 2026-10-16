package quota

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeClaudeStatusLine(t *testing.T) {
	now := time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)
	input := `{"model":{"display_name":"Opus"},"rate_limits":{"five_hour":{"used_percentage":100,"resets_at":1893582000},` +
		`"seven_day":{"used_percentage":41.2,"resets_at":1894100000}}}`
	snapshot, err := DecodeClaudeStatusLine(strings.NewReader(input), now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Provider != "claude" || snapshot.AccountID != ClaudeAccountID || len(snapshot.Windows) != 2 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	five := snapshot.Windows[0]
	if five.ID != "claude:five_hour" || five.UsedPercent != 100 || five.Duration != 5*time.Hour || five.ResetsAt.Unix() != 1893582000 {
		t.Fatalf("unexpected five-hour window: %#v", five)
	}
	if snapshot.Windows[1].Duration != 7*24*time.Hour {
		t.Fatalf("unexpected weekly window: %#v", snapshot.Windows[1])
	}
	if _, err := DecodeClaudeStatusLine(strings.NewReader("\xef\xbb\xbf"+input+"\r\n"), now); err != nil {
		t.Fatalf("PowerShell-piped input with a byte-order mark was rejected: %v", err)
	}
}

func TestDecodeClaudeStatusLineSkipsAbsentAndExpiredWindows(t *testing.T) {
	now := time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)
	input := `{"rate_limits":{"five_hour":{"used_percentage":100,"resets_at":1893578400},"seven_day":{"used_percentage":12,"resets_at":1894100000}}}`
	snapshot, err := DecodeClaudeStatusLine(strings.NewReader(input), now)
	if err != nil || len(snapshot.Windows) != 1 || snapshot.Windows[0].ID != "claude:seven_day" {
		t.Fatalf("expired five-hour window should be skipped: %#v, %v", snapshot, err)
	}
}

func TestDecodeClaudeStatusLineRejectsBadInput(t *testing.T) {
	now := time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"no rate limits":     `{"model":{"display_name":"Opus"}}`,
		"missing percentage": `{"rate_limits":{"five_hour":{"resets_at":1893582000}}}`,
		"not json":           `not json`,
		"percent over 100":   `{"rate_limits":{"five_hour":{"used_percentage":140,"resets_at":1893582000}}}`,
		"too large":          `{"pad":"` + strings.Repeat("x", maxStatusLine) + `"}`,
	}
	for name, input := range cases {
		if _, err := DecodeClaudeStatusLine(strings.NewReader(input), now); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := DecodeClaudeStatusLine(strings.NewReader(`{}`), now); !errors.Is(err, ErrNoClaudeLimits) {
		t.Errorf("missing limits should be ErrNoClaudeLimits, got %v", err)
	}
}
