package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// keyFileFixture is an sshtest server that trusts a key written to a temp
// "home" directory, plus a known_hosts file that trusts the server.
type keyFileFixture struct {
	srv  *sshtest.Server
	home string
	cfg  sshclient.Config
}

func newKeyFileFixture(t *testing.T, keyMode os.FileMode) keyFileFixture {
	t.Helper()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(filepath.Join(home, ".ssh", "id_test"), sshtest.Ed25519, keyMode)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("deploy", pub))
	kh := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(kh, []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return keyFileFixture{srv: srv, home: home, cfg: sshclient.Config{KnownHostsFiles: []string{kh}}}
}

func (f keyFileFixture) target(keyPath string) domain.Target {
	return domain.Target{Label: "web-prod", Hostname: f.srv.Host(), Port: f.srv.Port(), User: "deploy", KeyPath: keyPath}
}

func (f keyFileFixture) connect(keyPath string) error {
	c, err := f.cfg.Connect(context.Background(), f.target(keyPath), Credentials{Home: f.home}, nil)
	if c != nil {
		_ = c.Close()
	}
	return err
}

func TestSEC_FILE_009_UnprotectedKeyRefused(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604} {
		f := newKeyFileFixture(t, mode)
		err := f.connect("~/.ssh/id_test")
		if !errors.Is(err, ErrUnprotectedKeyFile) {
			t.Fatalf("mode %04o: err = %v, want ErrUnprotectedKeyFile", mode, err)
		}
		if !strings.Contains(err.Error(), "unprotected private key file") {
			t.Fatalf("mode %04o: message %q does not name the problem", mode, err)
		}
		for _, a := range f.srv.AuthAttempts() {
			if a.Success {
				t.Fatalf("mode %04o: an auth attempt succeeded: %+v", mode, a)
			}
		}
	}
}

func TestSEC_FILE_009_Mode0600KeyUsed(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400} {
		f := newKeyFileFixture(t, mode)
		if err := f.connect("~/.ssh/id_test"); err != nil {
			t.Fatalf("mode %04o: %v", mode, err)
		}
		got := f.srv.AuthAttempts()
		if last := got[len(got)-1]; last.Method != "publickey" || !last.Success {
			t.Fatalf("mode %04o: last auth attempt = %+v", mode, last)
		}
	}
}

func TestFR_IDENT_003_TildeExpanded(t *testing.T) {
	src := Credentials{Home: "/home/u"}
	for in, want := range map[string]string{
		"~/.ssh/id_ed25519": "/home/u/.ssh/id_ed25519",
		"~":                 "/home/u",
		"/abs/key":          "/abs/key",
		"rel/key":           "rel/key",
		"~other/key":        "~other/key", // ~user is not expanded
	} {
		got, err := src.expand(in)
		if err != nil || got != want {
			t.Errorf("expand(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	f := newKeyFileFixture(t, 0o600)
	if err := f.connect("~/.ssh/id_test"); err != nil {
		t.Fatalf("connect with a ~ path: %v", err)
	}
}

func TestFR_IDENT_003_KeyFileNotFound(t *testing.T) {
	f := newKeyFileFixture(t, 0o600)
	err := f.connect("~/.ssh/missing")
	if !errors.Is(err, ErrKeyFileNotFound) {
		t.Fatalf("err = %v, want ErrKeyFileNotFound", err)
	}
	want := "key file not found: " + filepath.Join(f.home, ".ssh", "missing")
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("message %q does not contain %q", err, want)
	}
}

func TestFR_IDENT_003_NoKeyPathNoSigner(t *testing.T) {
	s, err := Credentials{}.Signers(context.Background(), domain.Target{})
	if err != nil || s != nil {
		t.Fatalf("Signers() = %v, %v; want none", s, err)
	}
}

func TestFR_IDENT_003_KeyTypes(t *testing.T) {
	for _, kind := range []string{sshtest.Ed25519, sshtest.ECDSA, sshtest.RSA} {
		path := filepath.Join(t.TempDir(), "id")
		pub, err := sshtest.WriteKeyFile(path, kind, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Credentials{}.Signers(context.Background(), domain.Target{KeyPath: path})
		if err != nil || len(s) != 1 || string(s[0].PublicKey().Marshal()) != string(pub.Marshal()) {
			t.Fatalf("%s: Signers() = %v, %v", kind, s, err)
		}
	}
}
