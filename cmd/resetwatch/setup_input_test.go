package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestSetupPromptsDefaultsAndReprompt(t *testing.T) {
	var output bytes.Buffer
	prompt := setupPrompter{in: strings.NewReader("\r\nwrong\nYES\n9\n0\n2\n"), out: &output}
	answer, err := prompt.ask("Email", "fixture@example.test")
	if err != nil || answer != "fixture@example.test" {
		t.Fatalf("blank completed line did not use default: %v", err)
	}
	yes, err := prompt.confirm("Test email", false)
	if err != nil || !yes {
		t.Fatalf("confirmation did not reprompt: %v", err)
	}
	index, err := prompt.choose("Providers", []string{"Codex", "Claude", "Both"}, 2)
	if err != nil || index != 1 {
		t.Fatalf("selection did not reprompt: index=%d err=%v", index, err)
	}
	if !strings.Contains(output.String(), "Choose 1-3: ") {
		t.Fatal("choice prompt is missing")
	}
	for _, suggestion := range []string{"fixture@example.test", "[3]", "[n]"} {
		if strings.Contains(output.String(), suggestion) {
			t.Fatalf("prompt exposed a suggested value: %q", suggestion)
		}
	}
}

func TestSetupPromptsEOFDoesNotAcceptDefaults(t *testing.T) {
	for _, input := range []string{"", "incomplete", "\x03", "\x04"} {
		prompt := setupPrompter{in: strings.NewReader(input), out: io.Discard}
		if _, err := prompt.ask("Email", "fixture@example.test"); !errors.Is(err, errSetupCancelled) {
			t.Fatalf("EOF/control input accepted a default: %v", err)
		}
	}
}

func TestSetupInputBoundsAndErrors(t *testing.T) {
	value, err := readSetupLine(strings.NewReader(strings.Repeat("x", maxSetupInput) + "\n"))
	if err != nil || len(value) != maxSetupInput {
		t.Fatalf("bounded input rejected: %v", err)
	}
	if _, err := readSetupLine(strings.NewReader(strings.Repeat("x", maxSetupInput) + "\r\n")); err != nil {
		t.Fatalf("bounded CRLF input rejected: %v", err)
	}
	if _, err := readSetupLine(strings.NewReader(strings.Repeat("x", maxSetupInput+1) + "\n")); err == nil {
		t.Fatal("oversize input accepted")
	}
	if _, err := readSetupLine(setupFailureReader{}); err == nil || strings.Contains(err.Error(), "fixture_secret") {
		t.Fatal("read failure accepted or leaked")
	}
}

func TestSetupChoicesRejectInvalidDefaults(t *testing.T) {
	prompt := setupPrompter{in: strings.NewReader("\n"), out: io.Discard}
	if _, err := prompt.choose("Providers", nil, 0); err == nil {
		t.Fatal("empty choices accepted")
	}
	if _, err := prompt.choose("Providers", []string{"Codex"}, 1); err == nil {
		t.Fatal("invalid default accepted")
	}
}

func TestSetupPromptWriteErrorsAreRedacted(t *testing.T) {
	prompt := setupPrompter{in: strings.NewReader("\n"), out: setupFailureWriter{}}
	_, err := prompt.ask("Email", "")
	if err == nil || strings.Contains(err.Error(), "fixture_secret") {
		t.Fatal("prompt write failure accepted or leaked")
	}
}

type setupFailureWriter struct{}

func (setupFailureWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture_secret write failure")
}

type setupFailureReader struct{}

func (setupFailureReader) Read([]byte) (int, error) {
	return 0, errors.New("fixture_secret read failure")
}

func TestSetupPromptContextCancelsBlockedRead(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	started := make(chan struct{})
	input := setupStartedReader{Reader: reader, started: started}
	prompt := setupPrompter{ctx: ctx, in: &input, out: io.Discard}
	result := make(chan error, 1)
	go func() {
		_, err := prompt.ask("Email", "fixture@example.test")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt did not start reading")
	}
	cancel()
	assertSetupCancelled(t, result)
}

func TestSetupPromptCancelledContextDoesNotRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prompt := setupPrompter{ctx: ctx, in: setupFailureReader{}, out: io.Discard}
	if _, err := prompt.ask("Next", ""); !errors.Is(err, errSetupCancelled) {
		t.Fatalf("already canceled context was ignored: %v", err)
	}
}

func assertSetupCancelled(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if !errors.Is(err, errSetupCancelled) {
			t.Fatalf("context did not cancel setup: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("context cancellation did not unblock prompt")
	}
}

type setupStartedReader struct {
	io.Reader
	started chan struct{}
	read    bool
}

func (reader *setupStartedReader) Read(content []byte) (int, error) {
	if !reader.read {
		reader.read = true
		close(reader.started)
	}
	return reader.Reader.Read(content)
}
