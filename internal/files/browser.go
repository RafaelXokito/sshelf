package files

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/term"
)

// ErrNotConnected is returned by remote operations while the connection
// is lost (FR-SFTP-018).
var ErrNotConnected = errors.New("not connected")

// ConnState is whether an SFTP tab's connection is up.
type ConnState int

// Connection states.
const (
	Connected ConnState = iota
	Disconnected
)

// Count limits (design §8.9): Count stops one past these, so the caller
// can tell "more than".
const (
	ViewCountLimit   = 10_000  // item counts in the panes (FR-SFTP-006)
	DeleteCountLimit = 100_000 // the delete confirmation (FR-SFTP-011)
)

// Browser is the back end of one SFTP tab (ADR-018): its local side, its
// remote side with the two channels, and its transfer queue. It is what
// app.Browser wraps. Every method may block on I/O except Copy, Answer,
// Cancel's bookkeeping, Retry, Clear, Transfers, Updates, State and Done.
type Browser struct {
	local      LocalFS
	localStart string
	updates    chan struct{}
	ctx        context.Context // cancelled by Close: every running job stops
	stop       context.CancelFunc
	wg         sync.WaitGroup // running jobs

	mu        sync.Mutex
	remote    *RemoteFS
	state     ConnState
	closed    bool
	jobs      []*job // the rows, oldest first
	pending   []*job // the run order
	running   int
	nextID    TransferID
	nextBatch BatchID
}

// Open opens the remote side of an SFTP tab on lease, which it owns from
// now on, also when it fails (ADR-017), and returns the tab's browser. It
// fails with ErrNoSFTP when the server refuses the subsystem.
func Open(lease *sshclient.Lease) (*Browser, error) {
	r, err := OpenRemote(lease)
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Browser{
		local:      LocalFS{},
		localStart: LocalFS{}.Start(),
		updates:    make(chan struct{}, 1),
		ctx:        ctx,
		stop:       stop,
		remote:     r,
	}, nil
}

// ConnID identifies the connection the browser runs on.
func (b *Browser) ConnID() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remote.ConnID()
}

// Start is the side's starting directory (FR-SFTP-005).
func (b *Browser) Start(side Side) string {
	if side == Local {
		return b.localStart
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remote.Start()
}

// connected returns the remote side, or ErrNotConnected while the
// connection is lost or after Close.
func (b *Browser) connected() (*RemoteFS, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.state != Connected {
		return nil, ErrNotConnected
	}
	return b.remote, nil
}

// List lists dir on side (ARCH-SFTP-002).
func (b *Browser) List(ctx context.Context, side Side, dir string) (Listing, error) {
	if side == Local {
		return b.local.List(ctx, dir)
	}
	r, err := b.connected()
	if err != nil {
		return Listing{}, err
	}
	return r.List(ctx, dir)
}

// Count counts the entries in dir: with deep false its direct children,
// stopping past ViewCountLimit; with deep true the whole tree, links not
// followed, stopping past DeleteCountLimit.
func (b *Browser) Count(ctx context.Context, side Side, dir string, deep bool) (int, error) {
	stop := ViewCountLimit + 1
	if deep {
		stop = DeleteCountLimit + 1
	}
	if side == Local {
		return b.local.Count(ctx, dir, stop, deep)
	}
	r, err := b.connected()
	if err != nil {
		return 0, err
	}
	return r.Count(ctx, dir, stop, deep)
}

// Stat describes p on side, following a link.
func (b *Browser) Stat(ctx context.Context, side Side, p string) (Entry, error) {
	if side == Local {
		return b.local.Stat(ctx, p)
	}
	r, err := b.connected()
	if err != nil {
		return Entry{}, err
	}
	return r.Stat(ctx, p)
}

// Rename renames from to to inside dir on side, never replacing an
// existing entry (FR-SFTP-010).
func (b *Browser) Rename(ctx context.Context, side Side, dir, from, to string) error {
	if side == Local {
		return b.local.Rename(ctx, dir, from, to)
	}
	r, err := b.connected()
	if err != nil {
		return err
	}
	return r.Rename(ctx, dir, from, to)
}

// Mkdir creates the directory name in dir on side (FR-SFTP-012).
func (b *Browser) Mkdir(ctx context.Context, side Side, dir, name string) error {
	if side == Local {
		return b.local.Mkdir(ctx, dir, name)
	}
	r, err := b.connected()
	if err != nil {
		return err
	}
	return r.Mkdir(ctx, dir, name)
}

// Delete deletes names in dir on side, directories with their contents,
// links as links, stopping at the first error (FR-SFTP-011).
func (b *Browser) Delete(ctx context.Context, side Side, dir string, names []string) (int, error) {
	if side == Local {
		return b.local.Delete(ctx, dir, names)
	}
	r, err := b.connected()
	if err != nil {
		return 0, err
	}
	return r.Delete(ctx, dir, names)
}

// State reports whether the connection is up.
func (b *Browser) State() ConnState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Done is closed when the browser's connection ends.
func (b *Browser) Done() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remote.lease.Conn().Done()
}

// closeGrace is how long Close lets cancelled transfers clean up before
// it closes the channels under them.
const closeGrace = time.Second

// Close cancels every transfer, removing the local part files of
// downloads that had started, closes both channels and releases the
// lease (ARCH-SFTP-007). It waits for the running transfers to stop and
// is idempotent.
func (b *Browser) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	var parts []task
	for _, j := range b.jobs {
		if j.state != Running {
			b.cancelJob(j, &parts)
		}
	}
	b.pending = nil
	r := b.remote
	b.mu.Unlock()
	b.stop() // running jobs stop and remove their parts
	stopped := make(chan struct{})
	term.SafeGo(func() { b.wg.Wait(); close(stopped) }, func(any) { close(stopped) })
	select {
	case <-stopped:
	case <-time.After(closeGrace): // a stalled server: closing the channels unblocks it
	}
	err := r.Close()
	<-stopped
	for i := range parts {
		parts[i].remote = nil // the channels are closed
	}
	b.removeParts(parts)
	b.notify()
	return err
}
