package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by every Store (credential-storage §5).
var (
	ErrNotFound    = errors.New("secrets: not found")
	ErrUnavailable = errors.New("secrets: no credential store available")
	ErrDenied      = errors.New("secrets: access denied or cancelled")
	ErrTooLarge    = errors.New("secrets: secret exceeds 2800 bytes")
)

// MaxSecretSize is the largest secret, in bytes, that any Store accepts on
// any OS (SEC-CRED-003, D-008). It keeps a go-keyring `security -i` command
// on macOS under its 4096-byte limit.
const MaxSecretSize = 2800

// Field names one of an identity's secrets.
type Field string

// The three secret fields of an identity (SEC-CRED-002).
const (
	FieldPassword      Field = "password"
	FieldPrivateKey    Field = "private-key"
	FieldKeyPassphrase Field = "key-passphrase"
)

func (f Field) valid() bool {
	switch f {
	case FieldPassword, FieldPrivateKey, FieldKeyPassphrase:
		return true
	}
	return false
}

// Ref addresses one secret: one field of one identity.
type Ref struct {
	IdentityID string
	Field      Field
}

// Account returns the store account name, "<identity-id>/<field>"
// (SEC-CRED-002).
func (r Ref) Account() string {
	return r.IdentityID + "/" + string(r.Field)
}

// validate reports whether r maps to a well-formed account name that
// ParseRef reads back as r.
func (r Ref) validate() error {
	if r.IdentityID == "" {
		return errors.New("secrets: empty identity ID")
	}
	for _, c := range r.IdentityID {
		if c == '/' || c < 0x20 || c == 0x7f {
			return errors.New("secrets: invalid character in identity ID")
		}
	}
	if !r.Field.valid() {
		return fmt.Errorf("secrets: unknown field %q", string(r.Field))
	}
	return nil
}

// ParseRef parses an account name written by Ref.Account.
func ParseRef(account string) (Ref, error) {
	id, field, ok := strings.Cut(account, "/")
	if !ok {
		return Ref{}, errors.New("secrets: account has no field")
	}
	r := Ref{IdentityID: id, Field: Field(field)}
	if err := r.validate(); err != nil {
		return Ref{}, err
	}
	return r, nil
}

// Store is the only path to the OS credential store. Implementations are
// safe for concurrent use.
type Store interface {
	// Get returns the secret, or ErrNotFound, ErrDenied or ErrUnavailable.
	// The caller owns the result and must Destroy it.
	Get(ctx context.Context, ref Ref) (Secret, error)
	// Set stores a copy of s, or returns ErrTooLarge, ErrDenied or
	// ErrUnavailable. The caller keeps ownership of s.
	Set(ctx context.Context, ref Ref, s Secret) error
	// Delete removes the secret. It returns nil if it is already absent.
	Delete(ctx context.Context, ref Ref) error
	// Available is a cheap probe that returns nil when the store works.
	Available(ctx context.Context) error
}
