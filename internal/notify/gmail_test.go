package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const gmailAccount = "person@example.test"

func gmailMessage() Message {
	return Message{Key: strings.Repeat("a", 64), To: gmailAccount, From: gmailAccount, Provider: "claude", Window: "claude:five_hour",
		ResetAt: time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC), Duration: 5 * time.Hour, LocalResetAt: "2030-01-02 3:30 PM IST", Confirmed: true}
}

func staticToken(context.Context) (string, error) { return "access-token", nil }

// gmailServer records the raw message it receives and answers with status and body.
func gmailServer(t *testing.T, status int, body string, header http.Header, raw *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-token" || r.Method != http.MethodPost {
			t.Errorf("bad request: %s %s", r.Method, r.Header.Get("Authorization"))
		}
		var payload struct{ Raw string }
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if raw != nil {
			decoded, _ := base64.URLEncoding.DecodeString(payload.Raw)
			*raw = string(decoded)
		}
		for key, values := range header {
			w.Header()[key] = values
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func gmailSender(server *httptest.Server) Gmail {
	return Gmail{Address: gmailAccount, Credential: "ref-1", Token: staticToken, Endpoint: server.URL}
}

// parts reads a MIME message the way a mail client would and returns its headers and decoded parts.
func parts(t *testing.T, raw string) (mail.Header, map[string]string) {
	t.Helper()
	message, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("not a valid message: %v", err)
	}
	_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	reader := multipart.NewReader(message.Body, params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(quotedprintable.NewReader(part))
		found[strings.Split(part.Header.Get("Content-Type"), ";")[0]] = string(data)
	}
	return message.Header, found
}

func TestPrepareRendersValidStableMIME(t *testing.T) {
	first, err := Prepare(gmailMessage(), time.Date(2030, 1, 2, 10, 0, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Prepare(gmailMessage(), time.Date(2030, 1, 2, 11, 0, 0, 0, time.UTC))
	if first.MessageID == "" || first.MessageID != again.MessageID {
		t.Fatalf("Message-ID must be stable for one reminder: %q %q", first.MessageID, again.MessageID)
	}
	header, bodies := parts(t, first.MIME)
	subject, _ := new(mime.WordDecoder).DecodeHeader(header.Get("Subject"))
	if subject != "Your Claude 5-hour limit is back. Get back to work" || header.Get("Message-ID") != first.MessageID {
		t.Fatalf("headers lost the template subject or Message-ID: %v", header)
	}
	if !strings.Contains(header.Get("From"), gmailAccount) || header.Get("To") != "<"+gmailAccount+">" {
		t.Fatalf("sender or recipient is not the account: %v", header)
	}
	if !strings.Contains(bodies["text/plain"], "Your Claude 5-hour limit is back.") || !strings.Contains(bodies["text/html"], "<!doctype html>") {
		t.Fatalf("templates missing from parts: %v", bodies)
	}
}

func TestGmailSendsFrozenMIMEAndReportsAcceptance(t *testing.T) {
	var raw string
	server := gmailServer(t, http.StatusOK, `{"id":"gmail-1"}`, nil, &raw)
	frozen, _ := Prepare(gmailMessage(), time.Now())
	if err := gmailSender(server).Send(context.Background(), frozen); err != nil {
		t.Fatal(err)
	}
	if raw != frozen.MIME {
		t.Fatal("Gmail must receive the frozen MIME exactly")
	}
}

func TestGmailOnlySendsFromAndToTheAccount(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	m := gmailMessage()
	m.To = "someone-else@example.test"
	if err := gmailSender(server).Send(context.Background(), m); err == nil || hits.Load() != 0 {
		t.Fatalf("a different recipient must be refused before any request: %v", err)
	}
}

func TestGmailClassifiesResponses(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		auth      bool
		retryable bool
	}{
		{"revoked sign-in", http.StatusUnauthorized, `{"error":{"status":"UNAUTHENTICATED"}}`, true, false},
		{"missing permission", http.StatusForbidden, `{"error":{"errors":[{"reason":"insufficientPermissions"}]}}`, true, false},
		{"rate limit", http.StatusForbidden, `{"error":{"errors":[{"reason":"userRateLimitExceeded"}]}}`, false, true},
		{"too many requests", http.StatusTooManyRequests, `{}`, false, true},
		{"server error", http.StatusBadGateway, ``, false, true},
		{"rejected message", http.StatusBadRequest, `{"error":{"status":"INVALID_ARGUMENT"}}`, false, false},
		{"ambiguous receipt", http.StatusOK, `not json`, false, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := gmailSender(gmailServer(t, test.status, test.body, nil, nil)).Send(context.Background(), gmailMessage())
			failure, auth := AuthorizationFailure(err)
			if err == nil || auth != test.auth || Retryable(err) != test.retryable {
				t.Fatalf("err %v: authorization %v retryable %v", err, auth, Retryable(err))
			}
			if auth && failure.Credential != "ref-1" {
				t.Fatalf("authorization failure must name the credential that failed, got %q", failure.Credential)
			}
		})
	}
}

func TestGmailHonoursRetryAfter(t *testing.T) {
	err := gmailSender(gmailServer(t, http.StatusTooManyRequests, `{}`, http.Header{"Retry-After": {"120"}}, nil)).Send(context.Background(), gmailMessage())
	if !Retryable(err) || RetryAfter(err) != 2*time.Minute {
		t.Fatalf("want a retry after 2m, got %v (%s)", err, RetryAfter(err))
	}
}

func TestGmailTokenFailures(t *testing.T) {
	server := gmailServer(t, http.StatusOK, `{"id":"x"}`, nil, nil)
	revoked := gmailSender(server)
	revoked.Token = func(context.Context) (string, error) {
		return "", &AuthorizationError{Credential: "ref-1", Reason: "the Gmail sign-in was withdrawn"}
	}
	if _, ok := AuthorizationFailure(revoked.Send(context.Background(), gmailMessage())); !ok {
		t.Fatal("a revoked token must stay an authorization failure")
	}
	offline := gmailSender(server)
	offline.Token = func(context.Context) (string, error) { return "", errors.New("dial tcp: no route") }
	if err := offline.Send(context.Background(), gmailMessage()); !Retryable(err) {
		t.Fatalf("an unreachable token endpoint must be retried, got %v", err)
	}
}

func TestGmailDoesNotFollowRedirects(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if err := gmailSender(redirect).Send(context.Background(), gmailMessage()); err == nil || followed.Load() != 0 {
		t.Fatalf("a redirect must not carry the token elsewhere: err %v, followed %d", err, followed.Load())
	}
}

func TestGmailBoundsTheResponse(t *testing.T) {
	huge := `{"id":"` + strings.Repeat("x", maxResponse) + `"}`
	if err := gmailSender(gmailServer(t, http.StatusOK, huge, nil, nil)).Send(context.Background(), gmailMessage()); !Retryable(err) {
		t.Fatalf("an oversized response must be treated as unreadable, got %v", err)
	}
}
