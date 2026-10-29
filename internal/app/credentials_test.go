package app

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Test secrets. They are made up, and no test prints them.
const (
	testPassword   = "canary-password-7f3a"
	testPassphrase = "canary-passphrase-91c2" //nolint:gosec // a made-up test secret
)

// recStore is a MemStore that records every Get and keeps the buffer of
// every Secret it returned, so tests can check that it was zeroed.
type recStore struct {
	*secrets.MemStore
	mu   sync.Mutex
	refs []secrets.Ref
	bufs [][]byte
}

func newRecStore() *recStore { return &recStore{MemStore: secrets.NewMemStore()} }

func (r *recStore) Get(ctx context.Context, ref secrets.Ref) (secrets.Secret, error) {
	s, err := r.MemStore.Get(ctx, ref)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs = append(r.refs, ref)
	if err == nil {
		r.bufs = append(r.bufs, s.Bytes())
	}
	return s, err
}

func (r *recStore) gets() []secrets.Ref {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.refs)
}

func (r *recStore) put(t *testing.T, id string, f secrets.Field, value []byte) {
	t.Helper()
	if err := r.Set(context.Background(), secrets.Ref{IdentityID: id, Field: f}, secrets.NewSecret(value)); err != nil {
		t.Fatal(err)
	}
}

// newStoredKey returns a new Ed25519 private key in OpenSSH PEM form,
// encrypted when passphrase is not empty, and its public key.
func newStoredKey(t *testing.T, passphrase string) ([]byte, ssh.PublicKey) {
	t.Helper()
	k, err := sshtest.GenerateKey(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(k, "stored")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(k, "stored", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block), signer.PublicKey()
}

// credApp writes config.json (identities and hosts as JSON arrays, with
// %[1]q and %[2]d standing for the server's host and port) and a
// known_hosts that trusts every given server, and returns a loaded App on
// store.
func credApp(t *testing.T, store secrets.Store, srvs []*sshtest.Server, identities, hosts string) *App {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sshelf")
	if _, err := paths.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	var kh []byte
	for _, s := range srvs {
		kh = append(kh, s.KnownHostsLine()+"\n"...)
	}
	if err := os.WriteFile(paths.KnownHostsFile(dir), kh, 0o600); err != nil {
		t.Fatal(err)
	}
	var args []any
	for _, s := range srvs {
		args = append(args, s.Host(), s.Port())
	}
	cfg := fmt.Sprintf(`{"version": 1, "identities": `+identities+`, "hosts": `+hosts+`}`, args...)
	if err := os.WriteFile(paths.ConfigFile(dir), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Config{
		Dir:             dir,
		KnownHostsFiles: []string{paths.KnownHostsFile(dir)},
		Secrets:         store,
		Home:            t.TempDir(),
		Terminal:        stillTerm{},
	})
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a
}

// cancelPrompts makes App refuse every prompt, and returns how many
// were shown.
func cancelPrompts(a *App) *atomic.Int32 {
	var n atomic.Int32
	a.SetSend(func(msg any) {
		n.Add(1)
		switch m := msg.(type) {
		case AuthPromptMsg:
			m.Cancel()
		case HostKeyPromptMsg:
			m.Answer(false)
		}
	})
	return &n
}

// failOnPrompt makes any prompt fail the test (and cancels it).
func failOnPrompt(t *testing.T, a *App) {
	a.SetSend(func(msg any) {
		t.Errorf("unexpected prompt %T", msg)
		if m, ok := msg.(AuthPromptMsg); ok {
			m.Cancel()
		}
		if m, ok := msg.(HostKeyPromptMsg); ok {
			m.Answer(false)
		}
	})
}

func connectOK(t *testing.T, a *App, hostID string) {
	t.Helper()
	s, err := a.Connect(context.Background(), domain.ID(hostID))
	if err != nil {
		t.Fatalf("connect %s: %v", hostID, err)
	}
	_ = s.Close()
}

func methods(attempts []sshtest.AuthAttempt) []string {
	var out []string
	for _, a := range attempts {
		m := a.Method
		if a.Success {
			m += "+"
		}
		out = append(out, m)
	}
	return out
}

const oneHost = `[{"id": "h1", "label": "web-prod", "hostname": %[1]q, "port": %[2]d, "user": "deploy", "identity_id": "i1"}]`

func TestFR_CONN_002_KeyWithoutPrompt(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%v", encrypted), func(t *testing.T) {
			pass := ""
			if encrypted {
				pass = testPassphrase
			}
			key, pub := newStoredKey(t, pass)
			srv := sshtest.New(t, sshtest.WithAuthorizedKey("deploy", pub))
			store := newRecStore()
			store.put(t, "i1", secrets.FieldPrivateKey, key)
			if encrypted {
				store.put(t, "i1", secrets.FieldKeyPassphrase, []byte(pass))
			}
			a := credApp(t, store, []*sshtest.Server{srv},
				fmt.Sprintf(`[{"id": "i1", "name": "ops", "has_private_key": true, "has_key_passphrase": %v}]`, encrypted), oneHost)
			failOnPrompt(t, a)
			connectOK(t, a, "h1")
			if got := methods(srv.AuthAttempts()); !slices.Equal(got, []string{"none", "publickey+"}) {
				t.Fatalf("auth attempts = %v", got)
			}
		})
	}
}

func TestFR_CONN_002_KeyBeforePassword(t *testing.T) {
	key, pub := newStoredKey(t, "")
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("deploy", pub), sshtest.WithPassword("deploy", testPassword))
	store := newRecStore()
	store.put(t, "i1", secrets.FieldPrivateKey, key)
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	a := credApp(t, store, []*sshtest.Server{srv},
		`[{"id": "i1", "name": "ops", "has_private_key": true, "has_password": true}]`, oneHost)
	failOnPrompt(t, a)
	connectOK(t, a, "h1")
	if got := methods(srv.AuthAttempts()); !slices.Equal(got, []string{"none", "publickey+"}) {
		t.Fatalf("auth attempts = %v, want the key used and no password sent", got)
	}
	if got := store.gets(); len(got) != 1 || got[0].Field != secrets.FieldPrivateKey {
		t.Fatalf("store reads = %v, want only the key", got)
	}
}

func TestFR_CONN_002_SkipsUnofferedMethods(t *testing.T) {
	key, _ := newStoredKey(t, "")
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword)) // no public-key auth
	store := newRecStore()
	store.put(t, "i1", secrets.FieldPrivateKey, key)
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	a := credApp(t, store, []*sshtest.Server{srv},
		`[{"id": "i1", "name": "ops", "has_private_key": true, "has_password": true}]`, oneHost)
	failOnPrompt(t, a)
	connectOK(t, a, "h1")
	if got := methods(srv.AuthAttempts()); !slices.Equal(got, []string{"none", "password+"}) {
		t.Fatalf("auth attempts = %v, want publickey skipped", got)
	}
	if got := store.gets(); len(got) != 1 || got[0].Field != secrets.FieldPassword {
		t.Fatalf("store reads = %v, want only the password (the key is never read)", got)
	}
}

func TestSEC_MEM_009_NoGetAtStartup(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := newRecStore()
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	credApp(t, store, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "has_password": true}]`, oneHost)
	if n := store.Calls(secrets.OpGet); n != 0 {
		t.Fatalf("%d Get calls at startup, want 0", n)
	}
}

// storedEverything returns a server that accepts only the password, and a
// store holding an encrypted key the server does not accept, its
// passphrase and the password: every field is needed once.
func storedEverything(t *testing.T) (*sshtest.Server, *recStore, *App) {
	t.Helper()
	key, _ := newStoredKey(t, testPassphrase)
	other, err := sshtest.GenerateSigner(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("deploy", other.PublicKey()), sshtest.WithPassword("deploy", testPassword))
	store := newRecStore()
	store.put(t, "i1", secrets.FieldPrivateKey, key)
	store.put(t, "i1", secrets.FieldKeyPassphrase, []byte(testPassphrase))
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	a := credApp(t, store, []*sshtest.Server{srv},
		`[{"id": "i1", "name": "ops", "has_private_key": true, "has_key_passphrase": true, "has_password": true}]`, oneHost)
	failOnPrompt(t, a)
	return srv, store, a
}

func TestSEC_MEM_009_OneGetPerFieldPerConnect(t *testing.T) {
	srv, store, a := storedEverything(t)
	for i := 1; i <= 2; i++ {
		connectOK(t, a, "h1")
		count := map[secrets.Field]int{}
		for _, r := range store.gets() {
			count[r.Field]++
		}
		for _, f := range []secrets.Field{secrets.FieldPrivateKey, secrets.FieldKeyPassphrase, secrets.FieldPassword} {
			if count[f] != i {
				t.Fatalf("after connect %d: %d Get calls for %s, want %d", i, count[f], f, i)
			}
		}
	}
	if got := methods(srv.AuthAttempts()); !slices.Contains(got, "password+") || !slices.Contains(got, "publickey") {
		t.Fatalf("auth attempts = %v, want the key tried and the password accepted", got)
	}
}

func TestSEC_MEM_009_BuffersZeroedAfterAuth(t *testing.T) {
	_, store, a := storedEverything(t)
	connectOK(t, a, "h1")
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.bufs) != 3 {
		t.Fatalf("%d secrets read, want 3", len(store.bufs))
	}
	for i, b := range store.bufs {
		if len(b) == 0 {
			t.Fatalf("secret %d was empty", i)
		}
		for _, c := range b {
			if c != 0 {
				t.Fatalf("secret %d (%s) was not zeroed after auth", i, store.refs[i].Field)
			}
		}
	}
}

func TestSEC_SESSION_001_NoGetOnRejectedHostKey(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := newRecStore()
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	a := credApp(t, store, nil, `[{"id": "i1", "name": "ops", "has_password": true}]`,
		fmt.Sprintf(`[{"id": "h1", "label": "web-prod", "hostname": %q, "port": %d, "user": "deploy", "identity_id": "i1"}]`, srv.Host(), srv.Port()))
	// No SetSend: the unknown host key is rejected without a prompt.
	if s, err := a.Connect(context.Background(), "h1"); !errors.Is(err, ErrHostKeyRejected) {
		if s != nil {
			_ = s.Close()
		}
		t.Fatalf("err = %v, want ErrHostKeyRejected", err)
	}
	if n := store.Calls(secrets.OpGet); n != 0 {
		t.Fatalf("%d Get calls before the host key was trusted", n)
	}
	if n := len(srv.AuthAttempts()); n != 0 {
		t.Fatalf("%d auth attempts", n)
	}
}

func TestSEC_SESSION_001_PasswordNotSentToOtherHost(t *testing.T) {
	// Server B would accept host A's password; host B's identity has its
	// own password. Connecting to B must read and send only B's.
	srvA := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	srvB := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := newRecStore()
	store.put(t, "ia", secrets.FieldPassword, []byte(testPassword))
	store.put(t, "ib", secrets.FieldPassword, []byte("canary-other-password"))
	a := credApp(t, store, []*sshtest.Server{srvA, srvB},
		`[{"id": "ia", "name": "a", "has_password": true}, {"id": "ib", "name": "b", "has_password": true}]`,
		`[{"id": "ha", "label": "a", "hostname": %[1]q, "port": %[2]d, "user": "deploy", "identity_id": "ia"},
		  {"id": "hb", "label": "b", "hostname": %[3]q, "port": %[4]d, "user": "deploy", "identity_id": "ib"}]`)
	cancelPrompts(a) // B rejects its password, then asks the user: cancelled
	if s, err := a.Connect(context.Background(), "hb"); err == nil {
		_ = s.Close()
		t.Fatal("host B accepted a password it was never given")
	}
	for _, r := range store.gets() {
		if r.IdentityID != "ib" {
			t.Fatalf("connecting to B read %s/%s", r.IdentityID, r.Field)
		}
	}
	for _, at := range srvB.AuthAttempts() {
		if at.Success {
			t.Fatalf("auth succeeded on B: %+v", at)
		}
	}
	connectOK(t, a, "ha")
}

func TestFR_CRED_001_ConnectsAfterRestartWithoutPrompt(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := newRecStore() // outlives both "processes", as the OS store does
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	for run := 1; run <= 2; run++ {
		a := credApp(t, store, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "has_password": true}]`, oneHost)
		failOnPrompt(t, a)
		connectOK(t, a, "h1")
	}
	if got := methods(srv.AuthAttempts()); !slices.Equal(got, []string{"none", "password+", "none", "password+"}) {
		t.Fatalf("auth attempts = %v", got)
	}
}
