package app

import (
	"context"
	"log/slog"
	"slices"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// DeletedHost is the result of DeleteHost.
type DeletedHost struct {
	Hosts      []domain.Host     // every remaining host, in last-use order
	Identities []domain.Identity // every remaining identity
	Warnings   []string          // stored items that could not be deleted
}

// DeleteHost implements Service (FR-HOST-002). It removes the host with
// id and writes config.json through store.Save. A shared identity the host
// links to is kept, even when no other host uses it; the host's hidden
// identity (FR-HOST-010) is removed in the same write, and its secrets are
// deleted from the store afterwards, with a warning naming any item that
// could not be deleted (SEC-CRED-004). The in-memory config changes only
// after the write succeeded.
func (a *App) DeleteHost(ctx context.Context, id domain.ID) (DeletedHost, error) {
	if err := ctx.Err(); err != nil {
		return DeletedHost{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	i := slices.IndexFunc(a.data.Hosts, func(h domain.Host) bool { return h.ID == id })
	if i < 0 {
		return DeletedHost{}, ErrHostNotFound
	}
	host := a.data.Hosts[i]
	data := a.data
	data.Hosts = slices.Delete(slices.Clone(a.data.Hosts), i, i+1)
	hidden, ok := a.hiddenIdentity(host)
	if ok {
		data, _, _ = withoutIdentity(data, hidden.ID)
	}
	if err := a.saveConfig(data); err != nil {
		slog.Debug("delete host: failed", "host", host.Label, "err", err)
		return DeletedHost{}, err
	}
	a.data = data
	var warnings []string
	if ok {
		warnings = a.deleteSecrets(ctx, hidden, allSecretFields...)
	}
	slog.Debug("delete host: deleted", "host", host.Label, "id", id)
	return DeletedHost{Hosts: sortedHosts(data.Hosts), Identities: sortedIdentities(data.Identities), Warnings: warnings}, nil
}
