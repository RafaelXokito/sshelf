package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
)

// Errors returned by Credentials.
var (
	ErrKeyFileNotFound    = errors.New("key file not found")
	ErrUnprotectedKeyFile = errors.New("unprotected private key file")
	ErrKeyFileEncrypted   = errors.New("key is encrypted")
)

// maxKeyFileSize bounds how much of a key file is read.
const maxKeyFileSize = 1 << 20

// Credentials is the sshclient.CredentialSource of sshelf (ADR-003). It
// reads the identity's secrets from the credential store just in time,
// inside the auth callbacks, one Get per field and connection attempt, and
// destroys them as soon as they are parsed or handed over (SEC-MEM-009). A
// key file referenced by path is read at connect time and never copied
// (FR-IDENT-003).
type Credentials struct {
	// Store is the OS credential store; nil means none is configured.
	Store secrets.Store
	// Home is the directory "~" expands to. "" means os.UserHomeDir.
	Home string
}

var _ sshclient.CredentialSource = Credentials{}

// Signers returns the identity's key: the stored key if the identity has
// one, otherwise its key file. A stored passphrase decrypts an encrypted
// key. It returns no signer when the identity has no key.
func (c Credentials) Signers(ctx context.Context, t domain.Target) ([]ssh.Signer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		b    []byte
		name string // how the key is named in messages
	)
	switch {
	case t.HasKey:
		key, err := c.get(ctx, t, secrets.FieldPrivateKey)
		if err != nil {
			return nil, err
		}
		defer key.Destroy()
		b, name = key.Bytes(), "the stored key of "+t.IdentityLabel
	case t.KeyPath != "":
		path, data, err := c.readKeyFile(t.KeyPath)
		defer clear(data)
		if err != nil {
			return nil, err
		}
		b, name = data, path
	default:
		return nil, nil
	}
	signer, err := c.parse(ctx, t, b, name)
	if err != nil {
		return nil, err
	}
	return []ssh.Signer{signer}, nil
}

// parse parses a private key, decrypting it with the stored passphrase
// when it is encrypted.
func (c Credentials) parse(ctx context.Context, t domain.Target, b []byte, name string) (ssh.Signer, error) {
	signer, err := ssh.ParsePrivateKey(b)
	if _, ok := errors.AsType[*ssh.PassphraseMissingError](err); ok {
		if !t.HasPassphrase {
			return nil, fmt.Errorf("%w: %s (no passphrase is stored)", ErrKeyFileEncrypted, name)
		}
		pass, err := c.get(ctx, t, secrets.FieldKeyPassphrase)
		if err != nil {
			return nil, err
		}
		defer pass.Destroy()
		signer, err = ssh.ParsePrivateKeyWithPassphrase(b, pass.Bytes())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return signer, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return signer, nil
}

// Password returns the identity's stored password, or
// sshclient.ErrNoCredential when it has none. The Secret is destroyed
// before returning; x/crypto needs the password as a string, a copy that Go
// cannot wipe (accepted, credential-storage §6).
func (c Credentials) Password(ctx context.Context, t domain.Target) (string, error) {
	if !t.HasPassword {
		return "", sshclient.ErrNoCredential
	}
	s, err := c.get(ctx, t, secrets.FieldPassword)
	if err != nil {
		return "", err
	}
	defer s.Destroy()
	return string(s.Bytes()), nil
}

// get reads one of the identity's secrets. The caller must Destroy it.
func (c Credentials) get(ctx context.Context, t domain.Target, f secrets.Field) (secrets.Secret, error) {
	if c.Store == nil {
		return secrets.Secret{}, secrets.ErrUnavailable
	}
	return c.Store.Get(ctx, secrets.Ref{IdentityID: string(t.IdentityID), Field: f})
}

// readKeyFile expands, checks and reads a key file. The caller clears the
// returned bytes.
func (c Credentials) readKeyFile(keyPath string) (string, []byte, error) {
	path, err := c.expand(keyPath)
	if err != nil {
		return "", nil, err
	}
	f, err := os.Open(path) //nolint:gosec // the user chose this key file
	if errors.Is(err, fs.ErrNotExist) {
		return path, nil, fmt.Errorf("%w: %s", ErrKeyFileNotFound, path)
	}
	if err != nil {
		return path, nil, err
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return path, nil, err
	}
	// Same rule as OpenSSH: refuse keys readable by group or others
	// (SEC-FILE-009).
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return path, nil, fmt.Errorf("%w: permissions %04o for %s are too open", ErrUnprotectedKeyFile, perm, path)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxKeyFileSize))
	if err != nil {
		clear(b)
		return path, nil, err
	}
	return path, b, nil
}

// expand replaces a leading "~" or "~/" with the home directory.
func (c Credentials) expand(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home := c.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %s: %w", p, err)
		}
		home = h
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}
