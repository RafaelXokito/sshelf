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
		Dir:             dir,
		KnownHostsFiles: []string{kh},
		Home:            home,
	})
	return a, srv
}

func TestFR_CONN_001_ServiceConnectsKeyFileHost(t *testing.T) {
	a, srv := sliceFixture(t, true)
	l, err := a.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hosts := l.Hosts; len(hosts) != 2 || hosts[0].Label != "web-prod" || hosts[1].Label != "zeta" {
		t.Fatalf("hosts = %+v, want sorted by label", hosts)
	}
	s, err := a.Connect(context.Background(), "h1", Size{Cols: 100, Rows: 28})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	// The PTY is the session area, with the emulator's TERM (FR-CONN-001
	// revision).
	if pty := srv.RequestsOfType("pty-req"); len(pty) != 1 || pty[0].Width != 100 || pty[0].Height != 28 || pty[0].Term != "xterm-256color" {
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
	s, err := a.Connect(context.Background(), "h1", Size{})
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
	a := New(Config{Dir: t.TempDir()})
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Connect(context.Background(), "nope", Size{}); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("err = %v", err)
	}
	// Without SetSend an unknown host key is rejected without a prompt.
	if trust, err := a.HostKey(context.Background(), sshclient.HostKeyPrompt{Address: "h:22"}); trust || err != nil {
		t.Fatalf("HostKey without SetSend = %v, %v", trust, err)
	}
}
