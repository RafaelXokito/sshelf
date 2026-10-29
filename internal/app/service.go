package app

import (
	"cmp"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/session"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/store"
	"github.com/RafaelXokito/sshelf/internal/term"
)

// ErrHostNotFound means the host ID is not in the loaded config.
var ErrHostNotFound = errors.New("host not found")

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

// Service is what the UI calls. Every method may block on I/O, so the UI
// calls it only from tea.Cmd goroutines, never from Update (ARCH-UI-002).
type Service interface {
	// Load reads config.json and returns the hosts sorted by label.
	Load(ctx context.Context) ([]domain.Host, error)
	// Connect dials, verifies and authenticates the host, then opens an
	// interactive session ready for tea.Exec. Nothing is handed to the
	// terminal unless all of that succeeded (ARCH-SESS-002).
	Connect(ctx context.Context, hostID domain.ID) (Session, error)
}

// Config wires an App.
type Config struct {
	ConfigPath      string   // config.json
	KnownHostsFiles []string // from paths.KnownHostsFiles
	// Creds and Prompter default to KeyFileSource and
	// sshclient.RejectUnknownHosts.
	Creds    sshclient.CredentialSource
	Prompter sshclient.Prompter
	// Terminal is the local terminal sessions attach to; default os.Stdin.
	Terminal session.Terminal
}

// App implements Service.
type App struct {
	cfg Config

	mu   sync.Mutex
	data domain.Data
}

// New returns an App. It does no I/O.
func New(cfg Config) *App {
	if cfg.Creds == nil {
		cfg.Creds = KeyFileSource{}
	}
	if cfg.Prompter == nil {
		cfg.Prompter = sshclient.RejectUnknownHosts{}
	}
	if cfg.Terminal == nil {
		cfg.Terminal = term.NewTTY(os.Stdin)
	}
	return &App{cfg: cfg}
}

// Load implements Service.
func (a *App) Load(ctx context.Context) ([]domain.Host, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snap, err := store.Load(a.cfg.ConfigPath)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.data = snap.Data
	a.mu.Unlock()
	hosts := slices.Clone(snap.Data.Hosts)
	slices.SortStableFunc(hosts, func(x, y domain.Host) int {
		return cmp.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return hosts, nil
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

	ssh := sshclient.Config{KnownHostsFiles: a.cfg.KnownHostsFiles}
	client, err := ssh.Connect(ctx, target, a.cfg.Creds, a.cfg.Prompter)
	if err != nil {
		return nil, err
	}
	s, err := session.New(client, a.cfg.Terminal, session.Options{
		Term:    h.Options.Term,
		Env:     h.Options.SetEnv,
		Command: h.Options.RemoteCommand,
	})
	if err != nil {
		return nil, err // never a typed nil inside the interface
	}
	return s, nil
}
