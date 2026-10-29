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
	// Signers returns the signers for the target's explicit key, or none.
	Signers(ctx context.Context, t domain.Target) ([]ssh.Signer, error)
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
// It is the default until the TOFU prompt exists (T-007).
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
// (SEC-SESSION-004).
func (c Config) Connect(ctx context.Context, t domain.Target, creds CredentialSource, p Prompter) (*ssh.Client, error) {
	if p == nil {
		p = RejectUnknownHosts{}
	}
	known, err := c.hostKeyDB()
	if err != nil {
		return nil, err
	}
	cfg := clientConfig(ctx, t, creds, hostKeyCallback(ctx, known, p))

	port := t.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(t.Hostname, strconv.Itoa(port))
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
// algorithm list empty so x/crypto's secure defaults apply.
func clientConfig(ctx context.Context, t domain.Target, creds CredentialSource, hk ssh.HostKeyCallback) *ssh.ClientConfig {
	var auth []ssh.AuthMethod
	if creds != nil {
		auth = append(auth, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			return creds.Signers(ctx, t)
		}))
	}
	return &ssh.ClientConfig{
		User:            t.User,
		Auth:            auth,
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

// hostKeyCallback verifies the key against the known_hosts files and asks p
// about unknown keys (SEC-HOSTKEY-001). A changed or revoked key fails
// without asking.
func hostKeyCallback(ctx context.Context, known ssh.HostKeyCallback, p Prompter) ssh.HostKeyCallback {
	return func(addr string, remote net.Addr, key ssh.PublicKey) error {
		err := known(addr, remote, key)
		var ke *knownhosts.KeyError
		if err == nil || !errors.As(err, &ke) || len(ke.Want) > 0 {
			return err
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
