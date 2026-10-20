package gmailauth

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeyringSavesLoadsAndRemoves(t *testing.T) {
	keyring.MockInit()
	store := NewKeyring("/state/one")
	if err := store.Save("ref-1", "refresh-1"); err != nil {
		t.Fatal(err)
	}
	if secret, err := store.Load("ref-1"); err != nil || secret != "refresh-1" {
		t.Fatalf("load: %q %v", secret, err)
	}
	if _, err := NewKeyring("/state/two").Load("ref-1"); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("another state directory must not see this credential, got %v", err)
	}
	if err := store.Remove("ref-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("ref-1"); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("removed credential still loads: %v", err)
	}
	if err := store.Remove("ref-1"); err != nil {
		t.Fatalf("removing an absent credential must succeed: %v", err)
	}
}

func TestUnavailableCredentialStoreIsAnExplicitError(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	t.Cleanup(keyring.MockInit)
	err := NewKeyring("/state").Save("ref", "refresh")
	if err == nil || !strings.Contains(err.Error(), "credential store is unavailable") {
		t.Fatalf("want an explicit unavailable-store error, got %v", err)
	}
}

func TestReferencesAreFreshEachTime(t *testing.T) {
	if NewReference() == NewReference() {
		t.Fatal("two authorizations must get different credential references")
	}
}
