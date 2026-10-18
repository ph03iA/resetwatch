package notify

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestResendSenderWireFormat(t *testing.T) {
	cases := []struct{ name, input, canonical, legacy string }{
		{"bare", "onboarding@resend.dev", "onboarding@resend.dev", "<onboarding@resend.dev>"},
		{"angle only", "<onboarding@resend.dev>", "onboarding@resend.dev", "<onboarding@resend.dev>"},
		{"named", "ResetWatch <alerts@example.com>", `"ResetWatch" <alerts@example.com>`, `"ResetWatch" <alerts@example.com>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, canonical := range []bool{false, true} {
				m := sampleMessage()
				m.From, m.CanonicalSender = tc.input, canonical
				service := Resend{APIKey: "re_fake_test_key"}
				body, key := resendRequestData(t, service, m)
				var payload emailPayload
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatal(err)
				}
				want := tc.legacy
				if canonical {
					want = tc.canonical
				}
				if payload.From != want {
					t.Fatalf("canonical=%v: sender wire = %q, want %q", canonical, payload.From, want)
				}
				data, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				var restored Message
				if err := json.Unmarshal(data, &restored); err != nil {
					t.Fatal(err)
				}
				retryBody, retryKey := resendRequestData(t, service, restored)
				if key != retryKey || !bytes.Equal(body, retryBody) {
					t.Fatal("restoring a frozen message changed its delivery key or wire payload")
				}
			}
		})
	}
}

func TestRejectedEmailFieldsHaveSafeGuidance(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusUnprocessableEntity,
		Body: io.NopCloser(strings.NewReader(`{"message":"re_SECRET person@example.com"}`)), Header: http.Header{}}
	err := statusError(response)
	if Retryable(err) || !strings.Contains(err.Error(), "check the sender and recipient") {
		t.Fatalf("expected permanent rejection with actionable guidance: %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "person@example.com") {
		t.Fatalf("provider response leaked: %v", err)
	}
}
