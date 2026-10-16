package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClaudeStatusLineRecordsLimitAndSchedulesReminder(t *testing.T) {
	app, output := testApplication(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(app.run(ctx, []string{"notify", "--email", "person@example.test", "--from", "alerts@example.test"}))
	output.Reset()
	reset := time.Now().Add(2 * time.Hour).Unix()
	app.in = strings.NewReader(fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":100,"resets_at":%d},"seven_day":{"used_percentage":41.2,"resets_at":%d}}}`,
		reset, reset+86400))
	must(app.run(ctx, []string{"claude-statusline"}))
	if got := output.String(); got != "Claude 5h 100% · 7d 41%\n" {
		t.Fatalf("status line output = %q", got)
	}
	output.Reset()
	must(app.run(ctx, []string{"history", "--json"}))
	if !strings.Contains(output.String(), `"window": "claude:five_hour"`) || !strings.Contains(output.String(), `"state": "pending"`) {
		t.Fatalf("exhausted Claude window did not schedule a reminder: %s", output.String())
	}
}

func TestClaudeStatusLineStaysQuietWithoutPlanLimits(t *testing.T) {
	app, output := testApplication(t)
	app.in = strings.NewReader(`{"model":{"display_name":"Opus"}}`)
	if err := app.run(context.Background(), []string{"claude-statusline"}); err != nil || output.Len() != 0 {
		t.Fatalf("API-key sessions and fresh sessions must print nothing: err %v, output %q", err, output.String())
	}
	app.in = strings.NewReader(`not json`)
	if err := app.run(context.Background(), []string{"claude-statusline"}); err == nil {
		t.Fatal("malformed status line data must be an error")
	}
}
