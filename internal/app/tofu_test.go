package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// trustFixture is a config dir with one key-file host on an sshtest
// server whose key no file knows yet, and a fake ~/.ssh/known_hosts that
// lists another host.
type trustFixture struct {
	t       *testing.T
	srv     *sshtest.Server
	home    string
	dir     string // sshelf config dir
	openssh string // the fake ~/.ssh/known_hosts
	prompts chan HostKeyPromptMsg
}

func newTrustFixture(t *testing.T, opts ...sshtest.Option) *trustFixture {
	t.Helper()
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(filepath.Join(sshDir, "id_ed25519"), sshtest.Ed25519, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, append([]sshtest.Option{sshtest.WithAuthorizedKey("demo", pub)}, opts...)...)
	other, err := sshtest.GenerateSigner(sshtest.Ed25519)
	if err != nil {
		t.Fatal(err)
	}
	openssh := filepath.Join(sshDir, "known_hosts")
	line := knownhosts.Line([]string{"other.example.com"}, other.PublicKey())
	if err := os.WriteFile(openssh, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &trustFixture{t: t, srv: srv, home: home, dir: filepath.Join(t.TempDir(), "sshelf"), openssh: openssh}
	f.writeConfig()
	return f
}

func (f *trustFixture) writeConfig() {
	if _, err := paths.EnsureDir(f.dir); err != nil {
		f.t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"version": 1,
  "identities": [{"id": "i1", "name": "demo", "key_path": "~/.ssh/id_ed25519"}],
  "hosts": [{"id": "h1", "label": "web-prod", "hostname": %q, "port": %d, "user": "demo", "identity_id": "i1"}]}`,
		f.srv.Host(), f.srv.Port())
	if err := os.WriteFile(paths.ConfigFile(f.dir), []byte(cfg), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// app returns a freshly loaded App, as after a restart. Its prompts go to
// f.prompts.
func (f *trustFixture) app() *App {
	f.t.Helper()
	a := New(Config{
		Dir:             f.dir,
		KnownHostsFiles: []string{paths.KnownHostsFile(f.dir), f.openssh},
		Creds:           KeyFileSource{Home: f.home},
		Terminal:        stillTerm{},
	})
	f.prompts = make(chan HostKeyPromptMsg, 4)
	a.SetSend(func(msg any) { f.prompts <- msg.(HostKeyPromptMsg) })
	if _, err := a.Load(context.Background()); err != nil {
		f.t.Fatal(err)
	}
	return a
}

type connectResult struct {
	s   Session
	err error
}

func (f *trustFixture) connectAsync(a *App) chan connectResult {
	done := make(chan connectResult, 1)
	go func() {
		s, err := a.Connect(context.Background(), "h1")
		done <- connectResult{s, err}
	}()
	return done
}

func (f *trustFixture) nextPrompt() HostKeyPromptMsg {
	f.t.Helper()
	select {
	case p := <-f.prompts:
		return p
	case <-time.After(10 * time.Second):
		f.t.Fatal("no host-key prompt")
		return HostKeyPromptMsg{}
	}
}

// connectAnswering connects and answers one prompt with trust.
func (f *trustFixture) connectAnswering(a *App, trust bool) error {
	f.t.Helper()
	done := f.connectAsync(a)
	f.nextPrompt().Answer(trust)
	r := <-done
	if r.s != nil {
		_ = r.s.Close()
	}
	return r.err
}

func (f *trustFixture) sshelfKnownHosts() []byte {
	b, err := os.ReadFile(paths.KnownHostsFile(f.dir))
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func TestFR_KH_001_NoAuthBeforeChoice(t *testing.T) {
	f := newTrustFixture(t)
	a := f.app()
	done := f.connectAsync(a)
	p := f.nextPrompt()
	// The server has the client waiting on the prompt: give it time to do
	// anything it would do, then check that nothing was authenticated.
	time.Sleep(100 * time.Millisecond)
	if n := len(f.srv.AuthAttempts()); n != 0 {
		t.Fatalf("%d auth attempts before the choice", n)
	}
	p.Answer(true)
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	_ = r.s.Close()
	if len(f.srv.AuthAttempts()) == 0 {
		t.Fatal("no auth after trusting")
	}
}

func TestARCH_CONN_003_CancelWritesNothing(t *testing.T) {
	f := newTrustFixture(t)
	a := f.app()
	err := f.connectAnswering(a, false)
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("err = %v, want ErrHostKeyRejected", err)
	}
	if n := len(f.srv.AuthAttempts()); n != 0 {
		t.Fatalf("%d auth attempts after Cancel", n)
	}
	if _, err := os.Stat(paths.KnownHostsFile(f.dir)); err == nil {
		t.Fatal("sshelf known_hosts written after Cancel")
	}
}

func TestSEC_HOSTKEY_004_FingerprintMatchesSSH(t *testing.T) {
	f := newTrustFixture(t)
	a := f.app()
	done := f.connectAsync(a)
	p := f.nextPrompt()
	p.Answer(false)
	<-done
	key := f.srv.HostKeys()[0]
	if p.Fingerprint != ssh.FingerprintSHA256(key) || p.KeyType != ssh.KeyAlgoED25519 ||
		p.Host != f.srv.Host() || p.Port != f.srv.Port() {
		t.Fatalf("prompt = %+v", p)
	}
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not available")
	}
	pubFile := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(pubFile, ssh.MarshalAuthorizedKey(key), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(keygen, "-lf", pubFile).Output() //nolint:gosec // test-only
	if err != nil {
		t.Fatal(err)
	}
	// "256 SHA256:… comment (ED25519)"
	if fields := strings.Fields(string(out)); len(fields) < 2 || fields[1] != p.Fingerprint {
		t.Fatalf("ssh-keygen -lf = %q, prompt fingerprint %q", out, p.Fingerprint)
	}
}

func TestSEC_HOSTKEY_002_OneParseableLine(t *testing.T) {
	f := newTrustFixture(t)
	before, _ := os.ReadFile(f.openssh) //nolint:gosec // test temp file
	if err := f.connectAnswering(f.app(), true); err != nil {
		t.Fatal(err)
	}
	b := f.sshelfKnownHosts()
	if lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"); len(lines) != 1 {
		t.Fatalf("known_hosts has %d lines:\n%s", len(lines), b)
	}
	if !strings.HasPrefix(string(b), "["+f.srv.Host()+"]:") {
		t.Fatalf("host not stored unhashed as [host]:port: %q", b)
	}
	cb, err := knownhosts.New(paths.KnownHostsFile(f.dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := cb(f.srv.Addr(), &net.TCPAddr{IP: net.ParseIP(f.srv.Host()), Port: f.srv.Port()}, f.srv.HostKeys()[0]); err != nil {
		t.Fatalf("the saved line does not verify the key: %v", err)
	}
	if st, _ := os.Stat(paths.KnownHostsFile(f.dir)); st.Mode().Perm() != paths.FileMode {
		t.Fatalf("known_hosts mode %o", st.Mode().Perm())
	}
	if after, _ := os.ReadFile(f.openssh); !bytes.Equal(before, after) { //nolint:gosec // test temp file
		t.Fatal("~/.ssh/known_hosts changed")
	}
}

func TestFR_KH_004_TrustSurvivesRestart(t *testing.T) {
	f := newTrustFixture(t)
	if err := f.connectAnswering(f.app(), true); err != nil {
		t.Fatal(err)
	}
	a := f.app() // restart
	s, err := a.Connect(context.Background(), "h1")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	select {
	case p := <-f.prompts:
		t.Fatalf("prompted again after a restart: %+v", p)
	default:
	}
}

func TestFR_KH_004_AppendsToExistingFile(t *testing.T) {
	f := newTrustFixture(t)
	other, _ := sshtest.GenerateSigner(sshtest.Ed25519)
	existing := knownhosts.Line([]string{"old.example.com"}, other.PublicKey()) // no trailing newline
	if err := os.WriteFile(paths.KnownHostsFile(f.dir), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.connectAnswering(f.app(), true); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(f.sshelfKnownHosts()), "\n"), "\n")
	if len(lines) != 2 || lines[0] != existing {
		t.Fatalf("known_hosts lines = %q", lines)
	}
}

func TestFR_KH_004_SSHKeygenFindsEntry(t *testing.T) {
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not available")
	}
	f := newTrustFixture(t)
	if err := f.connectAnswering(f.app(), true); err != nil {
		t.Fatal(err)
	}
	host := knownhosts.Normalize(f.srv.Addr())
	out, err := exec.Command(keygen, "-F", host, "-f", paths.KnownHostsFile(f.dir)).Output() //nolint:gosec // test-only
	if err != nil {
		t.Fatalf("ssh-keygen -F %s: %v\n%s", host, err, out)
	}
	if !strings.Contains(string(out), strings.TrimSpace(string(f.sshelfKnownHosts()))) {
		t.Fatalf("ssh-keygen -F output:\n%s", out)
	}
}

func TestARCH_CONN_003_OpenSSHFileUntouched(t *testing.T) {
	f := newTrustFixture(t)
	st0, _ := os.Stat(f.openssh)
	b0, _ := os.ReadFile(f.openssh) //nolint:gosec // test temp file
	for _, trust := range []bool{false, true} {
		err := f.connectAnswering(f.app(), trust)
		if trust != (err == nil) {
			t.Fatalf("trust=%v: err = %v", trust, err)
		}
		st, _ := os.Stat(f.openssh)
		b, _ := os.ReadFile(f.openssh) //nolint:gosec // test temp file
		if st.Size() != st0.Size() || !st.ModTime().Equal(st0.ModTime()) || !bytes.Equal(b, b0) {
			t.Fatalf("trust=%v: ~/.ssh/known_hosts changed", trust)
		}
	}
}

func TestHostKeyPromptCancelledContext(t *testing.T) {
	f := newTrustFixture(t)
	a := f.app()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		s, err := a.Connect(ctx, "h1")
		if s != nil {
			_ = s.Close()
		}
		done <- err
	}()
	p := f.nextPrompt()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	p.Answer(true) // too late: never blocks, writes nothing
	if _, err := os.Stat(paths.KnownHostsFile(f.dir)); err == nil {
		t.Fatal("known_hosts written after the attempt was cancelled")
	}
}
