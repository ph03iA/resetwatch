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
)

type Message struct {
	Key        string
	To         string
	From       string
	Provider   string
	Window     string
	ResetAt    time.Time
	Confirmed  bool
	OtherLimit bool
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
	if m.ResetAt.IsZero() {
		return errors.New("email requires a reset time")
	}
	if err := validateLabel(m.Provider); err != nil {
		return errors.New("email provider label is empty, too long, or contains control characters")
	}
	if err := validateLabel(m.Window); err != nil {
		return errors.New("email window label is empty, too long, or contains control characters")
	}
	return ValidateAddresses(m.To, m.From)
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
	status := "EXPECTED"
	explanation := "The recorded reset time has arrived. Availability has not been confirmed by fresh account data."
	if m.Confirmed {
		status = "CONFIRMED"
		explanation = "Fresh account data shows allowance available in all reported windows."
	}
	subject = fmt.Sprintf("ResetWatch: %s %s reset %s", m.Provider, m.Window, strings.ToLower(status))
	text = fmt.Sprintf("%s RESET\n\n%s / %s\nReset time: %s\n\n%s", status, m.Provider, m.Window,
		m.ResetAt.UTC().Format("2006-01-02 15:04:05 UTC"), explanation)
	if m.OtherLimit {
		text += "\n\nA reported quota window is still exhausted. This reminder does not confirm access."
	}
	text += "\n\nSent by ResetWatch. Check your provider before starting work."
	paragraphs := strings.ReplaceAll(html.EscapeString(text), "\n\n", "</p><p>")
	markup = "<!doctype html><html><body><main style=\"font-family:Arial,sans-serif;line-height:1.6;max-width:600px;margin:24px auto;padding:24px\"><p>" + paragraphs + "</p></main></body></html>"
	return subject, text, markup
}
