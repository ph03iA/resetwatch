package watch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

type Engine struct {
	DB       *store.DB
	Provider quota.Provider
	Sender   notify.Sender
	Now      func() time.Time
	Log      func(string)
}

func (e *Engine) clock() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) log(message string) {
	if e.Log != nil {
		e.Log(message)
	}
}

func (e *Engine) Step(ctx context.Context) error {
	config, err := e.DB.Config(ctx)
	if err != nil {
		return err
	}
	snapshot, observationErr := e.Observe(ctx, config)
	if observationErr != nil {
		e.log("Quota refresh unavailable; due emails will describe expected resets only.")
	}
	deliveryErr := e.deliverDue(ctx, snapshot)
	return errors.Join(observationErr, deliveryErr)
}

func (e *Engine) Observe(ctx context.Context, config store.Config) (*quota.Snapshot, error) {
	snapshot, err := e.Provider.Read(ctx)
	if err != nil {
		return nil, err
	}
	if err := e.DB.SaveSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	config, err = e.DB.Config(ctx)
	if err != nil {
		return nil, err
	}
	if err := e.DB.Schedule(ctx, snapshot, config, e.clock()); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (e *Engine) deliverDue(ctx context.Context, snapshot *quota.Snapshot) error {
	for range 50 {
		alert, err := e.DB.Claim(ctx, e.clock())
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := e.deliver(ctx, alert, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) deliver(ctx context.Context, alert store.Alert, snapshot *quota.Snapshot) error {
	config, err := e.DB.Config(ctx)
	if err != nil {
		return err
	}
	if reason := e.cancelReason(ctx, alert, config); reason != "" {
		return e.DB.Finish(ctx, alert, "cancelled", reason, e.clock(), e.clock())
	}
	return e.sendAlert(ctx, alert, snapshot)
}

func (e *Engine) sendAlert(ctx context.Context, alert store.Alert, snapshot *quota.Snapshot) error {
	message, err := e.DB.FreezeMessage(ctx, alert, compose(alert, snapshot, e.clock()))
	if err != nil {
		return err
	}
	err = e.Sender.Send(ctx, message)
	if err != nil {
		return e.recordFailure(ctx, alert, err)
	}
	if err := e.DB.Finish(ctx, alert, "sent", "", e.clock(), e.clock()); err != nil {
		return err
	}
	e.log("Reset email accepted by email provider: " + alert.ID[:12])
	return nil
}

func (e *Engine) cancelReason(ctx context.Context, alert store.Alert, config store.Config) string {
	if alert.To != config.To || alert.From != config.From {
		return "notification settings changed"
	}
	if e.clock().Sub(alert.ResetAt) >= 24*time.Hour {
		return "reset is more than 24 hours old"
	}
	if e.clock().Sub(alert.FirstAttempt) >= 23*time.Hour {
		return "delivery retry window expired"
	}
	snapshot, err := e.DB.Snapshot(ctx, alert.Provider)
	if err != nil {
		return "account identity could not be verified from local state"
	}
	if snapshot.AccountID != alert.AccountID {
		return "connected account changed"
	}
	return ""
}

func compose(alert store.Alert, snapshot *quota.Snapshot, now time.Time) notify.Message {
	message := notify.Message{Key: alert.ID, To: alert.To, From: alert.From,
		Provider: alert.Provider, Window: alert.Window, ResetAt: alert.ResetAt, Duration: windowDuration(snapshot, alert)}
	if !freshForAlert(snapshot, alert, now) {
		return message
	}
	for _, window := range snapshot.Windows {
		if window.UsedPercent >= 100 {
			message.OtherLimit = true
		}
		if window.ID == alert.Window && window.UsedPercent < 100 {
			message.Confirmed = true
		}
	}
	if message.OtherLimit {
		message.Confirmed = false
	}
	return message
}

// windowDuration finds the alert window's length in a snapshot of the same account; zero when unknown.
func windowDuration(snapshot *quota.Snapshot, alert store.Alert) time.Duration {
	if snapshot == nil || snapshot.AccountID != alert.AccountID || snapshot.Provider != alert.Provider {
		return 0
	}
	for _, window := range snapshot.Windows {
		if window.ID == alert.Window {
			return window.Duration
		}
	}
	return 0
}

func freshForAlert(snapshot *quota.Snapshot, alert store.Alert, now time.Time) bool {
	if snapshot == nil {
		return false
	}
	if snapshot.AccountID != alert.AccountID || snapshot.Provider != alert.Provider {
		return false
	}
	if snapshot.ObservedAt.Before(alert.ResetAt) || snapshot.ObservedAt.After(now.Add(time.Minute)) {
		return false
	}
	return now.Sub(snapshot.ObservedAt) <= 2*time.Minute
}

func (e *Engine) recordFailure(ctx context.Context, alert store.Alert, sendErr error) error {
	state, next := retryDecision(alert, sendErr, e.clock())
	if err := e.DB.Finish(ctx, alert, state, sendErr.Error(), next, e.clock()); err != nil {
		return err
	}
	e.log(fmt.Sprintf("Email delivery %s (attempt %d): %s", state, alert.Attempts, sendErr))
	return nil
}

func retryDecision(alert store.Alert, err error, now time.Time) (string, time.Time) {
	if !notify.Retryable(err) || alert.Attempts >= 8 {
		return "failed", now
	}
	delay := 30 * time.Second * time.Duration(1<<min(max(alert.Attempts-1, 0), 7))
	delay = max(delay, notify.RetryAfter(err))
	next := now.Add(delay)
	if !next.Before(alert.FirstAttempt.Add(23 * time.Hour)) {
		return "failed", now
	}
	return "retry", next
}

func (e *Engine) Run(ctx context.Context, interval time.Duration, once bool) error {
	if interval < 15*time.Second || interval > time.Hour {
		return errors.New("poll interval must be between 15s and 1h")
	}
	if once {
		return e.Step(ctx)
	}
	return e.loop(ctx, interval)
}

func (e *Engine) loop(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if err := e.Step(ctx); err != nil {
				e.log("Watch cycle: " + err.Error())
			}
			timer.Reset(interval)
		}
	}
}
