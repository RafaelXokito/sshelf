package main

import (
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/RafaelXokito/sshelf/internal/paths"
)

// debugEnv turns debug logging on when set to exactly "1" (SEC-LOG-003).
const debugEnv = "SSHELF_DEBUG"

// maxLogSize is the size at which the debug log is rotated to its one
// ".1" backup (SEC-LOG-003).
const maxLogSize = 5 << 20

// setupLogging installs the default slog logger. Unless getenv(debugEnv)
// is "1", every record is discarded and no file is created. Otherwise
// records go to paths.LogFile(stateDir), mode 0600, rotated at 5 MiB. The
// returned function closes the log.
func setupLogging(getenv func(string) string, stateDir string) (closeLog func() error, err error) {
	if getenv(debugEnv) != "1" {
		// Also silences the log package, which would write to the TUI's
		// terminal.
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return func() error { return nil }, nil
	}
	if _, err := paths.EnsureDir(stateDir); err != nil {
		return nil, err
	}
	w, err := openRotatingFile(paths.LogFile(stateDir), maxLogSize)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return w.Close, nil
}

// rotatingFile appends to a file and, before a write would take it past
// max bytes, renames it to path+".1" (replacing any older backup) and
// starts a new one.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

var _ io.WriteCloser = (*rotatingFile)(nil)

func openRotatingFile(path string, maxSize int64) (*rotatingFile, error) {
	r := &rotatingFile{path: path, max: maxSize}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, paths.FileMode) //nolint:gosec // path comes from internal/paths
	if err != nil {
		return err
	}
	// An existing file, or the umask, may have left a wider mode.
	if err := f.Chmod(paths.FileMode); err != nil {
		_ = f.Close()
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

// Write implements io.Writer.
func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) rotate() error {
	if err := r.f.Close(); err != nil {
		r.f = nil
		return err
	}
	r.f = nil
	if err := os.Rename(r.path, r.path+".1"); err != nil {
		return err
	}
	return r.open()
}

// Close implements io.Closer.
func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
