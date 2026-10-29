package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// fileMode is the mode of every file the store writes (SEC-FILE-001).
const fileMode fs.FileMode = 0o600

// fileSystem is the part of the OS that an atomic write uses. Tests inject
// faults through it.
type fileSystem interface {
	EvalSymlinks(path string) (string, error)
	// CreateTemp creates a new file in dir with O_CREATE|O_EXCL and mode
	// 0600, named after pattern as os.CreateTemp does.
	CreateTemp(dir, pattern string) (tempFile, error)
	Rename(oldpath, newpath string) error
	Remove(path string) error
	SyncDir(dir string) error
}

type tempFile interface {
	Name() string
	Write(p []byte) (int, error)
	Chmod(mode fs.FileMode) error
	Sync() error
	Close() error
}

type osFS struct{}

func (osFS) EvalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }
func (osFS) Rename(oldpath, newpath string) error     { return os.Rename(oldpath, newpath) } //nolint:gosec // paths from internal/paths
func (osFS) Remove(path string) error                 { return os.Remove(path) }

func (osFS) CreateTemp(dir, pattern string) (tempFile, error) {
	return os.CreateTemp(dir, pattern) // O_RDWR|O_CREATE|O_EXCL, 0600
}

func (osFS) SyncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // the directory of a file the caller names
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

// WriteFile replaces the file at path with data atomically (SEC-FILE-003,
// credential-storage §3.4): a symlink at path is resolved first, so it stays
// a symlink; data goes to a new temp file in the target's directory
// (O_EXCL, 0600), which is synced, closed and renamed over the target, and
// then the directory is synced. After a crash at any point the target holds
// either its old or its new contents. The result has mode 0600 whatever the
// umask.
func WriteFile(path string, data []byte) error { return writeFile(osFS{}, path, data) }

func writeFile(fsys fileSystem, path string, data []byte) error {
	target, err := fsys.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		target, err = path, nil // first write
	}
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	f, err := fsys.CreateTemp(dir, "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	err = writeSyncClose(f, data)
	if err == nil {
		err = fsys.Rename(tmp, target)
	}
	if err != nil {
		_ = fsys.Remove(tmp)
		return err
	}
	// The new contents are in place; this makes the rename itself durable.
	return fsys.SyncDir(dir)
}

func writeSyncClose(f tempFile, data []byte) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Chmod(fileMode) // the umask may have narrowed it
	}
	if err == nil {
		err = f.Sync() // F_FULLFSYNC on macOS
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
