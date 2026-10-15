package quota

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
)

type Window struct {
	ID          string        `json:"id"`
	UsedPercent float64       `json:"used_percent"`
	ResetsAt    time.Time     `json:"resets_at"`
	Duration    time.Duration `json:"duration"`
}

type Snapshot struct {
	Provider   string    `json:"provider"`
	AccountID  string    `json:"account_id"`
	ObservedAt time.Time `json:"observed_at"`
	Windows    []Window  `json:"windows"`
}

type Provider interface {
	Read(context.Context) (Snapshot, error)
}

func ValidateSnapshot(snapshot Snapshot) error {
	if strings.TrimSpace(snapshot.Provider) == "" || strings.TrimSpace(snapshot.AccountID) == "" {
		return errors.New("quota snapshot is missing provider or account identity")
	}
	if !validTime(snapshot.ObservedAt) || len(snapshot.Windows) == 0 {
		return errors.New("quota snapshot has no observation time or usage windows")
	}
	seen := make(map[string]bool)
	for _, window := range snapshot.Windows {
		if err := validateWindow(window, snapshot.ObservedAt); err != nil {
			return err
		}
		if seen[window.ID] {
			return errors.New("quota snapshot has duplicate window identifiers")
		}
		seen[window.ID] = true
	}
	return nil
}

func validateWindow(window Window, observedAt time.Time) error {
	if strings.TrimSpace(window.ID) == "" || window.Duration <= 0 {
		return errors.New("quota window has no identifier or valid duration")
	}
	if !validPercent(window.UsedPercent) {
		return errors.New("quota window has an invalid usage percentage")
	}
	if !validTime(window.ResetsAt) || !window.ResetsAt.After(observedAt) {
		return errors.New("quota window has no valid next reset time")
	}
	return nil
}

func validPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

func validTime(value time.Time) bool {
	return value.Unix() > 0 && value.Year() <= 9999
}
