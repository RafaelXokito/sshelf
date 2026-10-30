package sshclient

import (
	"errors"
	"testing"
	"time"

	"go.uber.org/goleak"
	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The shared connection of ADR-017 against the in-process server.

func connServer(t *testing.T) *sshtest.Server {
	t.Helper()
	return sshtest.New(t, sshtest.WithPassword("u", "pw"))
}

func newConn(t *testing.T, srv *sshtest.Server) (*Conn, *Lease) {
	t.Helper()
	c, err := ssh.Dial("tcp", srv.Addr(), &ssh.ClientConfig{
		User:            "u",
		Auth:            []ssh.AuthMethod{ssh.Password("pw")},
		HostKeyCallback: ssh.FixedHostKey(srv.HostKeys()[0]),
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewConn(c)
}

func disconnects(srv *sshtest.Server) int {
	n := 0
	for _, c := range srv.Closes() {
		if c.Type == "disconnect" {
			n++
		}
	}
	return n
}

func waitConnDone(t *testing.T, c *Conn) {
	t.Helper()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done was not closed")
	}
}

func isDone(c *Conn) bool {
	select {
	case <-c.Done():
		return true
	default:
		return false
	}
}

// works opens and closes a channel on the lease's client.
func works(t *testing.T, l *Lease) {
	t.Helper()
	s, err := l.Client().NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	_ = s.Close()
}

func TestARCH_CONN_006_LastReleaseCloses(t *testing.T) {
	for _, firstFirst := range []bool{true, false} {
		name := "second first"
		if firstFirst {
			name = "first first"
		}
		t.Run(name, func(t *testing.T) {
			before := goleak.IgnoreCurrent()
			srv := connServer(t)
			c, l1 := newConn(t, srv)
			l2, err := c.Lease()
			if err != nil {
				t.Fatal(err)
			}
			a, b := l1, l2
			if !firstFirst {
				a, b = l2, l1
			}
			a.Release()
			works(t, b) // the other holder keeps working
			time.Sleep(50 * time.Millisecond)
			if n := disconnects(srv); n != 0 || isDone(c) {
				t.Fatalf("after one release: %d disconnects, done %v; want the connection open", n, isDone(c))
			}
			b.Release()
			waitConnDone(t, c)
			if err := c.Err(); err != nil {
				t.Errorf("Err() after the last release = %v, want nil", err)
			}
			if !srv.WaitFor(time.Second, func() bool { return disconnects(srv) == 1 }) {
				t.Fatalf("closes = %+v, want exactly one disconnect", srv.Closes())
			}
			_ = srv.Close()
			if n := disconnects(srv); n != 1 {
				t.Fatalf("%d disconnects, want 1", n)
			}
			goleak.VerifyNone(t, before)
		})
	}
}

func TestARCH_CONN_006_DoubleReleaseNoop(t *testing.T) {
	srv := connServer(t)
	c, l1 := newConn(t, srv)
	l2, err := c.Lease()
	if err != nil {
		t.Fatal(err)
	}
	l1.Release()
	l1.Release() // must not count as l2's release
	works(t, l2)
	if isDone(c) {
		t.Fatal("connection closed by a double release")
	}
	l3, err := c.Lease()
	if err != nil {
		t.Fatalf("Lease() with a holder left = %v", err)
	}
	l2.Release()
	l3.Release()
	l3.Release()
	waitConnDone(t, c)
	if !srv.WaitFor(time.Second, func() bool { return disconnects(srv) == 1 }) {
		t.Fatalf("closes = %+v, want one disconnect", srv.Closes())
	}
}

func TestARCH_CONN_006_LeaseAfterCloseFails(t *testing.T) {
	t.Run("after the last release", func(t *testing.T) {
		srv := connServer(t)
		c, l := newConn(t, srv)
		l.Release()
		if _, err := c.Lease(); !errors.Is(err, ErrConnClosed) {
			t.Fatalf("Lease() = %v, want ErrConnClosed", err)
		}
		waitConnDone(t, c)
	})
	t.Run("after a drop", func(t *testing.T) {
		srv := connServer(t)
		c, l := newConn(t, srv)
		defer l.Release()
		srv.DropConnections()
		waitConnDone(t, c)
		if _, err := c.Lease(); !errors.Is(err, ErrConnClosed) {
			t.Fatalf("Lease() = %v, want ErrConnClosed", err)
		}
	})
}

func TestARCH_CONN_006_DropClosesDone(t *testing.T) {
	before := goleak.IgnoreCurrent()
	srv := connServer(t)
	c, l1 := newConn(t, srv)
	l2, err := c.Lease()
	if err != nil {
		t.Fatal(err)
	}
	if c.Err() != nil {
		t.Fatalf("Err() while up = %v", c.Err())
	}
	srv.DropConnections()
	waitConnDone(t, c)
	if c.Err() == nil {
		t.Fatal("Err() after a drop = nil, want the transport error")
	}
	// Releasing after the drop changes nothing and closes nothing twice.
	l1.Release()
	l2.Release()
	if c.Err() == nil {
		t.Fatal("Err() changed after release")
	}
	if !srv.WaitFor(time.Second, func() bool { return disconnects(srv) == 1 }) {
		t.Fatalf("closes = %+v, want one disconnect", srv.Closes())
	}
	_ = srv.Close()
	goleak.VerifyNone(t, before)
}

func TestARCH_CONN_006_IDsDiffer(t *testing.T) {
	srv := connServer(t)
	a, la := newConn(t, srv)
	b, lb := newConn(t, srv)
	defer la.Release()
	defer lb.Release()
	if a.ID() == b.ID() || a.ID() == 0 {
		t.Fatalf("IDs %d and %d, want distinct non-zero", a.ID(), b.ID())
	}
	if la.Conn() != a {
		t.Fatal("Lease.Conn() is not its Conn")
	}
}
