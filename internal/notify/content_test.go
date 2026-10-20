package notify

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func sampleMessage() Message {
	return Message{
		Key:      "codex/primary/2026-10-15T15:00:00Z",
		To:       "person@example.com",
		From:     "person@example.com",
		Provider: "Codex",
		Window:   "five-hour",
		ResetAt:  time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC),
	}
}

func TestExplicitTestEmailIgnoresResetMetadata(t *testing.T) {
	m := sampleMessage()
	m.Test = true
	m.Confirmed = true
	m.OtherLimit = true
	m.Provider = "<script>misleading provider</script>"
	m.Window = "misleading window"
	subject, plain, markup := messageContent(m)
	if subject != "ResetWatch: test email" || !strings.Contains(plain, "does not confirm a reset or available allowance") {
		t.Fatal("test email must not claim a quota reset or confirmed allowance")
	}
	for _, forbidden := range []string{m.Provider, m.Window, "CONFIRMED RESET", "EXPECTED RESET", "2026-10-15", "still exhausted"} {
		if strings.Contains(plain+markup, forbidden) {
			t.Errorf("test email included reset metadata: %q", forbidden)
		}
	}
}

func TestExplicitTestEmailValidation(t *testing.T) {
	cases := []func(*Message){
		func(m *Message) { m.Key = "" },
		func(m *Message) { m.Key = strings.Repeat("x", 4097) },
		func(m *Message) { m.To = "invalid" },
		func(m *Message) { m.From = "alerts@example.com\r\nBcc:other@example.com" },
	}
	for _, mutate := range cases {
		m := Message{Key: "test", To: "person@example.com", From: "alerts@example.com", Test: true}
		mutate(&m)
		if err := validateMessage(m); err == nil {
			t.Fatal("test email bypassed key or address validation")
		}
	}
}

func TestMessageContentEscapesHTML(t *testing.T) {
	m := sampleMessage()
	m.Provider = "<script>alert('test')</script>"
	m.Window = "five-hour & weekly"
	m.Confirmed = true
	m.OtherLimit = true
	subject, plain, markup := messageContent(m)
	if strings.Contains(markup, "<script>") || !strings.Contains(markup, "&lt;script&gt;") {
		t.Fatalf("provider label was not escaped: %s", markup)
	}
	if !strings.Contains(plain, m.Provider) || !strings.Contains(markup, "&amp;") {
		t.Fatal("plain text or escaped window missing")
	}
	if !strings.Contains(subject, "is back") || !strings.Contains(plain, "Fresh account data confirms") {
		t.Fatal("confirmed status missing")
	}
	if !strings.Contains(plain, "limit is still used up") {
		t.Fatal("other-window warning missing")
	}
}

func TestResetEmailNamesWindowAndKeepsClaimsApart(t *testing.T) {
	m := sampleMessage()
	m.Provider = "codex"
	m.Duration = 5 * time.Hour
	m.Confirmed = true
	subject, plain, _ := messageContent(m)
	if subject != "Your Codex 5-hour limit is back. Get back to work" || !strings.Contains(plain, "get back to work") {
		t.Fatalf("confirmed email: subject %q, text %q", subject, plain)
	}
	m.Confirmed = false
	subject, plain, _ = messageContent(m)
	if subject != "Your Codex 5-hour limit should be back. Check before you start" || strings.Contains(plain, "get back to work") || !strings.Contains(plain, "check Codex") {
		t.Fatalf("unconfirmed email must not claim availability: subject %q, text %q", subject, plain)
	}
	if !strings.Contains(plain, "2026-10-15 15:00:00 UTC") {
		t.Fatal("exact UTC reset time missing")
	}
}

func TestWindowLabel(t *testing.T) {
	cases := map[time.Duration]string{0: "", -time.Hour: "", 5 * time.Hour: "5-hour", 7 * 24 * time.Hour: "weekly",
		24 * time.Hour: "1-day", 90 * time.Minute: "90-minute"}
	for duration, want := range cases {
		if got := windowLabel(duration); got != want {
			t.Errorf("windowLabel(%v) = %q, want %q", duration, got, want)
		}
	}
	if got := limitName(Message{Provider: "codex"}); got != "Codex limit" {
		t.Errorf("unknown window length should fall back to a generic name, got %q", got)
	}
}

func TestParseRetryAfterBounds(t *testing.T) {
	now := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		value string
		want  time.Duration
	}{
		{"120", 2 * time.Minute},
		{"-10", 0},
		{"0", 0},
		{"90000", 24 * time.Hour},
		{"9223372036854775807", 24 * time.Hour},
		{"999999999999999999999999", 0},
		{"junk", 0},
		{now.Add(5 * time.Minute).Format(http.TimeFormat), 5 * time.Minute},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{strings.Repeat("1", 129), 0},
	}
	for _, tc := range cases {
		if got := parseRetryAfter(tc.value, now); got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
	if RetryAfter(errors.New("other")) != 0 || Retryable(nil) {
		t.Fatal("unrelated errors must not have retry metadata")
	}
}

func TestInvalidMailboxesAreRejected(t *testing.T) {
	for _, address := range []string{"", "invalid", "a@example.com,b@example.com", "a@example.com\r\nBcc:other@example.com", "person\t@example.com", strings.Repeat("x", 513), "Person <person@example.com>"} {
		m := sampleMessage()
		m.To = address
		if validateMessage(m) == nil {
			t.Errorf("recipient %q was accepted", address)
		}
	}
	for _, address := range []string{"", "invalid", "a@example.com\r\nBcc:other@example.com"} {
		m := sampleMessage()
		m.From = address
		if validateMessage(m) == nil {
			t.Errorf("sender %q was accepted", address)
		}
	}
}

func TestInvalidMessage(t *testing.T) {
	cases := []func(*Message){
		func(m *Message) { m.Key = "" },
		func(m *Message) { m.Key = strings.Repeat("x", 4097) },
		func(m *Message) { m.ResetAt = time.Time{} },
		func(m *Message) { m.Provider = "" },
		func(m *Message) { m.Provider = "Codex\nInjected" },
		func(m *Message) { m.Window = strings.Repeat("x", 129) },
		func(m *Message) { m.Window = "\t" },
	}
	for _, mutate := range cases {
		m := sampleMessage()
		mutate(&m)
		if _, err := Prepare(m, time.Now()); err == nil {
			t.Fatalf("invalid message was rendered: %+v", m)
		}
	}
}

func TestFrozenLocalResetTimeSurvivesZoneChange(t *testing.T) {
	originalZone := time.Local
	t.Cleanup(func() { time.Local = originalZone })
	message := sampleMessage()
	message.LocalResetAt = "8:30 PM IST" // fixed when the watcher first froze the message
	time.Local = time.FixedZone("TestWest", -8*3600)
	_, plain, markup := messageContent(message)
	if !strings.Contains(plain, "Reset at 8:30 PM IST") || !strings.Contains(markup, "Reset at 8:30 PM IST") {
		t.Fatalf("frozen local reset time was not used: %s", plain)
	}
}
