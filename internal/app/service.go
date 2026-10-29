package app

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/session"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/store"
	"github.com/RafaelXokito/sshelf/internal/term"
)

// ErrHostNotFound means the host ID is not in the loaded config.
var ErrHostNotFound = errors.New("host not found")

// LoadError is returned by Load when config.json cannot be read or parsed.
// The file is never written afterwards (FR-CFG-003). Err is
// store.ErrCorrupt, store.ErrNewerVersion or the underlying I/O error.
type LoadError = store.LoadError

// Session is the handle of an attached session. *session.Session satisfies
// it, and its method set includes tea.ExecCommand's, so the UI can pass it
// to tea.Exec. Close releases a session that will never run.
type Session interface {
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
	Run() error
	Close() error
}

// Loaded is the result of Service.Load.
type Loaded struct {
	Hosts    []domain.Host // sorted by label
	FirstRun bool          // config.json did not exist
	// Notices are one-line messages to show once, such as repaired file
	// modes (SEC-FILE-002).
	Notices []string
}

// Service is what the UI calls. Every method may block on I/O, so the UI
// calls it only from tea.Cmd goroutines, never from Update (ARCH-UI-002).
type Service interface {
	// Load creates the config directory if needed, repairs loose file
	// modes, and reads config.json. Every failure is a *LoadError.
	Load(ctx context.Context) (Loaded, error)
	// Connect dials, verifies and authenticates the host, then opens an
	// interactive session ready for tea.Exec. Nothing is handed to the
	// terminal unless all of that succeeded (ARCH-SESS-002).
	Connect(ctx context.Context, hostID domain.ID) (Session, error)
}

// Config wires an App.
type Config struct {
	// Dir is the sshelf config directory (paths.Dirs.Config). It holds
	// config.json and sshelf's own known_hosts.
	Dir string
	// KnownHostsFiles are the files host keys are checked against, from
	// paths.KnownHostsFiles(Dir, home).
	KnownHostsFiles []string
	// Creds defaults to KeyFileSource.
	Creds sshclient.CredentialSource
	// Terminal is the local terminal sessions attach to; default os.Stdin.
	Terminal session.Terminal
}

// App implements Service, and sshclient.Prompter for its own connections.
type App struct {
	cfg Config

	mu   sync.Mutex
	data domain.Data
	send func(any) // delivers prompts to the UI; see SetSend

	khMu sync.Mutex // serialises writes to sshelf's known_hosts
}

// New returns an App. It does no I/O.
func New(cfg Config) *App {
	if cfg.Creds == nil {
		cfg.Creds = KeyFileSource{}
	}
	if cfg.Terminal == nil {
		cfg.Terminal = term.NewTTY(os.Stdin)
	}
	return &App{cfg: cfg}
}

// Load implements Service. On first launch it creates the config
// directory with mode 0700 (FR-CFG-001); it then restores 0700/0600 on the
// directory, config.json and known_hosts if group or others can access them
// (SEC-FILE-002).
func (a *App) Load(ctx context.Context) (Loaded, error) {
	if err := ctx.Err(); err != nil {
		return Loaded{}, err
	}
	fail := func(path string, err error) (Loaded, error) {
		var le *LoadError
		if !errors.As(err, &le) {
			err = &LoadError{Path: path, Problem: err.Error(), Err: err}
		}
		return Loaded{}, err
	}
	if _, err := paths.EnsureDir(a.cfg.Dir); err != nil {
		return fail(a.cfg.Dir, err)
	}
	var notices []string
	fixed, err := paths.CheckModes(a.cfg.Dir)
	if err != nil {
		return fail(a.cfg.Dir, err)
	}
	if len(fixed) > 0 {
		notices = append(notices, modesNotice(a.cfg.Dir, fixed))
	}
	snap, err := store.Load(paths.ConfigFile(a.cfg.Dir))
	if err != nil {
		return fail(paths.ConfigFile(a.cfg.Dir), err)
	}
	notices = append(notices, snap.Warnings...)
	a.mu.Lock()
	a.data = snap.Data
	a.mu.Unlock()
	hosts := slices.Clone(snap.Data.Hosts)
	slices.SortStableFunc(hosts, func(x, y domain.Host) int {
		return cmp.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return Loaded{Hosts: hosts, FirstRun: snap.FirstRun, Notices: notices}, nil
}

// modesNotice is the one-line notice for repaired modes, e.g.
// "permissions of config.json, known_hosts were too open; set to 0700/0600".
func modesNotice(dir string, fixed []string) string {
	names := make([]string, len(fixed))
	for i, f := range fixed {
		names[i] = filepath.Base(f)
		if f == dir {
			names[i] = "the config directory"
		}
	}
	return "permissions of " + strings.Join(names, ", ") + " were too open; set to 0700/0600"
}

// Connect implements Service.
func (a *App) Connect(ctx context.Context, hostID domain.ID) (Session, error) {
	a.mu.Lock()
	i := slices.IndexFunc(a.data.Hosts, func(h domain.Host) bool { return h.ID == hostID })
	if i < 0 {
		a.mu.Unlock()
		return nil, ErrHostNotFound
	}
	h := a.data.Hosts[i]
	target := domain.Resolve(h, a.data.Groups, a.data.Identities, a.data.Settings)
	a.mu.Unlock()

	slog.Debug("connect: start", "host", h.Label, "id", hostID)
	ssh := sshclient.Config{KnownHostsFiles: a.cfg.KnownHostsFiles}
	client, err := ssh.Connect(ctx, target, a.cfg.Creds, a)
	if err != nil {
		slog.Debug("connect: failed", "host", h.Label, "err", err)
		return nil, err
	}
	s, err := session.New(client, a.cfg.Terminal, session.Options{
		Term:    h.Options.Term,
		Env:     h.Options.SetEnv,
		Command: h.Options.RemoteCommand,
	})
	if err != nil {
		slog.Debug("connect: session failed", "host", h.Label, "err", err)
		return nil, err // never a typed nil inside the interface
	}
	slog.Debug("connect: session open", "host", h.Label)
	return s, nil
}
