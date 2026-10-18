package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"resetwatch/internal/notify"
)

func TestEmailSerializesSenderForResend(t *testing.T) {
	cases := []struct{ name, input, want string }{
		{"bare", "onboarding@resend.dev", "onboarding@resend.dev"},
		{"angle only", "<onboarding@resend.dev>", "onboarding@resend.dev"},
		{"named", "ResetWatch <onboarding@resend.dev>", `"ResetWatch" <onboarding@resend.dev>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := testEmailApplication(t)
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct{ From string }
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload.From != tc.want {
					t.Errorf("sender wire = %q, want %q", payload.From, tc.want)
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}
				_, _ = io.WriteString(w, `{"id":"fixture-receipt"}`)
			}))
			defer service.Close()
			app.testSender = notify.Resend{APIKey: "re_fake_test_key", Client: service.Client(), Endpoint: service.URL}
			err := app.run(context.Background(), []string{"test-email", "--email", "recipient@example.com", "--from", tc.input})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
