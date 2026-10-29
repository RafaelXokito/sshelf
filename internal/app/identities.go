package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/store"
)

// ErrIdentityNotFound means the identity ID is not in the loaded config.
var ErrIdentityNotFound = errors.New("identity not found")

// Secret is secrets.Secret, re-exported so that the UI can hand a typed
// secret to App without importing internal/secrets (design §5.2). It
// prints as [REDACTED] in every format (SEC-LOG-002), so a tea.Msg that
// carries one never shows it.
type Secret = secrets.Secret

// NewSecret wraps b, taking ownership of it.
func NewSecret(b []byte) Secret { return secrets.NewSecret(b) }

// SecretStatus is all the UI may show of a secret field (SEC-LOG-005).
type SecretStatus int

// The secret statuses.
const (
	SecretNotSet SecretStatus = iota
	SecretSet
	// SecretNotOnDevice means the config says the secret is set, but the
	// credential store did not have it when a connection asked for it
	// (D-108, SEC-CRED-007).
	SecretNotOnDevice
)

func (s SecretStatus) String() string {
	switch s {
	case SecretSet:
		return "set"
	case SecretNotOnDevice:
		return "not set on this device"
	}
	return "not set"
}

// IdentityInfo is an identity as the identities view shows it: its
// metadata, the status of each secret and how many hosts use it.
type IdentityInfo struct {
	Identity    domain.Identity
	Password    SecretStatus
	Key         SecretStatus
	Passphrase  SecretStatus
	LinkedHosts int
}

// IdentityInput is what the identity form submits. A secret that is empty
// is left as it is: an unchanged secret is never read back (SEC-LOG-005).
// SaveIdentity destroys the secrets before it returns.
type IdentityInput struct {
	ID       domain.ID // "" creates an identity
	Name     string
	Username string
	KeyPath  string
	// The secrets typed or pasted into the form; empty means unchanged.
	Password   Secret
	Key        Secret
	Passphrase Secret
}

// Destroy zeroes the input's secrets.
func (in *IdentityInput) Destroy() {
	in.Password.Destroy()
	in.Key.Destroy()
	in.Passphrase.Destroy()
}

// SavedIdentity is the result of SaveIdentity.
type SavedIdentity struct {
	Identity domain.Identity // as saved, with its ID
	// Identities are every identity after the save, hidden ones
	// included, sorted by name (the host form's picker uses them).
	Identities []domain.Identity
	// List is the identities view after the save.
	List []IdentityInfo
	// Warnings are one-line messages to show, such as a stored item
	// that could not be deleted (SEC-CRED-004).
	Warnings []string
}

// sortedIdentities returns a copy of ids sorted by name, case-insensitively.
func sortedIdentities(ids []domain.Identity) []domain.Identity {
	ids = slices.Clone(ids)
	slices.SortStableFunc(ids, func(x, y domain.Identity) int {
		return cmp.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return ids
}

// Identities returns the identities view (FR-IDENT-001): every identity
// that is not a hidden per-host one (FR-HOST-010), sorted by name. It
// does not touch the credential store; a secret's status comes from the
// config's flags and from what earlier connections found missing.
func (a *App) Identities(ctx context.Context) ([]IdentityInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.identityList(a.data), nil
}

// identityList builds the identities view from d. a.mu must be held.
func (a *App) identityList(d domain.Data) []IdentityInfo {
	var out []IdentityInfo
	for _, id := range sortedIdentities(d.Identities) {
		if id.Hidden {
			continue
		}
		info := IdentityInfo{
			Identity:   id,
			Password:   a.status(id, id.HasPassword, secrets.FieldPassword),
			Key:        a.status(id, id.HasKey, secrets.FieldPrivateKey),
			Passphrase: a.status(id, id.HasPassphrase, secrets.FieldKeyPassphrase),
		}
		for _, h := range d.Hosts {
			if h.IdentityID == id.ID {
				info.LinkedHosts++
			}
		}
		out = append(out, info)
	}
	return out
}

// status is the status of one secret field. a.mu must be held.
func (a *App) status(id domain.Identity, has bool, f secrets.Field) SecretStatus {
	switch {
	case !has:
		return SecretNotSet
	case a.missing[secrets.Ref{IdentityID: string(id.ID), Field: f}]:
		return SecretNotOnDevice
	}
	return SecretSet
}

// markMissing records that the store did not have ref when a connection
// asked for it, so the identity form shows "not set on this device"
// (D-108). It is not saved: the next run finds out again.
func (a *App) markMissing(ref secrets.Ref) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.missing == nil {
		a.missing = map[secrets.Ref]bool{}
	}
	a.missing[ref] = true
}

// SaveIdentity creates (in.ID == "") or edits an identity
// (FR-IDENT-001), following credential-storage §7. The name must be
// unique case-insensitively (a *domain.ValidationError otherwise). Each
// secret that holds a value is checked against the 2800-byte limit and
// written to the store with Set; a secret left empty is not touched or
// read. The has_* flags then record what is stored, and config.json is
// written last. If a Set or the config write fails, the items this call
// created are deleted again and the config is left as it was. Linked
// hosts use the new values from their next connection on, since every
// connection reads the store afresh.
func (a *App) SaveIdentity(ctx context.Context, in IdentityInput) (SavedIdentity, error) {
	defer in.Destroy()
	if err := ctx.Err(); err != nil {
		return SavedIdentity{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock() // held across the store and the config write, so saves never interleave

	data := a.data
	data.Identities = slices.Clone(a.data.Identities)
	now := time.Now().UTC()
	var id domain.Identity
	i := -1
	if in.ID == "" {
		id = domain.Identity{Meta: domain.Meta{ID: domain.NewID(), CreatedAt: now}}
	} else {
		i = slices.IndexFunc(data.Identities, func(o domain.Identity) bool { return o.ID == in.ID })
		if i < 0 {
			return SavedIdentity{}, ErrIdentityNotFound
		}
		id = data.Identities[i]
	}
	id.Label = strings.TrimSpace(in.Name)
	id.Username = strings.TrimSpace(in.Username)
	id.KeyPath = strings.TrimSpace(in.KeyPath)
	id.UpdatedAt = now
	if err := domain.ValidateIdentity(id, data); err != nil {
		return SavedIdentity{}, err
	}

	w := secretWrite{
		password: in.Password, key: in.Key, passphrase: in.Passphrase,
	}
	id, rollback, err := a.writeSecrets(ctx, id, w)
	if err != nil {
		return SavedIdentity{}, err
	}
	if i < 0 {
		data.Identities = append(data.Identities, id)
	} else {
		data.Identities[i] = id
	}
	if err := a.saveConfig(data); err != nil {
		rollback(ctx)
		return SavedIdentity{}, err
	}
	a.data = data
	slog.Debug("save identity: saved", "identity", id.Label, "id", id.ID)
	return SavedIdentity{
		Identity:   id,
		Identities: sortedIdentities(data.Identities),
		List:       a.identityList(data),
	}, nil
}

// secretWrite is the secrets of one save; an empty Secret is unchanged.
type secretWrite struct {
	password, key, passphrase Secret
}

// writeSecrets stores the non-empty secrets of w for id, sets id's flags
// for what was stored, and returns id. On failure it deletes the items it
// created and returns the error. rollback deletes the items this call
// created (not the ones it overwrote, whose old values are gone), for when
// the config write fails afterwards. a.mu must be held.
func (a *App) writeSecrets(ctx context.Context, id domain.Identity, w secretWrite) (domain.Identity, func(context.Context), error) {
	type field struct {
		f    secrets.Field
		s    Secret
		flag *bool
	}
	fields := []field{
		{secrets.FieldPassword, w.password, &id.HasPassword},
		{secrets.FieldPrivateKey, w.key, &id.HasKey},
		{secrets.FieldKeyPassphrase, w.passphrase, &id.HasPassphrase},
	}
	var created []secrets.Ref
	rollback := func(ctx context.Context) {
		for _, r := range created {
			if err := a.cfg.Secrets.Delete(ctx, r); err != nil {
				slog.Debug("save identity: rollback delete failed", "account", r.Account(), "err", err)
			}
		}
	}
	for _, fl := range fields {
		if fl.s.Len() == 0 {
			continue
		}
		ref := secrets.Ref{IdentityID: string(id.ID), Field: fl.f}
		if err := a.setSecret(ctx, ref, fl.s); err != nil {
			rollback(ctx)
			return domain.Identity{}, nil, err
		}
		if !*fl.flag {
			created = append(created, ref)
		}
		*fl.flag = true
		delete(a.missing, ref)
	}
	return id, rollback, nil
}

// fieldName is how a secret field is named in messages.
func fieldName(f secrets.Field) string {
	switch f {
	case secrets.FieldPrivateKey:
		return "key"
	case secrets.FieldKeyPassphrase:
		return "key passphrase"
	}
	return "password"
}

// setSecret checks the size limit and stores s under ref. A denied or
// unavailable store is a *StoreError.
func (a *App) setSecret(ctx context.Context, ref secrets.Ref, s Secret) error {
	if s.Len() > secrets.MaxSecretSize {
		return fmt.Errorf("the %s is larger than %d bytes: %w", fieldName(ref.Field), secrets.MaxSecretSize, secrets.ErrTooLarge)
	}
	if a.cfg.Secrets == nil {
		return &StoreError{Store: StoreName(), Err: secrets.ErrUnavailable}
	}
	err := a.cfg.Secrets.Set(ctx, ref, s)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, secrets.ErrDenied), errors.Is(err, secrets.ErrUnavailable):
		return &StoreError{Store: StoreName(), Err: err}
	case errors.Is(err, secrets.ErrTooLarge):
		return fmt.Errorf("the %s is larger than %d bytes: %w", fieldName(ref.Field), secrets.MaxSecretSize, err)
	}
	return fmt.Errorf("could not store the %s: %w", fieldName(ref.Field), err)
}

// saveConfig writes data to config.json. a.mu must be held.
func (a *App) saveConfig(data domain.Data) error {
	file := paths.ConfigFile(a.cfg.Dir)
	if err := store.Save(file, &store.Snapshot{Data: data}); err != nil {
		slog.Debug("save config: failed", "err", err)
		return fmt.Errorf("could not save %s: %w", filepath.Base(file), err)
	}
	return nil
}
