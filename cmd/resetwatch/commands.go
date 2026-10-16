package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
	"resetwatch/internal/watch"
)

func (a *application) connect(ctx context.Context, args []string) error {
	binary, err := a.connectFlags(args)
	if err != nil {
		return err
	}
	resolved, err := quota.ResolveBinary(binary)
	if err != nil {
		return fmt.Errorf("connect Codex: %w", err)
	}
	if err := a.connectCodex(ctx, resolved); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Connected to Codex. Account credentials remain with Codex.")
	return nil
}

func (a *application) connectFlags(args []string) (string, error) {
	if len(args) == 0 || args[0] != "codex" {
		return "", errors.New("supported provider: codex; run resetwatch connect codex")
	}
	flags := a.flags("connect codex")
	binary := flags.String("codex-bin", "", "native Codex executable path")
	if err := flags.Parse(args[1:]); err != nil {
		return "", err
	}
	if err := noArguments(flags.Args()); err != nil {
		return "", err
	}
	return *binary, nil
}

func (a *application) connectCodex(ctx context.Context, binary string) error {
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	provider := quota.Codex{Binary: binary, Timeout: 15 * time.Second, Version: version}
	engine := watch.Engine{DB: a.db, Provider: provider}
	if _, err := engine.Observe(ctx, config); err != nil {
		return fmt.Errorf("connect Codex: %w", err)
	}
	config, err = a.db.Config(ctx)
	if err != nil {
		return err
	}
	config.CodexBin = binary
	return a.db.SaveConfig(ctx, config)
}

func (a *application) configure(ctx context.Context, args []string) error {
	flags := a.flags("notify")
	to := flags.String("email", "", "recipient email address")
	from := flags.String("from", "", "verified Resend sender address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noArguments(flags.Args()); err != nil {
		return err
	}
	if err := notify.ValidateAddresses(*to, *from); err != nil {
		return err
	}
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	config.To, config.From = *to, *from
	if err := a.db.SaveConfig(ctx, config); err != nil {
		return err
	}
	if err := a.reschedule(ctx); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Email settings saved. Save RESEND_API_KEY in .env or set it in the environment, then run resetwatch watch.")
	return nil
}

func (a *application) reschedule(ctx context.Context) error {
	snapshot, err := a.db.Snapshot(ctx, "codex")
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	return a.db.Schedule(ctx, snapshot, config, time.Now().UTC())
}

func (a *application) status(ctx context.Context, args []string) error {
	flags := a.flags("status")
	refresh := flags.Bool("refresh", false, "read current account limits before displaying")
	asJSON := flags.Bool("json", false, "machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noArguments(flags.Args()); err != nil {
		return err
	}
	if *refresh {
		if err := a.refresh(ctx); err != nil {
			return err
		}
	}
	snapshot, err := a.db.Snapshot(ctx, "codex")
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("no connected account; run resetwatch connect codex")
	}
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(a.out, snapshot)
	}
	a.render(snapshot, false)
	return nil
}

func (a *application) refresh(ctx context.Context) error {
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	engine := watch.Engine{DB: a.db, Provider: quota.Codex{Binary: config.CodexBin, Timeout: 15 * time.Second, Version: version}}
	_, err = engine.Observe(ctx, config)
	return err
}

func (a *application) history(ctx context.Context, args []string) error {
	flags := a.flags("history")
	limit := flags.Int("limit", 20, "maximum rows (1–1000)")
	asJSON := flags.Bool("json", false, "machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noArguments(flags.Args()); err != nil {
		return err
	}
	if *limit < 1 || *limit > 1000 {
		return errors.New("history limit must be between 1 and 1000")
	}
	alerts, err := a.db.History(ctx, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(a.out, alerts)
	}
	a.renderHistory(alerts)
	return nil
}

func (a *application) watch(ctx context.Context, args []string) error {
	flags := a.flags("watch")
	interval := flags.Duration("interval", time.Minute, "poll interval (15s–1h)")
	once := flags.Bool("once", false, "run one refresh/delivery cycle, then exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noArguments(flags.Args()); err != nil {
		return err
	}
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	if err := notify.ValidateAddresses(config.To, config.From); err != nil {
		return errors.New("configure email first: resetwatch notify --email ... --from ...")
	}
	key, err := a.resendAPIKey()
	if err != nil {
		return err
	}
	engine := watch.Engine{DB: a.db, Provider: quota.Codex{Binary: config.CodexBin, Timeout: 15 * time.Second, Version: version},
		Sender: notify.Resend{APIKey: key}, Log: func(message string) { fmt.Fprintln(a.errOut, time.Now().Format(time.RFC3339), message) }}
	fmt.Fprintln(a.errOut, "Watching Codex reset windows. Ctrl+C stops safely.")
	return engine.Run(ctx, *interval, *once)
}

func writeJSON(out interface{ Write([]byte) (int, error) }, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (a *application) retry(ctx context.Context, args []string) error {
	flags := a.flags("retry")
	id := flags.String("id", "", "full alert ID from history --json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noArguments(flags.Args()); err != nil {
		return err
	}
	decoded, err := hex.DecodeString(*id)
	if err != nil || len(decoded) != 32 {
		return errors.New("provide the full 64-character alert ID from history --json")
	}
	if err := a.db.Retry(ctx, *id, time.Now().UTC()); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Alert requeued with its original message and deduplication key. Start the watcher to deliver it.")
	return nil
}
