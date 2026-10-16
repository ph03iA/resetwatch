package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"resetwatch/internal/notify"
)

const defaultSetupSender = "ResetWatch <onboarding@resend.dev>"

func (a *application) collectSetupEmail(s *setupState) error {
	if s.options.from == "" {
		s.options.from = defaultSetupSender
	}
	for {
		if err := a.promptSetupEmail(s); err != nil {
			return err
		}
		err := notify.ValidateAddresses(s.options.email, s.options.from)
		if err == nil {
			break
		}
		if !s.interactive || s.options.provided["email"] || s.options.provided["from"] {
			return err
		}
		fmt.Fprintln(a.out, err)
	}
	if strings.Contains(s.options.from, "onboarding@resend.dev") {
		fmt.Fprintln(a.out, "The resend.dev sender can send only to your Resend account email. Other recipients require a sender on your verified domain.")
	}
	return nil
}

func (a *application) promptSetupEmail(s *setupState) error {
	if !s.interactive {
		return nil
	}
	var err error
	if !s.options.provided["email"] {
		s.options.email, err = s.prompt.ask("Email for reminders", s.options.email)
		if err != nil {
			return err
		}
	}
	if !s.options.provided["from"] {
		s.options.from, err = s.prompt.ask("Sender address", s.options.from)
	}
	return err
}

func (a *application) collectSetupKey(s *setupState) error {
	key := strings.TrimSpace(a.getenv("RESEND_API_KEY"))
	var err error
	if key == "" {
		key, err = loadResendEnv()
	}
	if err != nil {
		return err
	}
	if key != "" {
		fmt.Fprintln(a.out, "Using your existing Resend API key (hidden).")
		return validateResendEnvKey(key)
	}
	if !s.interactive {
		return errors.New("RESEND_API_KEY is missing; configure the environment or current folder .env before noninteractive setup")
	}
	path, err := filepath.Abs(".env")
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, "The key will be saved in:", path)
	for {
		key, err = s.prompt.password("Resend API key (hidden)")
		if err != nil {
			return err
		}
		if err := validateResendEnvKey(key); err != nil {
			fmt.Fprintln(a.out, err)
			continue
		}
		s.newKey = key
		return nil
	}
}

func (a *application) offerSetupTest(ctx context.Context, s *setupState) error {
	requested := s.options.test
	if s.interactive && !requested {
		var err error
		requested, err = s.prompt.confirm("Send a test email now?", false)
		if err != nil {
			return fmt.Errorf("setup is saved; email test cancelled: %w", err)
		}
	}
	if !requested {
		fmt.Fprintln(a.out, "Email delivery: untested. Run resetwatch test-email to check later.")
		return nil
	}
	return a.sendSetupTest(ctx, s)
}

func (a *application) sendSetupTest(ctx context.Context, s *setupState) error {
	for {
		err := a.testEmail(ctx, []string{"--email", s.options.email, "--from", s.options.from})
		if err == nil {
			s.emailTested = true
			return nil
		}
		if !s.interactive {
			return fmt.Errorf("setup saved; test email failed: %w", err)
		}
		fmt.Fprintln(a.out, "Setup is saved, but the test was not accepted:", err)
		retry, promptErr := s.prompt.confirm("Retry the test email?", false)
		if promptErr != nil {
			return promptErr
		}
		if !retry {
			fmt.Fprintln(a.out, "Email delivery: untested.")
			return nil
		}
	}
}
