package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

func disconnectCount(srv *sshtest.Server) int {
	n := 0
	for _, c := range srv.Closes() {
		if c.Type == "disconnect" {
			n++
		}
	}
	return n
}

// TestFR_SFTP_004_OpenSFTPOnSharesConn: app.Connect creates the Conn and
// OpenSFTPOn opens the remote side on a second lease of it, with no new
// connection or auth attempt (FR-SFTP-003); either side closing first
// leaves the other working, and the connection closes once, after both
// (FR-SFTP-004).
func TestFR_SFTP_004_OpenSFTPOnSharesConn(t *testing.T) {
	a, srv := sliceFixture(t, true, sshtest.WithSFTP(t.TempDir()))
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := a.Connect(context.Background(), "h1", Size{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	attempts := len(srv.AuthAttempts())
	r, err := a.OpenSFTPOn(s)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if r.ConnID() != s.ConnID() {
		t.Fatalf("remote on connection %d, session on %d", r.ConnID(), s.ConnID())
	}
	if n := len(srv.AuthAttempts()); n != attempts {
		t.Fatalf("auth attempts went from %d to %d", attempts, n)
	}
	if n := len(srv.ClientVersions()); n != 1 {
		t.Fatalf("%d connections, want 1", n)
	}
	_ = s.Close()
	<-s.Done()
	if _, err := r.List(context.Background(), r.Start()); err != nil {
		t.Fatalf("listing after the session closed: %v", err)
	}
	if n := disconnectCount(srv); n != 0 {
		t.Fatalf("%d connection closes while the SFTP side is open", n)
	}
	_ = r.Close()
	if !srv.WaitFor(time.Second, func() bool { return disconnectCount(srv) == 1 }) {
		t.Fatalf("closes = %+v, want one connection close", srv.Closes())
	}
	if _, err := a.OpenSFTPOn(s); !errors.Is(err, sshclient.ErrConnClosed) {
		t.Fatalf("OpenSFTPOn after the connection closed = %v, want ErrConnClosed", err)
	}
}

// TestFR_SFTP_003_NoSFTPKeepsSession: a refused subsystem leaves the
// session running.
func TestFR_SFTP_003_NoSFTPKeepsSession(t *testing.T) {
	a, srv := sliceFixture(t, true)
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := a.Connect(context.Background(), "h1", Size{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := a.OpenSFTPOn(s); !errors.Is(err, ErrNoSFTP) {
		t.Fatalf("OpenSFTPOn = %v, want ErrNoSFTP", err)
	}
	select {
	case <-s.Done():
		t.Fatal("the session ended")
	case <-time.After(50 * time.Millisecond):
	}
	if n := disconnectCount(srv); n != 0 {
		t.Fatalf("%d connection closes", n)
	}
}

func TestFR_SFTP_004_OpenSFTPOnNeedsConn(t *testing.T) {
	if _, err := New(Config{}).OpenSFTPOn(nil); !errors.Is(err, ErrNoSharedConn) {
		t.Fatalf("err = %v, want ErrNoSharedConn", err)
	}
}
