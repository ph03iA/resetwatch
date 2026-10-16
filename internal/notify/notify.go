// Package notify sends reset notifications without exposing provider credentials.
package notify

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Message struct {
	Key      string
	To       string
	From     string
	Provider string
	Window   string
	ResetAt  time.Time
	Duration time.Duration // reset window length, used for "5-hour"/"weekly"; zero when unknown
	// LocalResetAt freezes the displayed reset time; new messages use IST, older retries keep their original text.
	LocalResetAt string `json:",omitempty"`
	Confirmed    bool
	OtherLimit   bool
	Test         bool
}

type Sender interface {
	Send(context.Context, Message) error
}

type deliveryError struct {
	message    string
	retryable  bool
	retryAfter time.Duration
}

func (e *deliveryError) Error() string { return e.message }

// Retryable reports whether a delivery can be retried with the same message key.
func Retryable(err error) bool {
	var failure *deliveryError
	return errors.As(err, &failure) && failure.retryable
}

// RetryAfter returns the provider's requested delay, capped at 24 hours.
func RetryAfter(err error) time.Duration {
	var failure *deliveryError
	if errors.As(err, &failure) {
		return failure.retryAfter
	}
	return 0
}

func validateMessage(m Message) error {
	if m.Key == "" || len(m.Key) > 4096 {
		return errors.New("email requires a nonempty message key of at most 4096 bytes")
	}
	if err := validateResetMetadata(m); err != nil {
		return err
	}
	return ValidateAddresses(m.To, m.From)
}

func validateResetMetadata(m Message) error {
	if m.Test {
		return nil
	}
	if m.ResetAt.IsZero() {
		return errors.New("email requires a reset time")
	}
	if err := validateLabel(m.Provider); err != nil {
		return errors.New("email provider label is empty, too long, or contains control characters")
	}
	if err := validateLabel(m.Window); err != nil {
		return errors.New("email window label is empty, too long, or contains control characters")
	}
	return nil
}

// ValidateAddresses checks the configured recipient and sender without sending mail.
func ValidateAddresses(to, from string) error {
	if _, err := mailbox(to, false); err != nil {
		return errors.New("email recipient must be one valid mailbox without a display name")
	}
	if _, err := mailbox(from, true); err != nil {
		return errors.New("email sender must be one valid mailbox")
	}
	return nil
}

func validateLabel(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 128 || strings.ContainsFunc(value, unicode.IsControl) {
		return errors.New("invalid label")
	}
	return nil
}

func mailbox(value string, displayName bool) (string, error) {
	if len(value) > 512 || strings.ContainsFunc(value, unicode.IsControl) {
		return "", errors.New("invalid mailbox")
	}
	address, err := mail.ParseAddress(value)
	if err != nil {
		return "", errors.New("invalid mailbox")
	}
	if len(address.Address) > 320 || (!displayName && address.Name != "") {
		return "", errors.New("invalid mailbox")
	}
	if displayName {
		return address.String(), nil
	}
	return address.Address, nil
}

func messageContent(m Message) (subject, text, markup string) {
	if m.Test {
		return testMessageContent()
	}
	provider, limit := providerName(m.Provider), limitName(m)
	headline := fmt.Sprintf("Your %s is back.", limit)
	lead := "Time to get back to work."
	paragraphs := []string{"Fresh account data confirms allowance is available in every reported window."}
	subject = fmt.Sprintf("Your %s is back. Get back to work", limit)
	// A scheduled reset and freshly confirmed allowance are different claims.
	if !m.Confirmed {
		headline = fmt.Sprintf("Your %s should be back.", limit)
		lead = "Its reset time has arrived."
		paragraphs[0] = fmt.Sprintf("ResetWatch couldn't confirm it with fresh account data, so check %s before you dive in.", provider)
		subject = fmt.Sprintf("Your %s should be back. Check before you start", limit)
	}
	if m.OtherLimit {
		paragraphs = append(paragraphs, fmt.Sprintf("Heads up: another %s limit is still used up, so access isn't confirmed yet.", provider))
	}
	details := fmt.Sprintf("Reset at %s · %s · %s", localResetAt(m), m.Window,
		m.ResetAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	text = headline + "\n" + lead + "\n\n" + strings.Join(paragraphs, "\n\n") + "\n\n" + details + "\n\nSent by ResetWatch from your computer."
	return subject, text, resetHTML(headline, lead, paragraphs, details)
}

// localResetAt falls back to UTC for messages frozen before LocalResetAt existed, keeping their retries identical.
func localResetAt(m Message) string {
	if m.LocalResetAt != "" {
		return m.LocalResetAt
	}
	return m.ResetAt.UTC().Format("3:04 PM MST")
}

func providerName(provider string) string {
	first, size := utf8.DecodeRuneInString(provider)
	return string(unicode.ToUpper(first)) + provider[size:]
}

func limitName(m Message) string {
	if label := windowLabel(m.Duration); label != "" {
		return providerName(m.Provider) + " " + label + " limit"
	}
	return providerName(m.Provider) + " limit"
}

// windowLabel names a reset window by its length, such as "5-hour" or "weekly"; empty when unknown.
func windowLabel(d time.Duration) string {
	day := 24 * time.Hour
	switch {
	case d <= 0:
		return ""
	case d == 7*day:
		return "weekly"
	case d%day == 0:
		return fmt.Sprintf("%d-day", d/day)
	case d%time.Hour == 0:
		return fmt.Sprintf("%d-hour", d/time.Hour)
	}
	return fmt.Sprintf("%d-minute", d/time.Minute)
}

func resetHTML(headline, lead string, paragraphs []string, details string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="margin:0;padding:24px;background:#f4f1ea;font-family:Arial,Helvetica,sans-serif;color:#1d1c19">`)
	b.WriteString(`<div style="max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #e4dfd3;border-radius:14px;overflow:hidden">`)
	b.WriteString(`<div style="background:#151513;padding:28px 32px;color:#ece9e1"><p style="margin:0 0 18px;font-size:12px;letter-spacing:.08em;text-transform:uppercase;color:#c89370">ResetWatch</p>`)
	fmt.Fprintf(&b, `<h1 style="margin:0;font-size:28px;line-height:1.2;font-weight:600">%s</h1>`, html.EscapeString(headline))
	fmt.Fprintf(&b, `<p style="margin:10px 0 0;font-size:18px;color:#e2b48f">%s</p></div><div style="padding:26px 32px">`, html.EscapeString(lead))
	for _, paragraph := range paragraphs {
		fmt.Fprintf(&b, `<p style="margin:0 0 14px;font-size:15px;line-height:1.6">%s</p>`, html.EscapeString(paragraph))
	}
	fmt.Fprintf(&b, `<p style="margin:18px 0 0;padding-top:14px;border-top:1px solid #eee9de;font-size:12px;line-height:1.6;color:#77736a">%s<br>Sent by ResetWatch from your computer.</p>`, html.EscapeString(details))
	b.WriteString(`</div></div></body></html>`)
	return b.String()
}

func testMessageContent() (subject, text, markup string) {
	subject = "ResetWatch: test email"
	text = "TEST EMAIL\n\nThis is a delivery test you requested from ResetWatch.\n\nNo AI quota was checked. This message does not confirm a reset or available allowance.\n\nIf this message reached your inbox, ResetWatch can deliver email to this address."
	return subject, text, emailHTML(text)
}

func emailHTML(text string) string {
	paragraphs := strings.ReplaceAll(html.EscapeString(text), "\n\n", "</p><p>")
	return "<!doctype html><html><body><main style=\"font-family:Arial,sans-serif;line-height:1.6;max-width:600px;margin:24px auto;padding:24px\"><p>" + paragraphs + "</p></main></body></html>"
}
