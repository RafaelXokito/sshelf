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
)

// Errors returned by KeyFileSource.
var (
	ErrKeyFileNotFound    = errors.New("key file not found")
	ErrUnprotectedKeyFile = errors.New("unprotected private key file")
	ErrKeyFileEncrypted   = errors.New("key file is encrypted")
)

// maxKeyFileSize bounds how much of a key file is read.
const maxKeyFileSize = 1 << 20

// KeyFileSource is the sshclient.CredentialSource for identities that
// reference a private key file (FR-IDENT-003). The file is read at connect
// time and never copied. Stored keys and passwords come in T-012.
type KeyFileSource struct {
	// Home is the directory "~" expands to. "" means os.UserHomeDir.
	Home string
}

// Signers reads, checks and parses t.KeyPath. It returns no signer when the
// target has no key file.
func (s KeyFileSource) Signers(ctx context.Context, t domain.Target) ([]ssh.Signer, error) {
	if t.KeyPath == "" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.expand(t.KeyPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) //nolint:gosec // the user chose this key file
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrKeyFileNotFound, path)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// Same rule as OpenSSH: refuse keys readable by group or others
	// (SEC-FILE-009).
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("%w: permissions %04o for %s are too open", ErrUnprotectedKeyFile, perm, path)
	}

	b, err := io.ReadAll(io.LimitReader(f, maxKeyFileSize))
	defer clear(b)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(b)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		return nil, fmt.Errorf("%w: %s (passphrase prompts are not supported yet)", ErrKeyFileEncrypted, path)
	}
	if err != nil {
		return nil, fmt.Errorf("key file %s: %w", path, err)
	}
	return []ssh.Signer{signer}, nil
}

// expand replaces a leading "~" or "~/" with the home directory.
func (s KeyFileSource) expand(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home := s.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %s: %w", p, err)
		}
		home = h
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}
