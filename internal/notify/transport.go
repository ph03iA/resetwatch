package notify

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	sendTimeout = 20 * time.Second
	maxResponse = 64 * 1024
)

func transportError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &deliveryError{message: "email delivery canceled"}
	}
	return &deliveryError{message: "email service unavailable or request timed out", retryable: true}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if len(value) > 128 {
		return 0
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err == nil {
		return retryDelay(seconds)
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	return retryDelay(int64(date.Sub(now) / time.Second))
}

func retryDelay(seconds int64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	if seconds > 86400 {
		return 24 * time.Hour
	}
	return time.Duration(seconds) * time.Second
}

func validEmailID(id string) bool {
	return strings.TrimSpace(id) != "" && len(id) <= 256 && !strings.ContainsFunc(id, unicode.IsControl)
}
