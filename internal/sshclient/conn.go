package sshclient

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/term"
)

// ErrConnClosed means the shared connection has ended, or its last lease
// was released, so no new lease can be taken (ARCH-CONN-006).
var ErrConnClosed = errors.New("connection closed")

// connIDs numbers connections for the UI (Conn.ID).
var connIDs atomic.Uint64

// Conn is one authenticated SSH connection shared by the tabs that use it
// (ADR-017). Each holder has a Lease; the client is closed exactly once,
// when the last lease is released or the connection fails.
type Conn struct {
	client *ssh.Client
	id     uint64
	done   chan struct{}

	mu       sync.Mutex
	refs     int
	released bool // the last lease was released; Close was called for it
	ended    bool // the waiter saw the connection end
	err      error

	closeOnce sync.Once
}

// Lease is one holder's reference to a Conn.
type Lease struct {
	conn *Conn
	once sync.Once
}

// NewConn wraps client, which the Conn owns from now on, and returns the
// first lease, which belongs to the caller. One goroutine, started
// through term.SafeGo, waits for the connection to end.
func NewConn(client *ssh.Client) (*Conn, *Lease) {
	c := &Conn{client: client, id: connIDs.Add(1), done: make(chan struct{}), refs: 1}
	term.SafeGo(func() { c.end(client.Wait()) }, func(r any) {
		c.closeClient()
		c.end(fmt.Errorf("connection waiter: %v", r))
	})
	return c, &Lease{conn: c}
}

// end records how the connection ended and closes Done. It runs once, on
// the waiter goroutine.
func (c *Conn) end(waitErr error) {
	c.closeClient() // a dropped connection still frees the client's resources
	c.mu.Lock()
	c.ended = true
	switch {
	case c.released:
		c.err = nil
	case waitErr != nil:
		c.err = waitErr
	default:
		c.err = io.EOF
	}
	err := c.err
	c.mu.Unlock()
	slog.Debug("conn: ended", "conn", c.id, "err", err)
	close(c.done)
}

// closeClient closes the client once.
func (c *Conn) closeClient() {
	c.closeOnce.Do(func() { _ = c.client.Close() })
}

// Lease takes another reference to the connection. It fails with
// ErrConnClosed once the connection ended or its last lease was released.
func (c *Conn) Lease() (*Lease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.released || c.ended {
		return nil, ErrConnClosed
	}
	c.refs++
	return &Lease{conn: c}, nil
}

// ID is a process-unique number for the connection, so the UI can match
// the tabs that share it.
func (c *Conn) ID() uint64 { return c.id }

// Done is closed when the connection has ended for any reason: the last
// release or a failure.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err reports, once Done is closed, why the connection ended: nil after
// the last release, otherwise the transport error (io.EOF when the server
// closed it cleanly). It returns nil while the connection is up.
func (c *Conn) Err() error {
	select {
	case <-c.done:
	default:
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// release drops one reference; the last one closes the client.
func (c *Conn) release() {
	c.mu.Lock()
	c.refs--
	last := c.refs == 0
	if last {
		c.released = true
	}
	c.mu.Unlock()
	if last {
		c.closeClient()
	}
}

// Client returns the shared client. A holder opens its channels on it
// and never closes it.
func (l *Lease) Client() *ssh.Client { return l.conn.client }

// Conn returns the connection the lease belongs to.
func (l *Lease) Conn() *Conn { return l.conn }

// Release gives the reference back. It is idempotent; the last release of
// a connection closes its client.
func (l *Lease) Release() { l.once.Do(l.conn.release) }
