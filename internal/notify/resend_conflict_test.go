package notify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func conflictTestService(status int, body io.Reader, retryAfter string) Resend {
	return Resend{APIKey: "re_fake_test_key", Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{retryAfter}}, Body: io.NopCloser(body)}, nil
	})}}
}

func TestResendConflictRetryClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		retry  bool
	}{
		{"concurrent request", 409, `{"name":"concurrent_idempotent_requests","message":"re_SECRET person@example.com"}`, true},
		{"different payload", 409, `{"name":"invalid_idempotent_request","message":"re_SECRET person@example.com"}`, false},
		{"unknown error", 409, `{"name":"other_error","message":"re_SECRET person@example.com"}`, false},
		{"wrong case", 409, `{"name":"Concurrent_Idempotent_Requests"}`, false},
		{"missing name", 409, `{"message":"re_SECRET person@example.com"}`, false},
		{"wrong type", 409, `{"name":42,"message":"re_SECRET person@example.com"}`, false},
		{"empty response", 409, "", false},
		{"malformed response", 409, `{"name":"concurrent_idempotent_requests"} re_SECRET person@example.com`, false},
		{"oversized response", 409, `{"name":"concurrent_idempotent_requests","message":"` + strings.Repeat("re_SECRET person@example.com", maxResponse) + `"}`, false},
		{"other client error", 422, `{"name":"concurrent_idempotent_requests"}`, false},
		{"server error unchanged", 500, `{"name":"invalid_idempotent_request"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := conflictTestService(tc.status, strings.NewReader(tc.body), "")
			err := service.Send(context.Background(), sampleMessage())
			requireError(t, err, tc.retry)
			if strings.Contains(err.Error(), "re_SECRET") || strings.Contains(err.Error(), "person@example.com") {
				t.Fatalf("provider body leaked: %v", err)
			}
		})
	}
}

func TestResendConcurrentConflictRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"90", 90 * time.Second},
		{"90000", 24 * time.Hour},
		{"invalid", 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			service := conflictTestService(409, strings.NewReader(`{"name":"concurrent_idempotent_requests"}`), tc.value)
			err := service.Send(context.Background(), sampleMessage())
			requireError(t, err, true)
			if got := RetryAfter(err); got != tc.want {
				t.Fatalf("RetryAfter = %v, want %v", got, tc.want)
			}
		})
	}
}

type conflictTestReader struct {
	read int
	err  error
}

func (r *conflictTestReader) Read(p []byte) (int, error) {
	r.read += len(p)
	if r.err != nil {
		return 0, r.err
	}
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestResendConflictBodyReadBound(t *testing.T) {
	body := &conflictTestReader{}
	service := conflictTestService(409, body, "")
	requireError(t, service.Send(context.Background(), sampleMessage()), false)
	if body.read != maxResponse+1 {
		t.Fatalf("read %d response bytes, want %d", body.read, maxResponse+1)
	}
}

func TestResendConflictReadErrorIsRedacted(t *testing.T) {
	body := &conflictTestReader{err: errors.New("re_SECRET person@example.com")}
	service := conflictTestService(409, body, "")
	err := service.Send(context.Background(), sampleMessage())
	requireError(t, err, false)
	if strings.Contains(err.Error(), "re_SECRET") || strings.Contains(err.Error(), "person@example.com") {
		t.Fatalf("response read error leaked: %v", err)
	}
}

func TestResendNonConflictResponseBodyIsNotRead(t *testing.T) {
	for _, status := range []int{401, 422, 429, 500} {
		body := &conflictTestReader{err: errors.New("must not read non-conflict body")}
		service := conflictTestService(status, body, "")
		requireError(t, service.Send(context.Background(), sampleMessage()), status == 429 || status >= 500)
		if body.read != 0 {
			t.Fatalf("read %d bytes from HTTP %d response", body.read, status)
		}
	}
}
