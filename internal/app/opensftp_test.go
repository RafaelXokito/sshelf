package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/files"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// TestFR_SFTP_002_OpenSFTPNoShell: OpenSFTP connects through the normal
// connect flow and opens the browser's channels with no PTY and no shell;
// the browser's Close closes the connection once.
func TestFR_SFTP_002_OpenSFTPNoShell(t *testing.T) {
	a, srv := sliceFixture(t, true, sshtest.WithSFTP(t.TempDir()))
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := a.OpenSFTP(context.Background(), "h1")
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Start(Remote); got != sshtest.SFTPHome {
		t.Fatalf("remote start = %q", got)
	}
	if _, err := b.List(context.Background(), Remote, b.Start(Remote)); err != nil {
		t.Fatal(err)
	}
	if fb, ok := b.(*files.Browser); !ok || fb.HostID() != "h1" {
		t.Fatalf("browser %T without the host", b)
	}
	if n := len(srv.RequestsOfType("pty-req")) + len(srv.RequestsOfType("shell")); n != 0 {
		t.Fatalf("%d pty-req or shell requests", n)
	}
	if n := len(srv.RequestsOfType("subsystem")); n < 1 {
		t.Fatal("no subsystem request")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if !srv.WaitFor(time.Second, func() bool { return disconnectCount(srv) == 1 }) {
		t.Fatalf("closes = %+v, want one connection close", srv.Closes())
	}
}

// TestFR_SFTP_002_NoSftpNoTab: a server that refuses the subsystem gives
// ErrNoSFTP and no browser, and the connection is closed.
func TestFR_SFTP_002_NoSftpNoTab(t *testing.T) {
	a, srv := sliceFixture(t, true, sshtest.WithoutSFTP())
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := a.OpenSFTP(context.Background(), "h1")
	if !errors.Is(err, ErrNoSFTP) || b != nil {
		t.Fatalf("OpenSFTP = %v, %v; want nil, ErrNoSFTP", b, err)
	}
	if !srv.WaitFor(time.Second, func() bool { return disconnectCount(srv) == 1 }) {
		t.Fatalf("closes = %+v, want one connection close", srv.Closes())
	}
	if _, err := a.OpenSFTP(context.Background(), "nope"); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("unknown host: %v", err)
	}
}
