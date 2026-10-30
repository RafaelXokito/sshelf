package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/secrets"
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

	// CopyOf is the host a new host duplicates (FR-HOST-004), or "".
	// The new host keeps the source's other fields, and the source's
	// hidden identity is copied with its secrets (FR-HOST-011).
	CopyOf domain.ID
}

// MsgCopyNotOnDevice is why a secret could not be copied when the store
// does not have it (D-108).
const MsgCopyNotOnDevice = "it is not set on this device; type it again or clear it"

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
//
// With c.CopyOf set, a new host is a duplicate of that host (FR-HOST-004,
// design §9.7): it keeps the source's other fields but not its last use or
// import source, and with no identity chosen it gets a hidden identity of
// its own holding new copies of the source hidden identity's secrets
// (FR-HOST-011), read with Get after the host validated and saved like
// typed ones. A shared identity is linked as it is.
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
	var hidden, src domain.Identity
	hasHidden, copying := false, false
	switch {
	case h.ID == "" && c.CopyOf != "":
		// A duplicate (FR-HOST-004) starts from the source as stored,
		// without its last use or import source; the form's fields
		// win, and the source's hidden identity is never shared.
		j := slices.IndexFunc(data.Hosts, func(o domain.Host) bool { return o.ID == c.CopyOf })
		if j < 0 {
			return SavedHost{}, ErrHostNotFound
		}
		dup := copyHost(data.Hosts[j])
		src, copying = a.hiddenIdentity(data.Hosts[j])
		dup.Label, dup.Hostname, dup.Port, dup.User, dup.IdentityID = h.Label, h.Hostname, h.Port, h.User, h.IdentityID
		if copying && dup.IdentityID == src.ID {
			dup.IdentityID = ""
		}
		h = dup
		fallthrough
	case h.ID == "":
		h.ID = domain.NewID()
		h.CreatedAt = now
	default:
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
	case h.IdentityID == "" && (hasHidden || copying || c.typed()):
		// Validate the host first, so that a form error reads and stores
		// nothing.
		probe := data
		probe.Hosts = withHost(data.Hosts, i, h)
		if err := domain.ValidateHost(h, probe); err != nil {
			return SavedHost{}, err
		}
		if copying {
			if err := a.copySecrets(ctx, src, &c); err != nil {
				return SavedHost{}, err
			}
		}
		in := IdentityInput{
			Name: h.Label, KeyPath: c.KeyPath,
			Password: c.Password, Key: c.Key, Passphrase: c.Passphrase,
			ClearPassword: c.ClearPassword, ClearKey: c.ClearKey, ClearPassphrase: c.ClearPassphrase,
		}
		switch {
		case hasHidden:
			in.ID, in.Username = hidden.ID, hidden.Username
		case copying:
			in.Username = src.Username
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

// copyHost returns a copy of h for a duplicate (FR-HOST-004): no ID,
// times, last use or import source, and slices and maps of its own.
func copyHost(h domain.Host) domain.Host {
	h.Meta = domain.Meta{}
	h.LastConnectedAt = nil
	h.Import = nil
	h.Tags = slices.Clone(h.Tags)
	h.JumpHostIDs = slices.Clone(h.JumpHostIDs)
	h.ForwardIDs = slices.Clone(h.ForwardIDs)
	h.Options.SetEnv = maps.Clone(h.Options.SetEnv)
	return h
}

// copySecrets reads into c, as if typed, each secret src has that c
// neither typed nor cleared (FR-HOST-011). The copies are destroyed with
// c. A failed Get names the field. a.mu must be held.
func (a *App) copySecrets(ctx context.Context, src domain.Identity, c *HostCredentials) error {
	for _, fl := range []struct {
		f          secrets.Field
		has, clear bool
		s          *Secret
	}{
		{secrets.FieldPassword, src.HasPassword, c.ClearPassword, &c.Password},
		{secrets.FieldPrivateKey, src.HasKey, c.ClearKey, &c.Key},
		{secrets.FieldKeyPassphrase, src.HasPassphrase, c.ClearPassphrase, &c.Passphrase},
	} {
		if !fl.has || fl.clear || fl.s.Len() > 0 {
			continue
		}
		ref := secrets.Ref{IdentityID: string(src.ID), Field: fl.f}
		err := error(&StoreError{Store: StoreName(), Err: secrets.ErrUnavailable})
		var s Secret
		if a.cfg.Secrets != nil {
			s, err = a.cfg.Secrets.Get(ctx, ref)
		}
		switch {
		case err == nil:
			*fl.s = s
			continue
		case errors.Is(err, secrets.ErrNotFound):
			err = msgError{msg: MsgCopyNotOnDevice, err: err}
		case errors.Is(err, secrets.ErrDenied), errors.Is(err, secrets.ErrUnavailable):
			if _, ok := errors.AsType[*StoreError](err); !ok {
				err = &StoreError{Store: StoreName(), Err: err}
			}
		}
		slog.Debug("duplicate host: copy failed", "account", ref.Account(), "err", err)
		return fmt.Errorf("could not copy the %s: %w", fieldName(fl.f), err)
	}
	return nil
}

// msgError is err shown as msg.
type msgError struct {
	msg string
	err error
}

func (e msgError) Error() string { return e.msg }
func (e msgError) Unwrap() error { return e.err }

// withHost returns hosts with h at index i, or appended when i < 0.
func withHost(hosts []domain.Host, i int, h domain.Host) []domain.Host {
	hosts = slices.Clone(hosts)
	if i < 0 {
		return append(hosts, h)
	}
	hosts[i] = h
	return hosts
}
