package store

import (
	"context"
	"testing"
)

func TestConcurrentConnectionAndNotificationChangesPreserveBoth(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	connection, notifications := openTestDB(t, dir), openTestDB(t, dir)
	now := storeTime()
	for range 20 {
		must(t, notifications.ConfigureNotifications(ctx, "old@example.test", "old-sender@example.test", now))
		must(t, connection.SaveCodexBinary(ctx, "old-native-codex"))
		start, results := make(chan struct{}), make(chan error, 2)
		go func() { <-start; results <- connection.SaveCodexBinary(ctx, "new-native-codex") }()
		go func() {
			<-start
			results <- notifications.ConfigureNotifications(ctx, "new@example.test", "new-sender@example.test", now)
		}()
		close(start)
		must(t, <-results)
		must(t, <-results)
		config, err := connection.Config(ctx)
		must(t, err)
		if config.CodexBin != "new-native-codex" || config.To != "new@example.test" || config.From != "new-sender@example.test" {
			t.Fatalf("concurrent updates overwrote part of the configuration: %#v", config)
		}
	}
}
