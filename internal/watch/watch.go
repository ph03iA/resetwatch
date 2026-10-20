package watch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

type Engine struct {
	DB              *store.DB
	Provider        quota.Provider
	ProviderFactory func(store.Config) []quota.Provider
	// Mailer returns a sender for the configured Gmail account, checking its credential first. It is resolved
	// every cycle, so account changes apply without restarting the watcher.
	Mailer func(context.Context, store.Config) (notify.Sender, error)
	Now    func() time.Time
	Log    func(string)
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
	var observationErrs []error
	for _, provider := range e.cycleProviders(config) {
		if _, err := e.observe(ctx, provider); err != nil {
			observationErrs = append(observationErrs, err)
		}
	}
	if len(observationErrs) > 0 {
		e.log("Quota refresh unavailable; due emails will describe expected resets only.")
	}
	return errors.Join(append(observationErrs, e.deliverDue(ctx))...)
}

// Observe reads the provider and records the reading; RecordSnapshot loads current settings itself.
func (e *Engine) Observe(ctx context.Context) (*quota.Snapshot, error) {
	return e.observe(ctx, e.Provider)
}

// cycleProviders lists the readers to poll this cycle; a single Provider is treated as Codex.
func (e *Engine) cycleProviders(config store.Config) []quota.Provider {
	if e.ProviderFactory != nil {
		return e.ProviderFactory(config)
	}
	if e.Provider == nil || !config.Selected("codex") {
		return nil
	}
	return []quota.Provider{e.Provider}
}

func (e *Engine) observe(ctx context.Context, provider quota.Provider) (*quota.Snapshot, error) {
	snapshot, err := provider.Read(ctx)
	if err != nil {
		return nil, err
	}
	if err := e.DB.RecordSnapshot(ctx, snapshot, e.clock()); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// errPaused ends a delivery pass after the Gmail sign-in stopped working.
var errPaused = errors.New("Gmail delivery paused")

// deliverDue sends due reminders. Without a working Gmail connection it claims nothing, while quota
// readings carry on, so reminders wait instead of using up their attempts.
func (e *Engine) deliverDue(ctx context.Context) error {
	config, err := e.DB.Config(ctx)
	if err != nil || !config.Gmail.Ready() {
		return err
	}
	sender, err := e.Mailer(ctx, config)
	if err != nil {
		return e.pause(ctx, err)
	}
	for range 50 {
		alert, err := e.DB.Claim(ctx, e.clock())
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := e.deliver(ctx, alert, sender); err != nil {
			return ignorePaused(err)
		}
	}
	return nil
}

func ignorePaused(err error) error {
	if errors.Is(err, errPaused) {
		return nil
	}
	return err
}

// pause records an authorization failure against the credential that failed; other errors (an
// unreachable Google, a locked credential store) just end this cycle.
func (e *Engine) pause(ctx context.Context, err error) error {
	failure, ok := notify.AuthorizationFailure(err)
	if !ok {
		return err
	}
	e.log("Email delivery paused: " + failure.Error())
	return e.DB.PauseGmail(ctx, failure.Credential, failure.Reason)
}

func (e *Engine) deliver(ctx context.Context, alert store.Alert, sender notify.Sender) error {
	config, err := e.DB.Config(ctx)
	if err != nil {
		return err
	}
	if reason := e.cancelReason(ctx, alert, config); reason != "" {
		return e.DB.Finish(ctx, alert, "cancelled", reason, e.clock(), e.clock())
	}
	return e.sendAlert(ctx, alert, sender)
}

func (e *Engine) sendAlert(ctx context.Context, alert store.Alert, sender notify.Sender) error {
	prepared, err := notify.Prepare(compose(alert, e.alertSnapshot(ctx, alert), e.clock()), e.clock())
	if err != nil {
		return err
	}
	message, err := e.DB.FreezeMessage(ctx, alert, prepared)
	if err != nil {
		return err
	}
	if err := e.DB.ValidateDelivery(ctx, alert, e.clock()); err != nil {
		return err
	}
	if err := sender.Send(ctx, message); err != nil {
		return e.sendFailed(ctx, alert, err)
	}
	if err := e.DB.Finish(ctx, alert, "sent", "", e.clock(), e.clock()); err != nil {
		return err
	}
	e.log("Reset email accepted by Gmail: " + alert.ID[:12])
	return nil
}

// sendFailed gives the attempt back when the sign-in failed, then pauses; other failures follow the retry schedule.
func (e *Engine) sendFailed(ctx context.Context, alert store.Alert, sendErr error) error {
	if _, ok := notify.AuthorizationFailure(sendErr); !ok {
		return e.recordFailure(ctx, alert, sendErr)
	}
	if err := e.DB.ReleaseDelivery(ctx, alert, e.clock()); err != nil {
		return err
	}
	return errors.Join(e.pause(ctx, sendErr), errPaused)
}

// alertSnapshot returns the provider's last saved reading, which this cycle's poll just recorded;
// compose only confirms a reset from data observed after it and within two minutes.
func (e *Engine) alertSnapshot(ctx context.Context, alert store.Alert) *quota.Snapshot {
	saved, err := e.DB.Snapshot(ctx, alert.Provider)
	if err != nil {
		return nil
	}
	return &saved
}

func (e *Engine) cancelReason(ctx context.Context, alert store.Alert, config store.Config) string {
	if !config.Selected(alert.Provider) {
		return "provider disabled"
	}
	if alert.Gmail != config.Gmail.Subject {
		return "Gmail account changed"
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
		Provider: alert.Provider, Window: alert.Window, ResetAt: alert.ResetAt, Duration: windowDuration(snapshot, alert),
		LocalResetAt: alert.ResetAt.Local().Format("2006-01-02 3:04 PM MST")}
	if !observedAfterReset(snapshot, alert, now) {
		return message
	}
	// Usage never drops inside a window, so a window still at 100% before its reset is still used up,
	// even when the reading is too old or unconfirmed to prove the alert's own window is back.
	for _, window := range snapshot.Windows {
		if window.UsedPercent >= 100 && window.ResetsAt.After(now) {
			message.OtherLimit = true
		}
	}
	message.Confirmed = !message.OtherLimit && freshForAlert(snapshot, alert, now) && windowAvailable(snapshot, alert.Window)
	return message
}

// windowAvailable reports whether the window is below 100%. After a reset, Claude leaves a window
// out until new usage starts it, so a missing Claude window in a fresh reading is unused.
func windowAvailable(snapshot *quota.Snapshot, id string) bool {
	i := slices.IndexFunc(snapshot.Windows, func(w quota.Window) bool { return w.ID == id })
	if i < 0 {
		return snapshot.Provider == "claude"
	}
	return snapshot.Windows[i].UsedPercent < 100
}

// observedAfterReset reports whether the reading is for this alert's account and was taken after its reset.
func observedAfterReset(snapshot *quota.Snapshot, alert store.Alert, now time.Time) bool {
	if snapshot == nil || snapshot.AccountID != alert.AccountID || snapshot.Provider != alert.Provider {
		return false
	}
	return !snapshot.ObservedAt.Before(alert.ResetAt) && !snapshot.ObservedAt.After(now.Add(time.Minute))
}

// windowDuration finds the alert window's length in a snapshot of the same account, falling back
// to Claude's fixed window lengths because Claude omits a window after it resets; zero when unknown.
func windowDuration(snapshot *quota.Snapshot, alert store.Alert) time.Duration {
	if snapshot == nil || snapshot.AccountID != alert.AccountID || snapshot.Provider != alert.Provider {
		return quota.ClaudeWindowDuration(alert.Window)
	}
	for _, window := range snapshot.Windows {
		if window.ID == alert.Window {
			return window.Duration
		}
	}
	return quota.ClaudeWindowDuration(alert.Window)
}

// freshForAlert reports whether the reading can prove the reset: explicit permission, observed after it, within two minutes.
func freshForAlert(snapshot *quota.Snapshot, alert store.Alert, now time.Time) bool {
	return observedAfterReset(snapshot, alert, now) && confirmedUsageAllowed(snapshot) && now.Sub(snapshot.ObservedAt) <= 2*time.Minute
}

func confirmedUsageAllowed(snapshot *quota.Snapshot) bool {
	if snapshot.Provider == "claude" {
		return true // Claude has no permission flag; fresh windows below 100% are the evidence
	}
	if snapshot.Provider == "codex" {
		return snapshot.OrdinaryUsageAllowed != nil && *snapshot.OrdinaryUsageAllowed
	}
	return false
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
