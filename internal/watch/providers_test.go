package watch

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

func TestStepReloadsSelectedProviderAndExecutableEveryCycle(t *testing.T) {
	engine, _, _, now := testEngine(t)
	ctx := context.Background()
	config, err := engine.DB.Config(ctx)
	must(t, err)
	config.Providers, config.CodexBin = "codex", "first-binary"
	must(t, engine.DB.ConfigureSetup(ctx, config, *now))
	var binaries []string
	engine.ProviderFactory = func(current store.Config) []quota.Provider {
		if !current.Selected("codex") {
			return nil
		}
		binaries = append(binaries, current.CodexBin)
		return []quota.Provider{&fakeProvider{snapshot: watchSnapshot(*now, now.Add(time.Hour), 10)}}
	}
	must(t, engine.Step(ctx))
	config.Providers = "claude"
	must(t, engine.DB.ConfigureSetup(ctx, config, *now))
	must(t, engine.Step(ctx))
	must(t, engine.DB.SaveCodexBinary(ctx, "second-binary"))
	must(t, engine.Step(ctx))
	if !reflect.DeepEqual(binaries, []string{"first-binary", "second-binary"}) {
		t.Fatalf("watcher retained its startup selection/path: %v", binaries)
	}
}

func TestStepDoesNotPollInjectedProviderWhenCodexDeselected(t *testing.T) {
	engine, _, sender, now := testEngine(t)
	ctx := context.Background()
	config, err := engine.DB.Config(ctx)
	must(t, err)
	config.Providers = "claude"
	must(t, engine.DB.ConfigureSetup(ctx, config, *now))
	engine.Provider = regressionRead(func(context.Context) (quota.Snapshot, error) {
		t.Fatal("deselected Codex provider was polled")
		return quota.Snapshot{}, nil
	})
	must(t, engine.Step(ctx))
	if len(sender.messages) != 0 {
		t.Fatal("unexpected provider email")
	}
}

func TestDeselectionDuringQuotaReadCannotScheduleOrSend(t *testing.T) {
	engine, _, sender, now := testEngine(t)
	ctx := context.Background()
	engine.Provider = regressionRead(func(context.Context) (quota.Snapshot, error) {
		config, err := engine.DB.Config(ctx)
		must(t, err)
		config.Providers = "claude"
		must(t, engine.DB.ConfigureSetup(ctx, config, *now))
		return watchSnapshot(*now, now.Add(time.Hour), 100), nil
	})
	must(t, engine.Step(ctx))
	if len(sender.messages) != 0 {
		t.Fatal("deselected provider sent mail after its quota read")
	}
	history, err := engine.DB.History(ctx, 10)
	must(t, err)
	if len(history) != 0 {
		t.Fatalf("in-flight quota read scheduled deselected provider: %+v", history)
	}
}

func TestExpiredLeaseIsRecheckedBeforeExternalSend(t *testing.T) {
	engine, _, sender, now := testEngine(t)
	ctx := context.Background()
	alert := claimDeliveryAlert(t, engine, *now)
	*now = now.Add(2 * time.Minute)
	if err := engine.deliver(ctx, alert); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired lease was not rejected immediately before send: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatal("expired claimant sent a message")
	}
}
