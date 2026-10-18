package notify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	resendEndpoint = "https://api.resend.com/emails"
	sendTimeout    = 20 * time.Second
	maxResponse    = 64 * 1024
)

type Resend struct {
	APIKey string
	Client *http.Client
	// Endpoint is only for local transport tests; application configuration must not override it.
	Endpoint string
}

type emailPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html"`
}

func (r Resend) Send(ctx context.Context, m Message) error {
	if err := validateMessage(m); err != nil {
		return err
	}
	if err := validateAPIKey(r.APIKey); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	request, err := r.request(ctx, m)
	if err != nil {
		return err
	}
	response, err := r.client().Do(request)
	if err != nil {
		return transportError(ctx)
	}
	defer response.Body.Close()
	return checkResponse(response)
}

func validateAPIKey(key string) error {
	if strings.TrimSpace(key) == "" || len(key) > 4096 || strings.ContainsFunc(key, unicode.IsSpace) || strings.ContainsFunc(key, unicode.IsControl) {
		return errors.New("a valid RESEND_API_KEY is required")
	}
	return nil
}

func (r Resend) request(ctx context.Context, m Message) (*http.Request, error) {
	to, _ := mailbox(m.To, false)
	from, _ := mailbox(m.From, true)
	if m.CanonicalSender && strings.HasPrefix(from, "<") {
		from = strings.TrimSuffix(strings.TrimPrefix(from, "<"), ">")
	}
	subject, text, markup := messageContent(m)
	body, err := json.Marshal(emailPayload{From: from, To: []string{to}, Subject: subject, Text: text, HTML: markup})
	if err != nil {
		return nil, errors.New("could not prepare email")
	}
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = resendEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid email service endpoint")
	}
	request.Header.Set("Authorization", "Bearer "+r.APIKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey(m.Key))
	request.Header.Set("User-Agent", "resetwatch")
	return request, nil
}

func idempotencyKey(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "resetwatch-" + hex.EncodeToString(digest[:])
}

func (r Resend) client() *http.Client {
	client := http.Client{Timeout: sendTimeout}
	if r.Client != nil {
		client = *r.Client
	}
	// A redirect must never forward the API key to a different endpoint.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

func transportError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &deliveryError{message: "email delivery canceled"}
	}
	return &deliveryError{message: "email service unavailable or request timed out", retryable: true}
}

func checkResponse(response *http.Response) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusError(response)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return &deliveryError{message: "could not read email service response", retryable: true}
	}
	if len(body) > maxResponse {
		return &deliveryError{message: "email service response exceeded 64 KiB", retryable: true}
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &result) != nil || !validEmailID(result.ID) {
		return &deliveryError{message: "email service returned an invalid delivery receipt", retryable: true}
	}
	return nil
}

func validEmailID(id string) bool {
	return strings.TrimSpace(id) != "" && len(id) <= 256 && !strings.ContainsFunc(id, unicode.IsControl)
}

func statusError(response *http.Response) error {
	code := response.StatusCode
	retryable := code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
	if code == http.StatusConflict {
		retryable = concurrentIdempotentRequest(response.Body)
	}
	message := fmt.Sprintf("email service returned HTTP %d", code)
	if code == http.StatusUnprocessableEntity {
		message += ": email fields were rejected; check the sender and recipient settings before retrying"
	}
	return &deliveryError{
		message:    message,
		retryable:  retryable,
		retryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
	}
}

func concurrentIdempotentRequest(body io.Reader) bool {
	data, err := io.ReadAll(io.LimitReader(body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return false
	}
	var result struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(data, &result) == nil && result.Name == "concurrent_idempotent_requests"
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
