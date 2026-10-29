package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
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
	Hosts []domain.Host // sorted by label
	// Identities are every identity, hidden ones included, sorted by
	// label. They carry no secrets.
	Identities []domain.Identity
	FirstRun   bool // config.json did not exist
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
	// SaveHost adds h (when h.ID is "") or updates the host with h.ID,
	// and writes config.json atomically. See App.SaveHost.
	SaveHost(ctx context.Context, h domain.Host) (SavedHost, error)
	// DeleteHost removes the host with id and writes config.json. See
	// App.DeleteHost.
	DeleteHost(ctx context.Context, id domain.ID) ([]domain.Host, error)
	// ImportSSHConfig copies ~/.ssh/config into config.json once. See
	// App.ImportSSHConfig.
	ImportSSHConfig(ctx context.Context) (ImportReport, error)
}

// SavedHost is the result of Service.SaveHost.
type SavedHost struct {
	Host  domain.Host   // as saved, with its ID
	Hosts []domain.Host // every host after the save, sorted by label
}

// Config wires an App.
type Config struct {
	// Dir is the sshelf config directory (paths.Dirs.Config). It holds
	// config.json and sshelf's own known_hosts.
	Dir string
	// KnownHostsFiles are the files host keys are checked against, from
	// paths.KnownHostsFiles(Dir, home).
	KnownHostsFiles []string
	// Secrets is the OS credential store the identities' secrets are read
	// from at connect time. nil means none is configured.
	Secrets secrets.Store
	// Home is the directory "~" in key paths expands to; "" means
	// os.UserHomeDir.
	Home string
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
	ids := slices.Clone(snap.Data.Identities)
	slices.SortStableFunc(ids, func(x, y domain.Identity) int {
		return cmp.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return Loaded{Hosts: sortedHosts(snap.Data.Hosts), Identities: ids, FirstRun: snap.FirstRun, Notices: notices}, nil
}

// sortedHosts returns a copy of hosts sorted by label, case-insensitively.
func sortedHosts(hosts []domain.Host) []domain.Host {
	hosts = slices.Clone(hosts)
	slices.SortStableFunc(hosts, func(x, y domain.Host) int {
		return cmp.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return hosts
}

// SaveHost implements Service (FR-HOST-001). A new host (h.ID == "") gets
// a UUIDv7 ID (ARCH-DATA-001). For an existing host only the form's
// fields (label, hostname, port, user and identity) are taken from h; the
// rest of the stored host is kept. Label, hostname and user are trimmed.
// The host is validated against the loaded hosts and identities
// (FR-HOST-003; the error is a *domain.ValidationError), then config.json
// is written through store.Save. The in-memory config changes only after
// that write succeeded, so a host counts as saved only when SaveHost
// returns nil.
func (a *App) SaveHost(ctx context.Context, h domain.Host) (SavedHost, error) {
	if err := ctx.Err(); err != nil {
		return SavedHost{}, err
	}
	h.Label = strings.TrimSpace(h.Label)
	h.Hostname = strings.TrimSpace(h.Hostname)
	h.User = strings.TrimSpace(h.User)

	a.mu.Lock()
	defer a.mu.Unlock() // held across the write, so saves never interleave
	hosts := slices.Clone(a.data.Hosts)
	now := time.Now().UTC()
	if h.ID == "" {
		h.ID = domain.NewID()
		h.CreatedAt, h.UpdatedAt = now, now
		hosts = append(hosts, h)
	} else {
		i := slices.IndexFunc(hosts, func(o domain.Host) bool { return o.ID == h.ID })
		if i < 0 {
			return SavedHost{}, ErrHostNotFound
		}
		stored := hosts[i]
		stored.Label, stored.Hostname, stored.Port, stored.User, stored.IdentityID = h.Label, h.Hostname, h.Port, h.User, h.IdentityID
		stored.UpdatedAt = now
		hosts[i], h = stored, stored
	}
	data := a.data
	data.Hosts = hosts
	if err := domain.ValidateHost(h, data); err != nil {
		return SavedHost{}, err
	}
	file := paths.ConfigFile(a.cfg.Dir)
	if err := store.Save(file, &store.Snapshot{Data: data}); err != nil {
		slog.Debug("save host: failed", "host", h.Label, "err", err)
		return SavedHost{}, fmt.Errorf("could not save %s: %w", filepath.Base(file), err)
	}
	a.data = data
	slog.Debug("save host: saved", "host", h.Label, "id", h.ID)
	return SavedHost{Host: h, Hosts: sortedHosts(hosts)}, nil
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
	a.mu.Lock()
	send := a.send
	a.mu.Unlock()
	ssh := sshclient.Config{KnownHostsFiles: a.cfg.KnownHostsFiles}
	if send != nil {
		ssh.Progress = func(st sshclient.Stage) { send(ConnectProgressMsg{HostID: hostID, Stage: st}) }
	}
	creds := Credentials{Store: a.cfg.Secrets, Home: a.cfg.Home, Prompter: a}
	client, err := ssh.Connect(ctx, target, creds, a)
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
