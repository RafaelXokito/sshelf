package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The canary harness (T-028: SEC-CRED-001, SEC-LOG-001, ARCH-SEC-001,
// FR-IDENT-004). Every canary is a made-up secret that sshelf is given
// through the credential store, a form or a prompt, and that must never
// show up anywhere else. Failures name the canary and where it was found,
// never its value.

// canary is one planted secret.
type canary struct {
	name  string
	value string
}

// canaries is the set of secrets planted in one run.
type canaries []canary

// keyCanaries returns the canaries of a stored private key: every line of
// its PEM body, so that any fragment of the key that reaches a sink is a
// hit.
func keyCanaries(name string, pemKey []byte) canaries {
	var out canaries
	for i, line := range strings.Split(string(pemKey), "\n") {
		if len(line) >= 16 && !strings.HasPrefix(line, "-----") {
			out = append(out, canary{name: fmt.Sprintf("%s line %d", name, i), value: line})
		}
	}
	return out
}

// newCanaryKey returns a new Ed25519 private key in OpenSSH PEM form,
// encrypted with passphrase, and its public key.
func newCanaryKey(t *testing.T, passphrase string) ([]byte, ssh.PublicKey) {
	t.Helper()
	k, err := sshtest.GenerateKey(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(k, "canary", []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block), signer.PublicKey()
}

// hits returns "<where> contains <canary>" for every canary in b. A
// canary is also found when the renderer split it with cursor movements or
// whitespace, through screenText.
func (cs canaries) hits(where string, b []byte) []string {
	var out []string
	var text string
	if bytes.IndexByte(b, 0x1b) >= 0 {
		text = screenText(b)
	}
	for _, c := range cs {
		if bytes.Contains(b, []byte(c.value)) || (text != "" && strings.Contains(text, c.value)) {
			out = append(out, where+" contains "+c.name)
		}
	}
	return out
}

// fileHits byte-searches every regular file under each root. It also
// returns how many files it read, so that a scan of nothing is noticed.
func (cs canaries) fileHits(t *testing.T, roots ...string) (hits []string, files int) {
	t.Helper()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			b, err := os.ReadFile(p) //nolint:gosec // a test temp dir
			if err != nil {
				return err
			}
			files++
			hits = append(hits, cs.hits(p, b)...)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return hits, files
}

// envHits checks the environment of this process.
func (cs canaries) envHits() []string {
	return cs.hits("os.Environ()", []byte(strings.Join(os.Environ(), "\n")))
}

// TestARCH_SEC_001_CanaryScan is the in-process ARCH-SEC-001 acceptance:
// with debug logging on and a MemStore, it saves a host with a canary
// password and one with a canary key and passphrase, connects both, edits
// and saves one, writes a crash report for a panic whose value is a
// canary, then scans the config dir, the log dir, HOME, the temp dir and
// os.Environ().
func TestARCH_SEC_001_CanaryScan(t *testing.T) {
	const (
		pw   = "CANARY-arch-pw-6c1f0e"   //nolint:gosec // a made-up test secret
		pass = "CANARY-arch-pass-29d7b4" //nolint:gosec // a made-up test secret
	)
	pemKey, pub := newCanaryKey(t, pass)
	cs := append(canaries{{"password", pw}, {"passphrase", pass}}, keyCanaries("private key", pemKey)...)

	home, state, tmp := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", tmp)
	closeLog := useLogging(t, "1", state)

	srv := sshtest.New(t, sshtest.WithPassword("alice", pw), sshtest.WithAuthorizedKey("carol", pub))
	cfgDir := filepath.Join(t.TempDir(), "sshelf")
	if _, err := paths.EnsureDir(cfgDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.KnownHostsFile(cfgDir), []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := secrets.NewMemStore()
	a := app.New(app.Config{
		Dir:             cfgDir,
		KnownHostsFiles: paths.KnownHostsFiles(cfgDir, home),
		Secrets:         store,
		Home:            home,
	})
	ctx := context.Background()
	if _, err := a.Load(ctx); err != nil {
		t.Fatal(err)
	}

	pwHost, err := a.SaveHostWithCredentials(ctx,
		domain.Host{Label: "pw", Hostname: srv.Host(), Port: srv.Port(), User: "alice"},
		app.HostCredentials{Password: app.NewSecret([]byte(pw))})
	if err != nil {
		t.Fatal(err)
	}
	keyHost, err := a.SaveHostWithCredentials(ctx,
		domain.Host{Label: "key", Hostname: srv.Host(), Port: srv.Port(), User: "carol"},
		app.HostCredentials{Key: app.NewSecret(bytes.Clone(pemKey)), Passphrase: app.NewSecret([]byte(pass))})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(store.Keys()); n != 3 {
		t.Fatalf("store holds %d items, want 3 (password, key, passphrase)", n)
	}
	for _, h := range []domain.Host{pwHost.Host, keyHost.Host} {
		sess, err := a.Connect(ctx, h.ID, app.Size{Cols: 80, Rows: 22})
		if err != nil {
			t.Fatalf("connect %s: %v", h.Label, err)
		}
		if err := sess.Close(); err != nil {
			t.Fatal(err)
		}
		<-sess.Done()
	}
	edited := pwHost.Host
	edited.Label = "pw-renamed"
	if _, err := a.SaveHost(ctx, edited); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	crash := crashHandler{stateDir: state, stdout: &stdout, stderr: &stderr}
	if code := crash.crash(fmt.Errorf("auth with %q failed", pw), debug.Stack()); code != exitCrash {
		t.Fatalf("crash exit status %d", code)
	}
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}

	log, err := os.ReadFile(paths.LogFile(state))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"method=password", "method=publickey", `msg="connect: authenticated"`, `msg=panic`} {
		if !bytes.Contains(log, []byte(want)) {
			t.Errorf("the log lacks %s, so the scan proves little", want)
		}
	}
	hits, files := cs.fileHits(t, cfgDir, state, home, tmp)
	if files < 4 { // config.json, known_hosts, the log, the crash report
		t.Errorf("scanned only %d files", files)
	}
	hits = append(hits, cs.envHits()...)
	hits = append(hits, cs.hits("stdout", stdout.Bytes())...)
	hits = append(hits, cs.hits("stderr", stderr.Bytes())...)
	for _, h := range hits {
		t.Error(h)
	}
}
