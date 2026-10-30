package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/RafaelXokito/sshelf/internal/term"
)

// One transfer (ADR-018): downloads through File.WriteTo into a local part
// file, uploads through 1 MiB WriteAt calls into a remote part file, and
// the part renamed over the target once complete (SEC-SFTP-003).

// uploadChunk is the size of each upload WriteAt (ARCH-SFTP-004).
const uploadChunk = 1 << 20

// errChanged means the source changed between its Lstat and its open.
var errChanged = errors.New("file changed while it was opened")

// task is what a running job works on: a copy of its fields taken under
// b.mu when it starts, so the goroutine reads no shared state.
type task struct {
	j      *job
	remote *RemoteFS
	from   Side
	srcDir string
	name   string
	dstTop string
	dstRel []string
	target string
	kind   Kind
}

// task copies j's fields. The caller holds b.mu.
func (b *Browser) task(j *job) task {
	return task{
		j: j, remote: b.remote, from: j.from, srcDir: j.srcDir, name: j.name,
		dstTop: j.dstTop, dstRel: slices.Clone(j.dstRel), target: j.target, kind: j.kind,
	}
}

func (t task) srcPath() string {
	if t.from == Remote {
		return path.Join(t.srcDir, t.name)
	}
	return filepath.Join(t.srcDir, t.name)
}

// remoteDir is an upload's target directory.
func (t task) remoteDir() string { return path.Join(append([]string{t.dstTop}, t.dstRel...)...) }

// run transfers one job.
func (b *Browser) run(ctx context.Context, t task) error {
	term.Fault("sftp-transfer")
	if t.from == Remote {
		return b.download(ctx, t)
	}
	return b.upload(ctx, t)
}

// setKind records the kind a job named in Copy turned out to be.
func (b *Browser) setKind(j *job, k Kind) {
	b.mu.Lock()
	j.kind = k
	b.mu.Unlock()
}

// skipKind is the skip for an entry that is not copied (FR-SFTP-001).
func skipKind(k Kind) error {
	if k == Symlink {
		return &skipError{"symbolic link"}
	}
	return &skipError{"special file"}
}

// download copies a remote file or directory into the local target.
func (b *Browser) download(ctx context.Context, t task) error {
	r, src := t.remote, t.srcPath()
	kind := t.kind
	if kind == kindUnknown { // named in Copy: a link to a file is copied as the file
		fi, err := r.lstat(src)
		if err != nil {
			return err
		}
		kind = kindOf(fi.Mode())
		if kind == Symlink {
			if fi, err := r.stat(src); err == nil && fi.Mode().IsRegular() {
				kind = File
			}
		}
		b.setKind(t.j, kind)
	}
	if kind != File && kind != Dir {
		return skipKind(kind)
	}
	parent, err := openLocalChain(t.dstTop, t.dstRel)
	if err != nil {
		return err
	}
	defer parent.Close() //nolint:errcheck // nothing written through the root itself
	if kind == Dir {
		sub, err := parent.Subdir(t.target)
		if err != nil {
			return err
		}
		_ = sub.Close()
		l, err := r.List(ctx, src)
		if err != nil {
			return err
		}
		children := make([]child, len(l.Entries))
		for i, e := range l.Entries {
			children[i] = child{name: e.Name, kind: e.Kind, size: e.Size}
		}
		b.addChildren(t.j, children, l.Skipped, l.Truncated)
		return nil
	}

	f, fi, err := r.openSource(src)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-only
	if !fi.Mode().IsRegular() {
		return skipKind(kindOf(fi.Mode()))
	}
	src2 := FileInfo{Size: fi.Size(), ModTime: fi.ModTime()}
	target, err := b.resolveTarget(t, src2, parent.Lstat)
	if err != nil {
		return err
	}
	size := fi.Size()
	if pfi, err := parent.Lstat(PartName(target)); err == nil {
		if !pfi.Mode().IsRegular() {
			return ErrUnsafePart // never written through (SEC-SFTP-002 AC3)
		}
		if err := parent.RemovePart(target); err != nil { // an earlier run's part is never resumed
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	pf, err := parent.CreatePart(target)
	if err != nil {
		return err
	}
	b.setTotal(t.j, size, 0)
	if testHookOpened != nil {
		testHookOpened(t.j.rel)
	}
	w := &limitWriter{ctx: ctx, w: pf, limit: size, add: func(n int64) { b.progress(t.j, n) }}
	n, err := f.copyTo(0, w)
	if err == nil && n != size {
		err = errSizeChanged
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = pf.Close()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if ctx.Err() != nil || errors.Is(err, errSizeChanged) {
			_ = parent.RemovePart(target)
		}
		return err
	}
	return parent.Finish(pf, target, fi.Mode(), fi.ModTime())
}

// testHookChunk runs before each block of data a transfer moves: each
// download write and each upload WriteAt.
var testHookChunk func()

// testHookOpened runs once a file transfer has opened its source and its
// part file, before any data moves.
var testHookOpened func(rel string)

// upload copies a local file or directory into the remote target.
func (b *Browser) upload(ctx context.Context, t task) error {
	r, src := t.remote, t.srcPath()
	lfi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	kind, follow := kindOf(lfi.Mode()), false
	if kind == Symlink && t.kind == kindUnknown { // named in Copy
		if sfi, err := os.Stat(src); err == nil && sfi.Mode().IsRegular() {
			kind, lfi, follow = File, sfi, true
		}
	}
	if t.kind == kindUnknown {
		b.setKind(t.j, kind)
	}
	if kind != File && kind != Dir {
		return skipKind(kind)
	}
	dir := t.remoteDir()
	if kind == Dir {
		dst := path.Join(dir, t.target)
		fi, err := r.lstat(dst)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if err := r.mkdir(dst); err != nil {
				return err
			}
		case err != nil:
			return err
		case !fi.IsDir():
			return ErrNotDir
		}
		l, err := b.local.List(ctx, src)
		if err != nil {
			return err
		}
		children := make([]child, len(l.Entries))
		for i, e := range l.Entries {
			children[i] = child{name: e.Name, kind: e.Kind, size: e.Size}
			if CheckName(e.Name) != nil { // a local name sshelf will not send
				children[i].kind = kindUnknown
			}
		}
		b.addChildren(t.j, children, 0, l.Truncated)
		return nil
	}

	flags := os.O_RDONLY
	if !follow {
		flags |= syscall.O_NOFOLLOW
	}
	lf, err := os.OpenFile(src, flags, 0) //nolint:gosec // a file the user chose to upload
	if err != nil {
		return err
	}
	defer lf.Close() //nolint:errcheck // read-only
	ffi, err := lf.Stat()
	if err != nil {
		return err
	}
	if !ffi.Mode().IsRegular() || !os.SameFile(lfi, ffi) {
		return errChanged
	}
	size := ffi.Size()
	target, err := b.resolveTarget(t, FileInfo{Size: size, ModTime: ffi.ModTime()}, func(name string) (fs.FileInfo, error) {
		return r.lstat(path.Join(dir, name))
	})
	if err != nil {
		return err
	}
	part, final := path.Join(dir, PartName(target)), path.Join(dir, target)
	rf, err := r.openPart(part, false)
	if err != nil {
		return err
	}
	b.setTotal(t.j, size, 0)
	if testHookOpened != nil {
		testHookOpened(t.j.rel)
	}
	_, err = b.send(ctx, t.j, lf, rf, 0, size)
	if err == nil {
		var st fs.FileInfo
		if st, err = rf.Stat(); err == nil && st.Size() != size {
			err = errSizeChanged
		}
	}
	if cerr := rf.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if ctx.Err() != nil || errors.Is(err, errSizeChanged) {
			if rerr := r.remove(part); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				return fmt.Errorf("remote part file not removed: %w", rerr)
			}
		}
		return err
	}
	return r.finishUpload(part, final, ffi.Mode(), ffi.ModTime())
}

// send uploads lf from off to size in uploadChunk WriteAt calls, checking
// ctx between them, and returns the offset the server has acknowledged
// (ADR-016 limitations 3 and 4). A source that ends early or grows fails
// with errSizeChanged.
func (b *Browser) send(ctx context.Context, j *job, lf *os.File, rf *remoteFile, off, size int64) (int64, error) {
	buf := make([]byte, uploadChunk)
	for off < size {
		if err := ctx.Err(); err != nil {
			return off, err
		}
		if testHookChunk != nil {
			testHookChunk()
		}
		n, rerr := lf.ReadAt(buf[:min(int64(len(buf)), size-off)], off)
		if n > 0 {
			w, werr := rf.WriteAt(buf[:n], off)
			off += int64(w)
			b.progress(j, int64(w))
			if werr != nil {
				return off, werr
			}
		}
		switch {
		case rerr == io.EOF && off < size:
			return off, errSizeChanged
		case rerr != nil && rerr != io.EOF:
			return off, rerr
		}
	}
	var one [1]byte
	if n, _ := lf.ReadAt(one[:], size); n > 0 {
		return off, errSizeChanged
	}
	return off, nil
}

// resolveTarget decides where a file goes when its target exists
// (FR-SFTP-016): it parks the job as Waiting unless it was answered or
// its batch has an answer for all. lstat describes a name in the target
// directory without following links. A directory in the way fails.
func (b *Browser) resolveTarget(t task, src FileInfo, lstat func(string) (fs.FileInfo, error)) (string, error) {
	fi, err := lstat(t.target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return t.target, nil
	case err != nil:
		return "", err
	case fi.IsDir():
		return "", ErrNotFile
	}
	a, ok := b.batchAnswer(t.j)
	if !ok {
		dst := FileInfo{Size: fi.Size(), ModTime: fi.ModTime()}
		c := Conflict{Local: dst, Remote: src}
		if t.from == Local {
			c = Conflict{Local: src, Remote: dst}
		}
		return "", &conflictError{c}
	}
	switch a {
	case Skip:
		return "", &skipError{ErrExists.Error()}
	case KeepBoth:
		for n := 1; n < 10_000; n++ {
			name := KeepBothName(t.name, n)
			if _, err := lstat(name); errors.Is(err, fs.ErrNotExist) {
				b.setTarget(t.j, name)
				return name, nil
			} else if err != nil {
				return "", err
			}
		}
		return "", ErrExists
	}
	return t.target, nil
}

// KeepBothName is the n-th "keep both" name of name: "<stem> (n)<ext>",
// where the extension starts at the first dot that does not begin the
// name, so "backup.tar.gz" becomes "backup (1).tar.gz" and ".bashrc"
// ".bashrc (1)" (FR-SFTP-016).
func KeepBothName(name string, n int) string {
	stem, ext := name, ""
	if i := strings.IndexByte(name[min(1, len(name)):], '.'); i >= 0 {
		stem, ext = name[:i+1], name[i+1:]
	}
	return stem + " (" + strconv.Itoa(n) + ")" + ext
}

// openLocalChain opens top and then each directory of rel below it, each
// from its parent's root, refusing anything that is not a directory, so
// a directory swapped for a link fails the open (design §8.9).
func openLocalChain(top string, rel []string) (*LocalDir, error) {
	d, err := OpenLocalDir(top)
	if err != nil {
		return nil, err
	}
	for _, name := range rel {
		sub, err := d.Subdir(name)
		_ = d.Close()
		if err != nil {
			return nil, err
		}
		d = sub
	}
	return d, nil
}

// removePart removes the part file a stopped job left, best effort: the
// local one of a download, the remote one of an upload while connected.
func removePart(t task) {
	if t.from == Remote {
		if d, err := openLocalChain(t.dstTop, t.dstRel); err == nil {
			_ = d.RemovePart(t.target)
			_ = d.Close()
		}
		return
	}
	if t.remote != nil {
		_ = t.remote.remove(path.Join(t.remoteDir(), PartName(t.target)))
	}
}

// removeParts removes the part files of jobs cancelled while not running.
func (b *Browser) removeParts(ts []task) {
	for _, t := range ts {
		removePart(t)
	}
}

// limitWriter is the download's writer (ARCH-SFTP-004): it fails once
// ctx is cancelled, which is how a download is stopped (WriteTo takes no
// context, ADR-016 limitation 3), and it stops at limit, the size the
// server reported, failing with errSizeChanged (SEC-SFTP-004).
type limitWriter struct {
	ctx      context.Context
	w        io.Writer
	n, limit int64
	add      func(int64)
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if testHookChunk != nil {
		testHookChunk()
	}
	if err := l.ctx.Err(); err != nil {
		return 0, err
	}
	over := false
	if rest := l.limit - l.n; int64(len(p)) > rest {
		p, over = p[:rest], true
	}
	n, err := l.w.Write(p)
	l.n += int64(n)
	l.add(int64(n))
	if err == nil && over {
		err = errSizeChanged
	}
	return n, err
}
