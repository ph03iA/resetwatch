package main

import (
	"context"
	"errors"
	"fmt"
)

// collectSetupGmail keeps a working connection or signs in to Google. Noninteractive setup never opens
// a browser, so it needs a working connection already.
func (a *application) collectSetupGmail(ctx context.Context, s *setupState) error {
	current := s.previous.Gmail
	if current.Ready() {
		keep, err := a.keepSetupGmail(s)
		if err != nil || keep {
			s.gmail = current
			return err
		}
	} else if !s.interactive {
		return errors.New("noninteractive setup needs a working Gmail connection; run resetwatch gmail connect first")
	} else if current.Connected() {
		fmt.Fprintln(a.out, "The saved Gmail sign-in no longer works; sign in again.")
	}
	fmt.Fprintln(a.out, "ResetWatch sends reminders from your Gmail account to itself. Google will ask to let it send email; it cannot read your inbox.")
	account, err := a.stageGmail(ctx, false)
	if err != nil {
		return err
	}
	s.gmail, s.staged = account, true
	fmt.Fprintln(a.out, "Gmail: signed in as", account.Address+".")
	return nil
}

// keepSetupGmail asks whether to keep the connected account. Saved values are never offered as defaults.
func (a *application) keepSetupGmail(s *setupState) (bool, error) {
	if !s.interactive {
		return true, nil
	}
	choice, err := s.prompt.choose("Gmail account for reminders", []string{"Keep the connected account", "Connect a different account", "Cancel setup"}, 0)
	if err != nil {
		return false, err
	}
	if choice == 2 {
		return false, errSetupCancelled
	}
	return choice == 0, nil
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
		err := a.testEmail(ctx, nil)
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
