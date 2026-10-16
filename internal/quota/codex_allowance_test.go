package quota

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeSnapshotPreservesBackendAllowance(t *testing.T) {
	tests := []struct{ name, field, want string }{
		{"allowed", `"ordinaryUsageAllowed":true,`, "true"},
		{"denied", `"ordinaryUsageAllowed":false,`, "false"},
		{"null", `"ordinaryUsageAllowed":null,`, ""},
		{"missing", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := `{` + test.field + `"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":2,"windowDurationMins":300,"resetsAt":4102444800}}}}`
			var response codexLimitsResponse
			if err := json.Unmarshal([]byte(encoded), &response); err != nil {
				t.Fatal(err)
			}
			snapshot, err := decodeSnapshot("synthetic-account", time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC), response)
			if err != nil {
				t.Fatal(err)
			}
			assertSerializedAllowance(t, snapshot, test.want)
		})
	}
}

func assertSerializedAllowance(t *testing.T, snapshot Snapshot, want string) {
	t.Helper()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if got := string(fields["ordinary_usage_allowed"]); got != want {
		t.Fatalf("serialized backend allowance = %q, want %q", got, want)
	}
}
