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
// OpenSFTPOn takes a second lease on it with no new connection or auth
// attempt (FR-SFTP-003, FR-SFTP-004 session side).
func TestFR_SFTP_004_OpenSFTPOnSharesConn(t *testing.T) {
	a, srv := sliceFixture(t, true)
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := a.Connect(context.Background(), "h1", Size{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	attempts := len(srv.AuthAttempts())
	lease, err := a.OpenSFTPOn(s)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Conn().ID() != s.ConnID() {
		t.Fatalf("lease on connection %d, session on %d", lease.Conn().ID(), s.ConnID())
	}
	if n := len(srv.AuthAttempts()); n != attempts {
		t.Fatalf("auth attempts went from %d to %d", attempts, n)
	}
	_ = s.Close()
	<-s.Done()
	ch, err := lease.Client().NewSession()
	if err != nil {
		t.Fatalf("lease unusable after the session closed: %v", err)
	}
	_ = ch.Close()
	if n := disconnectCount(srv); n != 0 {
		t.Fatalf("%d connection closes while the lease is held", n)
	}
	lease.Release()
	if !srv.WaitFor(time.Second, func() bool { return disconnectCount(srv) == 1 }) {
		t.Fatalf("closes = %+v, want one connection close", srv.Closes())
	}
	if _, err := a.OpenSFTPOn(s); !errors.Is(err, sshclient.ErrConnClosed) {
		t.Fatalf("OpenSFTPOn after the connection closed = %v, want ErrConnClosed", err)
	}
}

func TestFR_SFTP_004_OpenSFTPOnNeedsConn(t *testing.T) {
	if _, err := New(Config{}).OpenSFTPOn(nil); !errors.Is(err, ErrNoSharedConn) {
		t.Fatalf("err = %v, want ErrNoSharedConn", err)
	}
}
