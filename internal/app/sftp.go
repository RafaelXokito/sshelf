package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/files"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
)

// ErrCannotReconnect means ReconnectSFTP was given a browser that does not
// belong to internal/files (a test double).
var ErrCannotReconnect = errors.New("browser cannot reconnect")

// OpenSFTP implements Service (FR-SFTP-002): it connects to the host
// through the normal connect flow (dialling, host-key checks, prompts,
// authentication, exactly as Connect) and opens an SFTP tab's browser on
// the new connection, with no shell and no PTY. If the server refuses the
// subsystem it closes the connection and reports ErrNoSFTP.
func (a *App) OpenSFTP(ctx context.Context, hostID domain.ID) (Browser, error) {
	h, lease, err := a.dial(ctx, hostID)
	if err != nil {
		return nil, err
	}
	b, err := files.Open(lease, files.Options{HostID: hostID}) // owns the lease, also on failure
	if err != nil {
		slog.Debug("sftp: open failed", "host", h.Label, "err", err)
		return nil, err // never a typed nil inside the interface
	}
	slog.Debug("sftp: open", "host", h.Label)
	return b, nil
}

// reattacher is what *files.Browser offers for a reconnect.
type reattacher interface {
	HostID() domain.ID
	Attach(lease *sshclient.Lease) error
}

// ReconnectSFTP gives the disconnected browser b a new connection to its
// host through the normal connect flow (dialling, host-key checks,
// prompts, authentication) and resumes its paused transfers in their
// original order (FR-SFTP-018, ADR-018). A failed or cancelled reconnect
// leaves b disconnected. It reports ErrNoSFTP when the server refuses the
// subsystem. sshelf never calls it by itself (design §8.7).
func (a *App) ReconnectSFTP(ctx context.Context, b Browser) error {
	r, ok := b.(reattacher)
	if !ok {
		return ErrCannotReconnect
	}
	_, lease, err := a.dial(ctx, r.HostID())
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil { // cancelled while connecting
		lease.Release()
		return err
	}
	return r.Attach(lease) // owns the lease, also on failure
}

// ErrNoSharedConn means a session does not run on a shared connection
// (a test double), so no SFTP tab can be opened on it.
var ErrNoSharedConn = errors.New("session has no shared connection")

// connHolder is what a session that runs on an sshclient.Conn offers;
// *session.Session does.
type connHolder interface {
	Conn() *sshclient.Conn
}

// OpenSFTPOn implements Service: it opens an SFTP tab's browser on the
// connection of s, with a second lease and without dialling or
// authenticating again (ADR-017, design §8.8, FR-SFTP-003). hostID is the
// session's host, which a later reconnect of the tab dials (FR-SFTP-018).
// It fails with sshclient.ErrConnClosed once that connection has ended
// and with ErrNoSFTP when the server refuses the subsystem; the session
// keeps running either way.
func (a *App) OpenSFTPOn(s Session, hostID domain.ID) (Browser, error) {
	h, ok := s.(connHolder)
	if !ok {
		return nil, ErrNoSharedConn
	}
	lease, err := h.Conn().Lease()
	if err != nil {
		return nil, err
	}
	b, err := files.Open(lease, files.Options{HostID: hostID}) // owns the lease, also on failure
	if err != nil {
		return nil, err
	}
	return b, nil
}
