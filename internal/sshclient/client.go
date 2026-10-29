package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// DefaultConnectTimeout is used when Target.ConnectTimeout is zero
// (FR-CONN-006).
const DefaultConnectTimeout = 10 * time.Second

// ErrHostKeyRejected means the server's host key is unknown and the Prompter
// did not trust it.
var ErrHostKeyRejected = errors.New("host key rejected")

// CredentialSource gives the SSH layer its credentials (ARCH-SEC-002).
// sshclient calls it lazily, inside the auth callbacks, after the host key
// has been verified, so a rejected host key never loads a credential.
type CredentialSource interface {
	// Signers returns the signers for the target's explicit key (stored
	// or file), or none.
	Signers(ctx context.Context, t domain.Target) ([]ssh.Signer, error)
	// Password returns the identity's stored password, or ErrNoCredential
	// when it has none. Any other error aborts the connection.
	Password(ctx context.Context, t domain.Target) (string, error)
}

// HostKeyPrompt describes a host key that no known_hosts file knows.
type HostKeyPrompt struct {
	Address     string // "host:port" as dialled
	Remote      net.Addr
	Key         ssh.PublicKey
	KeyType     string // e.g. "ssh-ed25519"
	Fingerprint string // "SHA256:…"
}

// Prompter asks the user to decide on behalf of the connection.
type Prompter interface {
	// HostKey asks whether to trust an unknown host key. It runs on the
	// connect goroutine and may block until the user answers or ctx ends.
	HostKey(ctx context.Context, p HostKeyPrompt) (trust bool, err error)
}

// RejectUnknownHosts is a Prompter that never trusts an unknown host key.
// It is what Connect uses when it is given no Prompter.
type RejectUnknownHosts struct{}

// HostKey implements Prompter.
func (RejectUnknownHosts) HostKey(context.Context, HostKeyPrompt) (bool, error) { return false, nil }

// Config holds what every connection shares.
type Config struct {
	// KnownHostsFiles are read, in order, to verify host keys. Missing files
	// are skipped. sshclient never writes them.
	KnownHostsFiles []string
}

// Connect dials t, verifies its host key and authenticates with the
// credentials from creds. Cancelling ctx aborts the attempt and closes the
// connection. Only the default x/crypto/ssh algorithms are used
// (SEC-SESSION-004), except that the host-key algorithms are pinned to the
// types of the keys already known for the host (SEC-HOSTKEY-007). A changed
// key fails with *HostKeyMismatchError and a revoked one with
// *HostKeyRevokedError, both before any credential is used.
func (c Config) Connect(ctx context.Context, t domain.Target, creds CredentialSource, p Prompter) (*ssh.Client, error) {
	if p == nil {
		p = RejectUnknownHosts{}
	}
	known, err := c.hostKeyDB()
	if err != nil {
		return nil, err
	}
	port := t.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(t.Hostname, strconv.Itoa(port))
	auth := newAuthState(ctx, t, creds)
	defer auth.done()
	cfg := clientConfig(t, auth, hostKeyCallback(ctx, known, p))
	cfg.HostKeyAlgorithms = hostKeyAlgorithms(known, addr)
	timeout := t.ConnectTimeout
	if timeout == 0 {
		timeout = DefaultConnectTimeout
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// Closing the connection on cancel also unblocks the handshake and any
	// callback waiting on the network.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	stopped := stop()
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if !stopped {
		_ = sc.Close()
		return nil, ctx.Err()
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

// clientConfig builds the x/crypto client configuration. It leaves every
// algorithm list empty so x/crypto's secure defaults apply; Connect then
// pins HostKeyAlgorithms.
func clientConfig(t domain.Target, auth *authState, hk ssh.HostKeyCallback) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            t.User,
		AuthCallback:    auth.next,
		HostKeyCallback: hk,
	}
}

// hostKeyDB loads the known_hosts files that exist.
func (c Config) hostKeyDB() (ssh.HostKeyCallback, error) {
	var files []string
	for _, f := range c.KnownHostsFiles {
		if _, err := os.Stat(f); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		files = append(files, f)
	}
	cb, err := knownhosts.New(files...)
	if err != nil {
		return nil, fmt.Errorf("known_hosts: %w", err)
	}
	return cb, nil
}
