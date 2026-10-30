package files

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Errors of the local side. Their texts are what the UI shows.
var (
	ErrExists     = errors.New("already exists")
	ErrNotDir     = errors.New("exists and is not a directory")
	ErrUnsafePart = errors.New("unsafe part file")
	ErrBadName    = errors.New("invalid name")
)

// DirMode is the mode of a directory sshelf creates locally, before the
// umask (FR-SFTP-012).
const DirMode fs.FileMode = 0o755

// PartMode is the mode of a part file while it is written (SEC-SFTP-003).
const PartMode fs.FileMode = 0o600

// LocalFS is the local side of an SFTP tab. Every method may block on the
// file system and is called off the UI goroutine (ARCH-SFTP-002). Every
// change goes through an os.Root opened on the directory it happens in, on
// a single checked name, so it never leaves that directory and never
// writes through a symbolic link (SEC-SFTP-002).
type LocalFS struct{}

// Start is the directory the local pane opens in: the working directory
// sshelf was started in, or the home directory when that can no longer be
// read, or / (FR-SFTP-005). sshelf never changes its working directory.
func (LocalFS) Start() string {
	if wd, err := os.Getwd(); err == nil && readable(wd) {
		return wd
	}
	if home, err := os.UserHomeDir(); err == nil && readable(home) {
		return home
	}
	return "/"
}

// readable reports whether dir is a directory that can be listed.
func readable(dir string) bool {
	f, err := os.Open(dir) //nolint:gosec // reading a directory the user browses
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // read-only
	_, err = f.ReadDir(1)
	return err == nil || err == io.EOF
}

// listCap is MaxEntries; tests lower it.
var listCap = MaxEntries

// List reads dir with Lstat semantics (links are not followed), stopping
// after MaxEntries entries.
func (LocalFS) List(ctx context.Context, dir string) (Listing, error) {
	l := Listing{Side: Local, Dir: dir, Title: localTitle(dir)}
	f, err := os.Open(dir) //nolint:gosec // reading a directory the user browses
	if err != nil {
		return l, err
	}
	defer f.Close() //nolint:errcheck // read-only
	for {
		if err := ctx.Err(); err != nil {
			return l, err
		}
		ds, err := f.ReadDir(1024)
		for _, d := range ds {
			if len(l.Entries) == listCap {
				l.Truncated = true
				return l, nil
			}
			info, ierr := d.Info() // Lstat
			if ierr != nil {
				continue // removed since it was read
			}
			l.Entries = append(l.Entries, entryOf(d.Name(), info))
		}
		if err == io.EOF {
			return l, nil
		}
		if err != nil {
			return l, err
		}
	}
}

// Count counts the entries in dir and stops once it has counted stop of
// them. With deep, it counts every entry below dir, descending at most
// MaxDepth levels and never into a symbolic link (the delete question,
// FR-SFTP-011); without, only dir's own entries (the item counts of
// FR-SFTP-006).
func (LocalFS) Count(ctx context.Context, dir string, stop int, deep bool) (int, error) {
	n := 0
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		f, err := os.Open(dir) //nolint:gosec // reading a directory the user browses
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck // read-only
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			ds, err := f.ReadDir(1024)
			for _, d := range ds {
				n++
				if n >= stop {
					return nil
				}
				if deep && depth < MaxDepth && d.Type().IsDir() { // d_type: a link is not a dir
					if err := walk(filepath.Join(dir, d.Name()), depth+1); err != nil || n >= stop {
						return err
					}
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	err := walk(dir, 1)
	return n, err
}

// Stat describes path, following a symbolic link (enter on a link, FR-SFTP-007).
func (LocalFS) Stat(_ context.Context, path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	return entryOf(filepath.Base(path), info), nil
}

// Rename renames from to to inside dir. It never replaces an existing
// entry: when to exists, it fails with ErrExists (FR-SFTP-010).
func (LocalFS) Rename(_ context.Context, dir, from, to string) error {
	if err := checkLocalName(from); err != nil {
		return err
	}
	if err := checkLocalName(to); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // nothing written through the root itself
	if _, err := root.Lstat(to); err == nil {
		return ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return root.Rename(from, to)
}

// Mkdir creates the directory name in dir with DirMode (FR-SFTP-012).
func (LocalFS) Mkdir(_ context.Context, dir, name string) error {
	if err := checkLocalName(name); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // nothing written through the root itself
	if err := root.Mkdir(name, DirMode); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrExists
		}
		return err
	}
	return nil
}

// Delete removes each of names from dir with everything below it, removing
// symbolic links as links and never following them (FR-SFTP-011,
// SEC-SFTP-002 AC5). It stops at the first error and returns how many of
// names it removed.
func (LocalFS) Delete(ctx context.Context, dir string, names []string) (int, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, err
	}
	defer root.Close() //nolint:errcheck // nothing written through the root itself
	for i, name := range names {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := checkLocalName(name); err != nil {
			return i, err
		}
		if err := root.RemoveAll(name); err != nil {
			return i, err
		}
	}
	return len(names), nil
}

// LocalDir is a local target directory opened as an os.Root: the write
// primitives every download uses (ADR-018, SEC-SFTP-002, SEC-SFTP-003).
// Names passed to its methods are single path elements; os.Root refuses
// anything that leaves the directory, and the methods never write through
// a symbolic link.
type LocalDir struct {
	root *os.Root
}

// testHookOpenPart runs in OpenPart between the Lstat check and the open
// (SEC-SFTP-002 AC4).
var testHookOpenPart func()

// OpenLocalDir opens path as the root of a download.
func OpenLocalDir(path string) (*LocalDir, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &LocalDir{root: root}, nil
}

// Close closes the root.
func (d *LocalDir) Close() error { return d.root.Close() }

// Lstat describes name without following a link (to detect a conflict).
func (d *LocalDir) Lstat(name string) (fs.FileInfo, error) {
	if err := checkLocalName(name); err != nil {
		return nil, err
	}
	return d.root.Lstat(name)
}

// Subdir creates the directory name with DirMode if it does not exist and
// opens it as a new LocalDir from this root. An existing entry that is not
// a directory, a symbolic link included, fails with ErrNotDir
// (SEC-SFTP-002 AC2); a directory swapped for a link between the check
// and the open fails too.
func (d *LocalDir) Subdir(name string) (*LocalDir, error) {
	if err := checkLocalName(name); err != nil {
		return nil, err
	}
	if err := d.root.Mkdir(name, DirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	before, err := d.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() {
		return nil, ErrNotDir
	}
	sub, err := d.root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := sub.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		sub.Close() //nolint:errcheck,gosec // refused
		return nil, ErrNotDir
	}
	return &LocalDir{root: sub}, nil
}

// PartName is the part file of name (SEC-SFTP-003).
func PartName(name string) string { return name + PartSuffix }

// CreatePart creates name's part file, which must not exist, with
// O_CREATE|O_EXCL|O_WRONLY and PartMode. O_EXCL refuses a symbolic link
// in its place.
func (d *LocalDir) CreatePart(name string) (*os.File, error) {
	if err := checkLocalName(name); err != nil {
		return nil, err
	}
	return d.root.OpenFile(PartName(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, PartMode)
}

// OpenPart reopens name's existing part file to resume a download, with
// the write offset at its end. The part must be a regular file; it is
// opened with O_NOFOLLOW and must be the file the Lstat saw. Anything else
// fails with ErrUnsafePart (SEC-SFTP-002 AC3, AC4).
func (d *LocalDir) OpenPart(name string) (*os.File, error) {
	if err := checkLocalName(name); err != nil {
		return nil, err
	}
	part := PartName(name)
	before, err := d.root.Lstat(part)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, ErrUnsafePart
	}
	if testHookOpenPart != nil {
		testHookOpenPart()
	}
	f, err := d.root.OpenFile(part, os.O_WRONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnsafePart
	}
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		f.Close() //nolint:errcheck,gosec // refused
		return nil, ErrUnsafePart
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close() //nolint:errcheck,gosec // failed
		return nil, err
	}
	return f, nil
}

// RemovePart removes name's part file (a link in its place is removed, not
// followed). A missing part is not an error.
func (d *LocalDir) RemovePart(name string) error {
	if err := checkLocalName(name); err != nil {
		return err
	}
	if err := d.root.Remove(PartName(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Finish completes a download whose size the caller has checked: it syncs
// and closes f (name's part file), sets its permission bits to mode & 0777
// and its modification time, and renames it over name. Rename replaces
// whatever is at name, a symbolic link included, and never writes through
// it (SEC-SFTP-002 AC1, SEC-SFTP-003). f is closed whatever the outcome.
func (d *LocalDir) Finish(f *os.File, name string, mode fs.FileMode, mtime time.Time) error {
	if err := checkLocalName(name); err != nil {
		f.Close() //nolint:errcheck,gosec // failed
		return err
	}
	err := f.Sync()
	if err == nil {
		err = f.Chmod(mode & 0o777)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	part := PartName(name)
	if err := d.root.Chtimes(part, mtime, mtime); err != nil {
		return err
	}
	return d.root.Rename(part, name)
}

// checkLocalName refuses a name that is not one path element: empty, .,
// .., or containing /, NUL or a control character (the rules of
// FR-SFTP-010 and SEC-SFTP-001).
func checkLocalName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, '/') {
		return ErrBadName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return ErrBadName
		}
	}
	return nil
}

// entryOf builds an entry from Lstat (or Stat) information.
func entryOf(name string, info fs.FileInfo) Entry {
	return Entry{
		Name:    name,
		Display: display(name),
		Kind:    kindOf(info.Mode()),
		Size:    info.Size(),
		Mode:    info.Mode().Perm(),
		ModTime: info.ModTime(),
	}
}

// display makes a name safe to show on one line: escape sequences and
// controls removed, invalid UTF-8, tabs and newlines as U+FFFD.
func display(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' {
			return utf8.RuneError
		}
		return r
	}, domain.Sanitize(name))
}

// localTitle is dir for a pane title: sanitised, the home directory as ~.
func localTitle(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "/" {
		if dir == home {
			dir = "~"
		} else if rest, ok := strings.CutPrefix(dir, home+string(filepath.Separator)); ok {
			dir = "~/" + rest
		}
	}
	return display(dir)
}
