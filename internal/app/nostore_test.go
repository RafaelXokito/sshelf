package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// deadStore is a credential store that is never available, as on Linux
// without a Secret Service (D-101). It counts the calls it refused.
type deadStore struct{ calls atomic.Int32 }

func (d *deadStore) fail() error { d.calls.Add(1); return secrets.ErrUnavailable }

func (d *deadStore) Get(context.Context, secrets.Ref) (secrets.Secret, error) {
	return secrets.Secret{}, d.fail()
}
func (d *deadStore) Set(context.Context, secrets.Ref, secrets.Secret) error { return d.fail() }
func (d *deadStore) Delete(context.Context, secrets.Ref) error              { return d.fail() }
func (d *deadStore) Available(context.Context) error                        { return d.fail() }

// answerAllPrompts answers every authentication prompt with answer and
// records its notes.
func answerAllPrompts(a *App, answer string) func() []string {
	var (
		mu    sync.Mutex
		notes []string
	)
	a.SetSend(func(msg any) {
		if m, ok := msg.(AuthPromptMsg); ok {
			mu.Lock()
			notes = append(notes, m.Note)
			mu.Unlock()
			m.Answer([]string{answer})
		}
		if m, ok := msg.(HostKeyPromptMsg); ok {
			m.Answer(false)
		}
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(notes)
	}
}

func TestFR_CRED_002_SaveRefusedWithMessage(t *testing.T) {
	for name, store := range map[string]secrets.Store{"dead store": &deadStore{}, "no store": nil} {
		a, dir := storeApp(t, store)
		if err := a.SecretsAvailable(context.Background()); !errors.Is(err, secrets.ErrUnavailable) {
			t.Fatalf("%s: probe = %v", name, err)
		}
		_, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "ops", Password: secretOf("pw")})
		se, ok := errors.AsType[*StoreError](err)
		if !ok || !errors.Is(err, secrets.ErrUnavailable) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if want := "No Secret Service found: install or start GNOME Keyring or KWallet"; StoreName() == "Secret Service" && se.Explain() != want {
			t.Fatalf("%s: explain %q", name, se.Explain())
		}
		if _, err := os.Stat(paths.ConfigFile(dir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: config.json written: %v", name, err)
		}
		// A key-file identity still saves.
		if _, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "ops", KeyPath: "~/.ssh/id"}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Explain names the store of each OS.
	if got := (&StoreError{Store: "Keychain", Err: secrets.ErrUnavailable}).Explain(); !strings.HasPrefix(got, "Keychain is not available") {
		t.Fatalf("macOS explain %q", got)
	}
	// A working store passes the probe.
	if err := New(Config{Secrets: secrets.NewMemStore()}).SecretsAvailable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFR_CRED_002_KeyFileHostConnects(t *testing.T) {
	store := &deadStore{}
	a := credApp(t, store, nil, `[]`, `[]`)
	keyPath := filepath.Join(a.cfg.Home, ".ssh", "id_test")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(keyPath, sshtest.Ed25519, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("deploy", pub))
	if err := os.WriteFile(a.cfg.KnownHostsFiles[0], []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved, err := a.SaveHostWithCredentials(context.Background(),
		domain.Host{Label: "web", Hostname: srv.Host(), Port: srv.Port(), User: "deploy"},
		HostCredentials{KeyPath: "~/.ssh/id_test"})
	if err != nil {
		t.Fatal(err)
	}
	failOnPrompt(t, a)
	connectOK(t, a, string(saved.Host.ID))
	if n := store.calls.Load(); n != 0 {
		t.Fatalf("%d store calls for a key-file host", n)
	}
}

func TestFR_CRED_002_UnreadableSecretFallsBackToPrompt(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := &deadStore{}
	a := credApp(t, store, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "has_password": true, "has_private_key": true}]`, oneHost)
	notes := answerAllPrompts(a, testPassword)
	connectOK(t, a, "h1")
	got := notes()
	if len(got) != 1 || !strings.Contains(got[0], StoreName()+" is not available") || !strings.Contains(got[0], "not saved") {
		t.Fatalf("prompt notes = %q", got)
	}
	// Nothing was cached: the next connect asks again.
	connectOK(t, a, "h1")
	if len(notes()) != 2 {
		t.Fatalf("prompts = %d", len(notes()))
	}
}

func TestSEC_CRED_005_NoCanaryInAnyFile(t *testing.T) {
	const (
		pw   = "canary-nostore-pw-5150"   //nolint:gosec // a made-up test secret
		pass = "canary-nostore-pass-8a8a" //nolint:gosec // a made-up test secret
	)
	srv := sshtest.New(t, sshtest.WithPassword("deploy", pw))
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	a := credApp(t, &deadStore{}, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "has_password": true}]`, oneHost)
	ctx := context.Background()
	_, err1 := a.SaveIdentity(ctx, IdentityInput{Name: "x", Password: secretOf(pw), Passphrase: secretOf(pass)})
	_, err2 := a.SaveHostWithCredentials(ctx, domain.Host{Label: "y", Hostname: "h"}, HostCredentials{Password: secretOf(pw)})
	if !errors.Is(err1, secrets.ErrUnavailable) || !errors.Is(err2, secrets.ErrUnavailable) {
		t.Fatalf("saves: %v, %v", err1, err2)
	}
	answerAllPrompts(a, pw)
	connectOK(t, a, "h1")
	for _, dir := range []string{a.cfg.Dir, a.cfg.Home, tmp} {
		noCanaryUnder(t, dir, pw, pass)
	}
	cfg, err := os.ReadFile(paths.ConfigFile(a.cfg.Dir))
	if err != nil || bytes.Contains(cfg, []byte(`"name": "x"`)) {
		t.Fatalf("config.json: %v\n%s", err, cfg)
	}
}
