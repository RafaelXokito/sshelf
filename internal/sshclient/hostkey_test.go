package sshclient

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// trustSetup is an sshtest server with sshelf's known_hosts (kh) and a
// fake ~/.ssh/known_hosts (openssh), both empty until written.
type trustSetup struct {
	fixture
	openssh string
}

func newTrustSetup(t *testing.T, opts ...sshtest.Option) trustSetup {
	t.Helper()
	signer, err := sshtest.GenerateSigner(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, append([]sshtest.Option{sshtest.WithAuthorizedKey("alice", signer.PublicKey())}, opts...)...)
	dir := t.TempDir()
	return trustSetup{
		fixture: fixture{
			srv:    srv,
			creds:  &fakeCreds{signer: signer},
			target: domain.Target{Label: "web-prod", Hostname: srv.Host(), Port: srv.Port(), User: "alice"},
			kh:     filepath.Join(dir, "sshelf_known_hosts"),
		},
		openssh: filepath.Join(dir, "openssh_known_hosts"),
	}
}

// write writes lines to path.
func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeOpenSSH writes the fake ~/.ssh/known_hosts and checks, when the
// test ends, that it is byte-identical (FR-KH-003).
func (s trustSetup) writeOpenSSH(t *testing.T, lines ...string) {
	t.Helper()
	write(t, s.openssh, lines...)
	before, _ := os.ReadFile(s.openssh) //nolint:gosec // test temp file
	t.Cleanup(func() {
		if after, _ := os.ReadFile(s.openssh); !bytes.Equal(before, after) { //nolint:gosec // test temp file
			t.Error("the OpenSSH known_hosts fixture changed")
		}
	})
}

func (s trustSetup) connect(t *testing.T, p Prompter) error {
	t.Helper()
	cfg := Config{KnownHostsFiles: []string{s.kh, s.openssh}}
	c, err := cfg.Connect(context.Background(), s.target, s.creds, p)
	if c != nil {
		_ = c.Close()
	}
	return err
}

// line returns a known_hosts line for the server's address and key.
func (s trustSetup) line(key ssh.PublicKey) string {
	return knownhosts.Line([]string{knownhosts.Normalize(s.srv.Addr())}, key)
}

// keyField is the "<type> <base64>" part of a known_hosts line.
func keyField(key ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

func otherKey(t *testing.T, kind string) ssh.PublicKey {
	t.Helper()
	s, err := sshtest.GenerateSigner(kind)
	if err != nil {
		t.Fatal(err)
	}
	return s.PublicKey()
}

// noAuth checks that nothing was authenticated, loaded or prompted.
func (s trustSetup) noAuth(t *testing.T, p *recordingPrompter) {
	t.Helper()
	if n := len(s.srv.AuthAttempts()); n != 0 {
		t.Fatalf("%d auth attempts", n)
	}
	if n := s.creds.callCount(); n != 0 {
		t.Fatalf("credential source called %d times", n)
	}
	if len(p.prompts) != 0 {
		t.Fatalf("prompted: %+v", p.prompts)
	}
}

func TestFR_KH_002_ChangedKeyBlocks(t *testing.T) {
	s := newTrustSetup(t)
	stale := otherKey(t, sshtest.Ed25519)
	write(t, s.kh, s.line(stale))
	p := &recordingPrompter{trust: true}
	err := s.connect(t, p)
	if !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("err = %v, want ErrHostKeyMismatch", err)
	}
	me, _ := errors.AsType[*HostKeyMismatchError](err)
	if me.Fingerprint != ssh.FingerprintSHA256(s.srv.HostKeys()[0]) || me.KeyType != ssh.KeyAlgoED25519 ||
		len(me.Stale) != 1 || me.Stale[0].Fingerprint != ssh.FingerprintSHA256(stale) ||
		me.Name != "[127.0.0.1]:"+strconv.Itoa(s.srv.Port()) {
		t.Fatalf("mismatch = %+v", me)
	}
	s.noAuth(t, p)
}

func TestFR_KH_002_ShowsPathAndLine(t *testing.T) {
	for _, inOpenSSH := range []bool{false, true} {
		t.Run(map[bool]string{false: "sshelf", true: "openssh"}[inOpenSSH], func(t *testing.T) {
			s := newTrustSetup(t)
			lines := []string{
				"# trusted hosts",
				knownhosts.Line([]string{"a.example.com"}, otherKey(t, sshtest.Ed25519)),
				"",
				knownhosts.Line([]string{"b.example.com"}, otherKey(t, sshtest.ECDSA)),
				s.line(otherKey(t, sshtest.Ed25519)), // line 5
			}
			path := s.kh
			if inOpenSSH {
				path = s.openssh
				s.writeOpenSSH(t, lines...)
			} else {
				write(t, s.kh, lines...)
			}
			err := s.connect(t, &recordingPrompter{trust: true})
			me, ok := errors.AsType[*HostKeyMismatchError](err)
			if !ok || len(me.Stale) != 1 || me.Stale[0].File != path || me.Stale[0].Line != 5 {
				t.Fatalf("err = %#v", err)
			}
			if !strings.Contains(err.Error(), path+" line 5") {
				t.Fatalf("Error() = %q", err)
			}
		})
	}
}

func TestSEC_HOSTKEY_005_ZeroAuthAttempts(t *testing.T) {
	s := newTrustSetup(t)
	s.writeOpenSSH(t, s.line(otherKey(t, sshtest.Ed25519)))
	p := &recordingPrompter{trust: true}
	for range 2 {
		if err := s.connect(t, p); !errors.Is(err, ErrHostKeyMismatch) {
			t.Fatalf("err = %v", err)
		}
	}
	s.noAuth(t, p)
}

func TestSEC_HOSTKEY_005_DeletedLineThenTofu(t *testing.T) {
	s := newTrustSetup(t)
	write(t, s.kh, knownhosts.Line([]string{"keep.example.com"}, otherKey(t, sshtest.Ed25519)), s.line(otherKey(t, sshtest.Ed25519)))
	err := s.connect(t, &recordingPrompter{})
	me, ok := errors.AsType[*HostKeyMismatchError](err)
	if !ok {
		t.Fatalf("err = %v", err)
	}
	// The user follows the instructions: delete line N of <path>.
	b, _ := os.ReadFile(me.Stale[0].File) //nolint:gosec // test temp file
	lines := strings.Split(string(b), "\n")
	lines = slices.Delete(lines, me.Stale[0].Line-1, me.Stale[0].Line)
	if err := os.WriteFile(me.Stale[0].File, []byte(strings.Join(lines, "\n")), 0o600); err != nil { //nolint:gosec // a test temp file
		t.Fatal(err)
	}
	p := &recordingPrompter{trust: false}
	if err := s.connect(t, p); !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("after deleting the line: err = %v", err)
	}
	if len(p.prompts) != 1 {
		t.Fatalf("prompts = %d, want a fresh TOFU prompt", len(p.prompts))
	}
}

func TestFR_KH_003_HashedEntryTrusted(t *testing.T) {
	s := newTrustSetup(t)
	hashed := knownhosts.Line([]string{knownhosts.HashHostname(knownhosts.Normalize(s.srv.Addr()))}, s.srv.HostKeys()[0])
	if !strings.HasPrefix(hashed, "|1|") {
		t.Fatalf("not a hashed line: %q", hashed)
	}
	s.writeOpenSSH(t, hashed)
	p := &recordingPrompter{}
	if err := s.connect(t, p); err != nil || len(p.prompts) != 0 {
		t.Fatalf("err = %v, prompts = %d", err, len(p.prompts))
	}
}

func TestFR_KH_003_PortAndPatternEntries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hosts func(port string) string
	}{
		{"[host]:port", func(p string) string { return "[127.0.0.1]:" + p }},
		{"comma list", func(p string) string { return "db.example.com,[127.0.0.1]:" + p + ",10.9.9.9" }},
		{"wildcard", func(p string) string { return "[127.0.0.*]:" + p }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTrustSetup(t)
			s.writeOpenSSH(t, tc.hosts(strconv.Itoa(s.srv.Port()))+" "+keyField(s.srv.HostKeys()[0]))
			p := &recordingPrompter{}
			if err := s.connect(t, p); err != nil || len(p.prompts) != 0 {
				t.Fatalf("err = %v, prompts = %d", err, len(p.prompts))
			}
		})
	}
	// A plain "host" entry means port 22 only: it does not cover this port.
	s := newTrustSetup(t)
	s.writeOpenSSH(t, "127.0.0.1 "+keyField(otherKey(t, sshtest.Ed25519)))
	p := &recordingPrompter{}
	if err := s.connect(t, p); !errors.Is(err, ErrHostKeyRejected) || len(p.prompts) != 1 {
		t.Fatalf("port-22 entry applied to another port: err = %v, prompts = %d", err, len(p.prompts))
	}
}

func TestFR_KH_003_ChecksumUnchanged(t *testing.T) {
	// writeOpenSSH checks the fixture byte for byte when the test ends;
	// every kind of outcome runs against it.
	s := newTrustSetup(t)
	unrelated := knownhosts.Line([]string{"x.example.com"}, otherKey(t, sshtest.RSA))
	s.writeOpenSSH(t, "# fixture", unrelated)
	if err := s.connect(t, &recordingPrompter{trust: false}); !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("unknown, cancelled: %v", err)
	}
	if err := s.connect(t, &recordingPrompter{trust: true}); err != nil {
		t.Fatalf("unknown, trusted: %v", err)
	}

	s = newTrustSetup(t)
	s.writeOpenSSH(t, unrelated, s.line(s.srv.HostKeys()[0]))
	if err := s.connect(t, nil); err != nil {
		t.Fatalf("known: %v", err)
	}

	s = newTrustSetup(t)
	s.writeOpenSSH(t, unrelated, s.line(otherKey(t, sshtest.Ed25519)))
	if err := s.connect(t, nil); !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("changed: %v", err)
	}

	s = newTrustSetup(t)
	s.writeOpenSSH(t, "@revoked * "+keyField(s.srv.HostKeys()[0]))
	if err := s.connect(t, nil); !errors.Is(err, ErrHostKeyRevoked) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestFR_KH_003_RevokedRejected(t *testing.T) {
	s := newTrustSetup(t)
	s.writeOpenSSH(t, "# comment", "@revoked * "+keyField(s.srv.HostKeys()[0]))
	p := &recordingPrompter{trust: true}
	err := s.connect(t, p)
	re, ok := errors.AsType[*HostKeyRevokedError](err)
	if !ok || !errors.Is(err, ErrHostKeyRevoked) || re.Entry.File != s.openssh || re.Entry.Line != 2 {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(err.Error(), "host key revoked") {
		t.Fatalf("Error() = %q", err)
	}
	s.noAuth(t, p)
}

func TestSEC_HOSTKEY_003_RevokedBeatsSshelfTrust(t *testing.T) {
	s := newTrustSetup(t)
	write(t, s.kh, s.line(s.srv.HostKeys()[0]))
	s.writeOpenSSH(t, "@revoked * "+keyField(s.srv.HostKeys()[0]))
	p := &recordingPrompter{trust: true}
	if err := s.connect(t, p); !errors.Is(err, ErrHostKeyRevoked) {
		t.Fatalf("err = %v", err)
	}
	s.noAuth(t, p)
}

func TestSEC_HOSTKEY_007_PinnedToKnownAlgorithms(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		keys []ssh.PublicKey
		want []string
	}{
		{"none", nil, nil},
		{"ed25519", []ssh.PublicKey{otherKey(t, sshtest.Ed25519)}, []string{ssh.KeyAlgoED25519}},
		{"rsa", []ssh.PublicKey{otherKey(t, sshtest.RSA)}, []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512}},
		{"ecdsa and ed25519", []ssh.PublicKey{otherKey(t, sshtest.Ed25519), otherKey(t, sshtest.ECDSA)},
			[]string{ssh.KeyAlgoECDSA256, ssh.KeyAlgoED25519}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			lines := []string{knownhosts.Line([]string{"other.example.com"}, otherKey(t, sshtest.RSA))}
			for _, k := range tc.keys {
				lines = append(lines, knownhosts.Line([]string{"[db.example.com]:2222"}, k))
			}
			write(t, path, lines...)
			cb, err := knownhosts.New(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := hostKeyAlgorithms(cb, "db.example.com:2222"); !slices.Equal(got, tc.want) {
				t.Fatalf("hostKeyAlgorithms = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSEC_HOSTKEY_007_KnownTypeChosenOverPreferred(t *testing.T) {
	// The server offers ECDSA and RSA; only its RSA key is known. Without
	// pinning, x/crypto would pick ECDSA first and report a changed key.
	s := newTrustSetup(t, sshtest.WithHostKeyTypes(sshtest.ECDSA, sshtest.RSA))
	var rsaKey ssh.PublicKey
	for _, k := range s.srv.HostKeys() {
		if k.Type() == ssh.KeyAlgoRSA {
			rsaKey = k
		}
	}
	write(t, s.kh, s.line(rsaKey))
	p := &recordingPrompter{}
	if err := s.connect(t, p); err != nil || len(p.prompts) != 0 {
		t.Fatalf("err = %v, prompts = %d", err, len(p.prompts))
	}
}

func TestSEC_HOSTKEY_007_RSAOnlyServerFailsWithoutTofu(t *testing.T) {
	s := newTrustSetup(t, sshtest.WithHostKeyTypes(sshtest.RSA))
	write(t, s.kh, s.line(otherKey(t, sshtest.Ed25519)))
	p := &recordingPrompter{trust: true}
	err := s.connect(t, p)
	if err == nil || errors.Is(err, ErrHostKeyRejected) || errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("err = %v, want a failed algorithm negotiation", err)
	}
	if !strings.Contains(err.Error(), "host key") {
		t.Fatalf("err = %v", err)
	}
	s.noAuth(t, p)
}
