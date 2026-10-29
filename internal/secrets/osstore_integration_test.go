//go:build integration_keyring

package secrets

// Real OS credential-store tests (credential-storage §8). They run only in
// CI, on a throwaway macOS keychain or a throwaway gnome-keyring under
// dbus-run-session, and need SSHELF_KEYRING_TESTS=1 as a second guard so a
// stray `-tags integration_keyring` on a developer machine skips them
// instead of writing to the login keychain. Every item lives under a random
// "sshelf-test-<hex>" service and is deleted afterwards.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

func requireKeyringCI(t *testing.T) {
	t.Helper()
	if os.Getenv("SSHELF_KEYRING_TESTS") != "1" {
		t.Skip("real keychain test: runs only in CI with SSHELF_KEYRING_TESTS=1")
	}
}

// newTestOSStore returns an OS store under a fresh random service and
// deletes everything under that service when the test ends.
func newTestOSStore(t *testing.T) Store {
	t.Helper()
	requireKeyringCI(t)
	service := "sshelf-test-" + hex.EncodeToString(randomBytes(t, 8))
	t.Cleanup(func() {
		if err := keyring.DeleteAll(service); err != nil {
			t.Errorf("cleanup of %s: %v", service, err)
		}
	})
	return NewOSStore(service)
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestOSStore_IntegrationRoundTrip(t *testing.T) {
	store := newTestOSStore(t)
	ctx := context.Background()
	ref := Ref{IdentityID: "0192f0a4-7c1e-7b3a-9d2f-3c4b5a6e7f80", Field: FieldPassword}
	value := []byte("pässwörd with spaces, 'quotes' and \"more\"\nsecond line")

	if err := store.Available(ctx); err != nil {
		t.Fatalf("Available = %v", err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
	if err := store.Set(ctx, ref, NewSecret(bytes.Clone(value))); err != nil {
		t.Fatalf("Set = %v", err)
	}
	got, err := store.Get(ctx, ref)
	if err != nil || !bytes.Equal(got.Bytes(), value) {
		t.Fatalf("Get = %v, err %v; value differs", got.Len(), err)
	}
	got.Destroy()

	if err := store.Set(ctx, ref, NewSecret([]byte("second"))); err != nil {
		t.Fatalf("overwrite Set = %v", err)
	}
	if got, err := store.Get(ctx, ref); err != nil || string(got.Bytes()) != "second" {
		t.Fatalf("Get after overwrite: err %v", err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete absent = %v, want nil", err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}

// TestSEC_CRED_003_OSStoreBoundary2800 stores the largest allowed secret,
// random bytes, under the longest account name, through the real backend
// (on macOS this is the `security -i` 4096-byte command limit).
func TestSEC_CRED_003_OSStoreBoundary2800(t *testing.T) {
	store := newTestOSStore(t)
	ctx := context.Background()
	ref := Ref{IdentityID: "0192f0a4-7c1e-7b3a-9d2f-3c4b5a6e7f80", Field: FieldKeyPassphrase}

	value := randomBytes(t, MaxSecretSize)
	if err := store.Set(ctx, ref, NewSecret(bytes.Clone(value))); err != nil {
		t.Fatalf("Set(%d bytes) = %v", MaxSecretSize, err)
	}
	got, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get = %v", err)
	}
	if !bytes.Equal(got.Bytes(), value) {
		t.Fatalf("Get returned %d bytes that differ from the %d stored", got.Len(), len(value))
	}
	got.Destroy()

	if err := store.Set(ctx, ref, NewSecret(randomBytes(t, MaxSecretSize+1))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Set(%d bytes) = %v, want ErrTooLarge", MaxSecretSize+1, err)
	}
}
