package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"resetwatch/internal/gmailauth"
	"resetwatch/internal/store"
)

// memoryCredentials stands in for the system credential store.
type memoryCredentials struct {
	secrets map[string]string
	err     error
}

func newMemoryCredentials() *memoryCredentials {
	return &memoryCredentials{secrets: map[string]string{}}
}

func (m *memoryCredentials) Save(reference, secret string) error {
	if m.err != nil {
		return m.err
	}
	m.secrets[reference] = secret
	return nil
}

func (m *memoryCredentials) Load(reference string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	secret, ok := m.secrets[reference]
	if !ok {
		return "", gmailauth.ErrNoCredential
	}
	return secret, nil
}

func (m *memoryCredentials) Remove(reference string) error {
	delete(m.secrets, reference)
	return nil
}

// fakeGoogle answers sign-ins as the named account, counting them; err makes the next sign-in fail.
type fakeGoogle struct {
	name      string
	err       error
	signIns   int
	noBrowser bool
}

func (g *fakeGoogle) signIn(_ context.Context, noBrowser bool) (gmailauth.Grant, error) {
	g.signIns++
	g.noBrowser = noBrowser
	if g.err != nil {
		return gmailauth.Grant{}, g.err
	}
	return gmailauth.Grant{Account: gmailauth.Account{Address: g.name + "@example.test", Subject: "google-" + g.name}, RefreshToken: "refresh-" + g.name}, nil
}

// useFakeGmail gives app an in-memory credential store and a fake Google that signs in as name.
func useFakeGmail(app *application, name string) (*memoryCredentials, *fakeGoogle) {
	credentials, google := newMemoryCredentials(), &fakeGoogle{name: name}
	app.gmailStore, app.gmailSignIn = credentials, google.signIn
	return credentials, google
}

func savedGmail(t *testing.T, app *application) store.GmailAccount {
	t.Helper()
	return connectSavedConfig(t, app).Gmail
}

func TestGmailConnectStagesThenActivates(t *testing.T) {
	app, output := testApplication(t)
	credentials, google := useFakeGmail(app, "person")
	runOK(t, app, "gmail", "connect", "--no-browser")
	account := savedGmail(t, app)
	if account.Address != "person@example.test" || account.Subject != "google-person" || !google.noBrowser {
		t.Fatalf("unexpected connection: %+v (no-browser %v)", account, google.noBrowser)
	}
	if credentials.secrets[account.Credential] != "refresh-person" || len(credentials.secrets) != 1 {
		t.Fatalf("the refresh token must be saved under the configured reference only: %v", credentials.secrets)
	}
	if strings.Contains(output.String(), "refresh-person") {
		t.Fatal("the refresh token was printed")
	}
	google.name = "other"
	runOK(t, app, "gmail", "connect")
	if replaced := savedGmail(t, app); replaced.Address != "other@example.test" || len(credentials.secrets) != 1 || credentials.secrets[replaced.Credential] != "refresh-other" {
		t.Fatalf("changing accounts must replace the saved credential: %+v %v", replaced, credentials.secrets)
	}
}

func TestGmailConnectFailureKeepsTheExistingConnection(t *testing.T) {
	app, _ := testApplication(t)
	credentials, google := useFakeGmail(app, "person")
	runOK(t, app, "gmail", "connect")
	before := savedGmail(t, app)
	google.err = gmailauth.ErrDenied
	if err := app.run(context.Background(), []string{"gmail", "connect"}); !errors.Is(err, gmailauth.ErrDenied) {
		t.Fatalf("want the sign-in failure, got %v", err)
	}
	google.err, google.name = nil, "other"
	credentials.err = errors.New("the system credential store is unavailable")
	if err := app.run(context.Background(), []string{"gmail", "connect"}); err == nil {
		t.Fatal("an unavailable credential store must fail the connection")
	}
	credentials.err = nil
	if after := savedGmail(t, app); after != before || credentials.secrets[before.Credential] != "refresh-person" {
		t.Fatalf("a failed connection changed the working one: %+v", after)
	}
}

func TestGmailStatusReportsLocalState(t *testing.T) {
	app, output := testApplication(t)
	credentials, _ := useFakeGmail(app, "person")
	runOK(t, app, "gmail", "status")
	if !strings.Contains(app.out.(*strings.Builder).String(), "not connected") {
		t.Fatal("status must say Gmail is not connected")
	}
	runOK(t, app, "gmail", "connect")
	runOK(t, app, "gmail", "status", "--json")
	var report gmailReport
	if err := json.Unmarshal([]byte(app.out.(*strings.Builder).String()), &report); err != nil || !report.Connected || !report.CredentialStored || report.Address != "person@example.test" {
		t.Fatalf("unexpected report: %+v %v", report, err)
	}
	credentials.secrets = map[string]string{}
	runOK(t, app, "gmail", "status")
	if !strings.Contains(app.out.(*strings.Builder).String(), "missing from the system credential store") {
		t.Fatal("status must report a missing credential")
	}
	if strings.Contains(output.String(), "refresh-") {
		t.Fatal("status printed a token")
	}
}

func TestGmailDisconnectCancelsRemindersAndDeletesTheCredential(t *testing.T) {
	app, _ := testApplication(t)
	credentials, _ := useFakeGmail(app, "person")
	runOK(t, app, "gmail", "connect")
	reset := time.Now().Add(time.Hour).Unix()
	claudeLine(t, app, "", map[string][2]int64{"five_hour": {100, reset}})
	if err := app.run(context.Background(), []string{"gmail", "disconnect"}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("a noninteractive disconnect must ask for --yes, got %v", err)
	}
	runOK(t, app, "gmail", "disconnect", "--yes")
	if savedGmail(t, app).Connected() || len(credentials.secrets) != 0 {
		t.Fatal("disconnect must clear the connection and delete the credential")
	}
	if alerts := claudeAlerts(t, app); len(alerts) != 1 || alerts[0].State != "cancelled" {
		t.Fatalf("unsent reminders must be cancelled: %+v", alerts)
	}
}

func TestWatchRefusesLegacyStateUntilGmailUpgrade(t *testing.T) {
	app, _ := testApplication(t)
	useFakeGmail(app, "person")
	raw, err := sql.Open("sqlite", filepath.Join(app.getenv("RESETWATCH_HOME"), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
 CREATE TABLE snapshots (provider TEXT PRIMARY KEY, data TEXT NOT NULL);
 CREATE TABLE alerts (id TEXT PRIMARY KEY, provider TEXT NOT NULL, account_id TEXT NOT NULL, window_id TEXT NOT NULL,
  reset_at INTEGER NOT NULL, recipient TEXT NOT NULL, sender TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0, first_attempt INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL,
  lease_until INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL, payload TEXT NOT NULL DEFAULT '');
 INSERT INTO settings VALUES('config','{"email":"old@example.test","from":"sender@example.test","providers":"claude"}');
 PRAGMA user_version = 1;`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := app.run(context.Background(), []string{"watch", "--once"}); !errors.Is(err, store.ErrUpgradeRequired) {
		t.Fatalf("legacy state must ask for the Gmail upgrade, got %v", err)
	}
	runOK(t, app, "gmail", "connect")
	config := connectSavedConfig(t, app)
	if !config.Gmail.Connected() || config.Providers != "claude" || config.LegacyTo != "" {
		t.Fatalf("the upgrade must keep providers and drop the Resend settings: %+v", config)
	}
}
