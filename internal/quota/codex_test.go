package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(tests *testing.M) {
	mode := os.Getenv("RESETWATCH_QUOTA_HELPER")
	if mode != "" && len(os.Args) > 1 && os.Args[1] == "app-server" {
		serveFakeCodex(mode)
		os.Exit(0)
	}
	os.Exit(tests.Run())
}

func TestCodexReadHandshakeAndBuckets(t *testing.T) {
	provider := fakeProvider(t, "success")
	snapshot, err := provider.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshotIdentityAndPrivacy(t, snapshot)
	assertCodexQuotaBuckets(t, snapshot.Windows)
}

func assertSnapshotIdentityAndPrivacy(t *testing.T, snapshot Snapshot) {
	t.Helper()
	if snapshot.Provider != "codex" || len(snapshot.AccountID) != 64 {
		t.Fatalf("unexpected snapshot identity: %#v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "person@example.test") {
		t.Fatal("snapshot exposed the account email")
	}
}

func assertCodexQuotaBuckets(t *testing.T, windows []Window) {
	t.Helper()
	if len(windows) != 3 {
		t.Fatalf("expected all three quota windows, got %#v", windows)
	}
	if windows[0].ID != "codex:primary" || windows[0].UsedPercent != 100 {
		t.Fatalf("wrong primary window: %#v", windows[0])
	}
	if windows[1].Duration != 7*24*time.Hour || windows[2].ID != "codex_other:primary" {
		t.Fatalf("wrong secondary or additional bucket: %#v", windows)
	}
}

func TestCodexReadRejectsUnknownAndMalformedData(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{"missing", "usage windows"},
		{"incomplete", "usage is unknown"},
		{"malformed", "malformed protocol"},
		{"malformed-result", "invalid result"},
		{"null-result", "no result"},
		{"rpc-init", "RPC code -32000"},
		{"rpc-usage", "RPC code -32001"},
		{"signed-out", "signed-in ChatGPT"},
		{"api-key", "signed-in ChatGPT"},
		{"missing-email", "stable account identity"},
		{"exit", "output ended"},
	}
	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			provider := fakeProvider(t, test.mode)
			_, err := provider.Read(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if strings.Contains(err.Error(), "secret-canary") || strings.Contains(err.Error(), "person@example.test") {
				t.Fatalf("error included private server data: %v", err)
			}
		})
	}
}

func TestCodexReadTimeoutAndCancellation(t *testing.T) {
	provider := fakeProvider(t, "timeout")
	provider.Timeout = 100 * time.Millisecond
	started := time.Now()
	_, err := provider.Read(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("timeout did not stop the subprocess promptly")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = provider.Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestCodexIgnoresNullResponseID(t *testing.T) {
	provider := fakeProvider(t, "null-id")
	if _, err := provider.Read(context.Background()); err != nil {
		t.Fatalf("null-ID response was treated as the initialization reply: %v", err)
	}
}

func TestCodexReadLegacyAndZeroUsage(t *testing.T) {
	provider := fakeProvider(t, "legacy")
	snapshot, err := provider.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Windows) != 1 || snapshot.Windows[0].UsedPercent != 0 {
		t.Fatalf("explicit zero usage was not preserved: %#v", snapshot.Windows)
	}
	if snapshot.Windows[0].ID != "legacy:primary" {
		t.Fatalf("wrong legacy identifier: %s", snapshot.Windows[0].ID)
	}
}

func TestCodexBinaryRejectsShellShims(t *testing.T) {
	for _, path := range []string{"codex.cmd", "CODEX.BAT", "codex.ps1"} {
		if _, err := codexBinary(path); err == nil {
			t.Errorf("accepted shell shim %q", path)
		}
	}
	if _, err := codexBinary("/missing/resetwatch/codex-native"); err != nil {
		t.Fatal("explicit native binary should be resolved by the operating system at startup")
	}
	_, err := (Codex{Binary: "/missing/resetwatch/codex-native"}).Read(context.Background())
	if err == nil || !strings.Contains(err.Error(), "could not start") {
		t.Fatalf("expected a clear executable error, got %v", err)
	}
}

func TestValidateSnapshotRejectsInvalidValues(t *testing.T) {
	changes := map[string]func(*Snapshot){
		"provider":           func(s *Snapshot) { s.Provider = "" },
		"account":            func(s *Snapshot) { s.AccountID = "" },
		"observation":        func(s *Snapshot) { s.ObservedAt = time.Time{} },
		"windows":            func(s *Snapshot) { s.Windows = nil },
		"identifier":         func(s *Snapshot) { s.Windows[0].ID = "" },
		"duplicate":          func(s *Snapshot) { s.Windows = append(s.Windows, s.Windows[0]) },
		"negative":           func(s *Snapshot) { s.Windows[0].UsedPercent = -1 },
		"over-limit":         func(s *Snapshot) { s.Windows[0].UsedPercent = 101 },
		"nan":                func(s *Snapshot) { s.Windows[0].UsedPercent = math.NaN() },
		"infinite":           func(s *Snapshot) { s.Windows[0].UsedPercent = math.Inf(1) },
		"duration":           func(s *Snapshot) { s.Windows[0].Duration = 0 },
		"reset-missing":      func(s *Snapshot) { s.Windows[0].ResetsAt = time.Time{} },
		"reset-past":         func(s *Snapshot) { s.Windows[0].ResetsAt = s.ObservedAt.Add(-time.Second) },
		"reset-out-of-range": func(s *Snapshot) { s.Windows[0].ResetsAt = time.Unix(math.MaxInt64, 0) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			snapshot := validSnapshot()
			change(&snapshot)
			if err := ValidateSnapshot(snapshot); err == nil {
				t.Fatal("accepted invalid quota snapshot")
			}
		})
	}
	if err := ValidateSnapshot(validSnapshot()); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRejectsPartialMalformedAndConflictingBuckets(t *testing.T) {
	valid := `{"usedPercent":0,"windowDurationMins":300,"resetsAt":4102444800}`
	tests := []string{
		`{"rateLimitsByLimitId":{"codex":{"primary":{"windowDurationMins":300,"resetsAt":4102444800}}}}`,
		`{"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":null,"windowDurationMins":300,"resetsAt":4102444800}}}}`,
		`{"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":0,"windowDurationMins":9223372036854775807,"resetsAt":4102444800}}}}`,
		`{"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":0}}}}`,
		`{"rateLimitsByLimitId":{"codex":{"limitId":"other","primary":` + valid + `}}}`,
		`{"rateLimits":{"primary":` + valid + `},"rateLimitsByLimitId":{}}`,
	}
	for _, encoded := range tests {
		var response codexLimitsResponse
		if err := json.Unmarshal([]byte(encoded), &response); err != nil {
			t.Fatal(err)
		}
		if _, err := decodeSnapshot("account-hash", time.Now().UTC(), response); err == nil {
			t.Errorf("accepted invalid quota response %s", encoded)
		}
	}
}

func fakeProvider(t *testing.T, mode string) Codex {
	t.Helper()
	t.Setenv("RESETWATCH_QUOTA_HELPER", mode)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Codex{Binary: binary, Timeout: 3 * time.Second}
}

func validSnapshot() Snapshot {
	now := time.Now().UTC()
	return Snapshot{
		Provider: "codex", AccountID: "account-hash", ObservedAt: now,
		Windows: []Window{{ID: "codex:primary", UsedPercent: 0, Duration: 5 * time.Hour, ResetsAt: now.Add(time.Hour)}},
	}
}

func serveFakeCodex(mode string) {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	request := fakeRequest(decoder, "initialize")
	if fakeInitialization(mode, request, encoder) {
		return
	}
	fakeRequest(decoder, "initialized")
	request = fakeRequest(decoder, "account/read")
	fakeReply(encoder, request.ID, fakeAccount(mode))
	request = fakeRequest(decoder, "account/rateLimits/read")
	fakeUsageReply(mode, request.ID, decoder, encoder)
}

func fakeInitialization(mode string, request rpcMessage, encoder *json.Encoder) bool {
	if mode == "null-id" {
		fakeError(encoder, nil, -32601)
	}
	switch mode {
	case "timeout":
		time.Sleep(30 * time.Second)
		return true
	case "exit":
		return true
	case "malformed":
		fmt.Fprintln(os.Stdout, "invalid secret-canary")
		return true
	case "rpc-init":
		fakeError(encoder, request.ID, -32000)
		return true
	}
	fakeReply(encoder, request.ID, map[string]any{})
	return false
}

func fakeRequest(decoder *json.Decoder, method string) rpcMessage {
	var request rpcMessage
	if err := decoder.Decode(&request); err != nil || request.Method != method {
		os.Exit(41)
	}
	return request
}

func fakeAccount(mode string) map[string]any {
	account := map[string]any{"type": "chatgpt", "email": "person@example.test"}
	switch mode {
	case "signed-out":
		return map[string]any{"account": nil}
	case "api-key":
		account["type"] = "apiKey"
	case "missing-email":
		account["email"] = nil
	}
	return map[string]any{"account": account}
}

func fakeUsageReply(mode string, id *int, decoder *json.Decoder, encoder *json.Encoder) {
	switch mode {
	case "rpc-usage":
		fakeError(encoder, id, -32001)
	case "malformed-result":
		fakeReply(encoder, id, map[string]any{"rateLimitsByLimitId": "secret-canary"})
	case "null-result":
		fakeReply(encoder, id, nil)
	default:
		fakeInterleavedMessages(decoder, encoder)
		fakeReply(encoder, id, fakeLimits(mode))
	}
}

func fakeInterleavedMessages(decoder *json.Decoder, encoder *json.Encoder) {
	_ = encoder.Encode(map[string]any{"method": "account/rateLimits/updated", "params": map[string]any{}})
	_ = encoder.Encode(map[string]any{"id": 99, "result": map[string]any{}})
	_ = encoder.Encode(map[string]any{"id": 74, "method": "unsupported/server/request", "params": map[string]any{}})
	var response rpcResponse
	if err := decoder.Decode(&response); err != nil || response.Error == nil {
		os.Exit(42)
	}
}

func fakeLimits(mode string) map[string]any {
	primary := fakeWindow(100, 300)
	switch mode {
	case "missing":
		return map[string]any{}
	case "incomplete":
		delete(primary, "usedPercent")
	case "legacy":
		return map[string]any{"rateLimits": map[string]any{"primary": fakeWindow(0, 300)}}
	}
	return map[string]any{
		"rateLimits": map[string]any{"limitId": "ignored-legacy", "primary": fakeWindow(99, 15)},
		"rateLimitsByLimitId": map[string]any{
			"codex":       map[string]any{"limitId": "codex", "primary": primary, "secondary": fakeWindow(24, 7*24*60)},
			"codex_other": map[string]any{"limitId": "codex_other", "primary": fakeWindow(61, 60)},
		},
	}
}

func fakeWindow(used, duration int) map[string]any {
	return map[string]any{"usedPercent": used, "windowDurationMins": duration, "resetsAt": time.Now().Add(time.Hour).Unix()}
}

func fakeReply(encoder *json.Encoder, id *int, result any) {
	_ = encoder.Encode(map[string]any{"id": id, "result": result})
}

func fakeError(encoder *json.Encoder, id *int, code int) {
	_ = encoder.Encode(map[string]any{"id": id, "error": map[string]any{"code": code, "message": "secret-canary"}})
}
