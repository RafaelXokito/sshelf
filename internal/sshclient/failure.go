package sshclient

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Connection failures (FR-CONN-006). Connect wraps the underlying error,
// so errors.Is matches these and Error keeps the detail.
var (
	ErrNameResolution    = errors.New("name resolution failed")
	ErrConnectionRefused = errors.New("connection refused")
	ErrTimedOut          = errors.New("timed out")
)

// classify wraps err with the FR-CONN-006 cause it shows, if any.
func classify(err error) error {
	for _, known := range []error{ErrHostKeyRejected, ErrHostKeyMismatch, ErrHostKeyRevoked, ErrAuthCancelled, ErrAuthFailed} {
		if errors.Is(err, known) {
			return err
		}
	}
	var ne net.Error
	switch {
	case isDNSError(err):
		return wrap(ErrNameResolution, err)
	case errors.Is(err, syscall.ECONNREFUSED):
		return wrap(ErrConnectionRefused, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded),
		errors.As(err, &ne) && ne.Timeout():
		return wrap(ErrTimedOut, err)
	case strings.Contains(err.Error(), "unable to authenticate"):
		// x/crypto has no typed error for "every method failed".
		return wrap(ErrAuthFailed, err)
	}
	return err
}

func isDNSError(err error) bool {
	_, ok := errors.AsType[*net.DNSError](err)
	return ok
}

// causeError is a classified failure: Error keeps the detail, and
// errors.Is matches both the cause and the original error.
type causeError struct{ cause, err error }

func wrap(cause, err error) error { return &causeError{cause, err} }

func (e *causeError) Error() string   { return e.cause.Error() + ": " + e.err.Error() }
func (e *causeError) Unwrap() []error { return []error{e.cause, e.err} }

// watchdog closes the connection when a network wait lasts longer than the
// connect timeout. It is paused while sshclient waits for the user or the
// credential store, which may show an OS prompt, so only network time
// counts.
type watchdog struct {
	mu      sync.Mutex
	d       time.Duration
	t       *time.Timer
	paused  int
	fired   bool
	stopped bool
}

func newWatchdog(d time.Duration, onFire func()) *watchdog {
	w := &watchdog{d: d}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t = time.AfterFunc(d, func() {
		w.mu.Lock()
		if w.paused > 0 || w.stopped {
			w.mu.Unlock()
			return
		}
		w.fired = true
		w.mu.Unlock()
		onFire()
	})
	return w
}

func (w *watchdog) pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused++
	w.t.Stop()
}

func (w *watchdog) resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.paused--
	if w.paused == 0 && !w.fired && !w.stopped {
		w.t.Reset(w.d)
	}
}

// stop disarms the watchdog and reports whether it fired.
func (w *watchdog) stop() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	w.t.Stop()
	return w.fired
}

// pausingPrompter pauses the watchdog while the user decides.
type pausingPrompter struct {
	p  Prompter
	wd *watchdog
}

func (p pausingPrompter) HostKey(ctx context.Context, hp HostKeyPrompt) (bool, error) {
	p.wd.pause()
	defer p.wd.resume()
	return p.p.HostKey(ctx, hp)
}

func (p pausingPrompter) Ask(ctx context.Context, q AuthPrompt) ([]string, error) {
	p.wd.pause()
	defer p.wd.resume()
	return p.p.Ask(ctx, q)
}

// pausingCreds pauses the watchdog while the credential store, or a
// passphrase prompt, answers.
type pausingCreds struct {
	c  CredentialSource
	wd *watchdog
}

func (c pausingCreds) Signers(ctx context.Context, t domain.Target) ([]ssh.Signer, error) {
	c.wd.pause()
	defer c.wd.resume()
	return c.c.Signers(ctx, t)
}

func (c pausingCreds) Password(ctx context.Context, t domain.Target) (string, error) {
	c.wd.pause()
	defer c.wd.resume()
	return c.c.Password(ctx, t)
}
