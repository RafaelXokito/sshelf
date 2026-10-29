//go:build sshelf_faultinject

package main

import (
	"context"
	"os"

	"github.com/RafaelXokito/sshelf/internal/secrets"
)

// newSecretStore returns the credential store of a fault-injection build,
// which only the E2E tests run: an empty in-memory store, so that they
// never touch the OS credential store. SSHELF_FAULT_STORE=denied makes every
// read, write and delete fail with secrets.ErrDenied, and
// SSHELF_FAULT_STORE=unavailable makes every call, the startup probe
// included, fail with secrets.ErrUnavailable (SEC-LOG-001 canary harness).
func newSecretStore() secrets.Store {
	switch os.Getenv("SSHELF_FAULT_STORE") {
	case "denied":
		return failingStore{err: secrets.ErrDenied}
	case "unavailable":
		return failingStore{err: secrets.ErrUnavailable, probe: secrets.ErrUnavailable}
	}
	return secrets.NewMemStore()
}

// failingStore fails every call with err, and Available with probe.
type failingStore struct{ err, probe error }

func (f failingStore) Get(context.Context, secrets.Ref) (secrets.Secret, error) {
	return secrets.Secret{}, f.err
}

func (f failingStore) Set(context.Context, secrets.Ref, secrets.Secret) error { return f.err }

func (f failingStore) Delete(context.Context, secrets.Ref) error { return f.err }

func (f failingStore) Available(context.Context) error { return f.probe }
