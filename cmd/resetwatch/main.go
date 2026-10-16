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

	"resetwatch/internal/notify"
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
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
	getenv     func(string) string
	db         *store.DB
	testSender notify.Sender
}

func (a *application) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.help(nil)
	}
	if args[0] == "test-email" {
		return a.testEmail(ctx, args[1:])
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
		"claude-statusline": a.claudeStatusLine,
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
  resetwatch claude-statusline   (set as your Claude Code status line command)
  resetwatch notify --email you@example.com --from "ResetWatch <alerts@yourdomain.com>"
  resetwatch test-email --email your-resend-account-email [--from SENDER]
  resetwatch watch [--interval 1m] [--once]
  resetwatch status [--refresh] [--json]
  resetwatch history [--limit 20] [--json]
  resetwatch retry --id <full-alert-id>
  resetwatch demo
  resetwatch version

Quota commands require Codex signed in with ChatGPT. No inference requests are made.
Claude Pro/Max limits are read from Claude Code's status line data while Claude Code is open.
Save RESEND_API_KEY in .env in your current folder, or set it in the environment.
An existing environment key takes priority. The sender must be verified in Resend.
test-email sends one labeled test immediately without a Codex connection.
Its default resend.dev sender can send only to your Resend account email.
Set RESETWATCH_HOME to override the private local state directory.
The watcher must remain running on an awake, connected computer.
Emails distinguish scheduled resets from freshly confirmed allowance.`)
	return nil
}
