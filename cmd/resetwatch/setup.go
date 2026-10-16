package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"golang.org/x/term"
	"resetwatch/internal/claudeconfig"
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

type setupOptions struct {
	providers, email, from, binary string
	nonInteractive, replace, test  bool
	provided                       map[string]bool
}

type setupState struct {
	options                           setupOptions
	previous                          store.Config
	prompt                            setupPrompter
	interactive                       bool
	newKey                            string
	reading                           *quota.Snapshot
	installer                         claudeconfig.Installer
	installed, activated, emailTested bool
}

func (a *application) setup(ctx context.Context, args []string) error {
	previous, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	options, err := a.setupFlags(args, previous)
	if err != nil {
		return err
	}
	s := setupState{options: options, previous: previous, interactive: a.setupIsTerminal(options.nonInteractive)}
	s.prompt = setupPrompter{in: a.in, out: a.out, secret: a.setupPassword, ctx: ctx}
	restore := a.restoreSetupTerminal()
	defer restore()
	defer a.reportUnfinishedSetup(&s)
	fmt.Fprintln(a.out, "ResetWatch setup")
	if err := a.collectSetup(&s); err != nil {
		return err
	}
	if err := a.prepareSetupProviders(ctx, &s); err != nil {
		return err
	}
	if err := a.activateSetup(ctx, &s); err != nil {
		return err
	}
	err = a.offerSetupTest(ctx, &s)
	a.printSetupSummary(ctx, &s)
	return err
}

func (a *application) setupFlags(args []string, config store.Config) (setupOptions, error) {
	var options setupOptions
	flags := a.flags("setup")
	flags.StringVar(&options.providers, "providers", config.Providers, "codex, claude, or both")
	flags.StringVar(&options.email, "email", config.To, "recipient email address")
	flags.StringVar(&options.from, "from", config.From, "Resend sender (default: onboarding@resend.dev)")
	flags.StringVar(&options.binary, "codex-bin", config.CodexBin, "native Codex executable path")
	flags.BoolVar(&options.nonInteractive, "non-interactive", false, "never ask questions")
	flags.BoolVar(&options.replace, "replace-statusline", false, "replace an existing Claude status line")
	flags.BoolVar(&options.test, "test-email", false, "send one labeled test after setup")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	options.provided = make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { options.provided[f.Name] = true })
	return options, noArguments(flags.Args())
}

func (a *application) setupIsTerminal(nonInteractive bool) bool {
	if nonInteractive {
		return false
	}
	if a.setupTerminal != nil {
		return a.setupTerminal()
	}
	file, ok := a.in.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func (a *application) setupPassword() (string, error) {
	if a.setupSecret != nil {
		return a.setupSecret()
	}
	file, ok := a.in.(*os.File)
	if !ok {
		return "", errors.New("hidden key input requires a terminal")
	}
	data, err := term.ReadPassword(int(file.Fd()))
	return string(data), err
}

func (a *application) collectSetup(s *setupState) error {
	if err := a.selectSetupProviders(s); err != nil {
		return err
	}
	if err := a.collectSetupEmail(s); err != nil {
		return err
	}
	if err := a.collectSetupKey(s); err != nil {
		return err
	}
	return a.confirmSetupDisabling(s)
}

func (a *application) selectSetupProviders(s *setupState) error {
	choices := []string{"codex", "claude", "both"}
	if s.interactive && !s.options.provided["providers"] {
		preferred, err := a.setupDefaultProvider(s.options.providers)
		if err != nil {
			return err
		}
		index, err := s.prompt.choose("Which limits should ResetWatch watch?", []string{"Codex", "Claude", "Both"}, slices.Index(choices, preferred))
		if err != nil {
			return err
		}
		s.options.providers = choices[index]
	}
	if !slices.Contains(choices, s.options.providers) {
		return errors.New("choose --providers codex, claude, or both; run setup in a terminal for guided questions")
	}
	return nil
}

func (a *application) setupDefaultProvider(saved string) (string, error) {
	if saved != "" {
		return saved, nil
	}
	connected, err := a.connectedProviders(context.Background())
	if err != nil {
		return "", err
	}
	if len(connected) == 2 {
		return "both", nil
	}
	if len(connected) == 1 {
		return connected[0], nil
	}
	return "codex", nil
}

func (a *application) confirmSetupDisabling(s *setupState) error {
	if !s.interactive {
		return nil
	}
	previous := s.previous.ProvidersList()
	if s.previous.Providers == "" {
		var err error
		previous, err = a.connectedProviders(context.Background())
		if err != nil {
			return err
		}
	}
	current := store.Config{Providers: s.options.providers}
	var removed []string
	for _, provider := range previous {
		if !current.Selected(provider) {
			removed = append(removed, provider)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	ok, err := s.prompt.confirm("Cancel unsent reminders for "+strings.Join(removed, " and ")+"?", false)
	if err != nil {
		return err
	}
	if !ok {
		return errSetupCancelled
	}
	return nil
}

func (a *application) reportUnfinishedSetup(s *setupState) {
	if s.installed && !s.activated {
		fmt.Fprintln(a.errOut, "Claude's status line was installed, but the saved setup was not activated. Rerun setup to finish; the original status line backup is retained.")
	}
}

// Restore console echo even if a password read is interrupted while its goroutine exits.
func (a *application) restoreSetupTerminal() func() {
	file, ok := a.in.(*os.File)
	if !ok {
		return func() {}
	}
	state, err := term.GetState(int(file.Fd()))
	if err != nil {
		return func() {}
	}
	return func() { _ = term.Restore(int(file.Fd()), state) }
}
