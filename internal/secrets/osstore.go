package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// defaultTimeout bounds every OS store call. It is long enough for the user
// to answer an OS unlock or access dialog (credential-storage §5).
const defaultTimeout = 30 * time.Second

// probeAccount is the account Available looks up. It has no '/', so it can
// never collide with a Ref's account.
const probeAccount = "sshelf-availability-probe"

// keyringAPI is the slice of go-keyring the adapter uses, injectable for
// tests.
type keyringAPI struct {
	set func(service, user, password string) error
	get func(service, user string) (string, error)
	del func(service, user string) error
}

// osStore is the Store over the OS credential store: the macOS Keychain
// through /usr/bin/security, or the Linux Secret Service over D-Bus
// (D-008, SEC-CRED-009). go-keyring has no context support, so each call
// runs in a goroutine that is abandoned when the context ends.
type osStore struct {
	service string
	kr      keyringAPI
	timeout time.Duration
}

// NewOSStore returns the Store backed by the OS credential store, with every
// item under service ("sshelf" in production, "sshelf-test-<rand>" in
// tests).
func NewOSStore(service string) Store {
	return newOSStore(service, keyringAPI{set: keyring.Set, get: keyring.Get, del: keyring.Delete}, defaultTimeout)
}

func newOSStore(service string, kr keyringAPI, timeout time.Duration) *osStore {
	return &osStore{service: service, kr: kr, timeout: timeout}
}

// call runs f with the store's timeout. It returns ctx.Err() without
// waiting for f once the context ends; f then finishes in the background
// and its result is dropped.
func call[T any](ctx context.Context, timeout time.Duration, f func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1) // buffered: an abandoned f never blocks
	go func() {
		v, err := f()
		done <- result{v, err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// Get implements Store.
func (o *osStore) Get(ctx context.Context, ref Ref) (Secret, error) {
	if err := ref.validate(); err != nil {
		return Secret{}, err
	}
	v, err := call(ctx, o.timeout, func() (string, error) { return o.kr.get(o.service, ref.Account()) })
	if err != nil {
		return Secret{}, mapError(err)
	}
	// The library's string copy is left for the GC (credential-storage §6).
	return NewSecret([]byte(v)), nil
}

// Set implements Store.
func (o *osStore) Set(ctx context.Context, ref Ref, s Secret) error {
	if err := ref.validate(); err != nil {
		return err
	}
	if s.Len() > MaxSecretSize {
		return ErrTooLarge
	}
	// Converted here, not in the goroutine, so the caller may Destroy s
	// as soon as Set returns, even after a timeout.
	pw := string(s.Bytes())
	_, err := call(ctx, o.timeout, func() (struct{}, error) { return struct{}{}, o.kr.set(o.service, ref.Account(), pw) })
	return mapError(err)
}

// Delete implements Store. An absent item is not an error.
func (o *osStore) Delete(ctx context.Context, ref Ref) error {
	if err := ref.validate(); err != nil {
		return err
	}
	_, err := call(ctx, o.timeout, func() (struct{}, error) { return struct{}{}, o.kr.del(o.service, ref.Account()) })
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return mapError(err)
}

// Available implements Store. It looks up an item that does not exist: a
// working store answers "not found" without showing any dialog for a
// missing item.
func (o *osStore) Available(ctx context.Context) error {
	_, err := call(ctx, o.timeout, func() (string, error) { return o.kr.get(o.service, probeAccount) })
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return mapError(err)
}

// macOS `security` exits with the low byte of the OSStatus.
var deniedExitCodes = map[int]string{
	128: "errSecUserCanceled",
	51:  "errSecAuthFailed",
	36:  "errSecInteractionNotAllowed",
}

// Secret Service and D-Bus errors that mean the user or the provider said
// no, as opposed to there being no provider.
var deniedMessages = []string{
	"failed to unlock correct collection", // go-keyring: unlock prompt dismissed
	"org.freedesktop.Secret.Error.IsLocked",
	"org.freedesktop.DBus.Error.AccessDenied",
}

// mapError maps go-keyring, `security` and D-Bus errors to the sentinels.
// Context errors pass through. Anything unrecognised is ErrUnavailable. The
// wrapped detail is an exit status or a D-Bus message, never secret data.
func mapError(err error) error {
	var exit interface{ ExitCode() int }
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, keyring.ErrSetDataTooBig):
		return ErrTooLarge
	case errors.As(err, &exit):
		if name, ok := deniedExitCodes[exit.ExitCode()]; ok {
			return fmt.Errorf("%w (%s)", ErrDenied, name)
		}
	default:
		for _, m := range deniedMessages {
			if strings.Contains(err.Error(), m) {
				return fmt.Errorf("%w (%v)", ErrDenied, err)
			}
		}
	}
	return fmt.Errorf("%w (%v)", ErrUnavailable, err)
}
