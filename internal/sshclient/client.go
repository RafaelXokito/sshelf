package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	// Password returns the identity's stored password, ErrNoCredential
	// when it has none, or ErrCredentialMissing when the identity has one
	// that the store does not contain. Any other error aborts the
	// connection.
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
	// Ask shows an authentication prompt and returns one answer per
	// question, or ErrAuthCancelled when the user cancels. It runs on the
	// connect goroutine and may block until the user answers or ctx ends.
	Ask(ctx context.Context, p AuthPrompt) ([]string, error)
}

// RejectUnknownHosts is a Prompter that never trusts an unknown host key
// and cancels every authentication prompt. It is what Connect uses when it
// is given no Prompter.
type RejectUnknownHosts struct{}

// HostKey implements Prompter.
func (RejectUnknownHosts) HostKey(context.Context, HostKeyPrompt) (bool, error) { return false, nil }

// Ask implements Prompter.
func (RejectUnknownHosts) Ask(context.Context, AuthPrompt) ([]string, error) {
	return nil, ErrAuthCancelled
}

// Config holds what every connection shares.
type Config struct {
	// KnownHostsFiles are read, in order, to verify host keys. Missing files
	// are skipped. sshclient never writes them.
	KnownHostsFiles []string
	// Dial opens the TCP connection; nil means a net.Dialer. Tests use it
	// to stall or fail the dial.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Progress, if set, is told each stage the attempt reaches.
	Progress func(Stage)
}

// Stage is a step of a connection attempt, shown in the Connecting modal
// (design §9.2).
type Stage string

// The stages, in order.
const (
	StageDialling       Stage = "dialling"
	StageVerifying      Stage = "verifying"
	StageAuthenticating Stage = "authenticating"
)

func (c Config) stage(s Stage) {
	if c.Progress != nil {
		c.Progress(s)
	}
}

// Connect dials t, verifies its host key and authenticates with the
// credentials from creds. Cancelling ctx aborts the attempt and closes the
// connection. Only the default x/crypto/ssh algorithms are used
// (SEC-SESSION-004), except that the host-key algorithms are pinned to the
// types of the keys already known for the host (SEC-HOSTKEY-007). A changed
// key fails with *HostKeyMismatchError and a revoked one with
// *HostKeyRevokedError, both before any credential is used.
//
// Every network wait is bounded by t.ConnectTimeout (10 s by default,
// FR-CONN-006); time spent waiting for the user or the credential store
// does not count. Failures are classified (FR-CONN-006): ErrNameResolution,
// ErrConnectionRefused, ErrTimedOut, the host-key errors, ErrAuthCancelled
// and ErrAuthFailed, all usable with errors.Is. A cancelled ctx returns
// ctx.Err().
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
	timeout := t.ConnectTimeout
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	// Never log credentials: only the address, user, stages and errors.
	log := slog.With("addr", addr, "user", t.User)
	log.Debug("connect: dialling", "timeout", timeout)
	c.stage(StageDialling)
	conn, err := c.dial(ctx, addr, timeout)
	if err != nil {
		log.Debug("connect: dial failed", "err", err)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, classify(err)
	}
	log.Debug("connect: handshake (verifying host key, then authenticating)")
	c.stage(StageVerifying)
	// Closing the connection on cancel or on timeout also unblocks the
	// handshake and any callback waiting on the network.
	wd := newWatchdog(timeout, func() { _ = conn.Close() })
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	pp := pausingPrompter{p: p, wd: wd}
	var pc CredentialSource
	if creds != nil {
		pc = pausingCreds{c: creds, wd: wd}
	}
	auth := newAuthState(ctx, t, pc, pp)
	defer auth.done()
	verify := hostKeyCallback(ctx, known, pp)
	cfg := clientConfig(t, auth, func(host string, remote net.Addr, key ssh.PublicKey) error {
		if err := verify(host, remote, key); err != nil {
			return err
		}
		c.stage(StageAuthenticating)
		return nil
	})
	cfg.HostKeyAlgorithms = hostKeyAlgorithms(known, addr)
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	timedOut := wd.stop()
	stopped := stop()
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			log.Debug("connect: cancelled", "err", ctx.Err())
			return nil, ctx.Err()
		}
		log.Debug("connect: handshake failed", "err", err, "timed_out", timedOut)
		if timedOut {
			return nil, fmt.Errorf("%w after %v", ErrTimedOut, timeout)
		}
		return nil, classify(err)
	}
	if !stopped || timedOut {
		_ = sc.Close()
		if !stopped {
			log.Debug("connect: cancelled", "err", ctx.Err())
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w after %v", ErrTimedOut, timeout)
	}
	log.Debug("connect: authenticated")
	return ssh.NewClient(sc, chans, reqs), nil
}

// dial opens the TCP connection within timeout.
func (c Config) dial(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dial := c.Dial
	if dial == nil {
		var d net.Dialer
		dial = d.DialContext
	}
	return dial(dctx, "tcp", addr)
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

// logAuthAttempt records that an auth method is being tried. It logs the
// method name only, never a credential.
func logAuthAttempt(t domain.Target, method string) {
	slog.Debug("connect: auth attempt", "user", t.User, "host", t.Hostname, "method", method)
}
