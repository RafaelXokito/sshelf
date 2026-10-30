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
	// KeyType and KeyFingerprint describe the stored key ("ED25519",
	// "SHA256:…"); both are "" when there is none or it is encrypted
	// without a readable public part (FR-IDENT-003).
	KeyType        string
	KeyFingerprint string
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
	// Clear* delete a stored secret (SEC-CRED-004); a new value for the
	// same field wins.
	ClearPassword   bool
	ClearKey        bool
	ClearPassphrase bool
	// ImportKeyFile stores the key file at KeyPath in the credential
	// store, as if its contents were pasted, and drops the path; the
	// file is only read (ADR-010, "Store key in keychain"). A pasted Key
	// wins.
	ImportKeyFile bool
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
		if id.HasKey {
			info.KeyType, info.KeyFingerprint = keyDisplay(id.PublicKey)
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
// written; a cleared secret is deleted from the store only after that,
// with a warning naming any item that could not be deleted
// (SEC-CRED-004). If a Set or the config write fails, the items this call
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

	ch, err := a.prepareIdentity(ctx, a.data, in, false)
	if err != nil {
		return SavedIdentity{}, err
	}
	warnings, err := a.commit(ctx, ch.data, ch)
	if err != nil {
		return SavedIdentity{}, err
	}
	slog.Debug("save identity: saved", "identity", ch.id.Label, "id", ch.id.ID)
	return SavedIdentity{
		Identity:   ch.id,
		Identities: sortedIdentities(a.data.Identities),
		List:       a.identityList(a.data),
		Warnings:   warnings,
	}, nil
}

// identityChange is a created or edited identity whose new secrets are
// stored but whose config is not written yet.
type identityChange struct {
	data domain.Data     // the config with the identity added or replaced
	id   domain.Identity // as it will be saved
	old  domain.Identity // as it was, for the warnings of cleared fields
	// created are the items this change added (not the ones it
	// overwrote, whose old values are gone); rollback deletes them.
	created []secrets.Ref
	cleared []secrets.Field // deleted once the config is written
}

// prepareIdentity applies in to a copy of d: it validates the identity,
// stores its new secrets (deleting them again if one fails) and sets its
// flags. hidden marks a new identity as a per-host one (FR-HOST-010).
// a.mu must be held.
func (a *App) prepareIdentity(ctx context.Context, d domain.Data, in IdentityInput, hidden bool) (identityChange, error) {
	d.Identities = slices.Clone(d.Identities)
	now := time.Now().UTC()
	var id domain.Identity
	i := -1
	if in.ID == "" {
		id = domain.Identity{Meta: domain.Meta{ID: domain.NewID(), CreatedAt: now}, Hidden: hidden}
	} else {
		i = slices.IndexFunc(d.Identities, func(o domain.Identity) bool { return o.ID == in.ID })
		if i < 0 {
			return identityChange{}, ErrIdentityNotFound
		}
		id = d.Identities[i]
	}
	ch := identityChange{old: id}
	id.Label = strings.TrimSpace(in.Name)
	id.Username = strings.TrimSpace(in.Username)
	id.KeyPath = strings.TrimSpace(in.KeyPath)
	id.UpdatedAt = now
	if err := domain.ValidateIdentity(id, d); err != nil {
		return identityChange{}, err
	}
	key := in.Key
	if in.ImportKeyFile && key.Len() == 0 {
		b, err := a.importKeyFile(id.KeyPath)
		if err != nil {
			return identityChange{}, err
		}
		key = NewSecret(b)
		defer key.Destroy()
		id.KeyPath = ""
	}
	switch {
	case key.Len() > 0:
		pub, err := checkKey(key.Bytes(), in.Passphrase.Bytes())
		if err != nil {
			return identityChange{}, err
		}
		id.PublicKey = pub
	case in.ClearKey:
		id.PublicKey = ""
	}

	for _, fl := range []struct {
		f     secrets.Field
		s     Secret
		clear bool
		flag  *bool
	}{ // in allSecretFields order
		{secrets.FieldPassword, in.Password, in.ClearPassword, &id.HasPassword},
		{secrets.FieldPrivateKey, key, in.ClearKey, &id.HasKey},
		{secrets.FieldKeyPassphrase, in.Passphrase, in.ClearPassphrase, &id.HasPassphrase},
	} {
		if fl.s.Len() == 0 {
			if fl.clear {
				ch.cleared = append(ch.cleared, fl.f)
				*fl.flag = false
			}
			continue
		}
		ref := secrets.Ref{IdentityID: string(id.ID), Field: fl.f}
		if err := a.setSecret(ctx, ref, fl.s); err != nil {
			a.rollback(ctx, ch)
			return identityChange{}, err
		}
		if !*fl.flag {
			ch.created = append(ch.created, ref)
		}
		*fl.flag = true
		delete(a.missing, ref)
	}
	if i < 0 {
		d.Identities = append(d.Identities, id)
	} else {
		d.Identities[i] = id
	}
	ch.data, ch.id = d, id
	return ch, nil
}

// commit writes data, which includes the changes chs prepared, to
// config.json and makes it the loaded config, then deletes the secrets the
// changes cleared and returns a warning for each one left behind. If the
// write fails, it rolls the changes back. a.mu must be held.
func (a *App) commit(ctx context.Context, data domain.Data, chs ...identityChange) ([]string, error) {
	if err := a.saveConfig(data); err != nil {
		for _, ch := range chs {
			a.rollback(ctx, ch)
		}
		return nil, err
	}
	a.data = data
	var warnings []string
	for _, ch := range chs {
		warnings = append(warnings, a.deleteSecrets(ctx, ch.old, ch.cleared...)...)
	}
	return warnings, nil
}

// rollback deletes the items ch created. a.mu must be held.
func (a *App) rollback(ctx context.Context, ch identityChange) {
	for _, r := range ch.created {
		if err := a.cfg.Secrets.Delete(ctx, r); err != nil {
			slog.Debug("save identity: rollback delete failed", "account", r.Account(), "err", err)
		}
	}
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

// SecretsAvailable is the startup probe of the credential store
// (credential-storage §7): nil when it works, or a *StoreError wrapping
// secrets.ErrUnavailable when there is none (D-101), in which case the UI
// shows a banner and disables the secret fields (ARCH-SEC-006). Other
// probe failures, such as a locked keyring, are not reported: saving or
// connecting will ask the store again.
func (a *App) SecretsAvailable(ctx context.Context) error {
	if a.cfg.Secrets == nil {
		return &StoreError{Store: StoreName(), Err: secrets.ErrUnavailable}
	}
	err := a.cfg.Secrets.Available(ctx)
	if errors.Is(err, secrets.ErrUnavailable) {
		slog.Debug("credential store: not available", "err", err)
		return &StoreError{Store: StoreName(), Err: err}
	}
	if err != nil {
		slog.Debug("credential store: probe failed", "err", err)
	}
	return nil
}

// SecretsService is the credential-store service sshelf's items are
// stored under (SEC-CRED-002); warnings name it.
const SecretsService = "sshelf"

// DeletedIdentity is the result of DeleteIdentity.
type DeletedIdentity struct {
	// Hosts are every host after the delete, in last-use order; the ones
	// that used the identity are unlinked.
	Hosts []domain.Host
	// Identities are every identity left, hidden ones included.
	Identities []domain.Identity
	// List is the identities view after the delete.
	List []IdentityInfo
	// Warnings name the stored items that could not be deleted
	// (SEC-CRED-004).
	Warnings []string
}

// DeleteIdentity deletes an identity (FR-IDENT-001): it unlinks every
// host that uses it (ARCH-DATA-001), writes config.json, and only then
// deletes the identity's three items from the credential store
// (credential-storage §7: a crash leaves at worst an orphaned item, never
// a dangling reference). An item that cannot be deleted is named in a
// warning and recorded nowhere (SEC-CRED-004, ARCH-DATA-004).
func (a *App) DeleteIdentity(ctx context.Context, id domain.ID) (DeletedIdentity, error) {
	if err := ctx.Err(); err != nil {
		return DeletedIdentity{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	data, gone, err := withoutIdentity(a.data, id)
	if err != nil {
		return DeletedIdentity{}, err
	}
	if err := a.saveConfig(data); err != nil {
		return DeletedIdentity{}, err
	}
	a.data = data
	warnings := a.deleteSecrets(ctx, gone, allSecretFields...)
	slog.Debug("delete identity: deleted", "identity", gone.Label, "id", id, "warnings", len(warnings))
	return DeletedIdentity{
		Hosts:      sortedHosts(data.Hosts),
		Identities: sortedIdentities(data.Identities),
		List:       a.identityList(data),
		Warnings:   warnings,
	}, nil
}

// withoutIdentity returns a copy of d without identity id and with the
// hosts that used it unlinked, and the removed identity.
func withoutIdentity(d domain.Data, id domain.ID) (domain.Data, domain.Identity, error) {
	i := slices.IndexFunc(d.Identities, func(o domain.Identity) bool { return o.ID == id })
	if i < 0 {
		return d, domain.Identity{}, ErrIdentityNotFound
	}
	gone := d.Identities[i]
	d.Identities = slices.Delete(slices.Clone(d.Identities), i, i+1)
	d.Hosts = slices.Clone(d.Hosts)
	now := time.Now().UTC()
	for j := range d.Hosts {
		if d.Hosts[j].IdentityID == id {
			d.Hosts[j].IdentityID = ""
			d.Hosts[j].UpdatedAt = now
		}
	}
	return d, gone, nil
}

var allSecretFields = []secrets.Field{secrets.FieldPassword, secrets.FieldPrivateKey, secrets.FieldKeyPassphrase}

// deleteSecrets deletes the given fields of id from the store and returns
// a warning for each item that may be left behind: any failed delete of a
// field the config marked as set, and any failure other than a missing
// store. a.mu must be held.
func (a *App) deleteSecrets(ctx context.Context, id domain.Identity, fields ...secrets.Field) []string {
	var warnings []string
	for _, f := range fields {
		ref := secrets.Ref{IdentityID: string(id.ID), Field: f}
		delete(a.missing, ref)
		err := error(&StoreError{Store: StoreName(), Err: secrets.ErrUnavailable})
		if a.cfg.Secrets != nil {
			err = a.cfg.Secrets.Delete(ctx, ref)
		}
		if err == nil {
			continue
		}
		slog.Debug("delete secret: failed", "account", ref.Account(), "err", err)
		if hasField(id, f) || !errors.Is(err, secrets.ErrUnavailable) {
			warnings = append(warnings, orphanWarning(ref, err))
		}
	}
	return warnings
}

func hasField(id domain.Identity, f secrets.Field) bool {
	switch f {
	case secrets.FieldPassword:
		return id.HasPassword
	case secrets.FieldPrivateKey:
		return id.HasKey
	}
	return id.HasPassphrase
}

// orphanWarning names an item that could not be deleted, by service and
// account (SEC-CRED-004), e.g. `could not delete "sshelf" /
// "0192…/password" from Keychain (access denied); remove it with the OS
// tools`.
func orphanWarning(ref secrets.Ref, err error) string {
	reason := "error"
	switch {
	case errors.Is(err, secrets.ErrDenied):
		reason = "access denied"
	case errors.Is(err, secrets.ErrUnavailable):
		reason = "not available"
	}
	return fmt.Sprintf("could not delete %q / %q from %s (%s); remove it with the OS tools",
		SecretsService, ref.Account(), StoreName(), reason)
}
