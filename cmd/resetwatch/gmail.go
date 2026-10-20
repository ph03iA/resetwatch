package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"resetwatch/internal/gmailauth"
	"resetwatch/internal/notify"
	"resetwatch/internal/store"
)

func (a *application) gmail(ctx context.Context, args []string) error {
	commands := map[string]func(context.Context, []string) error{
		"connect": a.gmailConnect, "status": a.gmailStatus, "disconnect": a.gmailDisconnect,
	}
	if len(args) == 0 || commands[args[0]] == nil {
		return errors.New("use resetwatch gmail connect, gmail status or gmail disconnect")
	}
	return commands[args[0]](ctx, args[1:])
}

func (a *application) gmailConnect(ctx context.Context, args []string) error {
	flags := a.flags("gmail connect")
	noBrowser := flags.Bool("no-browser", false, "print the sign-in link instead of opening a browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := noArguments(flags.Args()); err != nil {
		return err
	}
	previous, err := a.db.Config(ctx)
	if err != nil {
		return err
	}
	account, err := a.stageGmail(ctx, *noBrowser)
	if err != nil {
		return err
	}
	if err := a.db.ConnectGmail(ctx, account, time.Now().UTC()); err != nil {
		_ = a.credentials().Remove(account.Credential)
		return err
	}
	a.dropReplacedCredential(previous.Gmail, account)
	fmt.Fprintln(a.out, "Gmail connected:", account.Address)
	fmt.Fprintln(a.out, "Reminders are sent from this account to itself. A running watcher uses it on its next cycle.")
	return nil
}

// stageGmail signs in and saves the refresh token under a fresh reference. Nothing is activated yet,
// so a failure afterwards leaves any existing Gmail connection working.
func (a *application) stageGmail(ctx context.Context, noBrowser bool) (store.GmailAccount, error) {
	grant, err := a.signInGmail(ctx, noBrowser)
	if err != nil {
		return store.GmailAccount{}, err
	}
	reference := gmailauth.NewReference()
	if err := a.credentials().Save(reference, grant.RefreshToken); err != nil {
		return store.GmailAccount{}, err
	}
	return store.GmailAccount{Address: grant.Address, Subject: grant.Subject, Credential: reference}, nil
}

func (a *application) signInGmail(ctx context.Context, noBrowser bool) (gmailauth.Grant, error) {
	if a.gmailSignIn != nil {
		return a.gmailSignIn(ctx, noBrowser)
	}
	client, err := gmailauth.DefaultClient()
	if err != nil {
		return gmailauth.Grant{}, err
	}
	flow := gmailauth.Flow{Client: client, Out: a.out}
	if !noBrowser {
		flow.Open = gmailauth.OpenBrowser
	}
	return flow.Authorize(ctx)
}

// dropReplacedCredential deletes the previous refresh token once the new connection is active.
func (a *application) dropReplacedCredential(previous, current store.GmailAccount) {
	if previous.Credential != "" && previous.Credential != current.Credential {
		_ = a.credentials().Remove(previous.Credential)
	}
}

func (a *application) credentials() gmailauth.Store {
	if a.gmailStore != nil {
		return a.gmailStore
	}
	dir, _ := stateDirectory(a.getenv("RESETWATCH_HOME"))
	return gmailauth.NewKeyring(dir)
}

type gmailReport struct {
	Connected        bool   `json:"connected"`
	Address          string `json:"address,omitempty"`
	Paused           string `json:"paused,omitempty"`
	CredentialStored bool   `json:"credential_stored"`
}

// gmailStatus reports what this computer knows; it does not ask Google whether the sign-in still works.
func (a *application) gmailStatus(ctx context.Context, args []string) error {
	flags := a.flags("gmail status")
	asJSON := flags.Bool("json", false, "machine-readable output")
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
	report := gmailReport{Connected: config.Gmail.Connected(), Address: config.Gmail.Address, Paused: config.Gmail.Paused}
	if report.Connected {
		_, loadErr := a.credentials().Load(config.Gmail.Credential)
		report.CredentialStored = loadErr == nil
	}
	if *asJSON {
		return writeJSON(a.out, report)
	}
	a.printGmailReport(report)
	return nil
}

func (a *application) printGmailReport(report gmailReport) {
	if !report.Connected {
		fmt.Fprintln(a.out, "Gmail: not connected. Run resetwatch gmail connect.")
		return
	}
	fmt.Fprintln(a.out, "Gmail: connected as", report.Address)
	switch {
	case report.Paused != "":
		fmt.Fprintln(a.out, "Delivery: paused ("+report.Paused+"). Run resetwatch gmail connect.")
	case !report.CredentialStored:
		fmt.Fprintln(a.out, "Delivery: the sign-in is missing from the system credential store. Run resetwatch gmail connect.")
	default:
		fmt.Fprintln(a.out, "Sign-in: saved in the system credential store (not checked with Google just now).")
	}
}

func (a *application) gmailDisconnect(ctx context.Context, args []string) error {
	flags := a.flags("gmail disconnect")
	yes := flags.Bool("yes", false, "disconnect without asking")
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
	if !config.Gmail.Connected() {
		fmt.Fprintln(a.out, "Gmail is not connected.")
		return nil
	}
	if err := a.confirmDisconnect(ctx, *yes); err != nil {
		return err
	}
	if err := a.db.DisconnectGmail(ctx, time.Now().UTC()); err != nil {
		return err
	}
	if err := a.credentials().Remove(config.Gmail.Credential); err != nil {
		return fmt.Errorf("Gmail is disconnected, but %w", err)
	}
	fmt.Fprintln(a.out, "Gmail disconnected; unsent reminders were cancelled.")
	fmt.Fprintln(a.out, "To withdraw ResetWatch's access from Google too, remove it at https://myaccount.google.com/permissions")
	return nil
}

func (a *application) confirmDisconnect(ctx context.Context, yes bool) error {
	if yes {
		return nil
	}
	if !a.setupIsTerminal(false) {
		return errors.New("add --yes to disconnect Gmail without a prompt")
	}
	prompt := setupPrompter{in: a.in, out: a.out, ctx: ctx}
	ok, err := prompt.confirm("Disconnect Gmail and cancel unsent reminders?", false)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("Gmail stays connected")
	}
	return nil
}

// mailer resolves the configured Gmail account to a sender and checks its credential first, so a dead
// sign-in pauses delivery before any reminder is claimed. Access tokens are cached per credential.
func (a *application) mailer(ctx context.Context, config store.Config) (notify.Sender, error) {
	if a.testSender != nil {
		return a.testSender, nil
	}
	account := config.Gmail
	tokens, err := a.tokenSource(account.Credential)
	if err != nil {
		return nil, err
	}
	token := func(context.Context) (string, error) {
		current, err := tokens.Token()
		if gmailauth.Revoked(err) {
			return "", &notify.AuthorizationError{Credential: account.Credential, Reason: "Google withdrew the saved Gmail sign-in"}
		}
		if err != nil {
			return "", err
		}
		return current.AccessToken, nil
	}
	if _, err := token(ctx); err != nil {
		return nil, err
	}
	return notify.Gmail{Address: account.Address, Credential: account.Credential, Token: token}, nil
}

func (a *application) tokenSource(credential string) (oauth2.TokenSource, error) {
	if tokens, ok := a.tokens[credential]; ok {
		return tokens, nil
	}
	client, err := gmailauth.DefaultClient()
	if err != nil {
		return nil, err
	}
	refresh, err := a.credentials().Load(credential)
	if errors.Is(err, gmailauth.ErrNoCredential) {
		return nil, &notify.AuthorizationError{Credential: credential, Reason: "the Gmail sign-in is missing from the system credential store"}
	}
	if err != nil {
		return nil, err
	}
	if a.tokens == nil {
		a.tokens = map[string]oauth2.TokenSource{}
	}
	a.tokens[credential] = client.Tokens(refresh)
	return a.tokens[credential], nil
}
