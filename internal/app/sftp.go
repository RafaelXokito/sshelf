package app

import (
	"errors"

	"github.com/RafaelXokito/sshelf/internal/files"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
)

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
