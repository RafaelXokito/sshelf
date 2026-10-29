package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// stillTerm is a session.Terminal of fixed size that never resizes.
type stillTerm struct{}

func (stillTerm) MakeRaw() (func() error, error)     { return func() error { return nil }, nil }
func (stillTerm) Size() (int, int, error)            { return 80, 24, nil }
func (stillTerm) Resizes() (<-chan struct{}, func()) { return nil, func() {} }

// sliceFixture is a config dir with one key-file host pointing at an
// sshtest server, as in the T-004 demo.
func sliceFixture(t *testing.T, authorize bool) (*App, *sshtest.Server) {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(filepath.Join(home, ".ssh", "id_ed25519"), sshtest.Ed25519, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var opts []sshtest.Option
	if authorize {
		opts = append(opts, sshtest.WithAuthorizedKey("demo", pub))
	} else {
		other, err := sshtest.GenerateSigner(sshtest.Ed25519)
		if err != nil {
			t.Fatal(err)
		}
		opts = append(opts, sshtest.WithAuthorizedKey("demo", other.PublicKey()))
	}
	srv := sshtest.New(t, opts...)
	kh := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(kh, []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"version": 1,
  "identities": [{"id": "i1", "name": "demo", "key_path": "~/.ssh/id_ed25519"}],
  "hosts": [
    {"id": "h2", "label": "zeta", "hostname": "127.0.0.1", "port": 1},
    {"id": "h1", "label": "web-prod", "hostname": %q, "port": %d, "user": "demo", "identity_id": "i1"}
  ]}`, srv.Host(), srv.Port())
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Config{
		ConfigPath:      filepath.Join(dir, "config.json"),
		KnownHostsFiles: []string{kh},
		Creds:           KeyFileSource{Home: home},
		Terminal:        stillTerm{},
	})
	return a, srv
}

func TestFR_CONN_001_ServiceConnectsKeyFileHost(t *testing.T) {
	a, srv := sliceFixture(t, true)
	hosts, err := a.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0].Label != "web-prod" || hosts[1].Label != "zeta" {
		t.Fatalf("hosts = %+v, want sorted by label", hosts)
	}
	s, err := a.Connect(context.Background(), "h1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if pty := srv.RequestsOfType("pty-req"); len(pty) != 1 || pty[0].Width != 80 || pty[0].Height != 24 {
		t.Fatalf("pty-req = %+v", pty)
	}
	if sh := srv.RequestsOfType("shell"); len(sh) != 1 {
		t.Fatalf("shell requests = %+v", sh)
	}
}

func TestARCH_SESS_002_AuthFailureReturnsNoSession(t *testing.T) {
	a, srv := sliceFixture(t, false)
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := a.Connect(context.Background(), "h1")
	if err == nil || s != nil {
		t.Fatalf("Connect() = %v, %v; want an auth error and no session", s, err)
	}
	if !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("err = %v", err)
	}
	if n := len(srv.RequestsOfType("session")); n != 0 {
		t.Fatalf("%d session channels opened after a failed auth", n)
	}
}

func TestConnectUnknownHost(t *testing.T) {
	a := New(Config{ConfigPath: filepath.Join(t.TempDir(), "config.json"), Terminal: stillTerm{}})
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Connect(context.Background(), "nope"); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := any(a.cfg.Prompter).(sshclient.RejectUnknownHosts); !ok {
		t.Fatal("default prompter is not RejectUnknownHosts")
	}
}
