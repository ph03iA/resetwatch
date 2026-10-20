package gmailauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Account is the connected Google account. Subject is Google's stable account ID; the address can change.
type Account struct {
	Address string `json:"address"`
	Subject string `json:"subject"`
}

// Grant is a completed sign-in: the account and its long-lived refresh token.
type Grant struct {
	Account
	RefreshToken string
}

// ErrDenied means the person cancelled sign-in or declined the requested permission.
var ErrDenied = errors.New("Google sign-in was cancelled or permission was not granted")

// Flow runs one browser sign-in through a temporary callback on 127.0.0.1.
type Flow struct {
	Client  Client
	Open    func(url string) error // opens the system browser; nil only prints the link
	Out     io.Writer              // receives the sign-in link
	Timeout time.Duration          // five minutes when zero
}

type callback struct {
	code string
	err  error
}

// Authorize signs in with PKCE (S256) and a random state, then checks the granted scope and verified address.
func (f Flow) Authorize(ctx context.Context) (Grant, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Grant{}, fmt.Errorf("start the local sign-in callback: %w", err)
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
	config := f.Client.oauth(redirect)
	state, verifier := rand.Text(), oauth2.GenerateVerifier()
	results := make(chan callback, 1)
	server := &http.Server{Handler: callbackHandler(state, results), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	link := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "consent select_account"))
	f.showLink(link)
	code, err := f.wait(ctx, results)
	if err != nil {
		return Grant{}, err
	}
	return f.Client.finish(ctx, config, code, verifier)
}

func (f Flow) showLink(link string) {
	if f.Out != nil {
		fmt.Fprintln(f.Out, "Sign in to Google on this computer to connect Gmail:\n"+link)
	}
	if f.Open != nil && f.Open(link) != nil && f.Out != nil {
		fmt.Fprintln(f.Out, "Could not open a browser; open the link above on this computer.")
	}
}

func (f Flow) wait(ctx context.Context, results <-chan callback) (string, error) {
	timeout := f.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", fmt.Errorf("Google sign-in timed out after %s; run the command again", timeout)
	case result := <-results:
		return result.code, result.err
	}
}

// callbackHandler accepts one redirect for this request. A wrong state ends the attempt instead of being ignored.
func callbackHandler(state string, results chan<- callback) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		result := readCallback(r.URL.Query(), state)
		select {
		case results <- result:
		default:
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if result.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "ResetWatch could not connect Gmail. Return to the terminal for details.")
			return
		}
		fmt.Fprintln(w, "Gmail is connected to ResetWatch. You can close this tab.")
	})
	return mux
}

func readCallback(query map[string][]string, state string) callback {
	get := func(key string) string {
		if values := query[key]; len(values) > 0 {
			return values[0]
		}
		return ""
	}
	if subtle.ConstantTimeCompare([]byte(get("state")), []byte(state)) != 1 {
		return callback{err: errors.New("the sign-in response did not match this request; run the command again")}
	}
	if get("error") != "" {
		return callback{err: ErrDenied}
	}
	if get("code") == "" {
		return callback{err: errors.New("Google returned no authorization code")}
	}
	return callback{code: get("code")}
}

func (c Client) finish(ctx context.Context, config *oauth2.Config, code, verifier string) (Grant, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.httpClient())
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Grant{}, fmt.Errorf("finish Google sign-in: %w", err)
	}
	if !grantedSend(token) {
		return Grant{}, fmt.Errorf("%w: allow ResetWatch to send email on your behalf", ErrDenied)
	}
	if token.RefreshToken == "" {
		return Grant{}, errors.New("Google returned no refresh token; remove ResetWatch from your Google account's third-party access and connect again")
	}
	account, err := c.identity(ctx, token)
	if err != nil {
		return Grant{}, err
	}
	return Grant{Account: account, RefreshToken: token.RefreshToken}, nil
}

func grantedSend(token *oauth2.Token) bool {
	scope, _ := token.Extra("scope").(string)
	return slices.Contains(strings.Fields(scope), sendScope)
}

// identity reads the verified address and stable ID from Google's authenticated userinfo endpoint.
func (c Client) identity(ctx context.Context, token *oauth2.Token) (Account, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.UserinfoURL, nil)
	if err != nil {
		return Account{}, err
	}
	token.SetAuthHeader(request)
	response, err := c.httpClient().Do(request)
	if err != nil {
		return Account{}, fmt.Errorf("read the Google account address: %w", err)
	}
	defer response.Body.Close()
	var info struct {
		Subject  string `json:"sub"`
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil || response.StatusCode != http.StatusOK || json.Unmarshal(body, &info) != nil || info.Subject == "" || info.Email == "" {
		return Account{}, errors.New("Google did not return the account address; run the command again")
	}
	if !info.Verified {
		return Account{}, errors.New("this Google account's email address is not verified")
	}
	return Account{Address: info.Email, Subject: info.Subject}, nil
}
