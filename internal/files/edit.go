package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Edit locally (FR-SFTP-019, FR-SFTP-020, design §8.10): a remote file is
// downloaded into a private temporary copy, the UI runs the user's editor
// on it (internal/ui/editor), and the copy is uploaded back if it changed.
// The copy and its directory are removed on every path (SEC-SFTP-005).

// EditMaxSize is the largest file edit-locally takes (FR-SFTP-019).
const EditMaxSize = 64 << 20

// EditDirPattern names the private directory of each edit copy, in the
// OS temp directory.
const EditDirPattern = "sshelf-edit-"

// Errors of edit-locally. Their texts are what the UI shows.
var (
	ErrTooLarge   = errors.New("too large to edit (over 64 MiB)")
	ErrIsDir      = errors.New("is a directory")
	ErrNotRegular = errors.New("not a regular file")
)

// LocalEditSuffix ends the name an edit is saved under when it is not
// uploaded over the remote file (FR-SFTP-019, FR-SFTP-020).
const LocalEditSuffix = ".local-edit"

// EditCopy is one file being edited locally: the private copy and what
// was recorded about the remote file when it was downloaded. Its methods
// are used from one goroutine at a time (the UI's commands run the edit's
// steps one after another).
type EditCopy struct {
	// Name is the remote entry's name, which the copy keeps.
	Name string
	// Path is the copy, which the editor gets as its last argument.
	Path string
	// Remote is the remote file's path, a link resolved with the server's
	// realpath, so the upload replaces the link's target.
	Remote string

	dir   string // the private directory holding Path
	size  int64  // the remote file's size and time at the download
	mtime time.Time
	mode  fs.FileMode
	sum   [sha256.Size]byte // the copy's SHA-256 after the download
	once  sync.Once
}

// EditOutcome is how one step of an edit ended.
type EditOutcome int

// Edit outcomes.
const (
	// EditUnchanged: the copy was not changed; nothing was uploaded.
	EditUnchanged EditOutcome = iota
	// EditSaved: the copy was uploaded over the remote file.
	EditSaved
	// EditSavedAs: the copy was saved under SavedAs, remotely next to the
	// file or in the local pane's directory (Local).
	EditSavedAs
	// EditRemoteChanged: the remote file changed while it was edited;
	// the copy is kept until EditResolve (FR-SFTP-020).
	EditRemoteChanged
	// EditRemoteGone: the remote file was deleted while it was edited.
	EditRemoteGone
	// EditDiscarded: the edit was dropped (keep remote, discard).
	EditDiscarded
)

// EditResult is the outcome of EditFinish or EditResolve.
type EditResult struct {
	Outcome EditOutcome
	Size    int64  // the bytes saved
	SavedAs string // the name saved under, for EditSavedAs
	Local   bool   // SavedAs is in the local pane's directory
}

// EditChoice is the user's answer once an edit could not simply be
// uploaded: the remote file changed (FR-SFTP-020) or the upload failed
// (FR-SFTP-019).
type EditChoice int

// Edit choices.
const (
	EditOverwrite  EditChoice = iota // upload over the remote file anyway
	EditKeepRemote                   // keep the remote file, drop the edit
	EditSaveRemote                   // save as <name>.local-edit next to the remote file
	EditSaveLocal                    // save as <name>.local-edit in the local pane's directory
	EditDiscard                      // drop the edit
	EditRetry                        // try the upload again, checking the remote file first
)

// The crash-cleanup registry (SEC-SFTP-005): the directories of every
// edit copy that exists, so that cmd/sshelf's crash handler can remove
// them.
var editDirs = struct {
	sync.Mutex
	dirs map[string]bool
}{dirs: map[string]bool{}}

// RemoveEditCopies removes every edit copy and its directory. The crash
// handler calls it; it is safe from any goroutine.
func RemoveEditCopies() {
	editDirs.Lock()
	defer editDirs.Unlock()
	for d := range editDirs.dirs {
		_ = os.RemoveAll(d)
		delete(editDirs.dirs, d)
	}
}

// newEditCopy creates the private directory, mode 0700, in the OS temp
// directory and the empty copy in it, mode 0600 (SEC-SFTP-005), and
// registers the directory for the crash handler.
func newEditCopy(name, remote string) (*EditCopy, *os.File, error) {
	if err := CheckName(name); err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", EditDirPattern) // 0700
	if err != nil {
		return nil, nil, err
	}
	editDirs.Lock()
	editDirs.dirs[dir] = true
	editDirs.Unlock()
	c := &EditCopy{Name: name, Path: filepath.Join(dir, name), Remote: remote, dir: dir}
	f, err := os.OpenFile(c.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		c.remove()
		return nil, nil, err
	}
	return c, f, nil
}

// remove deletes the copy and its directory and drops them from the
// crash-cleanup registry, once.
func (c *EditCopy) remove() {
	c.once.Do(func() {
		_ = os.RemoveAll(c.dir)
		editDirs.Lock()
		delete(editDirs.dirs, c.dir)
		editDirs.Unlock()
	})
}

// Dir is the copy's private directory.
func (c *EditCopy) Dir() string { return c.dir }

// changed reports whether the copy's SHA-256 differs from the download's,
// and the copy's size.
func (c *EditCopy) changed() (bool, int64, error) {
	f, err := os.Open(c.Path)
	if err != nil {
		return false, 0, err
	}
	defer f.Close() //nolint:errcheck // read-only
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return false, 0, err
	}
	return !bytes.Equal(h.Sum(nil), c.sum[:]), n, nil
}

// EditFetch downloads the remote file name in dir into a new private copy
// on the transfer channel, outside the queue, recording its size,
// modification time, mode and SHA-256 (design §8.10). A link is resolved
// with the server's realpath first. Directories, other non-regular files
// and files over EditMaxSize are refused before anything is downloaded.
func (b *Browser) EditFetch(ctx context.Context, dir, name string) (*EditCopy, error) {
	if err := CheckName(name); err != nil {
		return nil, err
	}
	r, err := b.connected()
	if err != nil {
		return nil, err
	}
	target := path.Join(dir, name)
	lfi, err := r.lstat(target)
	if err != nil {
		return nil, err
	}
	if lfi.Mode()&fs.ModeSymlink != 0 {
		if target, err = r.realPath(target); err != nil {
			return nil, err
		}
	}
	if err := editable(r.stat(target)); err != nil {
		return nil, err
	}
	src, fi, err := r.openSource(target)
	if err != nil {
		return nil, err
	}
	defer src.Close() //nolint:errcheck // read-only
	if err := editable(fi, nil); err != nil {
		return nil, err
	}
	c, f, err := newEditCopy(name, target)
	if err != nil {
		return nil, err
	}
	if !b.addEdit(c) {
		_ = f.Close()
		c.remove()
		return nil, ErrNotConnected
	}
	h := sha256.New()
	w := &limitWriter{ctx: ctx, w: io.MultiWriter(f, h), limit: fi.Size(), add: func(int64) {}}
	n, err := src.copyTo(0, w)
	if err == nil && n != fi.Size() {
		err = errSizeChanged
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		b.dropEdit(c)
		return nil, err
	}
	c.size, c.mtime, c.mode = fi.Size(), fi.ModTime(), fi.Mode()
	copy(c.sum[:], h.Sum(nil))
	return c, nil
}

// editable refuses what edit-locally does not take.
func editable(fi fs.FileInfo, err error) error {
	switch {
	case err != nil:
		return err
	case fi.IsDir():
		return ErrIsDir
	case !fi.Mode().IsRegular():
		return ErrNotRegular
	case fi.Size() > EditMaxSize:
		return ErrTooLarge
	}
	return nil
}

// addEdit records c as one of the browser's edits, which Close removes;
// it fails once the browser is closed.
func (b *Browser) addEdit(c *EditCopy) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	if b.edits == nil {
		b.edits = map[*EditCopy]bool{}
	}
	b.edits[c] = true
	return true
}

// dropEdit removes c and forgets it.
func (b *Browser) dropEdit(c *EditCopy) {
	c.remove()
	b.mu.Lock()
	delete(b.edits, c)
	b.mu.Unlock()
}

// EditFinish ends an edit after the editor exited with status 0
// (design §8.10): an unchanged copy is removed; a changed one is uploaded
// over the remote file unless that changed or disappeared meanwhile, in
// which case the copy is kept for EditResolve (FR-SFTP-020). When the
// upload fails the copy is kept and the error returned (FR-SFTP-019).
func (b *Browser) EditFinish(ctx context.Context, c *EditCopy) (EditResult, error) {
	changed, size, err := c.changed()
	if err != nil {
		return EditResult{}, err
	}
	if !changed {
		b.dropEdit(c)
		return EditResult{Outcome: EditUnchanged, Size: size}, nil
	}
	r, err := b.connected()
	if err != nil {
		return EditResult{}, err
	}
	fi, err := r.stat(c.Remote)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return EditResult{Outcome: EditRemoteGone, Size: size}, nil
	case err != nil:
		return EditResult{}, err
	case fi.Size() != c.size || !fi.ModTime().Equal(c.mtime):
		return EditResult{Outcome: EditRemoteChanged, Size: size}, nil
	}
	return b.editUpload(ctx, r, c, c.Remote, EditSaved)
}

// EditResolve ends an edit with the user's choice (FR-SFTP-019,
// FR-SFTP-020). localDir is the local pane's directory, for
// EditSaveLocal. The copy is removed once the choice succeeded; when an
// upload or save fails it is kept and the error returned, so the user can
// choose again.
func (b *Browser) EditResolve(ctx context.Context, c *EditCopy, choice EditChoice, localDir string) (EditResult, error) {
	switch choice {
	case EditKeepRemote, EditDiscard:
		b.dropEdit(c)
		return EditResult{Outcome: EditDiscarded}, nil
	case EditRetry:
		return b.EditFinish(ctx, c)
	case EditSaveLocal:
		return b.editSaveLocal(c, localDir)
	}
	r, err := b.connected()
	if err != nil {
		return EditResult{}, err
	}
	if choice == EditOverwrite {
		return b.editUpload(ctx, r, c, c.Remote, EditSaved)
	}
	dir := path.Dir(c.Remote)
	name, err := freeName(c.Name+LocalEditSuffix, func(n string) (fs.FileInfo, error) { return r.lstat(path.Join(dir, n)) })
	if err != nil {
		return EditResult{}, err
	}
	res, err := b.editUpload(ctx, r, c, path.Join(dir, name), EditSavedAs)
	res.SavedAs = name
	return res, err
}

// freeName returns name, or "name (1)", "name (2)" … if it exists
// (FR-SFTP-020). lstat describes a name in the target directory.
func freeName(name string, lstat func(string) (fs.FileInfo, error)) (string, error) {
	for n := 0; n < 10_000; n++ {
		try := name
		if n > 0 {
			try = name + " (" + strconv.Itoa(n) + ")"
		}
		_, err := lstat(try)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return try, nil
		case err != nil:
			return "", err
		}
	}
	return "", ErrExists
}

// editUpload uploads the copy to dst through a part file next to it and
// renames that over dst, keeping the remote file's permission bits
// (FR-SFTP-019, SEC-SFTP-003). On success the copy is removed.
func (b *Browser) editUpload(ctx context.Context, r *RemoteFS, c *EditCopy, dst string, outcome EditOutcome) (EditResult, error) {
	lf, err := os.Open(c.Path)
	if err != nil {
		return EditResult{}, err
	}
	defer lf.Close() //nolint:errcheck // read-only
	st, err := lf.Stat()
	if err != nil {
		return EditResult{}, err
	}
	size := st.Size()
	part := path.Join(path.Dir(dst), PartName(path.Base(dst)))
	rf, err := r.openPart(part, false)
	if err != nil {
		return EditResult{}, err
	}
	err = writeAll(ctx, lf, rf, size)
	if cerr := rf.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = r.finishUpload(part, dst, c.mode, st.ModTime())
	}
	if err != nil {
		_ = r.remove(part) // best effort; the connection may be gone
		return EditResult{}, err
	}
	b.dropEdit(c)
	return EditResult{Outcome: outcome, Size: size}, nil
}

// writeAll writes size bytes of lf to rf in uploadChunk WriteAt calls,
// checking ctx between them.
func writeAll(ctx context.Context, lf *os.File, rf *remoteFile, size int64) error {
	buf := make([]byte, uploadChunk)
	for off := int64(0); off < size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := lf.ReadAt(buf[:min(int64(len(buf)), size-off)], off)
		if n > 0 {
			w, werr := rf.WriteAt(buf[:n], off)
			off += int64(w)
			if werr != nil {
				return werr
			}
		}
		if rerr != nil && (rerr != io.EOF || off < size) {
			return fmt.Errorf("reading the edited copy: %w", rerr)
		}
	}
	return nil
}

// editSaveLocal saves the copy as <name>.local-edit, or the first free
// "<name>.local-edit (n)", in the local directory dir, through a part
// file and a rename in an os.Root (SEC-SFTP-002).
func (b *Browser) editSaveLocal(c *EditCopy, dir string) (EditResult, error) {
	d, err := OpenLocalDir(dir)
	if err != nil {
		return EditResult{}, err
	}
	defer d.Close() //nolint:errcheck // nothing written through the root itself
	name, err := freeName(c.Name+LocalEditSuffix, d.Lstat)
	if err != nil {
		return EditResult{}, err
	}
	src, err := os.Open(c.Path)
	if err != nil {
		return EditResult{}, err
	}
	defer src.Close() //nolint:errcheck // read-only
	pf, err := d.CreatePart(name)
	if err != nil {
		return EditResult{}, err
	}
	n, err := io.Copy(pf, src)
	if err != nil {
		_ = pf.Close()
		_ = d.RemovePart(name)
		return EditResult{}, err
	}
	if err := d.Finish(pf, name, c.mode, time.Now()); err != nil {
		_ = d.RemovePart(name)
		return EditResult{}, err
	}
	b.dropEdit(c)
	return EditResult{Outcome: EditSavedAs, Size: n, SavedAs: name, Local: true}, nil
}

// removeEdits removes every edit copy of the browser (Close).
func (b *Browser) removeEdits() {
	b.mu.Lock()
	edits := b.edits
	b.edits = nil
	b.mu.Unlock()
	for c := range edits {
		c.remove()
	}
}
