package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/oauth2"
	"resetwatch/internal/gmailauth"
	"resetwatch/internal/notify"
	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

var version = "0.1.0"

func main() { os.Exit(execute()) }

func execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app := application{in: os.Stdin, out: os.Stdout, errOut: os.Stderr, getenv: os.Getenv}
	err := app.run(ctx, os.Args[1:])
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	fmt.Fprintln(os.Stderr, "resetwatch:", err)
	return 1
}

type application struct {
	in             io.Reader
	out            io.Writer
	errOut         io.Writer
	getenv         func(string) string
	db             *store.DB
	testSender     notify.Sender // tests replace the Gmail sender
	setupTerminal  func() bool
	setupReadCodex func(context.Context, string) (quota.Snapshot, error)
	claude         quota.Provider                                       // tests replace the Claude usage reader
	gmailStore     gmailauth.Store                                      // tests replace the system credential store
	gmailSignIn    func(context.Context, bool) (gmailauth.Grant, error) // tests replace the Google sign-in
	tokens         map[string]oauth2.TokenSource                        // access tokens per credential, in memory only
}

func (a *application) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.help(nil)
	}
	local := map[string]func([]string) error{
		"help": a.help, "--help": a.help, "-h": a.help,
		"version": a.printVersion, "--version": a.printVersion,
		"claude-statusline": a.claudeStatusLine,
	}
	if command, ok := local[args[0]]; ok {
		return command(args[1:])
	}
	return a.stateCommand(ctx, args)
}

func (a *application) stateCommand(ctx context.Context, args []string) error {
	commands := map[string]func(context.Context, []string) error{
		"setup": a.setup, "watch": a.watch, "status": a.status, "history": a.history,
		"gmail": a.gmail, "test-email": a.testEmail,
	}
	command, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q; run resetwatch help", args[0])
	}
	dir, err := stateDirectory(a.getenv("RESETWATCH_HOME"))
	if err != nil {
		return err
	}
	a.db, err = store.Open(dir)
	if err != nil {
		return err
	}
	defer a.db.Close()
	return command(ctx, args[1:])
}

func stateDirectory(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "resetwatch"), nil
}

func (a *application) flags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(a.errOut)
	return flags
}

func noArguments(args []string) error {
	if len(args) != 0 {
		return errors.New("unexpected arguments; use --help for this command")
	}
	return nil
}

func (a *application) printVersion(args []string) error {
	if err := noArguments(args); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "resetwatch", version)
	return nil
}

func (a *application) help(args []string) error {
	if err := noArguments(args); err != nil {
		return err
	}
	fmt.Fprintln(a.out, `ResetWatch — leave the clock to us.

Usage:
  resetwatch setup [--providers codex|claude|both] [--codex-bin PATH] [--test-email] [--non-interactive]
  resetwatch gmail connect [--no-browser]
  resetwatch gmail status [--json]
  resetwatch gmail disconnect [--yes]
  resetwatch test-email
  resetwatch watch [--interval 1m] [--once]
  resetwatch status [--provider codex|claude | --all] [--refresh] [--json]
  resetwatch history [--limit 20] [--json]
  resetwatch version

Run setup for guided provider and Gmail configuration; the watcher remains a separate command.
Reminders are sent from your connected Gmail account to that same account. ResetWatch asks Google
only for permission to send email; it never reads your inbox. The sign-in is kept in your system
credential store (Windows Credential Manager, macOS Keychain or Linux Secret Service).
Codex quota commands require Codex signed in with ChatGPT. No inference requests are made.
Claude Pro/Max limits are read from Claude Code (claude on PATH) without sending any prompt.
test-email sends one labeled test to your Gmail account without checking AI usage.
Set RESETWATCH_HOME to override the private local state directory.
Provider and Gmail changes apply on the next watcher cycle.
The watcher must remain running on an awake, connected computer.
Emails distinguish scheduled resets from freshly confirmed allowance.`)
	return nil
}
