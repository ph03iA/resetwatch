package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"resetwatch/internal/notify"
)

func TestProviderSelectionPreservesLegacyAndRejectsUnknownProviders(t *testing.T) {
	for _, test := range []struct {
		choice string
		want   []string
	}{
		{"", []string{"codex", "claude"}}, {"codex", []string{"codex"}},
		{"claude", []string{"claude"}}, {"both", []string{"codex", "claude"}}, {"invalid", nil},
	} {
		config := Config{Providers: test.choice}
		if !reflect.DeepEqual(config.ProvidersList(), test.want) {
			t.Fatalf("selection %q = %v, want %v", test.choice, config.ProvidersList(), test.want)
		}
		if config.Selected("unknown") {
			t.Fatal("unsupported provider was selected")
		}
	}
}

func providerConfig(choice string) Config {
	config := testConfig()
	config.Providers = choice
	return config
}

func TestSetupCancelsDeselectedProviderAndKeepsItsReading(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	legacy := providerConfig("")
	must(t, db.SaveConfig(ctx, legacy))
	codex := testSnapshot(now, now.Add(time.Hour), 100)
	claude := claudeAuditSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.RecordSnapshot(ctx, codex, now))
	must(t, db.RecordSnapshot(ctx, claude, now))
	selected := providerConfig("codex")
	selected.CodexBin = "new-codex-path"
	must(t, db.ConfigureSetup(ctx, selected, now))
	saved, err := db.Config(ctx)
	must(t, err)
	if saved != selected {
		t.Fatalf("setup did not save all settings together: %+v", saved)
	}
	assertStates(t, db, map[string]int{"pending": 1, "cancelled": 1})
	claude.ObservedAt = now.Add(time.Minute)
	must(t, db.RecordSnapshot(ctx, claude, claude.ObservedAt))
	must(t, db.Schedule(ctx, claude, legacy, claude.ObservedAt))
	assertStates(t, db, map[string]int{"pending": 1, "cancelled": 1})
	reading, err := db.Snapshot(ctx, "claude")
	must(t, err)
	if !reading.ObservedAt.Equal(claude.ObservedAt) {
		t.Fatal("disabled-provider observation was lost")
	}
	alert, err := db.Claim(ctx, now.Add(time.Hour))
	must(t, err)
	if alert.Provider != "codex" {
		t.Fatalf("deselected provider was claimed: %+v", alert)
	}
}

func TestReenableWaitsForNewReadingAndPreservesFrozenRetry(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	config := providerConfig("claude")
	must(t, db.ConfigureSetup(ctx, config, now))
	snapshot := claudeAuditSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.RecordSnapshot(ctx, snapshot, now))
	id := alertID(snapshot, snapshot.Windows[0], config)
	frozen, err := json.Marshal(notify.Message{Key: id, To: config.To, From: config.From,
		Provider: "claude", Window: "claude:five_hour", ResetAt: now.Add(time.Hour)})
	must(t, err)
	first := now.Add(-time.Minute)
	_, err = db.db.Exec(`UPDATE alerts SET state='retry',attempts=1,first_attempt=?,payload=? WHERE id=?`, first.Unix(), string(frozen), id)
	must(t, err)
	must(t, db.ConfigureSetup(ctx, providerConfig("codex"), now))
	must(t, db.ConfigureSetup(ctx, config, now))
	must(t, db.Schedule(ctx, snapshot, config, now))
	assertStates(t, db, map[string]int{"cancelled": 1})
	snapshot.ObservedAt = now.Add(time.Minute)
	must(t, db.RecordSnapshot(ctx, snapshot, snapshot.ObservedAt))
	var restored Alert
	must(t, scanRegressionAlert(db, id, &restored))
	if restored.State != "pending" || restored.Attempts != 1 || !restored.FirstAttempt.Equal(first) {
		t.Fatalf("rearming reset its delivery identity/budget: %+v", restored)
	}
	if storedPayload(t, db, id) != string(frozen) {
		t.Fatal("rearming changed the original immutable payload")
	}
}

func TestNewReadingCannotReviveTerminalOrExpiredCancelledAlerts(t *testing.T) {
	for _, test := range []struct {
		name string
		edit string
	}{
		{"sent", "state='sent'"},
		{"exhausted", "state='cancelled',attempts=8"},
		{"expired retry budget", "state='cancelled',first_attempt=1"},
		{"past reset", "state='cancelled',reset_at=1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db := openTestDB(t, t.TempDir())
			now := storeTime()
			config := providerConfig("claude")
			must(t, db.ConfigureSetup(ctx, config, now))
			snapshot := claudeAuditSnapshot(now, now.Add(time.Hour), 100)
			must(t, db.RecordSnapshot(ctx, snapshot, now))
			id := alertID(snapshot, snapshot.Windows[0], config)
			_, err := db.db.Exec("UPDATE alerts SET "+test.edit+",last_error=? WHERE id=?", providerDisabled, id)
			must(t, err)
			must(t, db.RecordSnapshot(ctx, snapshot, now))
			var alert Alert
			must(t, scanRegressionAlert(db, id, &alert))
			if alert.State == "pending" || alert.State == "retry" {
				t.Fatalf("terminal alert was revived: %+v", alert)
			}
		})
	}
}

func TestSetupRejectsLiveDeliveryAndCancelsExpiredClaim(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	config := providerConfig("claude")
	must(t, db.ConfigureSetup(ctx, config, now))
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
	due := now.Add(time.Hour)
	alert, err := db.Claim(ctx, due)
	must(t, err)
	if err := db.ConfigureSetup(ctx, providerConfig("codex"), due); err == nil {
		t.Fatal("setup disabled a provider with a live delivery")
	}
	saved, err := db.Config(ctx)
	must(t, err)
	if saved != config {
		t.Fatal("rejected setup partially changed configuration")
	}
	must(t, db.ConfigureSetup(ctx, providerConfig("codex"), due.Add(2*time.Minute)))
	assertStates(t, db, map[string]int{"cancelled": 1})
	if err := db.ValidateDelivery(ctx, alert, due.Add(2*time.Minute)); err == nil {
		t.Fatal("an expired disabled claim was permitted to send")
	}
}

func TestClaimAndRetryEnforceCurrentSelection(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	config := providerConfig("claude")
	must(t, db.ConfigureSetup(ctx, config, now))
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
	due := now.Add(time.Hour)
	alert, err := db.Claim(ctx, due)
	must(t, err)
	must(t, db.Finish(ctx, alert, "failed", "synthetic failure", due, due))
	must(t, db.SaveConfig(ctx, providerConfig("codex")))
	if err := db.Retry(ctx, alert.ID, due); err == nil {
		t.Fatal("manual retry bypassed provider deselection")
	}
	_, err = db.db.Exec("UPDATE alerts SET state='retry' WHERE id=?", alert.ID)
	must(t, err)
	if _, err := db.Claim(ctx, due); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("claim bypassed current selection: %v", err)
	}
	assertStates(t, db, map[string]int{"cancelled": 1})
}

func TestManualProviderEnablePreservesOtherSettingsWithoutScheduling(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	config := providerConfig("claude")
	must(t, db.ConfigureSetup(ctx, config, now))
	must(t, db.SaveSnapshot(ctx, testSnapshot(now, now.Add(time.Hour), 100)))
	must(t, db.SaveCodexBinary(ctx, "replacement-codex"))
	saved, err := db.Config(ctx)
	must(t, err)
	if saved.Providers != "both" || saved.CodexBin != "replacement-codex" || saved.To != config.To || saved.From != config.From {
		t.Fatalf("manual Codex enable lost settings: %+v", saved)
	}
	assertStates(t, db, map[string]int{})
	saved.Providers = "codex"
	must(t, db.SaveConfig(ctx, saved))
	must(t, db.EnableProvider(ctx, "claude"))
	if err := db.EnableProvider(ctx, "unknown"); err == nil {
		t.Fatal("unsupported provider enabled")
	}
	assertStates(t, db, map[string]int{})
}

func TestInitialSetupSkipsNewlyEnabledCachedProvider(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	must(t, db.SaveConfig(ctx, Config{Providers: "claude"}))
	codex := testSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.SaveSnapshot(ctx, codex))
	must(t, db.SaveSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100)))
	must(t, db.ConfigureSetup(ctx, providerConfig("both"), now))
	assertStates(t, db, map[string]int{"pending": 1})
	must(t, db.RecordSnapshot(ctx, codex, now))
	assertStates(t, db, map[string]int{"pending": 2})
}

func TestReaddressDoesNotRearmDisabledDestinationFromCache(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	config := providerConfig("claude")
	must(t, db.ConfigureSetup(ctx, config, now))
	snapshot := claudeAuditSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.RecordSnapshot(ctx, snapshot, now))
	must(t, db.ConfigureSetup(ctx, providerConfig("codex"), now))
	must(t, db.ConfigureSetup(ctx, config, now))
	must(t, db.ConfigureNotifications(ctx, "other@example.test", config.From, now))
	must(t, db.RecordSnapshot(ctx, snapshot, now))
	must(t, db.ConfigureNotifications(ctx, config.To, config.From, now))
	assertStates(t, db, map[string]int{"cancelled": 2})
	must(t, db.RecordSnapshot(ctx, snapshot, now))
	assertStates(t, db, map[string]int{"pending": 1, "cancelled": 1})
}

func TestConcurrentClaimAndDeselectionHaveOneSafeOutcome(t *testing.T) {
	for range 10 {
		ctx := context.Background()
		dir := t.TempDir()
		db, other := openTestDB(t, dir), openTestDB(t, dir)
		now := storeTime()
		must(t, db.ConfigureSetup(ctx, providerConfig("claude"), now))
		must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
		start := make(chan struct{})
		claimed, configured := make(chan error, 1), make(chan error, 1)
		go func() { <-start; _, err := db.Claim(ctx, now.Add(time.Hour)); claimed <- err }()
		go func() { <-start; configured <- other.ConfigureSetup(ctx, providerConfig("codex"), now.Add(time.Hour)) }()
		close(start)
		claimErr, setupErr := <-claimed, <-configured
		assertSelectionRace(t, db, claimErr, setupErr)
	}
}

func TestInvalidSetupLeavesConfigurationAndRemindersUnchanged(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	config := providerConfig("both")
	must(t, db.ConfigureSetup(ctx, config, now))
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
	for _, candidate := range []Config{
		{Providers: "", To: config.To, From: config.From},
		{Providers: "unknown", To: config.To, From: config.From},
		{Providers: "codex", To: "invalid", From: config.From},
		{Providers: "codex", To: config.To, From: "invalid"},
	} {
		if err := db.ConfigureSetup(ctx, candidate, now); err == nil {
			t.Fatalf("invalid setup accepted: %+v", candidate)
		}
		saved, err := db.Config(ctx)
		must(t, err)
		if saved != config {
			t.Fatalf("invalid setup partially changed configuration: %+v", saved)
		}
		assertStates(t, db, map[string]int{"pending": 1})
	}
}

func TestFailedSetupRollsBackProviderCancellationAndBinaryChange(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, t.TempDir())
	now := storeTime()
	config := providerConfig("both")
	config.CodexBin = "original-codex"
	must(t, db.ConfigureSetup(ctx, config, now))
	snapshot := testSnapshot(now, now.Add(time.Hour), 100)
	must(t, db.RecordSnapshot(ctx, snapshot, now))
	must(t, db.RecordSnapshot(ctx, claudeAuditSnapshot(now, now.Add(time.Hour), 100), now))
	destination := providerConfig("codex")
	destination.To, destination.CodexBin = "failed@example.test", "replacement-codex"
	id := alertID(snapshot, snapshot.Windows[0], destination)
	_, err := db.db.Exec(`INSERT INTO alerts(id,provider,account_id,window_id,reset_at,recipient,sender,state,next_attempt,updated_at)
 VALUES(?,?,?,?,?,?,?,'failed',?,?)`, id, snapshot.Provider, snapshot.AccountID, snapshot.Windows[0].ID,
		snapshot.Windows[0].ResetsAt.Unix(), destination.To, destination.From, now.Unix(), now.Unix())
	must(t, err)
	if err := db.ConfigureSetup(ctx, destination, now); err == nil {
		t.Fatal("setup accepted a failed delivery destination")
	}
	saved, err := db.Config(ctx)
	must(t, err)
	if saved != config {
		t.Fatalf("failed setup partially changed configuration: %+v", saved)
	}
	assertStates(t, db, map[string]int{"pending": 2, "failed": 1})
}

func assertSelectionRace(t *testing.T, db *DB, claimErr, setupErr error) {
	t.Helper()
	config, err := db.Config(context.Background())
	must(t, err)
	if setupErr == nil {
		if config.Providers != "codex" || !errors.Is(claimErr, sql.ErrNoRows) {
			t.Fatalf("disabled provider was claimed: config=%+v claim=%v", config, claimErr)
		}
		return
	}
	if claimErr != nil || config.Providers != "claude" {
		t.Fatalf("claim/setup race had an unsafe outcome: config=%+v claim=%v setup=%v", config, claimErr, setupErr)
	}
}
