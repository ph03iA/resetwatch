// Package gmailauth connects a Google account with send-only Gmail access: browser sign-in with
// PKCE, the verified address, and the refresh token kept in the system credential store.
package gmailauth

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// Scopes are send-only Gmail plus the verified address. ResetWatch never asks to read the inbox.
var Scopes = []string{"https://www.googleapis.com/auth/gmail.send", "openid", "email"}

const sendScope = "https://www.googleapis.com/auth/gmail.send"

// google-client.json is the project owner's Desktop OAuth client, downloaded from Google Cloud
// in Google's "installed" format. Google treats desktop client secrets as public.
//
//go:embed google-client.json
var clientJSON []byte

// ErrNoClient means this build carries no Google sign-in client.
var ErrNoClient = errors.New("this ResetWatch build has no Google sign-in client; the project owner must add internal/gmailauth/google-client.json (see docs/details.md)")

// Client is a Desktop OAuth client and the Google endpoints it talks to.
type Client struct {
	ID, Secret                     string
	AuthURL, TokenURL, UserinfoURL string
	HTTP                           *http.Client // tests point this at local servers
}

// DefaultClient returns the embedded client with Google's endpoints.
func DefaultClient() (Client, error) {
	var file struct {
		Installed struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(clientJSON, &file); err != nil || file.Installed.ClientID == "" {
		return Client{}, ErrNoClient
	}
	return Client{ID: file.Installed.ClientID, Secret: file.Installed.ClientSecret,
		AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
		UserinfoURL: "https://openidconnect.googleapis.com/v1/userinfo"}, nil
}

func (c Client) oauth(redirect string) *oauth2.Config {
	return &oauth2.Config{ClientID: c.ID, ClientSecret: c.Secret, RedirectURL: redirect, Scopes: Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: c.AuthURL, TokenURL: c.TokenURL, AuthStyle: oauth2.AuthStyleInParams}}
}

// httpClient never follows redirects, so a token is never forwarded to another host.
func (c Client) httpClient() *http.Client {
	client := http.Client{Timeout: 20 * time.Second}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
