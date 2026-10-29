package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"

	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/sshclient"
	"github.com/RafaelXokito/sshelf/internal/store"
)

// Host-key failures, re-exported from sshclient for the UI.
var (
	// ErrHostKeyRejected means the user did not trust an unknown host key
	// (ARCH-CONN-003).
	ErrHostKeyRejected = sshclient.ErrHostKeyRejected
	// ErrHostKeyMismatch means the host is known with another key; the
	// error is a *HostKeyMismatchError (FR-KH-002).
	ErrHostKeyMismatch = sshclient.ErrHostKeyMismatch
	// ErrHostKeyRevoked means the key is marked @revoked; the error is a
	// *HostKeyRevokedError (FR-KH-003).
	ErrHostKeyRevoked = sshclient.ErrHostKeyRevoked
)

type (
	// HostKeyMismatchError carries both fingerprints and the stale
	// entries' file and line.
	HostKeyMismatchError = sshclient.HostKeyMismatchError
	// HostKeyRevokedError carries the @revoked entry's file and line.
	HostKeyRevokedError = sshclient.HostKeyRevokedError
	// KnownKey is one known_hosts entry.
	KnownKey = sshclient.KnownKey
)

// HostKeyPromptMsg asks the UI whether to trust a host key that no
// known_hosts file knows (FR-KH-001, SEC-HOSTKEY-004). App sends it with the
// function given to SetSend while the connection waits, before any
// credential is used. The UI answers once on Reply, which never blocks:
// true for "Trust and save", false for "Cancel".
type HostKeyPromptMsg struct {
	Host        string
	Port        int
	KeyType     string // e.g. "ssh-ed25519"
	Fingerprint string // "SHA256:…", as ssh-keygen -lf prints it
	Reply       chan<- bool
}

// Answer sends the user's choice. Only the first answer counts.
func (m HostKeyPromptMsg) Answer(trust bool) {
	select {
	case m.Reply <- trust:
	default:
	}
}

// SetSend sets how App delivers prompts to the UI: tea.Program.Send. Until
// it is set, unknown host keys are rejected without a prompt.
func (a *App) SetSend(send func(msg any)) {
	a.mu.Lock()
	a.send = send
	a.mu.Unlock()
}

// HostKey implements sshclient.Prompter (design §8.3): it shows the TOFU
// prompt and waits for the answer or for ctx to end. On "Trust and save" it
// appends the key to sshelf's known_hosts before the connection continues
// (FR-KH-004); if that write fails, the connection fails too.
func (a *App) HostKey(ctx context.Context, p sshclient.HostKeyPrompt) (bool, error) {
	a.mu.Lock()
	send := a.send
	a.mu.Unlock()
	if send == nil {
		return false, nil
	}
	host, portStr, err := net.SplitHostPort(p.Address)
	if err != nil {
		return false, err
	}
	port, _ := strconv.Atoi(portStr)
	reply := make(chan bool, 1)
	send(HostKeyPromptMsg{Host: host, Port: port, KeyType: p.KeyType, Fingerprint: p.Fingerprint, Reply: reply})
	select {
	case trust := <-reply:
		if !trust {
			return false, nil
		}
		if err := a.trustHostKey(p); err != nil {
			return false, fmt.Errorf("saving the host key: %w", err)
		}
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// maxKnownHostsSize bounds how much of sshelf's known_hosts is rewritten.
const maxKnownHostsSize = 16 << 20

// trustHostKey appends one OpenSSH known_hosts line for the key to
// sshelf's own known_hosts, atomically and with mode 0600 (SEC-HOSTKEY-002).
// The host name is stored unhashed (ADR-004). OpenSSH's files are never
// written (D-102).
func (a *App) trustHostKey(p sshclient.HostKeyPrompt) error {
	a.khMu.Lock()
	defer a.khMu.Unlock()
	path := paths.KnownHostsFile(a.cfg.Dir)
	old, err := os.ReadFile(path) //nolint:gosec // path comes from internal/paths
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(old) > maxKnownHostsSize {
		return fmt.Errorf("%s is larger than 16 MiB", path)
	}
	var b bytes.Buffer
	b.Write(old)
	if len(old) > 0 && old[len(old)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString(knownhosts.Line([]string{knownhosts.Normalize(p.Address)}, p.Key))
	b.WriteByte('\n')
	return store.WriteFile(path, b.Bytes())
}
