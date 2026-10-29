package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/store"
)

// DeleteHost implements Service (FR-HOST-002). It removes the host with id,
// writes config.json through store.Save, and returns every remaining host
// sorted by label. The identity the host links to is kept, even when no
// other host uses it; deleting a host's hidden identity and its secrets
// belongs to FR-HOST-010. The in-memory config changes only after the
// write succeeded.
func (a *App) DeleteHost(ctx context.Context, id domain.ID) ([]domain.Host, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	i := slices.IndexFunc(a.data.Hosts, func(h domain.Host) bool { return h.ID == id })
	if i < 0 {
		return nil, ErrHostNotFound
	}
	label := a.data.Hosts[i].Label
	data := a.data
	data.Hosts = slices.Delete(slices.Clone(a.data.Hosts), i, i+1)
	file := paths.ConfigFile(a.cfg.Dir)
	if err := store.Save(file, &store.Snapshot{Data: data}); err != nil {
		slog.Debug("delete host: failed", "host", label, "err", err)
		return nil, fmt.Errorf("could not save %s: %w", filepath.Base(file), err)
	}
	a.data = data
	slog.Debug("delete host: deleted", "host", label, "id", id)
	return sortedHosts(data.Hosts), nil
}
