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
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

type setupOptions struct {
	providers, binary    string
	nonInteractive, test bool
	provided             map[string]bool
}

type setupState struct {
	options                setupOptions
	previous               store.Config
	prompt                 setupPrompter
	interactive            bool
	gmail                  store.GmailAccount // the account setup will activate
	staged                 bool               // gmail's credential was saved by this run and is not active yet
	readings               []quota.Snapshot
	activated, emailTested bool
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
	s.prompt = setupPrompter{in: a.in, out: a.out, ctx: ctx}
	fmt.Fprintln(a.out, "ResetWatch setup")
	if err := a.collectSetup(&s); err != nil {
		return err
	}
	if err := a.prepareSetupProviders(ctx, &s); err != nil {
		return err
	}
	if err := a.collectSetupGmail(ctx, &s); err != nil {
		return err
	}
	if err := a.activateSetup(ctx, &s); err != nil {
		if s.staged {
			_ = a.credentials().Remove(s.gmail.Credential) // the existing connection stays as it was
		}
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
	email := flags.String("email", "", "removed; reminders go to your Gmail account")
	from := flags.String("from", "", "removed; reminders come from your Gmail account")
	flags.StringVar(&options.binary, "codex-bin", config.CodexBin, "native Codex executable path")
	flags.BoolVar(&options.nonInteractive, "non-interactive", false, "never ask questions")
	flags.BoolVar(&options.test, "test-email", false, "send one labeled test after setup")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	options.provided = make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { options.provided[f.Name] = true })
	if *email != "" || *from != "" {
		return options, errAddressFlags
	}
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

func (a *application) collectSetup(s *setupState) error {
	if err := a.selectSetupProviders(s); err != nil {
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
