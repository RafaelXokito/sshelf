package sshclient

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Host-key failures that the user cannot override (SEC-HOSTKEY-003/005).
var (
	// ErrHostKeyMismatch means the host is known with a different key. A
	// *HostKeyMismatchError carries the details.
	ErrHostKeyMismatch = errors.New("host key changed")
	// ErrHostKeyRevoked means the presented key is marked @revoked in a
	// known_hosts file. A *HostKeyRevokedError carries the details.
	ErrHostKeyRevoked = errors.New("host key revoked")
)

// KnownKey is one known_hosts entry.
type KnownKey struct {
	File        string
	Line        int // 1-based
	KeyType     string
	Fingerprint string // "SHA256:…"
}

// HostKeyMismatchError reports a changed host key (FR-KH-002). The
// connection was aborted before authentication.
type HostKeyMismatchError struct {
	Address     string // "host:port" as dialled
	Name        string // the host as known_hosts writes it: "host" or "[host]:port"
	KeyType     string // of the presented key
	Fingerprint string // of the presented key
	// Stale are the trusted entries of the presented key's type (all the
	// host's entries if none has that type), in file order.
	Stale []KnownKey
}

func (e *HostKeyMismatchError) Error() string {
	s := ErrHostKeyMismatch.Error() + " for " + e.Name
	if len(e.Stale) > 0 {
		s += fmt.Sprintf(" (stale entry: %s line %d)", e.Stale[0].File, e.Stale[0].Line)
	}
	return s
}

// Is makes errors.Is(err, ErrHostKeyMismatch) true.
func (e *HostKeyMismatchError) Is(target error) bool { return target == ErrHostKeyMismatch }

// HostKeyRevokedError reports a key marked @revoked (FR-KH-003).
type HostKeyRevokedError struct {
	Address     string
	KeyType     string
	Fingerprint string
	Entry       KnownKey // the @revoked line
}

func (e *HostKeyRevokedError) Error() string {
	return fmt.Sprintf("%s (%s line %d)", ErrHostKeyRevoked, e.Entry.File, e.Entry.Line)
}

// Is makes errors.Is(err, ErrHostKeyRevoked) true.
func (e *HostKeyRevokedError) Is(target error) bool { return target == ErrHostKeyRevoked }

func knownKey(k knownhosts.KnownKey) KnownKey {
	return KnownKey{File: k.Filename, Line: k.Line, KeyType: k.Key.Type(), Fingerprint: ssh.FingerprintSHA256(k.Key)}
}

// hostKeyCallback verifies the key against the known_hosts files
// (SEC-HOSTKEY-001). A revoked key fails with *HostKeyRevokedError, even
// when another file trusts it, and a changed key with
// *HostKeyMismatchError; neither asks p. Only an unknown key is passed to
// p.
func hostKeyCallback(ctx context.Context, known ssh.HostKeyCallback, p Prompter) ssh.HostKeyCallback {
	return func(addr string, remote net.Addr, key ssh.PublicKey) error {
		slog.Debug("connect: verifying host key", "addr", addr, "type", key.Type(), "fingerprint", ssh.FingerprintSHA256(key))
		err := known(addr, remote, key)
		if err == nil {
			slog.Debug("connect: host key known", "addr", addr)
			return nil
		}
		if re, ok := errors.AsType[*knownhosts.RevokedError](err); ok {
			return &HostKeyRevokedError{
				Address: addr, KeyType: key.Type(), Fingerprint: ssh.FingerprintSHA256(key),
				Entry: knownKey(re.Revoked),
			}
		}
		ke, ok := errors.AsType[*knownhosts.KeyError](err)
		if !ok {
			return err
		}
		if len(ke.Want) > 0 {
			return mismatch(addr, key, ke.Want)
		}
		trust, err := p.HostKey(ctx, HostKeyPrompt{
			Address:     addr,
			Remote:      remote,
			Key:         key,
			KeyType:     key.Type(),
			Fingerprint: ssh.FingerprintSHA256(key),
		})
		if err != nil {
			return err
		}
		if !trust {
			return ErrHostKeyRejected
		}
		return nil
	}
}

func mismatch(addr string, key ssh.PublicKey, want []knownhosts.KnownKey) *HostKeyMismatchError {
	e := &HostKeyMismatchError{
		Address:     addr,
		Name:        knownhosts.Normalize(addr),
		KeyType:     key.Type(),
		Fingerprint: ssh.FingerprintSHA256(key),
	}
	for _, w := range want {
		if w.Key.Type() == key.Type() {
			e.Stale = append(e.Stale, knownKey(w))
		}
	}
	if len(e.Stale) == 0 {
		for _, w := range want {
			e.Stale = append(e.Stale, knownKey(w))
		}
	}
	return e
}

// probeKey is a key no known_hosts file lists. Checking it reveals which
// keys are known for an address.
var probeKey = func() ssh.PublicKey {
	k, err := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	if err != nil {
		panic(err)
	}
	return k
}()

// hostKeyAlgorithms returns the host-key algorithms of the keys known for
// addr, in x/crypto's preference order, or nil when none is known
// (SEC-HOSTKEY-007, design §8.2). Pinning them stops a server from
// downgrading to another key type, and stops a host with a known RSA key
// from being reported as changed because it offers ed25519 first.
func hostKeyAlgorithms(known ssh.HostKeyCallback, addr string) []string {
	ke, ok := errors.AsType[*knownhosts.KeyError](known(addr, &net.TCPAddr{}, probeKey))
	if !ok || len(ke.Want) == 0 {
		return nil
	}
	types := map[string]bool{}
	for _, w := range ke.Want {
		types[w.Key.Type()] = true
	}
	var algos []string
	for _, a := range ssh.SupportedAlgorithms().HostKeys { // no SHA-1 ssh-rsa, no DSA
		format := a
		if a == ssh.KeyAlgoRSASHA256 || a == ssh.KeyAlgoRSASHA512 {
			format = ssh.KeyAlgoRSA
		}
		if types[format] && !slices.Contains(algos, a) {
			algos = append(algos, a)
		}
	}
	return algos
}
