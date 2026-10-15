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

	"resetwatch/internal/store"
)

var version = "0.1.0"

func main() { os.Exit(execute()) }

func execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app := application{out: os.Stdout, errOut: os.Stderr, getenv: os.Getenv}
	err := app.run(ctx, os.Args[1:])
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	fmt.Fprintln(os.Stderr, "resetwatch:", err)
	return 1
}

type application struct {
	out    io.Writer
	errOut io.Writer
	getenv func(string) string
	db     *store.DB
}

func (a *application) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.help(nil)
	}
	local := map[string]func([]string) error{
		"help": a.help, "--help": a.help, "-h": a.help,
		"version": a.printVersion, "--version": a.printVersion, "demo": a.demo,
	}
	if command, ok := local[args[0]]; ok {
		return command(args[1:])
	}
	return a.stateCommand(ctx, args)
}

func (a *application) stateCommand(ctx context.Context, args []string) error {
	commands := map[string]func(context.Context, []string) error{
		"connect": a.connect, "notify": a.configure, "status": a.status,
		"history": a.history, "watch": a.watch, "retry": a.retry,
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
  resetwatch connect codex [--codex-bin /path/to/codex]
  resetwatch notify --email you@example.com --from "ResetWatch <alerts@yourdomain.com>"
  resetwatch watch [--interval 1m] [--once]
  resetwatch status [--refresh] [--json]
  resetwatch history [--limit 20] [--json]
  resetwatch retry --id <full-alert-id>
  resetwatch demo
  resetwatch version

Requires Codex signed in with ChatGPT. No inference requests are made.
Set RESEND_API_KEY in the watcher environment. The sender must be verified in Resend.
Set RESETWATCH_HOME to override the private local state directory.
The watcher must remain running on an awake, connected computer.
Emails distinguish scheduled resets from freshly confirmed allowance.`)
	return nil
}
