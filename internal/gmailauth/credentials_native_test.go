package gmailauth

import (
	"errors"
	"os"
	"testing"
)

// TestNativeCredentialStore round-trips one disposable entry through the real system credential store.
// It is opt-in because it touches the person's store: RESETWATCH_NATIVE_KEYRING_TEST=1 go test ./internal/gmailauth/
func TestNativeCredentialStore(t *testing.T) {
	if os.Getenv("RESETWATCH_NATIVE_KEYRING_TEST") != "1" {
		t.Skip("set RESETWATCH_NATIVE_KEYRING_TEST=1 to use the real credential store")
	}
	store := NewKeyring(t.TempDir())
	reference := "disposable-" + NewReference()
	t.Cleanup(func() { _ = store.Remove(reference) })
	if err := store.Save(reference, "not-a-real-token"); err != nil {
		t.Fatal(err)
	}
	if secret, err := store.Load(reference); err != nil || secret != "not-a-real-token" {
		t.Fatalf("native round trip failed: %v", err)
	}
	if err := store.Remove(reference); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(reference); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("the disposable entry was not removed: %v", err)
	}
}
