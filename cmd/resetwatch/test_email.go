package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"resetwatch/internal/notify"
)

// errAddressFlags explains the removed Resend-era flags.
var errAddressFlags = errors.New("--email and --from were removed: reminders now go from your connected Gmail account to itself; run resetwatch gmail connect")

// testEmail sends one labeled test to the connected account: one attempt, no quota reads, no reminder.
func (a *application) testEmail(ctx context.Context, args []string) error {
	if err := a.testEmailFlags(args); err != nil {
		return err
	}
	config, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	if !config.Gmail.Connected() {
		return errors.New("connect Gmail first: run resetwatch gmail connect")
	}
	sender, err := a.mailer(ctx, config)
	if err != nil {
		return fmt.Errorf("test email: %w", err)
	}
	address := config.Gmail.Address
	message, err := notify.Prepare(notify.Message{Key: "test-" + rand.Text(), To: address, From: address, Test: true}, time.Now())
	if err != nil {
		return err
	}
	if err := sender.Send(ctx, message); err != nil {
		return fmt.Errorf("test email: %w", err)
	}
	fmt.Fprintln(a.out, "Test email accepted by Gmail for", address+". Acceptance does not guarantee it reaches your inbox.")
	return nil
}

func (a *application) testEmailFlags(args []string) error {
	flags := a.flags("test-email")
	email := flags.String("email", "", "removed; reminders go to your Gmail account")
	from := flags.String("from", "", "removed; reminders come from your Gmail account")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *email != "" || *from != "" {
		return errAddressFlags
	}
	return noArguments(flags.Args())
}
