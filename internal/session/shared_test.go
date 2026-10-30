package session

import (
	"errors"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Sessions on a shared connection (ADR-017, ARCH-SESS-008 M3 revision).

func closesOf(srv *sshtest.Server, typ string) []sshtest.Request {
	var out []sshtest.Request
	for _, c := range srv.Closes() {
		if c.Type == typ {
			out = append(out, c)
		}
	}
	return out
}

// sharedSession opens a session and takes a second lease on its
// connection, as an SFTP browser would.
func sharedSession(t *testing.T, srv *sshtest.Server) (*Session, *sshclient.Lease) {
	t.Helper()
	s := open(t, srv, Options{})
	other, err := s.Conn().Lease()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Release)
	if s.ConnID() != s.Conn().ID() || s.ConnID() == 0 {
		t.Fatalf("ConnID() = %d", s.ConnID())
	}
	waitScreen(t, s, "$")
	return s, other
}

func TestARCH_SESS_008_M3_ExitKeepsSharedConn(t *testing.T) {
	before := goleak.IgnoreCurrent()
	srv, err := sshtest.Start(sshtest.WithPassword("u", "pw"))
	if err != nil {
		t.Fatal(err)
	}
	s, other := sharedSession(t, srv)
	_ = s.SendBytes([]byte("exit 0\r"))
	waitDone(t, s)
	if err := s.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if !srv.WaitFor(time.Second, func() bool { return len(closesOf(srv, "close")) == 1 }) {
		t.Fatalf("closes = %+v, want the session channel closed", srv.Closes())
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(closesOf(srv, "disconnect")); n != 0 {
		t.Fatalf("%d connection closes while the browser's lease is held", n)
	}
	// The other holder keeps using the connection.
	ch, err := other.Client().NewSession()
	if err != nil {
		t.Fatalf("second holder cannot open a channel: %v", err)
	}
	_ = ch.Close()
	other.Release()
	if !srv.WaitFor(time.Second, func() bool { return len(closesOf(srv, "disconnect")) == 1 }) {
		t.Fatalf("closes = %+v, want one connection close after the last release", srv.Closes())
	}
	_ = srv.Close()
	if n := len(closesOf(srv, "disconnect")); n != 1 {
		t.Fatalf("%d connection closes, want exactly 1", n)
	}
	goleak.VerifyNone(t, before)
}

func TestARCH_SESS_008_M3_DropIsDisconnectError(t *testing.T) {
	srv := newServer(t)
	s, other := sharedSession(t, srv)
	srv.DropConnections()
	waitDone(t, s)
	var de *DisconnectError
	if !errors.As(s.Err(), &de) {
		t.Fatalf("Err() = %v, want *DisconnectError", s.Err())
	}
	select {
	case <-other.Conn().Done():
	case <-time.After(time.Second):
		t.Fatal("the shared connection's Done was not closed by the drop")
	}
	if other.Conn().Err() == nil {
		t.Fatal("Conn.Err() = nil after a drop")
	}
}

// TestFR_SFTP_004_ShellExitKeepsConn: the session side of FR-SFTP-004.
// exit in the shell and Close of the session both leave the other holder
// working; the connection closes once, after the last holder.
func TestFR_SFTP_004_ShellExitKeepsConn(t *testing.T) {
	for _, how := range []string{"exit", "close"} {
		t.Run(how, func(t *testing.T) {
			srv := newServer(t)
			s, other := sharedSession(t, srv)
			if how == "exit" {
				_ = s.SendBytes([]byte("exit 0\r"))
			} else {
				_ = s.Close()
			}
			waitDone(t, s)
			for range 3 {
				ch, err := other.Client().NewSession()
				if err != nil {
					t.Fatalf("connection unusable after the session ended: %v", err)
				}
				_ = ch.Close()
			}
			if n := len(closesOf(srv, "disconnect")); n != 0 {
				t.Fatalf("%d connection closes, want none", n)
			}
			other.Release()
			if !srv.WaitFor(time.Second, func() bool { return len(closesOf(srv, "disconnect")) == 1 }) {
				t.Fatalf("closes = %+v, want one connection close", srv.Closes())
			}
		})
	}
}

// TestFR_CONN_014_M3_LastUserClosesConn: closing a session that holds the
// only lease (Ctrl+] q, y) closes the session channel and the connection;
// with a second holder, only the channel.
func TestFR_CONN_014_M3_LastUserClosesConn(t *testing.T) {
	t.Run("only user", func(t *testing.T) {
		srv := newServer(t)
		s := open(t, srv, Options{})
		waitScreen(t, s, "$")
		_ = s.Close()
		waitDone(t, s)
		if !srv.WaitFor(time.Second, func() bool {
			return len(closesOf(srv, "close")) == 1 && len(closesOf(srv, "disconnect")) == 1
		}) {
			t.Fatalf("closes = %+v, want the channel and the connection closed", srv.Closes())
		}
	})
	t.Run("shared", func(t *testing.T) {
		srv := newServer(t)
		s, other := sharedSession(t, srv)
		_ = s.Close()
		waitDone(t, s)
		if !srv.WaitFor(time.Second, func() bool { return len(closesOf(srv, "close")) == 1 }) {
			t.Fatalf("closes = %+v, want the session channel closed", srv.Closes())
		}
		time.Sleep(50 * time.Millisecond)
		if n := len(closesOf(srv, "disconnect")); n != 0 {
			t.Fatalf("%d connection closes, want none while the SFTP lease is held", n)
		}
		other.Release()
		if !srv.WaitFor(time.Second, func() bool { return len(closesOf(srv, "disconnect")) == 1 }) {
			t.Fatalf("closes = %+v, want one connection close", srv.Closes())
		}
	})
}
