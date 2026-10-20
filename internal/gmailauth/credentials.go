package gmailauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// Store keeps refresh tokens. Access tokens are never stored; they live in memory and are renewed.
type Store interface {
	Save(reference, refreshToken string) error
	Load(reference string) (string, error)
	Remove(reference string) error
}

// ErrNoCredential means the configured credential is gone from the system credential store.
var ErrNoCredential = errors.New("the saved Gmail credential is missing from the system credential store; run resetwatch gmail connect")

const keyringService = "ResetWatch Gmail"

// Keyring is the system credential store: Windows Credential Manager, macOS Keychain or Linux Secret Service.
// There is deliberately no plaintext fallback.
type Keyring struct{ Namespace string }

// NewKeyring namespaces credentials by state directory, so separate state directories never share one.
func NewKeyring(stateDir string) Keyring {
	sum := sha256.Sum256([]byte(stateDir))
	return Keyring{Namespace: hex.EncodeToString(sum[:8])}
}

func (k Keyring) user(reference string) string { return k.Namespace + "/" + reference }

func (k Keyring) Save(reference, refreshToken string) error {
	if err := keyring.Set(keyringService, k.user(reference), refreshToken); err != nil {
		return fmt.Errorf("the system credential store is unavailable, so the Gmail connection cannot be saved: %w", err)
	}
	return nil
}

func (k Keyring) Load(reference string) (string, error) {
	secret, err := keyring.Get(keyringService, k.user(reference))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoCredential
	}
	if err != nil {
		return "", fmt.Errorf("the system credential store is unavailable: %w", err)
	}
	return secret, nil
}

func (k Keyring) Remove(reference string) error {
	err := keyring.Delete(keyringService, k.user(reference))
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("remove the Gmail credential from the system credential store: %w", err)
}

// NewReference names the credential of one successful authorization; reconnecting never overwrites the old one in place.
func NewReference() string { return rand.Text() }

// Tokens renews access tokens from a refresh token and caches each until it expires.
func (c Client) Tokens(refreshToken string) oauth2.TokenSource {
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, c.httpClient())
	return c.oauth("").TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
}

// Revoked reports whether a token error means the person must connect Gmail again.
func Revoked(err error) bool {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		return false
	}
	switch retrieve.ErrorCode {
	case "invalid_grant", "invalid_client", "unauthorized_client", "invalid_scope":
		return true
	}
	return false
}

// OpenBrowser opens a link in the system browser without a shell.
func OpenBrowser(link string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", link).Start()
	case "darwin":
		return exec.Command("open", link).Start()
	}
	return exec.Command("xdg-open", link).Start()
}
