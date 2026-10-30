package app

import (
	"context"
	"errors"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/files"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
)

// ErrCannotReconnect means ReconnectSFTP was given a browser that does not
// belong to internal/files (a test double).
var ErrCannotReconnect = errors.New("browser cannot reconnect")

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
	err = r.Attach(lease) // owns the lease, also on failure
	if errors.Is(err, files.ErrNoSFTP) {
		return ErrNoSFTP
	}
	return err
}

// ErrNoSharedConn means a session does not run on a shared connection
// (a test double), so no SFTP tab can be opened on it.
var ErrNoSharedConn = errors.New("session has no shared connection")

// connHolder is what a session that runs on an sshclient.Conn offers;
// *session.Session does.
type connHolder interface {
	Conn() *sshclient.Conn
}

// OpenSFTPOn opens the remote side of an SFTP tab on the connection of s,
// with a second lease and without dialling or authenticating again
// (ADR-017, design §8.8, FR-SFTP-003). It fails with
// sshclient.ErrConnClosed once that connection has ended and with
// ErrNoSFTP when the server refuses the subsystem; the session keeps
// running either way. T-054 wraps the result in the tab's app.Browser.
func (a *App) OpenSFTPOn(s Session) (*files.RemoteFS, error) {
	h, ok := s.(connHolder)
	if !ok {
		return nil, ErrNoSharedConn
	}
	lease, err := h.Conn().Lease()
	if err != nil {
		return nil, err
	}
	r, err := files.OpenRemote(lease) // owns the lease, also on failure
	if errors.Is(err, files.ErrNoSFTP) {
		return nil, ErrNoSFTP
	}
	return r, err
}
