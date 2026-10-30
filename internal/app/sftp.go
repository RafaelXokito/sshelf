package app

import (
	"errors"

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

// OpenSFTPOn takes a second lease on the connection of s (ADR-017,
// design §8.8), without dialling or authenticating again (FR-SFTP-003).
// It fails with sshclient.ErrConnClosed once that connection has ended.
// The caller owns the lease.
func (a *App) OpenSFTPOn(s Session) (*sshclient.Lease, error) {
	h, ok := s.(connHolder)
	if !ok {
		return nil, ErrNoSharedConn
	}
	return h.Conn().Lease()
}
