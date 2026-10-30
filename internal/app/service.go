package app

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/session"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/store"
)

// ErrHostNotFound means the host ID is not in the loaded config.
var ErrHostNotFound = errors.New("host not found")

// LoadError is returned by Load when config.json cannot be read or parsed.
// The file is never written afterwards (FR-CFG-003). Err is
// store.ErrCorrupt, store.ErrNewerVersion or the underlying I/O error.
type LoadError = store.LoadError

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
	// interactive session whose PTY and emulator have size, the session
	// area. No session is opened unless all of that succeeded
	// (ARCH-SESS-002).
	Connect(ctx context.Context, hostID domain.ID, size Size) (Session, error)
	// SaveHost adds h (when h.ID is "") or updates the host with h.ID,
	// and writes config.json atomically. See App.SaveHost.
	SaveHost(ctx context.Context, h domain.Host) (SavedHost, error)
	// Identities returns the identities view. See App.Identities.
	Identities(ctx context.Context) ([]IdentityInfo, error)
	// SaveIdentity creates or edits an identity and stores its secrets.
	// See App.SaveIdentity.
	SaveIdentity(ctx context.Context, in IdentityInput) (SavedIdentity, error)
	// DeleteIdentity deletes an identity and its stored secrets. See
	// App.DeleteIdentity.
	DeleteIdentity(ctx context.Context, id domain.ID) (DeletedIdentity, error)
	// DeleteHost removes the host with id and writes config.json. See
	// App.DeleteHost.
	DeleteHost(ctx context.Context, id domain.ID) (DeletedHost, error)
	// SaveHostWithCredentials saves a host and the credentials typed in
	// its form as its hidden identity (FR-HOST-010). See
	// App.SaveHostWithCredentials.
	SaveHostWithCredentials(ctx context.Context, h domain.Host, c HostCredentials) (SavedHost, error)
	// SecretsAvailable probes the credential store. See
	// App.SecretsAvailable.
	SecretsAvailable(ctx context.Context) error
	// ImportSSHConfig copies ~/.ssh/config into config.json once. See
	// App.ImportSSHConfig.
	ImportSSHConfig(ctx context.Context) (ImportReport, error)
}

// SavedHost is the result of Service.SaveHost.
type SavedHost struct {
	Host  domain.Host   // as saved, with its ID
	Hosts []domain.Host // every host after the save, sorted by label
	// Identities are every identity after the save, hidden ones
	// included, sorted by name.
	Identities []domain.Identity
	// Warnings name stored items that could not be deleted
	// (SEC-CRED-004).
	Warnings []string
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
}

// App implements Service, and sshclient.Prompter for its own connections.
type App struct {
	cfg Config

	mu   sync.Mutex
	data domain.Data
	// missing are the secrets the store did not have when a connection
	// asked for them: "not set on this device" (D-108).
	missing map[secrets.Ref]bool
	send    func(any) // delivers prompts to the UI; see SetSend

	khMu sync.Mutex // serialises writes to sshelf's known_hosts
}

// New returns an App. It does no I/O.
func New(cfg Config) *App {
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
	return Loaded{Hosts: sortedHosts(snap.Data.Hosts), Identities: sortedIdentities(snap.Data.Identities), FirstRun: snap.FirstRun, Notices: notices}, nil
}

// sortedHosts returns a copy of hosts sorted by label, case-insensitively.
func sortedHosts(hosts []domain.Host) []domain.Host {
	hosts = slices.Clone(hosts)
	slices.SortStableFunc(hosts, func(x, y domain.Host) int {
		return cmp.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return hosts
}

// SaveHost implements Service (FR-HOST-001): SaveHostWithCredentials
// with no credentials typed.
func (a *App) SaveHost(ctx context.Context, h domain.Host) (SavedHost, error) {
	return a.SaveHostWithCredentials(ctx, h, HostCredentials{})
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
func (a *App) Connect(ctx context.Context, hostID domain.ID, size Size) (Session, error) {
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
	creds := Credentials{Store: a.cfg.Secrets, Home: a.cfg.Home, Prompter: a, OnMissing: a.markMissing}
	client, err := ssh.Connect(ctx, target, creds, a)
	if err != nil {
		slog.Debug("connect: failed", "host", h.Label, "err", err)
		return nil, err
	}
	s, err := session.New(client, session.Options{
		Term:    h.Options.Term,
		Env:     h.Options.SetEnv,
		Command: h.Options.RemoteCommand,
		Size:    size,
	})
	if err != nil {
		slog.Debug("connect: session failed", "host", h.Label, "err", err)
		return nil, err // never a typed nil inside the interface
	}
	slog.Debug("connect: session open", "host", h.Label)
	return s, nil
}
