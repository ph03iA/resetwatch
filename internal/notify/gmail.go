package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const gmailEndpoint = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"

// Gmail sends from the connected account to that same account through users.messages.send.
type Gmail struct {
	Address    string                                // the connected account; every reminder goes from and to it
	Credential string                                // reference of the saved sign-in, reported with authorization failures
	Token      func(context.Context) (string, error) // a current access token; renewed from the stored refresh token
	Client     *http.Client
	// Endpoint is only for local transport tests; application configuration must not override it.
	Endpoint string
}

// AuthorizationError means the saved Gmail sign-in no longer works. Delivery pauses until the person
// reconnects; it is never retried on its own, so it cannot drain the queue's attempt budgets.
type AuthorizationError struct {
	Credential string
	Reason     string
}

func (e *AuthorizationError) Error() string { return e.Reason + "; run resetwatch gmail connect" }

// AuthorizationFailure returns the authorization error inside err, if any.
func AuthorizationFailure(err error) (*AuthorizationError, bool) {
	var failure *AuthorizationError
	ok := errors.As(err, &failure)
	return failure, ok
}

func (g Gmail) Send(ctx context.Context, m Message) error {
	if !strings.EqualFold(m.To, g.Address) || !strings.EqualFold(m.From, g.Address) {
		return errors.New("reminders go only from the connected Gmail account to itself")
	}
	if m.MIME == "" {
		var err error
		if m, err = Prepare(m, time.Now()); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	token, err := g.Token(ctx)
	if err != nil {
		if _, ok := AuthorizationFailure(err); ok {
			return err
		}
		return &deliveryError{message: "could not renew the Gmail sign-in; Google may be unreachable", retryable: true}
	}
	request, err := g.request(ctx, m.MIME, token)
	if err != nil {
		return err
	}
	response, err := g.client().Do(request)
	if err != nil {
		return transportError(ctx)
	}
	defer response.Body.Close()
	return g.checkResponse(response)
}

func (g Gmail) request(ctx context.Context, raw, token string) (*http.Request, error) {
	body, err := json.Marshal(map[string]string{"raw": base64.URLEncoding.EncodeToString([]byte(raw))})
	if err != nil {
		return nil, errors.New("could not prepare email")
	}
	endpoint := g.Endpoint
	if endpoint == "" {
		endpoint = gmailEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid email service endpoint")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "resetwatch")
	return request, nil
}

func (g Gmail) client() *http.Client {
	client := http.Client{Timeout: sendTimeout}
	if g.Client != nil {
		client = *g.Client
	}
	// A redirect must never forward the access token to a different endpoint.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

func (g Gmail) checkResponse(response *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse {
		return &deliveryError{message: "could not read Gmail's response", retryable: true}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		var receipt struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(body, &receipt) != nil || !validEmailID(receipt.ID) {
			// Gmail may have sent it; retrying risks a duplicate, which is the accepted trade-off.
			return &deliveryError{message: "Gmail returned an unreadable receipt", retryable: true}
		}
		return nil
	}
	return g.statusError(response, body)
}

func (g Gmail) statusError(response *http.Response, body []byte) error {
	code := response.StatusCode
	reasons := gmailReasons(body)
	if code == http.StatusUnauthorized || (code == http.StatusForbidden && permissionMissing(reasons)) {
		return &AuthorizationError{Credential: g.Credential, Reason: "Gmail no longer accepts the saved sign-in"}
	}
	limited := code == http.StatusForbidden && slices.ContainsFunc(reasons, func(r string) bool { return strings.Contains(r, "RateLimit") || strings.Contains(r, "LimitExceeded") })
	retryable := limited || code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
	return &deliveryError{
		message:    fmt.Sprintf("Gmail returned HTTP %d", code),
		retryable:  retryable,
		retryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
	}
}

func permissionMissing(reasons []string) bool {
	return slices.ContainsFunc(reasons, func(r string) bool {
		return r == "insufficientPermissions" || r == "ACCESS_TOKEN_SCOPE_INSUFFICIENT" || r == "authError"
	})
}

// gmailReasons lists the reason codes of a Google API error body.
func gmailReasons(body []byte) []string {
	var result struct {
		Error struct {
			Status  string                    `json:"status"`
			Errors  []struct{ Reason string } `json:"errors"`
			Details []struct{ Reason string } `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil
	}
	reasons := []string{result.Error.Status}
	for _, e := range result.Error.Errors {
		reasons = append(reasons, e.Reason)
	}
	for _, d := range result.Error.Details {
		reasons = append(reasons, d.Reason)
	}
	return reasons
}
