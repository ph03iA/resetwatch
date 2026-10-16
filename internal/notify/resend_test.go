package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sampleMessage() Message {
	return Message{
		Key:      "codex/primary/2026-10-15T15:00:00Z",
		To:       "person@example.com",
		From:     "ResetWatch <alerts@example.com>",
		Provider: "Codex",
		Window:   "five-hour",
		ResetAt:  time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC),
	}
}

func testService(t *testing.T, handler http.HandlerFunc) Resend {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return Resend{APIKey: "re_fake_test_key", Client: server.Client(), Endpoint: server.URL}
}

func receipt(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"id":"test-receipt"}`) }

func requireError(t *testing.T, err error, retry bool) {
	t.Helper()
	if err == nil {
		t.Fatal("expected delivery error")
	}
	if Retryable(err) != retry {
		t.Fatalf("Retryable(%v) = %v, want %v", err, Retryable(err), retry)
	}
}

func TestResendSuccess(t *testing.T) {
	m := sampleMessage()
	service := testService(t, func(w http.ResponseWriter, r *http.Request) {
		checkRequestHeaders(t, r, m.Key)
		var payload emailPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		checkPayload(t, payload)
		receipt(w)
	})
	if err := service.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitTestEmailDelivery(t *testing.T) {
	m := Message{Key: "explicit-test-unique-id", To: "person@example.com", From: "ResetWatch <alerts@example.com>", Test: true}
	service := testService(t, func(w http.ResponseWriter, r *http.Request) {
		checkRequestHeaders(t, r, m.Key)
		var payload emailPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Subject != "ResetWatch: test email" {
			t.Errorf("unexpected test subject: %q", payload.Subject)
		}
		if !strings.Contains(payload.Text, "No AI quota was checked") || !strings.Contains(payload.HTML, "TEST EMAIL") {
			t.Error("test email must be clearly labeled and explain that quota was not checked")
		}
		receipt(w)
	})
	if err := service.Send(context.Background(), m); err != nil {
		t.Fatal(err)
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

func checkRequestHeaders(t *testing.T, r *http.Request, key string) {
	t.Helper()
	checks := map[string]string{
		"Authorization":   "Bearer re_fake_test_key",
		"Content-Type":    "application/json",
		"Idempotency-Key": idempotencyKey(key),
		"User-Agent":      "resetwatch",
	}
	for name, want := range checks {
		if got := r.Header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if r.Method != http.MethodPost {
		t.Errorf("method = %s", r.Method)
	}
}

func checkPayload(t *testing.T, payload emailPayload) {
	t.Helper()
	if len(payload.To) != 1 || payload.To[0] != "person@example.com" {
		t.Errorf("unexpected recipient: %v", payload.To)
	}
	if !strings.Contains(payload.From, "alerts@example.com") {
		t.Errorf("unexpected sender: %q", payload.From)
	}
	if !strings.Contains(payload.Subject, "should be back") {
		t.Errorf("unexpected subject: %q", payload.Subject)
	}
	if !strings.Contains(payload.Text, "couldn't confirm it with fresh account data") {
		t.Errorf("expected uncertainty explanation in %q", payload.Text)
	}
	if !strings.Contains(payload.HTML, "should be back.</h1>") {
		t.Errorf("expected HTML alternative")
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

func TestHTTPErrorClassification(t *testing.T) {
	cases := []struct {
		status int
		retry  bool
	}{
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusUnprocessableEntity, false},
		{http.StatusRequestTimeout, true},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			service := testService(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":"re_SECRET never expose person@example.com"}`)
			})
			err := service.Send(context.Background(), sampleMessage())
			requireError(t, err, tc.retry)
			if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "person@example.com") {
				t.Fatalf("raw provider response leaked: %v", err)
			}
		})
	}
}

func TestRetryAfter(t *testing.T) {
	service := testService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	err := service.Send(context.Background(), sampleMessage())
	requireError(t, err, true)
	if got := RetryAfter(err); got != 90*time.Second {
		t.Fatalf("RetryAfter = %v", got)
	}
	wrapped := errors.Join(errors.New("send failed"), err)
	if !Retryable(wrapped) || RetryAfter(wrapped) != 90*time.Second {
		t.Fatal("wrapped delivery error lost retry metadata")
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

func TestInvalidReceipt(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"id":""}`, `{"id":"   "}`, `{"id":42}`, `{"id":"ok"} trailing`, `{"id":"bad\nvalue"}`} {
		t.Run(body, func(t *testing.T) {
			service := testService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			requireError(t, service.Send(context.Background(), sampleMessage()), true)
		})
	}
}

func TestResponseLimit(t *testing.T) {
	service := testService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"`+strings.Repeat("x", maxResponse)+`"}`)
	})
	err := service.Send(context.Background(), sampleMessage())
	requireError(t, err, true)
	if !strings.Contains(err.Error(), "64 KiB") {
		t.Fatalf("unexpected response limit error: %v", err)
	}
}

func TestInvalidMailboxes(t *testing.T) {
	for _, address := range []string{"", "invalid", "a@example.com,b@example.com", "a@example.com\r\nBcc:other@example.com", "person\t@example.com", strings.Repeat("x", 513)} {
		t.Run(address, func(t *testing.T) {
			m := sampleMessage()
			m.To = address
			requireError(t, (Resend{APIKey: "test"}).Send(context.Background(), m), false)
			m = sampleMessage()
			m.From = address
			requireError(t, (Resend{APIKey: "test"}).Send(context.Background(), m), false)
		})
	}
}

func TestRecipientDisplayNameRejected(t *testing.T) {
	m := sampleMessage()
	m.To = "Person <person@example.com>"
	requireError(t, (Resend{APIKey: "test"}).Send(context.Background(), m), false)
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
		requireError(t, (Resend{APIKey: "test"}).Send(context.Background(), m), false)
	}
}

func TestInvalidAPIKey(t *testing.T) {
	for _, key := range []string{"", " ", "secret\nInjected", "with spaces", strings.Repeat("x", 4097)} {
		err := (Resend{APIKey: key}).Send(context.Background(), sampleMessage())
		requireError(t, err, false)
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("key leaked in error: %v", err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMandatoryRequestDeadline(t *testing.T) {
	service := Resend{APIKey: "test", Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		deadline, exists := r.Context().Deadline()
		if !exists || time.Until(deadline) > sendTimeout || time.Until(deadline) <= 0 {
			t.Errorf("missing or invalid request deadline: %v", deadline)
		}
		if r.URL.String() != resendEndpoint {
			t.Errorf("default endpoint = %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"ok"}`)), Header: http.Header{}}, nil
	})}}
	if err := service.Send(context.Background(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
}

func TestRequestTimeout(t *testing.T) {
	service := testService(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	requireError(t, service.Send(ctx, sampleMessage()), true)
}

func TestCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requireError(t, (Resend{APIKey: "test"}).Send(ctx, sampleMessage()), false)
}

func TestTransportErrorRedaction(t *testing.T) {
	service := Resend{APIKey: "re_SECRET", Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("sensitive re_SECRET endpoint and credential body")
	})}}
	err := service.Send(context.Background(), sampleMessage())
	requireError(t, err, true)
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "credential") {
		t.Fatalf("transport details leaked: %v", err)
	}
}

func TestRedirectRejected(t *testing.T) {
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Store(true)
		receipt(w)
	}))
	defer target.Close()
	service := testService(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	requireError(t, service.Send(context.Background(), sampleMessage()), false)
	if reached.Load() {
		t.Fatal("email request followed redirect")
	}
}

func TestIdempotencyKey(t *testing.T) {
	key := idempotencyKey("event\nwith arbitrary data")
	if len(key) != len("resetwatch-")+64 || strings.ContainsAny(key, "\r\n") {
		t.Fatalf("invalid bounded idempotency key: %q", key)
	}
	if key != idempotencyKey("event\nwith arbitrary data") || key == idempotencyKey("other event") {
		t.Fatal("idempotency key must be stable and event-specific")
	}
}
