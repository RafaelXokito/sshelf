package app

import (
	"context"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/files"
)

// The SFTP tab's handle (design §8.9). The types are internal/files's, so
// that *files.Browser is an app.Browser; UI tests use
// apptest.FakeBrowser.

// ErrNoSFTP means the server refused the SFTP subsystem (FR-SFTP-002,
// FR-SFTP-003).
var ErrNoSFTP = files.ErrNoSFTP

// ErrNotConnected is returned by Browser operations that need the
// connection while it is lost (FR-SFTP-018).
var ErrNotConnected = files.ErrNotConnected

// Side is one pane of an SFTP tab.
type Side = files.Side

// Sides.
const (
	Local  = files.Local
	Remote = files.Remote
)

// Kind is what a directory entry is. Links are never followed in a
// listing.
type Kind = files.Kind

// Entry kinds.
const (
	KindFile    = files.File
	KindDir     = files.Dir
	KindSymlink = files.Symlink
	KindOther   = files.Other // devices, FIFOs, sockets
)

// Entry is one directory entry.
type Entry = files.Entry

// Listing is one directory, already checked and capped by internal/files
// (ARCH-SFTP-002).
type Listing = files.Listing

// Count limits (design §8.9): Count stops one past these, so the caller
// can tell "more than".
const (
	ViewCountLimit   = files.ViewCountLimit   // item counts in the panes (FR-SFTP-006)
	DeleteCountLimit = files.DeleteCountLimit // the delete confirmation (FR-SFTP-011)
)

// TransferID names one transfer of a tab.
type TransferID = files.TransferID

// BatchID names the transfers queued by one copy (one c or drag).
type BatchID = files.BatchID

// TransferState is where a transfer is (design §8.9).
type TransferState = files.TransferState

// Transfer states.
const (
	Queued    = files.Queued
	Running   = files.Running
	Waiting   = files.Waiting // the target exists; Conflict says what to ask
	Paused    = files.Paused  // the connection was lost
	Done      = files.Done
	Failed    = files.Failed // Reason says why
	Cancelled = files.Cancelled
	Skipped   = files.Skipped
)

// FileInfo is the size and modification time of one side of a conflict.
type FileInfo = files.FileInfo

// Conflict is what a waiting transfer asks (FR-SFTP-016): the target
// exists.
type Conflict = files.Conflict

// Transfer is one row of the transfers snapshot.
type Transfer = files.Transfer

// Answer is the reply to a waiting transfer's question (FR-SFTP-016).
type Answer = files.Answer

// Answers.
const (
	Overwrite = files.Overwrite
	Skip      = files.Skip
	KeepBoth  = files.KeepBoth
)

// ConnState is whether an SFTP tab's connection is up.
type ConnState = files.ConnState

// Connection states.
const (
	Connected    = files.Connected
	Disconnected = files.Disconnected
)

// EditCopy is one file being edited locally (FR-SFTP-019, design §8.10).
type EditCopy = files.EditCopy

// EditMaxSize is the largest file edit-locally takes.
const EditMaxSize = files.EditMaxSize

// EditOutcome is how one step of an edit ended.
type EditOutcome = files.EditOutcome

// Edit outcomes.
const (
	EditUnchanged     = files.EditUnchanged
	EditSaved         = files.EditSaved
	EditSavedAs       = files.EditSavedAs
	EditRemoteChanged = files.EditRemoteChanged
	EditRemoteGone    = files.EditRemoteGone
	EditDiscarded     = files.EditDiscarded
)

// EditResult is the outcome of EditFinish or EditResolve.
type EditResult = files.EditResult

// EditChoice is the user's answer when an edit could not simply be
// uploaded (FR-SFTP-019, FR-SFTP-020).
type EditChoice = files.EditChoice

// Edit choices.
const (
	EditOverwrite  = files.EditOverwrite
	EditKeepRemote = files.EditKeepRemote
	EditSaveRemote = files.EditSaveRemote
	EditSaveLocal  = files.EditSaveLocal
	EditDiscard    = files.EditDiscard
	EditRetry      = files.EditRetry
)

// LocalEditSuffix ends the name an edit is saved under when it is not
// uploaded over the remote file.
const LocalEditSuffix = files.LocalEditSuffix

// Browser is the UI-facing handle of one SFTP tab (design §8.9),
// implemented by *files.Browser; UI tests use apptest.FakeBrowser. Every
// method may block on I/O, so the UI calls them from commands only
// (ARCH-UI-002).
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

	// EditFetch downloads the remote file name in dir into a private
	// temporary copy for the editor (design §8.10).
	EditFetch(ctx context.Context, dir, name string) (*EditCopy, error)
	// EditFinish uploads the copy if it changed, once the editor exited
	// with status 0, or reports that the remote file changed meanwhile.
	EditFinish(ctx context.Context, c *EditCopy) (EditResult, error)
	// EditResolve ends the edit with the user's choice; localDir is the
	// local pane's directory, for EditSaveLocal.
	EditResolve(ctx context.Context, c *EditCopy, choice EditChoice, localDir string) (EditResult, error)

	State() ConnState
	// Done is closed when the connection drops; after a reconnect it is
	// a new channel.
	Done() <-chan struct{}
	// Close cancels every transfer, closes both channels and releases
	// the lease.
	Close() error
}

// The SFTP back end is a Browser.
var _ Browser = (*files.Browser)(nil)

// SFTPService opens SFTP tabs (design §8.9); Service includes it. Each
// method reports ErrNoSFTP when the server refuses the subsystem.
type SFTPService interface {
	// OpenSFTP connects to the host through the normal connect flow and
	// opens a browser on the new connection, with no shell.
	OpenSFTP(ctx context.Context, hostID domain.ID) (Browser, error)
	// OpenSFTPOn opens a browser on s's connection, without dialling or
	// authenticating again (FR-SFTP-003).
	// hostID is the session's host, which a reconnect of the tab dials.
	OpenSFTPOn(s Session, hostID domain.ID) (Browser, error)
	// ReconnectSFTP gives b a new connection through the normal connect
	// flow and resumes its paused transfers (FR-SFTP-018).
	ReconnectSFTP(ctx context.Context, b Browser) error
}
