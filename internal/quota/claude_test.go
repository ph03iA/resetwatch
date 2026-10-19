package quota

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

var claudeNow = time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)

func claudeFixture(email string, available bool, five, week string) (claudeInitResponse, claudeUsageResponse) {
	var init claudeInitResponse
	var usage claudeUsageResponse
	_ = json.Unmarshal([]byte(fmt.Sprintf(`{"account":{"email":%q,"subscriptionType":"Claude Pro"}}`, email)), &init)
	_ = json.Unmarshal([]byte(fmt.Sprintf(`{"rate_limits_available":%v,"rate_limits":{"five_hour":%s,"seven_day":%s}}`, available, five, week)), &usage)
	return init, usage
}

func TestDecodeClaudeUsage(t *testing.T) {
	init, usage := claudeFixture("Person@Example.test", true,
		`{"utilization":100,"resets_at":"2030-01-02T12:39:59.793609+00:00"}`,
		`{"utilization":25,"resets_at":"2030-01-08T22:00:00.188573+00:00"}`)
	snapshot, err := decodeClaudeUsage(init, usage, claudeNow)
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{Provider: "claude", AccountID: snapshot.AccountID, ObservedAt: claudeNow, Partial: true, Windows: []Window{
		{ID: "claude:five_hour", UsedPercent: 100, ResetsAt: time.Date(2030, 1, 2, 12, 40, 0, 0, time.UTC), Duration: 5 * time.Hour},
		{ID: "claude:seven_day", UsedPercent: 25, ResetsAt: time.Date(2030, 1, 8, 22, 0, 0, 0, time.UTC), Duration: 7 * 24 * time.Hour},
	}}
	if len(snapshot.AccountID) != 64 || !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("unexpected snapshot (reset times must round to the minute): %#v", snapshot)
	}
}

func TestClaudeAccountIdentityIsPrivateAndCaseInsensitive(t *testing.T) {
	window := `{"utilization":1,"resets_at":"2030-01-02T12:40:00Z"}`
	upper, usage := claudeFixture("Person@Example.test", true, window, `null`)
	lower, _ := claudeFixture("person@example.test", true, window, `null`)
	first, err := decodeClaudeUsage(upper, usage, claudeNow)
	if err != nil {
		t.Fatal(err)
	}
	second, err := decodeClaudeUsage(lower, usage, claudeNow)
	encoded, _ := json.Marshal(first)
	if err != nil || first.AccountID != second.AccountID || strings.Contains(strings.ToLower(string(encoded)), "person@example.test") {
		t.Fatalf("account identity must ignore email case and never store the email: %s %v", encoded, err)
	}
}

// Claude recomputes resets_at with sub-second drift; both values must land on the same reminder.
func TestClaudeResetDriftKeepsOneWindow(t *testing.T) {
	first, _ := claudeWindowAt("w", time.Hour, claudeTestWindow(100, "2030-01-02T12:39:59.793609Z"), claudeNow)
	second, _ := claudeWindowAt("w", time.Hour, claudeTestWindow(100, "2030-01-02T12:40:00.188552Z"), claudeNow)
	if !first.ResetsAt.Equal(second.ResetsAt) || first.ResetsAt.Unix() != second.ResetsAt.Unix() {
		t.Fatalf("drifting reset times split into %s and %s", first.ResetsAt, second.ResetsAt)
	}
}

func claudeTestWindow(used float64, reset string) *claudeUsageWindow {
	return &claudeUsageWindow{Utilization: &used, ResetsAt: &reset}
}

func TestDecodeClaudeUsageRejectsMissingDataAndSkipsUnknownWindows(t *testing.T) {
	valid := `{"utilization":10,"resets_at":"2030-01-02T12:40:00Z"}`
	cases := map[string]struct {
		email          string
		available      bool
		five, week     string
		noPlanLimits   bool
		acceptsWindows int
	}{
		"not signed in":        {"", true, valid, valid, false, 0},
		"no plan limits":       {"p@example.test", false, valid, valid, true, 0},
		"only unknown windows": {"p@example.test", true, `{"utilization":null,"resets_at":null}`, `null`, true, 0},
		"expired window":       {"p@example.test", true, `{"utilization":100,"resets_at":"2030-01-02T09:00:00Z"}`, valid, false, 1},
		"usage over 100":       {"p@example.test", true, `{"utilization":140,"resets_at":"2030-01-02T12:40:00Z"}`, `null`, false, 0},
	}
	for name, test := range cases {
		init, usage := claudeFixture(test.email, test.available, test.five, test.week)
		snapshot, err := decodeClaudeUsage(init, usage, claudeNow)
		if test.acceptsWindows > 0 {
			if err != nil || len(snapshot.Windows) != test.acceptsWindows {
				t.Errorf("%s: kept %d windows, err %v", name, len(snapshot.Windows), err)
			}
			continue
		}
		if err == nil || errors.Is(err, ErrNoClaudeLimits) != test.noPlanLimits {
			t.Errorf("%s: unexpected result %v", name, err)
		}
	}
}

func TestClaudeReadSpeaksTheControlProtocol(t *testing.T) {
	snapshot, err := fakeClaude(t, "success").Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Windows) != 2 || snapshot.Windows[0].UsedPercent != 42 {
		t.Fatalf("unexpected reading: %#v", snapshot)
	}
	if _, err := fakeClaude(t, "usage-error").Read(context.Background()); err == nil || !strings.Contains(err.Error(), "get_usage") {
		t.Fatalf("a failed get_usage must surface as an error: %v", err)
	}
	if _, err := fakeClaude(t, "exit").Read(context.Background()); err == nil {
		t.Fatal("Claude exiting early must be an error")
	}
}

func TestClaudeBinaryRejectsShellShims(t *testing.T) {
	for _, name := range []string{`C:\npm\claude.cmd`, "claude.ps1", "claude.bat"} {
		if _, err := claudeBinary(name); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func fakeClaude(t *testing.T, mode string) Claude {
	t.Helper()
	t.Setenv("RESETWATCH_CLAUDE_HELPER", mode)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Claude{Binary: binary, Timeout: 3 * time.Second}
}

// serveFakeClaude answers control requests the way Claude Code's stream-json mode does,
// interleaving unrelated stream messages that the reader must skip.
func serveFakeClaude(mode string) {
	if mode == "exit" {
		return
	}
	reset := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	input := bufio.NewScanner(os.Stdin)
	for input.Scan() {
		var request struct {
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
		}
		if json.Unmarshal(input.Bytes(), &request) != nil {
			continue
		}
		fmt.Println(`{"type":"system","subtype":"init"}`)
		switch {
		case request.Request.Subtype == "initialize":
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"account":{"email":"person@example.test"}}}}`+"\n", request.RequestID)
		case mode == "usage-error":
			fmt.Printf(`{"type":"control_response","response":{"subtype":"error","request_id":%q,"error":"unsupported"}}`+"\n", request.RequestID)
		default:
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"rate_limits_available":true,"rate_limits":{"five_hour":{"utilization":42,"resets_at":%q},"seven_day":{"utilization":10,"resets_at":%q}}}}}`+"\n",
				request.RequestID, reset, reset)
		}
	}
}
