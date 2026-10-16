package main

import (
	"context"
	"crypto/rand"
	"fmt"

	"resetwatch/internal/notify"
)

func (a *application) testEmail(ctx context.Context, args []string) error {
	to, from, err := a.testEmailFlags(args)
	if err != nil {
		return err
	}
	key, err := a.resendAPIKey()
	if err != nil {
		return err
	}
	sender := a.testSender
	if sender == nil {
		sender = notify.Resend{APIKey: key}
	}
	message := notify.Message{Key: "test-" + rand.Text(), To: to, From: from, Test: true}
	if err := sender.Send(ctx, message); err != nil {
		return fmt.Errorf("test email: %w", err)
	}
	fmt.Fprintln(a.out, "Test email accepted by Resend. Check your inbox and spam folder; acceptance does not guarantee inbox delivery.")
	return nil
}

func (a *application) testEmailFlags(args []string) (string, string, error) {
	flags := a.flags("test-email")
	to := flags.String("email", "", "test recipient; use your Resend account email with the default sender")
	from := flags.String("from", "ResetWatch <onboarding@resend.dev>", "sender address; other recipients require your verified domain")
	if err := flags.Parse(args); err != nil {
		return "", "", err
	}
	if err := noArguments(flags.Args()); err != nil {
		return "", "", err
	}
	if err := notify.ValidateAddresses(*to, *from); err != nil {
		return "", "", err
	}
	return *to, *from, nil
}
