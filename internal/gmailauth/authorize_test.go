package gmailauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeGoogle checks PKCE for real: the token request must carry the verifier matching the challenge.
type fakeGoogle struct {
	scope, refresh string
	verified       bool
	tokenStatus    int
	challenge      string
}

func (g *fakeGoogle) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if g.tokenStatus != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(g.tokenStatus)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		if r.Form.Get("grant_type") == "authorization_code" {
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-1", "token_type": "Bearer", "expires_in": 3600,
			"refresh_token": g.refresh, "scope": g.scope})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-1" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": "google-123", "email": "person@example.test", "email_verified": g.verified})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func okGoogle() *fakeGoogle {
	return &fakeGoogle{scope: "openid " + sendScope + " email", refresh: "refresh-1", verified: true}
}

func testClient(server *httptest.Server) Client {
	return Client{ID: "client", Secret: "secret", AuthURL: server.URL + "/auth", TokenURL: server.URL + "/token", UserinfoURL: server.URL + "/userinfo"}
}

// browser plays the person: it reads the sign-in link and follows Google's redirect with the given answer.
func browser(t *testing.T, g *fakeGoogle, answer func(url.Values)) func(string) error {
	return func(link string) error {
		parsed, err := url.Parse(link)
		if err != nil {
			return err
		}
		q := parsed.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || !strings.Contains(q.Get("scope"), sendScope) {
			t.Errorf("sign-in link lacks PKCE, offline access or the send scope: %s", link)
		}
		g.challenge = q.Get("code_challenge")
		reply := url.Values{"state": {q.Get("state")}, "code": {"good-code"}}
		answer(reply)
		go func() {
			response, err := http.Get(q.Get("redirect_uri") + "?" + reply.Encode())
			if err == nil {
				response.Body.Close()
			}
		}()
		return nil
	}
}

func TestAuthorizeSucceedsWithPKCE(t *testing.T) {
	g := okGoogle()
	flow := Flow{Client: testClient(g.server(t)), Open: browser(t, g, func(url.Values) {})}
	grant, err := flow.Authorize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if grant.Address != "person@example.test" || grant.Subject != "google-123" || grant.RefreshToken != "refresh-1" {
		t.Fatalf("unexpected grant: %+v", grant)
	}
}

func TestAuthorizeRejectsBadOutcomes(t *testing.T) {
	cases := map[string]struct {
		google *fakeGoogle
		answer func(url.Values)
		want   string
	}{
		"denied consent":   {okGoogle(), func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") }, "cancelled"},
		"wrong state":      {okGoogle(), func(q url.Values) { q.Set("state", "forged") }, "did not match"},
		"missing scope":    {&fakeGoogle{scope: "openid email", refresh: "r", verified: true}, func(url.Values) {}, "send email"},
		"no refresh token": {&fakeGoogle{scope: sendScope, verified: true}, func(url.Values) {}, "no refresh token"},
		"unverified email": {&fakeGoogle{scope: sendScope, refresh: "r"}, func(url.Values) {}, "not verified"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			flow := Flow{Client: testClient(test.google.server(t)), Open: browser(t, test.google, test.answer)}
			_, err := flow.Authorize(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestAuthorizeTimesOutAndCancels(t *testing.T) {
	g := okGoogle()
	client := testClient(g.server(t))
	flow := Flow{Client: client, Timeout: 50 * time.Millisecond, Out: &strings.Builder{}}
	if _, err := flow.Authorize(context.Background()); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Flow{Client: client}).Authorize(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
}

func TestNoBrowserPrintsTheLink(t *testing.T) {
	out := &strings.Builder{}
	flow := Flow{Client: testClient(okGoogle().server(t)), Timeout: 20 * time.Millisecond, Out: out}
	_, _ = flow.Authorize(context.Background())
	if !strings.Contains(out.String(), "/auth?") || !strings.Contains(out.String(), "code_challenge=") {
		t.Fatalf("the sign-in link was not printed: %q", out.String())
	}
}

func TestTokensRenewAndRevocationIsRecognised(t *testing.T) {
	g := okGoogle()
	client := testClient(g.server(t))
	token, err := client.Tokens("refresh-1").Token()
	if err != nil || token.AccessToken != "access-1" {
		t.Fatalf("renewal failed: %v %+v", err, token)
	}
	g.tokenStatus = http.StatusBadRequest
	if _, err := client.Tokens("refresh-1").Token(); !Revoked(err) {
		t.Fatalf("invalid_grant must count as revoked, got %v", err)
	}
	if Revoked(errors.New("network down")) {
		t.Fatal("a network error is not a revocation")
	}
}

func TestEmbeddedClientIsEmptyUntilTheOwnerAddsOne(t *testing.T) {
	if _, err := DefaultClient(); !errors.Is(err, ErrNoClient) {
		t.Fatalf("an empty embedded client must give ErrNoClient, got %v", err)
	}
}
