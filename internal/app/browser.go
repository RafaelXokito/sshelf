package app

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// The SFTP tab's handle (design §8.9). The types are defined here so
// the UI can be built and tested against apptest.FakeBrowser; T-054
// turns them into aliases of the internal/files types, which keep the
// same names and fields.

// ErrNoSFTP means the server refused the SFTP subsystem (FR-SFTP-002,
// FR-SFTP-003).
var ErrNoSFTP = errors.New("the server does not offer SFTP")

// ErrNotConnected is returned by Browser operations that need the
// connection while it is lost (FR-SFTP-018).
var ErrNotConnected = errors.New("not connected")

// Side is one pane of an SFTP tab.
type Side int

// Sides.
const (
	Local Side = iota
	Remote
)

// Other returns the other side.
func (s Side) Other() Side { return 1 - s }

// String is "Local" or "Remote", as the pane titles say.
func (s Side) String() string {
	if s == Remote {
		return "Remote"
	}
	return "Local"
}

// Kind is what a directory entry is. Links are never followed in a
// listing.
type Kind int

// Entry kinds.
const (
	KindFile Kind = iota
	KindDir
	KindSymlink
	KindOther // devices, FIFOs, sockets
)

// Entry is one directory entry.
type Entry struct {
	Name    string      // checked by SEC-SFTP-001; the exact bytes used for operations
	Display string      // sanitised for the screen; invalid UTF-8 as U+FFFD
	Kind    Kind        // Dir, File, Symlink, Other
	Size    int64       // of the entry itself (a link's own size)
	Mode    fs.FileMode // permission bits
	ModTime time.Time
}

// Listing is one directory, already checked and capped by internal/files
// (ARCH-SFTP-002).
type Listing struct {
	Side      Side
	Dir       string  // as used for operations
	Title     string  // sanitised, ~ for the local home
	Entries   []Entry // unsorted; the view sorts
	Skipped   int     // unsafe names left out (FR-SFTP-024)
	Truncated bool    // stopped at 100,000 entries or 64 MiB (SEC-SFTP-004)
}

// Count limits (design §8.9): Count stops one past these, so the caller
// can tell "more than".
const (
	ViewCountLimit   = 10_000  // item counts in the panes (FR-SFTP-006)
	DeleteCountLimit = 100_000 // the delete confirmation (FR-SFTP-011)
)

// TransferID names one transfer of a tab.
type TransferID uint64

// BatchID names the transfers queued by one copy (one c or drag).
type BatchID uint64

// TransferState is where a transfer is (design §8.9).
type TransferState int

// Transfer states.
const (
	Queued TransferState = iota
	Running
	Waiting // the target exists; Conflict says what to ask
	Paused  // the connection was lost
	Done
	Failed // Reason says why
	Cancelled
	Skipped
)

// String is the state as the transfers view writes it (FR-SFTP-015).
func (s TransferState) String() string {
	return [...]string{"queued", "running", "waiting", "paused", "done", "failed", "cancelled", "skipped"}[s]
}

// Finished reports whether the transfer will not run again unless it is
// retried.
func (s TransferState) Finished() bool {
	return s == Done || s == Failed || s == Cancelled || s == Skipped
}

// Pending reports whether the transfer still counts as work to stop:
// queued, running, waiting or paused (FR-SFTP-025, FR-TAB-009).
func (s TransferState) Pending() bool { return !s.Finished() }

// FileInfo is the size and modification time of one side of a conflict.
type FileInfo struct {
	Size    int64
	ModTime time.Time
}

// Conflict is what a waiting transfer asks (FR-SFTP-016): the target
// exists.
type Conflict struct {
	Local, Remote FileInfo
}

// Transfer is one row of the transfers snapshot.
type Transfer struct {
	ID    TransferID
	Batch BatchID
	// Direction is the side the data comes from: Remote is a download
	// (↓), Local an upload (↑).
	From   Side
	Name   string // displayed, sanitised; a path relative to the copy's source dir
	State  TransferState
	Done   int64         // bytes transferred
	Total  int64         // bytes in all; 0 when unknown (a directory)
	Rate   float64       // bytes per second, a 5 s moving average
	ETA    time.Duration // 0 when the rate is 0
	Reason string        // why it failed, was skipped or restarted; sanitised
	// DstDir is the directory the transfer writes into, so the UI can
	// refresh a pane showing it when the transfer is done.
	DstDir   string
	Conflict *Conflict // set while Waiting
	// Dir marks a directory: it is done once its target exists and its
	// contents are queued as rows of their own, so it is not a copied file.
	Dir bool
}

// Answer is the reply to a waiting transfer's question (FR-SFTP-016).
type Answer int

// Answers.
const (
	Overwrite Answer = iota
	Skip
	KeepBoth
)

// ConnState is whether an SFTP tab's connection is up.
type ConnState int

// Connection states.
const (
	Connected ConnState = iota
	Disconnected
)

// Browser is the UI-facing handle of one SFTP tab (design §8.9),
// implemented by *files.Browser; UI tests use apptest.FakeBrowser. Every
// method may block on I/O, so the UI calls them from commands only
// (ARCH-UI-002). The edit-locally methods arrive with T-055.
type Browser interface {
	ConnID() uint64
	// Start is the side's starting directory (FR-SFTP-005).
	Start(side Side) string
	List(ctx context.Context, side Side, dir string) (Listing, error)
	// Count counts the entries in dir: with deep false its direct
	// children, stopping past ViewCountLimit (the panes' item counts);
	// with deep true the whole tree, links not followed, stopping past
	// DeleteCountLimit (the delete confirmation).
	Count(ctx context.Context, side Side, dir string, deep bool) (int, error)
	// Stat follows a link (enter on a symlink).
	Stat(ctx context.Context, side Side, path string) (Entry, error)
	Rename(ctx context.Context, side Side, dir, from, to string) error
	Mkdir(ctx context.Context, side Side, dir, name string) error
	// Delete deletes names in dir, directories with their contents,
	// links as links, stopping at the first error.
	Delete(ctx context.Context, side Side, dir string, names []string) (deleted int, err error)

	// Copy queues a copy of names from srcDir on side from into dstDir on
	// the other side and returns at once.
	Copy(from Side, srcDir string, names []string, dstDir string) BatchID
	// Transfers is a snapshot of the queue, oldest first.
	Transfers() []Transfer
	Answer(id TransferID, a Answer, all bool)
	Cancel(id TransferID)
	Retry(id TransferID)
	// Clear removes the finished rows (C in the transfers view).
	Clear()
	// Updates delivers coalesced state changes (ARCH-SFTP-003).
	Updates() <-chan struct{}

	State() ConnState
	// Done is closed when the connection drops; after a reconnect it is
	// a new channel.
	Done() <-chan struct{}
	// Close cancels every transfer, closes both channels and releases
	// the lease.
	Close() error
}

// SFTPService opens SFTP tabs (design §8.9). The UI finds it on its
// Service with a type assertion until T-054 adds these methods to
// Service itself.
type SFTPService interface {
	// OpenSFTP connects to the host through the normal connect flow and
	// opens a browser on the new connection, with no shell.
	OpenSFTP(ctx context.Context, hostID domain.ID) (Browser, error)
	// OpenSFTPOn opens a browser on s's connection, without dialling or
	// authenticating again (FR-SFTP-003).
	OpenSFTPOn(s Session) (Browser, error)
	// ReconnectSFTP gives b a new connection through the normal connect
	// flow and resumes its paused transfers (FR-SFTP-018).
	ReconnectSFTP(ctx context.Context, b Browser) error
}
