package app

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
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
	// Prompter asks for the passphrase of an encrypted key that has none
	// stored. nil means encrypted keys need a stored passphrase.
	Prompter sshclient.Prompter
	// OnMissing, if set, is told about each secret the store did not
	// have, so the identity form can show "not set on this device".
	OnMissing func(secrets.Ref)
}

var _ sshclient.CredentialSource = Credentials{}

// Signers returns the identity's key: the stored key if the identity has
// one, otherwise its key file. It returns no signer when the identity has
// no key, or when its stored key is not set on this device.
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
		if errors.Is(err, secrets.ErrNotFound) || errors.Is(err, secrets.ErrUnavailable) {
			// A key cannot be typed in: skip it, and let password or
			// keyboard-interactive authentication follow (SEC-CRED-007,
			// D-101).
			slog.Debug("connect: stored key not readable", "identity", t.IdentityLabel, "err", err)
			return nil, nil
		}
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

// parse parses a private key. An encrypted key is decrypted with the
// stored passphrase, if there is one, or with the passphrase the user types
// (FR-CONN-003); after MaxAuthFailures wrong passphrases it fails with
// ErrAuthFailed.
func (c Credentials) parse(ctx context.Context, t domain.Target, b []byte, name string) (ssh.Signer, error) {
	signer, err := ssh.ParsePrivateKey(b)
	if _, ok := errors.AsType[*ssh.PassphraseMissingError](err); !ok {
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return signer, nil
	}
	var note string
	for try := range sshclient.MaxAuthFailures {
		var pass []byte
		if try == 0 && t.HasPassphrase {
			s, err := c.get(ctx, t, secrets.FieldKeyPassphrase)
			switch {
			case err == nil:
				pass = bytes.Clone(s.Bytes())
				s.Destroy()
			case errors.Is(err, secrets.ErrNotFound):
				note = "The stored passphrase is not set on this device."
			case errors.Is(err, secrets.ErrUnavailable):
				note = StoreName() + " is not available: the passphrase is used for this connection only and not saved."
			default:
				return nil, err
			}
		}
		if pass == nil {
			if pass, err = c.askPassphrase(ctx, t, name, note); err != nil {
				return nil, err
			}
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(b, pass)
		clear(pass)
		if errors.Is(err, x509.IncorrectPasswordError) {
			note = "Wrong passphrase, please try again."
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return signer, nil
	}
	return nil, fmt.Errorf("%w: wrong passphrase for %s", sshclient.ErrAuthFailed, name)
}

// askPassphrase asks the user for the passphrase of the key called name.
func (c Credentials) askPassphrase(ctx context.Context, t domain.Target, name, note string) ([]byte, error) {
	if c.Prompter == nil {
		return nil, fmt.Errorf("%w: %s (no passphrase is stored)", ErrKeyFileEncrypted, name)
	}
	answers, err := c.Prompter.Ask(ctx, sshclient.AuthPrompt{
		Kind: sshclient.PromptPassphrase, Label: t.Label, User: t.User, Host: t.Hostname,
		Key: name, Questions: []sshclient.Question{{Text: "Passphrase:"}}, Note: note,
	})
	if err != nil {
		return nil, err
	}
	if len(answers) != 1 {
		return nil, errors.New("passphrase prompt returned no answer")
	}
	return []byte(answers[0]), nil
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
	if errors.Is(err, secrets.ErrNotFound) {
		return "", sshclient.ErrCredentialMissing // asked for instead (SEC-CRED-007)
	}
	if errors.Is(err, secrets.ErrUnavailable) {
		// Asked for instead, never saved (D-101, SEC-CRED-005).
		return "", fmt.Errorf("%w: %s is not available", sshclient.ErrStoreUnavailable, StoreName())
	}
	if err != nil {
		return "", err
	}
	defer s.Destroy()
	return string(s.Bytes()), nil
}

// get reads one of the identity's secrets. The caller must Destroy it. A
// denied or unavailable store is a *StoreError. Denied aborts the attempt,
// with no retry and nothing cached, so the next connect asks the store
// again (SEC-CRED-006); with no store the callers ask the user instead
// (D-101, SEC-CRED-005).
func (c Credentials) get(ctx context.Context, t domain.Target, f secrets.Field) (secrets.Secret, error) {
	if c.Store == nil {
		return secrets.Secret{}, &StoreError{Store: StoreName(), Err: secrets.ErrUnavailable}
	}
	ref := secrets.Ref{IdentityID: string(t.IdentityID), Field: f}
	s, err := c.Store.Get(ctx, ref)
	if errors.Is(err, secrets.ErrNotFound) && c.OnMissing != nil {
		c.OnMissing(ref)
	}
	if errors.Is(err, secrets.ErrDenied) || errors.Is(err, secrets.ErrUnavailable) {
		return secrets.Secret{}, &StoreError{Store: StoreName(), Err: err}
	}
	return s, err
}

// StoreError reports that the OS credential store denied access or is not
// available (SEC-CRED-006). Its message names the store.
type StoreError struct {
	Store string // "Keychain" or "Secret Service"
	Err   error  // secrets.ErrDenied or secrets.ErrUnavailable
}

func (e *StoreError) Error() string {
	if errors.Is(e.Err, secrets.ErrDenied) {
		return "Access to " + e.Store + " was denied"
	}
	return e.Store + " is not available"
}

func (e *StoreError) Unwrap() error { return e.Err }

// Explain is the one-line D-101 explanation of an unavailable store,
// naming what is missing and how to fix it, e.g. "No Secret Service
// found: install or start GNOME Keyring or KWallet" (FR-CRED-002).
func (e *StoreError) Explain() string {
	if errors.Is(e.Err, secrets.ErrDenied) {
		return e.Error()
	}
	if e.Store == "Keychain" {
		return "Keychain is not available: unlock or repair your login keychain"
	}
	return "No " + e.Store + " found: install or start GNOME Keyring or KWallet"
}

// StoreName is the user-facing name of this OS's credential store.
func StoreName() string {
	if runtime.GOOS == "darwin" {
		return "Keychain"
	}
	return "Secret Service"
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
