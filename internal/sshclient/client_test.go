package sshclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/testutil"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }

// fakeCreds is a CredentialSource that hands out one signer and records
// every call.
type fakeCreds struct {
	signer ssh.Signer
	mu     sync.Mutex
	calls  []domain.Target
}

func (f *fakeCreds) Signers(_ context.Context, t domain.Target) ([]ssh.Signer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, t)
	return []ssh.Signer{f.signer}, nil
}

func (f *fakeCreds) Password(context.Context, domain.Target) (string, error) {
	return "", ErrNoCredential
}

func (f *fakeCreds) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// recordingPrompter records host-key prompts and answers with trust.
type recordingPrompter struct {
	trust   bool
	prompts []HostKeyPrompt
}

func (p *recordingPrompter) HostKey(_ context.Context, hp HostKeyPrompt) (bool, error) {
	p.prompts = append(p.prompts, hp)
	return p.trust, nil
}

type fixture struct {
	srv    *sshtest.Server
	creds  *fakeCreds
	target domain.Target
	kh     string // sshelf known_hosts path in a temp dir
}

func newFixture(t *testing.T, trusted bool) fixture {
	t.Helper()
	signer, err := sshtest.GenerateSigner(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("alice", signer.PublicKey()))
	kh := filepath.Join(t.TempDir(), "known_hosts")
	if trusted {
		if err := os.WriteFile(kh, []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{
		srv:    srv,
		creds:  &fakeCreds{signer: signer},
		target: domain.Target{Label: "web-prod", Hostname: srv.Host(), Port: srv.Port(), User: "alice", HasKey: true},
		kh:     kh,
	}
}

func (f fixture) config() Config {
	return Config{KnownHostsFiles: []string{f.kh, filepath.Join(filepath.Dir(f.kh), "missing_known_hosts")}}
}

func TestSEC_HOSTKEY_001_UnknownKeyCancelBeforeAuth(t *testing.T) {
	f := newFixture(t, false)
	before, _ := os.Stat(f.kh)

	for _, p := range []Prompter{nil, RejectUnknownHosts{}, &recordingPrompter{trust: false}} {
		c, err := f.config().Connect(context.Background(), f.target, f.creds, p)
		if err == nil {
			_ = c.Close()
			t.Fatalf("prompter %T: connected to a host with an unknown key", p)
		}
		if !errors.Is(err, ErrHostKeyRejected) {
			t.Fatalf("prompter %T: err = %v, want ErrHostKeyRejected", p, err)
		}
	}
	if got := f.srv.AuthAttempts(); len(got) != 0 {
		t.Fatalf("server recorded %d auth attempts, want 0: %+v", len(got), got)
	}
	if n := f.creds.callCount(); n != 0 {
		t.Fatalf("credential source called %d times before the host key was trusted", n)
	}
	if _, err := os.Stat(f.kh); before != nil || err == nil {
		t.Fatal("known_hosts was created or changed")
	}
}

func TestSEC_HOSTKEY_001_PromptCarriesFingerprint(t *testing.T) {
	f := newFixture(t, false)
	p := &recordingPrompter{trust: true}
	c, err := f.config().Connect(context.Background(), f.target, f.creds, p)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if len(p.prompts) != 1 {
		t.Fatalf("prompts = %d, want 1", len(p.prompts))
	}
	got, key := p.prompts[0], f.srv.HostKeys()[0]
	if got.Fingerprint != ssh.FingerprintSHA256(key) || got.KeyType != ssh.KeyAlgoED25519 || got.Address != f.srv.Addr() {
		t.Fatalf("prompt = %+v", got)
	}
}

func TestSEC_HOSTKEY_001_TrustedKeyConnects(t *testing.T) {
	f := newFixture(t, true)
	p := &recordingPrompter{}
	c, err := f.config().Connect(context.Background(), f.target, f.creds, p)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if len(p.prompts) != 0 {
		t.Fatal("a known host key was prompted for")
	}
}

func TestSEC_HOSTKEY_001_ChangedKeyFailsWithoutPrompt(t *testing.T) {
	f := newFixture(t, false)
	other := sshtest.New(t)
	line := knownhosts.Line([]string{knownhosts.Normalize(f.srv.Addr())}, other.HostKeys()[0])
	if err := os.WriteFile(f.kh, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &recordingPrompter{trust: true}
	if c, err := f.config().Connect(context.Background(), f.target, f.creds, p); err == nil {
		_ = c.Close()
		t.Fatal("connected despite a changed host key")
	}
	if len(p.prompts) != 0 || len(f.srv.AuthAttempts()) != 0 {
		t.Fatalf("prompts = %d, auth attempts = %d; want 0 and 0", len(p.prompts), len(f.srv.AuthAttempts()))
	}
}

func TestSEC_SESSION_004_NoLegacyAlgorithms(t *testing.T) {
	cfg := clientConfig(domain.Target{}, newAuthState(context.Background(), domain.Target{}, nil), nil)
	if cfg.Ciphers != nil || cfg.KeyExchanges != nil || cfg.MACs != nil || cfg.HostKeyAlgorithms != nil {
		t.Fatalf("client config overrides the default algorithms: %+v", cfg.Config)
	}
	legacy := func(a string) bool {
		return a == "ssh-dss" || a == "3des-cbc" || strings.HasPrefix(a, "arcfour") || a == "diffie-hellman-group1-sha1"
	}
	def := ssh.SupportedAlgorithms()
	for name, list := range map[string][]string{
		"ciphers": def.Ciphers, "kex": def.KeyExchanges, "macs": def.MACs,
		"host keys": def.HostKeys, "public keys": def.PublicKeyAuths,
	} {
		if i := slices.IndexFunc(list, legacy); i >= 0 {
			t.Errorf("default %s include legacy algorithm %s", name, list[i])
		}
	}
}

func TestARCH_SESS_001_ConnectWithoutOpenSSH(t *testing.T) {
	t.Setenv("PATH", "")
	f := newFixture(t, true)
	c, err := f.config().Connect(context.Background(), f.target, f.creds, nil)
	if err != nil {
		t.Fatalf("connect with an empty PATH: %v", err)
	}
	defer func() { _ = c.Close() }()
	s, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	out, err := s.Output("echo in-process")
	if err != nil || string(out) != "in-process\n" {
		t.Fatalf("exec output = %q, %v", out, err)
	}
}

func TestARCH_SEC_002_CredentialsOnlyViaInterface(t *testing.T) {
	// Target and Config carry no credential material.
	for _, typ := range []reflect.Type{reflect.TypeFor[domain.Target](), reflect.TypeFor[Config]()} {
		for i := range typ.NumField() {
			field := typ.Field(i)
			k := field.Type.Kind()
			bad := field.Type.Implements(reflect.TypeFor[ssh.Signer]()) ||
				(k == reflect.Slice && field.Type.Elem().Kind() == reflect.Uint8) ||
				(k != reflect.Bool && // Has* flags only say which secrets exist
					(strings.Contains(strings.ToLower(field.Name), "password") ||
						strings.Contains(strings.ToLower(field.Name), "passphrase")))
			if bad {
				t.Errorf("%s.%s can carry a credential", typ.Name(), field.Name)
			}
		}
	}

	// The only key used is the one the CredentialSource returned, and it is
	// fetched once, after the host key was verified.
	f := newFixture(t, true)
	c, err := f.config().Connect(context.Background(), f.target, f.creds, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if n := f.creds.callCount(); n != 1 {
		t.Fatalf("credential source called %d times, want 1", n)
	}
	if f.creds.calls[0] != f.target {
		t.Fatalf("credential source got %+v, want %+v", f.creds.calls[0], f.target)
	}
	got := f.srv.AuthAttempts()
	if last := got[len(got)-1]; last.Method != "publickey" || !last.Success {
		t.Fatalf("last auth attempt = %+v", last)
	}

	// Without a CredentialSource there is nothing to authenticate with.
	if c, err := f.config().Connect(context.Background(), f.target, nil, nil); err == nil {
		_ = c.Close()
		t.Fatal("connected without credentials")
	}
}

// blockingPrompter blocks until ctx ends.
type blockingPrompter struct{ asked chan struct{} }

func (p blockingPrompter) HostKey(ctx context.Context, _ HostKeyPrompt) (bool, error) {
	close(p.asked)
	<-ctx.Done()
	return false, ctx.Err()
}

func TestARCH_CONN_005_CancelClosesConnection(t *testing.T) {
	f := newFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	p := blockingPrompter{asked: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		c, err := f.config().Connect(ctx, f.target, f.creds, p)
		if c != nil {
			_ = c.Close()
		}
		errc <- err
	}()
	<-p.asked
	start := time.Now()
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Fatalf("cancel took %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Connect did not return after cancel")
	}
}
