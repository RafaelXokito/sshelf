package files

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/sshclient"
)

// The remote side (ADR-016, ADR-018): the only file of sshelf that imports
// github.com/pkg/sftp (ARCH-SFTP-001).

// ErrNoSFTP means the server refused the sftp subsystem.
var ErrNoSFTP = errors.New("the server does not offer SFTP")

// ErrTooDeep means a recursive walk reached MaxDepth (SEC-SFTP-004).
var ErrTooDeep = errors.New("too many nested directories")

const (
	// ListTimeout bounds every remote listing (SEC-SFTP-004).
	ListTimeout = 30 * time.Second
	// ListBudget is the most listing data read from the browse channel
	// for one listing (SEC-SFTP-004, ADR-016 limitation 1).
	ListBudget = 64 << 20
)

// listBudget and listTimeout are the limits; tests lower them.
var (
	listBudget  int64 = ListBudget
	listTimeout       = ListTimeout
)

// RemoteFS is the remote side of one SFTP tab: a browse channel for
// listings and operations and a transfer channel for the queue, both on
// one connection lease (ADR-018).
type RemoteFS struct {
	lease    *sshclient.Lease
	browse   *sftp.Client
	transfer *sftp.Client // == browse when the server refused a second channel
	counter  *countingReader
	start    string

	listMu    sync.Mutex // one listing at a time measures the budget
	closeOnce sync.Once
	closeErr  error
}

// OpenRemote opens the browse channel through sftp.NewClientPipe behind a
// byte-counting reader, and the transfer channel; when the server refuses
// the second channel (MaxSessions) both uses share the first. It owns
// lease from now on, also when it fails, and releases it on Close
// (ADR-017). It fails with ErrNoSFTP when the server refuses the
// subsystem.
func OpenRemote(lease *sshclient.Lease) (*RemoteFS, error) {
	client := lease.Client()
	browse, counter, err := openBrowse(client)
	if err != nil {
		lease.Release()
		return nil, err
	}
	r := &RemoteFS{lease: lease, browse: browse, transfer: browse, counter: counter}
	if t, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true)); err == nil {
		r.transfer = t
	} else {
		slog.Debug("sftp: one channel only", "err", err)
	}
	r.start = "/"
	if wd, err := browse.RealPath("."); err == nil && path.IsAbs(wd) {
		r.start = path.Clean(wd)
	}
	return r, nil
}

// openBrowse starts the sftp subsystem on a new channel and runs a client
// on it whose replies are counted.
func openBrowse(client *ssh.Client) (*sftp.Client, *countingReader, error) {
	s, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	w, err := s.StdinPipe()
	if err != nil {
		_ = s.Close()
		return nil, nil, err
	}
	out, err := s.StdoutPipe()
	if err != nil {
		_ = s.Close()
		return nil, nil, err
	}
	if err := s.RequestSubsystem("sftp"); err != nil {
		_ = s.Close()
		return nil, nil, ErrNoSFTP
	}
	cr := &countingReader{r: out}
	c, err := sftp.NewClientPipe(cr, sessionCloser{w, s})
	if err != nil {
		_ = s.Close()
		return nil, nil, err
	}
	return c, cr, nil
}

// sessionCloser closes the channel when the client closes its writer.
type sessionCloser struct {
	io.WriteCloser
	s *ssh.Session
}

func (c sessionCloser) Close() error {
	_ = c.WriteCloser.Close()
	return c.s.Close()
}

// countingReader counts the bytes the browse channel delivers and
// cancels the running listing once it read more than its budget.
type countingReader struct {
	r io.Reader
	n atomic.Int64

	mu     sync.Mutex
	limit  int64 // n at which to cancel; 0: no listing watched
	cancel context.CancelFunc
	hit    bool
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	total := c.n.Add(int64(n))
	c.mu.Lock()
	if c.limit > 0 && total > c.limit && !c.hit {
		c.hit = true
		c.cancel()
	}
	c.mu.Unlock()
	return n, err
}

// watch cancels with cancel once budget more bytes were read, until the
// returned stop is called; stop reports whether the budget was hit.
func (c *countingReader) watch(budget int64, cancel context.CancelFunc) (stop func() bool) {
	c.mu.Lock()
	c.limit, c.cancel, c.hit = c.n.Load()+budget, cancel, false
	c.mu.Unlock()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		hit := c.hit
		c.limit, c.cancel, c.hit = 0, nil, false
		return hit
	}
}

// ConnID identifies the connection the remote side runs on.
func (r *RemoteFS) ConnID() uint64 { return r.lease.Conn().ID() }

// Channels reports how many SFTP channels the remote side uses: 2, or 1
// when the server refused the second (ADR-018).
func (r *RemoteFS) Channels() int {
	if r.transfer == r.browse {
		return 1
	}
	return 2
}

// Start is the remote starting directory: the server's realpath of ".",
// or "/" when that failed or was not absolute (FR-SFTP-005). It holds the
// exact bytes; show it through Display.
func (r *RemoteFS) Start() string { return r.start }

// List reads dir on the browse channel within ListTimeout and, with two
// channels, within ListBudget bytes of replies; a listing cut by the
// budget or by MaxEntries is Truncated. Entries whose names fail
// CheckName are counted in Skipped and left out (SEC-SFTP-001,
// SEC-SFTP-004, ARCH-SFTP-002).
func (r *RemoteFS) List(ctx context.Context, dir string) (Listing, error) {
	l := Listing{Side: Remote, Dir: dir, Title: Display(dir)}
	infos, cut, err := r.readDir(ctx, dir)
	if err != nil {
		return l, err
	}
	l.Truncated = cut
	for _, fi := range infos {
		if len(l.Entries) == listCap {
			l.Truncated = true
			break
		}
		if CheckName(fi.Name()) != nil {
			l.Skipped++
			continue
		}
		l.Entries = append(l.Entries, entryOf(fi.Name(), fi))
	}
	return l, nil
}

// readDir lists dir with the limits of List; cut reports the budget was
// hit (the entries read so far are returned).
func (r *RemoteFS) readDir(ctx context.Context, dir string) (infos []fs.FileInfo, cut bool, err error) {
	r.listMu.Lock()
	defer r.listMu.Unlock()
	lctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	budget := func() bool { return false }
	if r.Channels() == 2 {
		budget = r.counter.watch(listBudget, cancel)
	}
	infos, err = r.browse.ReadDirContext(lctx, dir)
	if budget() {
		return infos, true, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, remoteErr(err)
	}
	return infos, false, nil
}

// Count counts the entries in dir with safe names and stops once it has
// counted stop of them. With deep, it counts every entry below dir,
// descending at most MaxDepth levels and never into a symbolic link.
func (r *RemoteFS) Count(ctx context.Context, dir string, stop int, deep bool) (int, error) {
	n := 0
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		infos, _, err := r.readDir(ctx, dir)
		if err != nil {
			return err
		}
		for _, fi := range infos {
			if CheckName(fi.Name()) != nil {
				continue
			}
			n++
			if n >= stop {
				return nil
			}
			if deep && depth < MaxDepth && fi.IsDir() { // listing attrs are Lstat's: a link is not a dir
				if err := walk(path.Join(dir, fi.Name()), depth+1); err != nil || n >= stop {
					return err
				}
			}
		}
		return nil
	}
	err := walk(dir, 1)
	return n, err
}

// Stat describes p, following a symbolic link (FR-SFTP-007).
func (r *RemoteFS) Stat(_ context.Context, p string) (Entry, error) {
	fi, err := r.browse.Stat(p)
	if err != nil {
		return Entry{}, remoteErr(err)
	}
	return entryOf(path.Base(p), fi), nil
}

// Rename renames from to to inside dir. It never replaces an existing
// entry: when to exists, it fails with ErrExists (FR-SFTP-010).
func (r *RemoteFS) Rename(_ context.Context, dir, from, to string) error {
	if err := CheckName(from); err != nil {
		return err
	}
	if err := CheckName(to); err != nil {
		return err
	}
	if err := r.absent(path.Join(dir, to)); err != nil {
		return err
	}
	return remoteErr(r.browse.Rename(path.Join(dir, from), path.Join(dir, to)))
}

// Mkdir creates the directory name in dir with the server's default mode
// (FR-SFTP-012); an existing name fails with ErrExists.
func (r *RemoteFS) Mkdir(_ context.Context, dir, name string) error {
	if err := CheckName(name); err != nil {
		return err
	}
	p := path.Join(dir, name)
	if err := r.absent(p); err != nil {
		return err
	}
	return remoteErr(r.browse.Mkdir(p))
}

// absent fails with ErrExists when p exists.
func (r *RemoteFS) absent(p string) error {
	_, err := r.browse.Lstat(p)
	switch {
	case err == nil:
		return ErrExists
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return remoteErr(err)
	}
}

// Delete removes each of names from dir with everything below it, walking
// with Lstat itself: links are removed as links and never followed, and
// it descends at most MaxDepth levels (FR-SFTP-011, SEC-SFTP-004). It
// stops at the first error and returns how many of names it removed.
func (r *RemoteFS) Delete(ctx context.Context, dir string, names []string) (int, error) {
	for i, name := range names {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := CheckName(name); err != nil {
			return i, err
		}
		if err := r.removeAll(ctx, path.Join(dir, name), 1); err != nil {
			return i, err
		}
	}
	return len(names), nil
}

func (r *RemoteFS) removeAll(ctx context.Context, p string, depth int) error {
	fi, err := r.browse.Lstat(p)
	if err != nil {
		return remoteErr(err)
	}
	if !fi.IsDir() {
		return remoteErr(r.browse.Remove(p))
	}
	if depth > MaxDepth {
		return ErrTooDeep
	}
	infos, _, err := r.readDir(ctx, p)
	if err != nil {
		return err
	}
	for _, c := range infos {
		if err := ctx.Err(); err != nil {
			return err
		}
		if CheckName(c.Name()) != nil {
			return ErrBadName // an entry sshelf cannot name safely: leave the directory
		}
		if err := r.removeAll(ctx, path.Join(p, c.Name()), depth+1); err != nil {
			return err
		}
	}
	return remoteErr(r.browse.RemoveDirectory(p))
}

// Close closes both channels and releases the lease, once.
func (r *RemoteFS) Close() error {
	r.closeOnce.Do(func() {
		if r.transfer != r.browse {
			_ = r.transfer.Close()
		}
		r.closeErr = r.browse.Close()
		r.lease.Release()
	})
	return r.closeErr
}

// ServerError is a remote failure whose text came from the server, made
// safe to show (SEC-SFTP-008). It unwraps to the library's error, so
// errors.Is(err, fs.ErrNotExist) and fs.ErrPermission still work.
type ServerError struct {
	Msg string
	Err error
}

func (e *ServerError) Error() string { return e.Msg }
func (e *ServerError) Unwrap() error { return e.Err }

// remoteErr wraps a pkg/sftp error so that its text is safe (SEC-SFTP-008).
// A status message is taken out of the library's %q quoting first
// (ADR-016 limitation 8).
func remoteErr(err error) error {
	if err == nil {
		return nil
	}
	var se *sftp.StatusError
	msg := err.Error()
	if errors.As(err, &se) {
		msg = statusText(se)
	}
	return &ServerError{Msg: Reason(msg), Err: err}
}

// statusText returns a status error's message: StatusError.Error is
// `sftp: "<msg>" (<code>)`.
func statusText(se *sftp.StatusError) string {
	s := strings.TrimPrefix(se.Error(), "sftp: ")
	if i := strings.LastIndex(s, " ("); i > 0 {
		if m, err := strconv.Unquote(s[:i]); err == nil {
			return m
		}
	}
	return s
}
