//go:build integration_keyring && linux

package secrets

import (
	"context"
	"errors"
	"os"
	"testing"
)

// TestSEC_CRED_005_NoSessionBusIsUnavailable runs in its own CI job, in a
// container with no D-Bus session bus, no dbus-launch and no Secret Service
// (SSHELF_KEYRING_NO_BUS=1). Every call must report ErrUnavailable (D-101).
func TestSEC_CRED_005_NoSessionBusIsUnavailable(t *testing.T) {
	requireKeyringCI(t)
	if os.Getenv("SSHELF_KEYRING_NO_BUS") != "1" {
		t.Skip("runs only in the no-session-bus CI job")
	}
	if addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); addr != "" {
		t.Fatalf("DBUS_SESSION_BUS_ADDRESS is set (%q); the job must run without a session bus", addr)
	}
	store := NewOSStore("sshelf-test-nobus")
	ctx := context.Background()
	ref := Ref{IdentityID: "0192f0a4-7c1e-7b3a-9d2f-3c4b5a6e7f80", Field: FieldPassword}

	if err := store.Available(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Available = %v, want ErrUnavailable", err)
	}
	if err := store.Set(ctx, ref, NewSecret([]byte("pw"))); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Set = %v, want ErrUnavailable", err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Get = %v, want ErrUnavailable", err)
	}
	if err := store.Delete(ctx, ref); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Delete = %v, want ErrUnavailable", err)
	}
}
