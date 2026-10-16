package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestResendRetryPreservesWirePayloadAcrossLocalZones(t *testing.T) {
	originalZone := time.Local
	t.Cleanup(func() { time.Local = originalZone })
	message := sampleMessage()
	service := Resend{APIKey: "re_fake_test_key"}
	time.Local = time.FixedZone("TestEast", 5*3600+30*60)
	firstBody, firstKey := resendRequestData(t, service, message)

	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	time.Local = time.FixedZone("TestWest", -8*3600)
	retryBody, retryKey := resendRequestData(t, service, restored)
	if firstKey == "" || firstKey != retryKey {
		t.Fatalf("persisted retry changed its idempotency key: %q, %q", firstKey, retryKey)
	}
	if !bytes.Equal(firstBody, retryBody) {
		t.Fatal("persisted retry reused its idempotency key with different email JSON after a local time zone change")
	}
	var payload emailPayload
	if err := json.Unmarshal(retryBody, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.Text, "Reset at 3:00 PM UTC") || !strings.Contains(payload.HTML, "2026-10-15 15:00:00 UTC") {
		t.Fatal("email reset details must use fixed UTC in both alternatives")
	}
}

func resendRequestData(t *testing.T, service Resend, message Message) ([]byte, string) {
	t.Helper()
	request, err := service.request(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body, request.Header.Get("Idempotency-Key")
}

func TestFrozenLocalResetTimeSurvivesZoneChange(t *testing.T) {
	originalZone := time.Local
	t.Cleanup(func() { time.Local = originalZone })
	message := sampleMessage()
	message.LocalResetAt = "8:30 PM IST" // fixed when the watcher first froze the message
	time.Local = time.FixedZone("TestWest", -8*3600)
	body, _ := resendRequestData(t, Resend{APIKey: "re_fake_test_key"}, message)
	var payload emailPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.Text, "Reset at 8:30 PM IST") || !strings.Contains(payload.HTML, "Reset at 8:30 PM IST") {
		t.Fatalf("frozen local reset time was not used: %s", payload.Text)
	}
}
