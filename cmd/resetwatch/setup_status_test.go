package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"resetwatch/internal/store"
)

func setupStatusFixture(t *testing.T) (*application, func() string) {
	t.Helper()
	app, output, _ := setupTestApplication(t, "re_fixture")
	db, err := store.Open(app.getenv("RESETWATCH_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SaveConfig(ctx, store.Config{Providers: "both", To: "fixture@example.test", From: "sender@example.test"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSnapshot(ctx, setupTestSnapshot("codex")); err != nil {
		t.Fatal(err)
	}
	return app, output.String
}

func TestSetupStatusShowsSelectedProvidersAndAwaitingClaude(t *testing.T) {
	app, output := setupStatusFixture(t)
	if err := app.run(context.Background(), []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output(), "Provider: codex") || !strings.Contains(output(), "claude: awaiting first reading.") {
		t.Fatalf("combined status did not show both selected providers: %s", output())
	}
}

func TestSetupStatusJSONPreservesSingleSnapshotContract(t *testing.T) {
	app, output := setupStatusFixture(t)
	if err := app.run(context.Background(), []string{"status", "--json"}); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output()), &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["provider"]) != `"codex"` || fields["windows"] == nil || fields["codex"] != nil {
		t.Fatal("default JSON contract changed from a single snapshot")
	}
}

func TestSetupStatusAllJSONIncludesMissingSelectedReading(t *testing.T) {
	app, output := setupStatusFixture(t)
	if err := app.run(context.Background(), []string{"status", "--all", "--json"}); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output()), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || string(fields["claude"]) != "null" || fields["codex"] == nil {
		t.Fatal("all-provider JSON did not include awaiting Claude as null")
	}
}

func TestSetupStatusRejectsAmbiguousProviderFlags(t *testing.T) {
	app, _ := setupStatusFixture(t)
	if err := app.run(context.Background(), []string{"status", "--all", "--provider", "codex"}); err == nil {
		t.Fatal("combined and explicit provider flags were accepted together")
	}
}
