package app

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// HostCredentials are the credentials typed directly into the host form
// (FR-HOST-010). They are saved as the host's hidden identity: a per-host
// identity that the identities view does not list. As in IdentityInput,
// an empty secret is unchanged and Clear* deletes a stored one. The
// username stays on the host. SaveHostWithCredentials destroys the
// secrets before it returns.
type HostCredentials struct {
	KeyPath    string
	Password   Secret
	Key        Secret
	Passphrase Secret

	ClearPassword   bool
	ClearKey        bool
	ClearPassphrase bool
}

// Destroy zeroes the secrets.
func (c *HostCredentials) Destroy() {
	c.Password.Destroy()
	c.Key.Destroy()
	c.Passphrase.Destroy()
}

// typed reports whether c holds anything to save.
func (c HostCredentials) typed() bool {
	return strings.TrimSpace(c.KeyPath) != "" || c.Password.Len() > 0 || c.Key.Len() > 0 || c.Passphrase.Len() > 0
}

// hiddenIdentity returns h's hidden identity, if it has one. a.mu must be
// held.
func (a *App) hiddenIdentity(h domain.Host) (domain.Identity, bool) {
	if h.IdentityID == "" {
		return domain.Identity{}, false
	}
	i := slices.IndexFunc(a.data.Identities, func(o domain.Identity) bool { return o.ID == h.IdentityID })
	if i < 0 || !a.data.Identities[i].Hidden {
		return domain.Identity{}, false
	}
	return a.data.Identities[i], true
}

// SaveHostWithCredentials saves a host (FR-HOST-001) together with the
// credentials typed in its form (FR-HOST-010).
//
// A new host gets a UUIDv7 ID (ARCH-DATA-001); for an existing one only
// the form's fields (label, hostname, port, user and identity) are taken
// from h. With no identity chosen (h.IdentityID "" or the host's own
// hidden identity), c is saved as the host's hidden identity: created on
// the first credential typed, named after the host, and edited like any
// identity afterwards (SaveIdentity's rules: size check, Set per changed
// field, rollback, config last, cleared items deleted after the write).
// Choosing a shared identity instead deletes the hidden one and, after the
// config write, its secrets. The host is validated against the hosts and
// identities (FR-HOST-003: a *domain.ValidationError). Nothing changes in
// memory unless config.json was written.
func (a *App) SaveHostWithCredentials(ctx context.Context, h domain.Host, c HostCredentials) (SavedHost, error) {
	defer c.Destroy()
	if err := ctx.Err(); err != nil {
		return SavedHost{}, err
	}
	h.Label = strings.TrimSpace(h.Label)
	h.Hostname = strings.TrimSpace(h.Hostname)
	h.User = strings.TrimSpace(h.User)

	a.mu.Lock()
	defer a.mu.Unlock() // held across the store and the config write, so saves never interleave
	data := a.data
	data.Hosts = slices.Clone(a.data.Hosts)
	now := time.Now().UTC()
	i := -1
	var hidden domain.Identity
	hasHidden := false
	if h.ID == "" {
		h.ID = domain.NewID()
		h.CreatedAt = now
	} else {
		i = slices.IndexFunc(data.Hosts, func(o domain.Host) bool { return o.ID == h.ID })
		if i < 0 {
			return SavedHost{}, ErrHostNotFound
		}
		stored := data.Hosts[i]
		hidden, hasHidden = a.hiddenIdentity(stored)
		stored.Label, stored.Hostname, stored.Port, stored.User, stored.IdentityID = h.Label, h.Hostname, h.Port, h.User, h.IdentityID
		h = stored
	}
	h.UpdatedAt = now
	if hasHidden && h.IdentityID == hidden.ID {
		h.IdentityID = ""
	}

	var (
		chs  []identityChange
		gone *domain.Identity // a hidden identity replaced by a shared one
	)
	switch {
	case h.IdentityID != "" && hasHidden:
		data, _, _ = withoutIdentity(data, hidden.ID)
		gone = &hidden
	case h.IdentityID == "" && (hasHidden || c.typed()):
		in := IdentityInput{
			Name: h.Label, KeyPath: c.KeyPath,
			Password: c.Password, Key: c.Key, Passphrase: c.Passphrase,
			ClearPassword: c.ClearPassword, ClearKey: c.ClearKey, ClearPassphrase: c.ClearPassphrase,
		}
		if hasHidden {
			in.ID, in.Username = hidden.ID, hidden.Username
		}
		// Validate the host first, so that a form error stores nothing.
		probe := data
		probe.Hosts = withHost(data.Hosts, i, h)
		if err := domain.ValidateHost(h, probe); err != nil {
			return SavedHost{}, err
		}
		ch, err := a.prepareIdentity(ctx, data, in, true)
		if err != nil {
			return SavedHost{}, err
		}
		chs = append(chs, ch)
		data = ch.data
		h.IdentityID = ch.id.ID
	}
	data.Hosts = withHost(data.Hosts, i, h)
	if err := domain.ValidateHost(h, data); err != nil {
		for _, ch := range chs {
			a.rollback(ctx, ch)
		}
		return SavedHost{}, err
	}
	warnings, err := a.commit(ctx, data, chs...)
	if err != nil {
		return SavedHost{}, err
	}
	if gone != nil {
		warnings = append(warnings, a.deleteSecrets(ctx, *gone, allSecretFields...)...)
	}
	slog.Debug("save host: saved", "host", h.Label, "id", h.ID)
	return SavedHost{
		Host:       h,
		Hosts:      sortedHosts(a.data.Hosts),
		Identities: sortedIdentities(a.data.Identities),
		Warnings:   warnings,
	}, nil
}

// withHost returns hosts with h at index i, or appended when i < 0.
func withHost(hosts []domain.Host, i int, h domain.Host) []domain.Host {
	hosts = slices.Clone(hosts)
	if i < 0 {
		return append(hosts, h)
	}
	hosts[i] = h
	return hosts
}
