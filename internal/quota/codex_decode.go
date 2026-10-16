package quota

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

type codexAccountResponse struct {
	Account *struct {
		Type  string  `json:"type"`
		Email *string `json:"email"`
	} `json:"account"`
}

type codexLimitsResponse struct {
	RateLimits           *codexLimit            `json:"rateLimits"`
	RateLimitsByLimitID  map[string]*codexLimit `json:"rateLimitsByLimitId"`
	OrdinaryUsageAllowed *bool                  `json:"ordinaryUsageAllowed"`
}

type codexLimit struct {
	LimitID   string       `json:"limitId"`
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
}

type codexWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *int64   `json:"windowDurationMins"`
	ResetsAt           *int64   `json:"resetsAt"`
}

func accountIdentity(response codexAccountResponse) (string, error) {
	if response.Account == nil || response.Account.Type != "chatgpt" {
		return "", errors.New("Codex needs a signed-in ChatGPT subscription account; run codex login")
	}
	if response.Account.Email == nil || strings.TrimSpace(*response.Account.Email) == "" {
		return "", errors.New("Codex did not provide a stable account identity")
	}
	email := strings.ToLower(strings.TrimSpace(*response.Account.Email))
	digest := sha256.Sum256([]byte("codex:chatgpt:" + email))
	return hex.EncodeToString(digest[:]), nil
}

func decodeSnapshot(identity string, observedAt time.Time, response codexLimitsResponse) (Snapshot, error) {
	limits := response.RateLimitsByLimitID
	if limits == nil && response.RateLimits != nil {
		id := response.RateLimits.LimitID
		if id == "" {
			id = "legacy"
		}
		limits = map[string]*codexLimit{id: response.RateLimits}
	}
	windows, err := decodeLimits(limits)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Provider: "codex", AccountID: identity, ObservedAt: observedAt, Windows: windows,
		OrdinaryUsageAllowed: response.OrdinaryUsageAllowed}
	if err := ValidateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func decodeLimits(limits map[string]*codexLimit) ([]Window, error) {
	ids := make([]string, 0, len(limits))
	for id := range limits {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var windows []Window
	for _, id := range ids {
		decoded, err := decodeLimit(id, limits[id])
		if err != nil {
			return nil, err
		}
		windows = append(windows, decoded...)
	}
	return windows, nil
}

func decodeLimit(id string, limit *codexLimit) ([]Window, error) {
	if limit == nil {
		return nil, nil
	}
	if strings.TrimSpace(id) == "" || (limit.LimitID != "" && limit.LimitID != id) {
		return nil, errors.New("Codex returned an invalid quota bucket identifier")
	}
	var windows []Window
	for _, entry := range []struct {
		name string
		data *codexWindow
	}{{"primary", limit.Primary}, {"secondary", limit.Secondary}} {
		if entry.data == nil {
			continue
		}
		window, err := decodeWindow(id+":"+entry.name, entry.data)
		if err != nil {
			return nil, err
		}
		windows = append(windows, window)
	}
	return windows, nil
}

func decodeWindow(id string, data *codexWindow) (Window, error) {
	if data.UsedPercent == nil || data.WindowDurationMins == nil || data.ResetsAt == nil {
		return Window{}, errors.New("Codex quota data is incomplete; usage is unknown")
	}
	minutes := *data.WindowDurationMins
	if minutes <= 0 || minutes > math.MaxInt64/int64(time.Minute) {
		return Window{}, errors.New("Codex returned an invalid quota window duration")
	}
	return Window{
		ID: id, UsedPercent: *data.UsedPercent,
		ResetsAt: time.Unix(*data.ResetsAt, 0).UTC(), Duration: time.Duration(minutes) * time.Minute,
	}, nil
}
