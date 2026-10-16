package quota_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"resetwatch/internal/quota"
	"resetwatch/internal/store"
)

func TestSnapshotStoragePreservesBackendAllowance(t *testing.T) {
	allowed, denied := true, false
	tests := []struct {
		name      string
		allowance *bool
	}{{"allowed", &allowed}, {"denied", &denied}, {"unknown", nil}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			db, err := store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			now := time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)
			snapshot := quota.Snapshot{Provider: "codex", AccountID: "synthetic-account", ObservedAt: now,
				OrdinaryUsageAllowed: test.allowance,
				Windows:              []quota.Window{{ID: "codex:primary", UsedPercent: 2, Duration: 5 * time.Hour, ResetsAt: now.Add(time.Hour)}}}
			if err := db.SaveSnapshot(ctx, snapshot); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = store.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			loaded, err := db.Snapshot(ctx, "codex")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(loaded, snapshot) {
				t.Fatalf("persisted snapshot changed backend allowance: got %#v, want %#v", loaded, snapshot)
			}
		})
	}
}
