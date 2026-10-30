package files

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
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
		slog.Debug("sftp: one channel only", "err", Reason(err.Error()))
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

// The transfer channel's primitives (ADR-018). transfer.go builds the
// queue's downloads and uploads on them; they all wrap errors with
// remoteErr.

// remoteFile is a file open on the transfer channel.
type remoteFile struct {
	f *sftp.File
}

// openSource opens p for reading and describes the open file (fstat), so
// the size and time recorded are those of what is read.
func (r *RemoteFS) openSource(p string) (*remoteFile, fs.FileInfo, error) {
	f, err := r.transfer.Open(p)
	if err != nil {
		return nil, nil, remoteErr(err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, remoteErr(err)
	}
	return &remoteFile{f: f}, fi, nil
}

// openPart opens the upload part file p for writing, creating it, and
// emptying it unless keep (to resume). A new part's mode is set to
// PartMode (SEC-SFTP-003).
func (r *RemoteFS) openPart(p string, keep bool) (*remoteFile, error) {
	flags := os.O_WRONLY | os.O_CREATE
	if !keep {
		flags |= os.O_TRUNC
	}
	f, err := r.transfer.OpenFile(p, flags)
	if err != nil {
		return nil, remoteErr(err)
	}
	if !keep {
		_ = f.Chmod(PartMode) // best effort: some servers refuse fsetstat
	}
	return &remoteFile{f: f}, nil
}

// copyTo writes the file from offset off to w through the library's
// concurrent WriteTo; it stops when w fails (ADR-016 limitation 3).
func (f *remoteFile) copyTo(off int64, w io.Writer) (int64, error) {
	if _, err := f.f.Seek(off, io.SeekStart); err != nil {
		return 0, remoteErr(err)
	}
	n, err := f.f.WriteTo(w)
	if err != nil && !errors.Is(err, errSizeChanged) && !errors.Is(err, context.Canceled) {
		err = remoteErr(err)
	}
	return n, err
}

// WriteAt writes b at off and returns how much of b, from its start, the
// server acknowledged (ADR-016 limitation 4).
func (f *remoteFile) WriteAt(b []byte, off int64) (int, error) {
	n, err := f.f.WriteAt(b, off)
	return n, remoteErr(err)
}

// Stat describes the open file.
func (f *remoteFile) Stat() (fs.FileInfo, error) {
	fi, err := f.f.Stat()
	return fi, remoteErr(err)
}

// Truncate cuts the open file to size.
func (f *remoteFile) Truncate(size int64) error { return remoteErr(f.f.Truncate(size)) }

// Close closes the file.
func (f *remoteFile) Close() error { return remoteErr(f.f.Close()) }

// lstat describes p on the transfer channel without following a link.
func (r *RemoteFS) lstat(p string) (fs.FileInfo, error) {
	fi, err := r.transfer.Lstat(p)
	return fi, remoteErr(err)
}

// stat describes p on the transfer channel, following a link.
func (r *RemoteFS) stat(p string) (fs.FileInfo, error) {
	fi, err := r.transfer.Stat(p)
	return fi, remoteErr(err)
}

// realPath resolves p, a link included, with the server's realpath. The
// result must be an absolute path.
func (r *RemoteFS) realPath(p string) (string, error) {
	rp, err := r.transfer.RealPath(p)
	if err != nil {
		return "", remoteErr(err)
	}
	if !path.IsAbs(rp) {
		return "", &ServerError{Msg: "realpath: not an absolute path", Err: fs.ErrInvalid}
	}
	return path.Clean(rp), nil
}

// mkdir creates the directory p with the server's default mode.
func (r *RemoteFS) mkdir(p string) error { return remoteErr(r.transfer.Mkdir(p)) }

// remove removes the file p.
func (r *RemoteFS) remove(p string) error { return remoteErr(r.transfer.Remove(p)) }

// renameFallback forces the remove-and-rename path; tests set it.
var renameFallback bool

// finishUpload sets the part's permission bits to mode & 0777 and its
// modification time, then moves it over final: with posix-rename when
// the server offers it, otherwise by removing final first and renaming,
// which leaves a short window without the file (ADR-016 limitation 5,
// SEC-SFTP-003 AC3).
func (r *RemoteFS) finishUpload(part, final string, mode fs.FileMode, mtime time.Time) error {
	if err := r.transfer.Chmod(part, mode&0o777); err != nil {
		return remoteErr(err)
	}
	if err := r.transfer.Chtimes(part, mtime, mtime); err != nil {
		return remoteErr(err)
	}
	if _, ok := r.transfer.HasExtension("posix-rename@openssh.com"); ok && !renameFallback {
		return remoteErr(r.transfer.PosixRename(part, final))
	}
	if err := r.transfer.Remove(final); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return remoteErr(err)
	}
	return remoteErr(r.transfer.Rename(part, final))
}

// lostWait is how long a failed transfer waits for its connection to be
// reported down before it counts as failed.
const lostWait = time.Second

// lost reports whether err, from an operation of the remote side, came
// from the connection dropping rather than from the server's answer: it
// is not a status, and the connection ends within lostWait.
func (r *RemoteFS) lost(err error) bool {
	var se *ServerError
	if !errors.As(err, &se) || answered(se.Err) {
		return false
	}
	t := time.NewTimer(lostWait)
	defer t.Stop()
	select {
	case <-r.lease.Conn().Done():
		return true
	case <-t.C:
		return false
	}
}

// answered reports whether err is the server's answer to a request (a
// status), so the connection was still up when it was sent.
func answered(err error) bool {
	var se *sftp.StatusError
	return errors.As(err, &se) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission)
}
