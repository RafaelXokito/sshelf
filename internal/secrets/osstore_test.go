package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/RafaelXokito/sshelf/internal/testutil"
)

// These tests never reach go-keyring's real backends: every osStore here
// gets injected functions. Real-store tests are in
// osstore_integration_test.go behind the integration_keyring build tag.

func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }

const testService = "sshelf-test-unit"

// fakeKeyring is an in-memory stand-in for go-keyring.
type fakeKeyring struct {
	mu    sync.Mutex
	items map[string]string // "service\x00account" -> value
	err   error             // returned by every call when set
	seen  []string          // "op service account" per call
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{items: map[string]string{}} }

func (f *fakeKeyring) record(op, service, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, op+" "+service+" "+user)
	return f.err
}

func (f *fakeKeyring) api() keyringAPI {
	return keyringAPI{
		set: func(service, user, password string) error {
			if err := f.record("set", service, user); err != nil {
				return err
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			f.items[service+"\x00"+user] = password
			return nil
		},
		get: func(service, user string) (string, error) {
			if err := f.record("get", service, user); err != nil {
				return "", err
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			v, ok := f.items[service+"\x00"+user]
			if !ok {
				return "", keyring.ErrNotFound
			}
			return v, nil
		},
		del: func(service, user string) error {
			if err := f.record("del", service, user); err != nil {
				return err
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			k := service + "\x00" + user
			if _, ok := f.items[k]; !ok {
				return keyring.ErrNotFound
			}
			delete(f.items, k)
			return nil
		},
	}
}

func (f *fakeKeyring) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

func TestNewOSStore_UsesServiceAndDefaultTimeout(t *testing.T) {
	s, ok := NewOSStore("sshelf").(*osStore)
	if !ok {
		t.Fatalf("NewOSStore returned %T", s)
	}
	if s.service != "sshelf" || s.timeout != 30*time.Second {
		t.Fatalf("service %q timeout %v", s.service, s.timeout)
	}
	if s.kr.get == nil || s.kr.set == nil || s.kr.del == nil {
		t.Fatal("keyring functions not wired")
	}
}

func TestOSStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	fk := newFakeKeyring()
	var store Store = newOSStore(testService, fk.api(), time.Second)
	ref := Ref{IdentityID: testID, Field: FieldPrivateKey}
	value := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjE=\n-----END OPENSSH PRIVATE KEY-----\n"

	if err := store.Available(ctx); err != nil {
		t.Fatalf("Available = %v", err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v", err)
	}
	in := NewSecret([]byte(value))
	if err := store.Set(ctx, ref, in); err != nil {
		t.Fatal(err)
	}
	in.Destroy()
	got, err := store.Get(ctx, ref)
	if err != nil || string(got.Bytes()) != value {
		t.Fatalf("Get = %q, %v", got.Bytes(), err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete absent = %v, want nil", err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}

	account := testID + "/private-key"
	want := []string{
		"get " + testService + " " + probeAccount,
		"get " + testService + " " + account,
		"set " + testService + " " + account,
		"get " + testService + " " + account,
		"del " + testService + " " + account,
		"del " + testService + " " + account,
		"get " + testService + " " + account,
	}
	if got := fk.calls(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("keyring calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOSStore_RejectsBeforeCallingKeyring(t *testing.T) {
	ctx := context.Background()
	fk := newFakeKeyring()
	store := newOSStore(testService, fk.api(), time.Second)
	ok := Ref{IdentityID: testID, Field: FieldPassword}
	bad := Ref{IdentityID: "a/b", Field: FieldPassword}

	if err := store.Set(ctx, ok, NewSecret(bytes.Repeat([]byte("x"), MaxSecretSize+1))); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Set 2801 bytes = %v, want ErrTooLarge", err)
	}
	if _, err := store.Get(ctx, bad); err == nil {
		t.Error("Get accepted invalid ref")
	}
	if err := store.Set(ctx, bad, NewSecret([]byte("x"))); err == nil {
		t.Error("Set accepted invalid ref")
	}
	if err := store.Delete(ctx, bad); err == nil {
		t.Error("Delete accepted invalid ref")
	}
	if c := fk.calls(); len(c) != 0 {
		t.Errorf("keyring called: %v", c)
	}
	if err := store.Set(ctx, ok, NewSecret(bytes.Repeat([]byte("x"), MaxSecretSize))); err != nil {
		t.Errorf("Set 2800 bytes = %v", err)
	}
}

// exitError mimics *exec.ExitError from /usr/bin/security.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitError) ExitCode() int { return int(e) }

func TestOSStore_ErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"not found", keyring.ErrNotFound, ErrNotFound},
		{"too big", keyring.ErrSetDataTooBig, ErrTooLarge},
		{"macOS user cancelled", exitError(128), ErrDenied},
		{"macOS wrong unlock password", exitError(51), ErrDenied},
		{"macOS interaction not allowed", exitError(36), ErrDenied},
		{"macOS other exit", exitError(1), ErrUnavailable},
		{"wrapped exit", fmt.Errorf("run: %w", exitError(128)), ErrDenied},
		{"linux prompt dismissed", errors.New("failed to unlock correct collection '/org/freedesktop/secrets/aliases/default'"), ErrDenied},
		{"linux locked", errors.New("org.freedesktop.Secret.Error.IsLocked"), ErrDenied},
		{"dbus access denied", errors.New("org.freedesktop.DBus.Error.AccessDenied: nope"), ErrDenied},
		{"no session bus", errors.New(`exec: "dbus-launch": executable file not found in $PATH`), ErrUnavailable},
		{"no provider", errors.New("The name org.freedesktop.secrets was not provided by any .service files"), ErrUnavailable},
		{"no security binary", errors.New(`fork/exec /usr/bin/security: no such file or directory`), ErrUnavailable},
	}
	ctx := context.Background()
	ref := Ref{IdentityID: testID, Field: FieldPassword}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fk := newFakeKeyring()
			fk.err = tc.err
			store := newOSStore(testService, fk.api(), time.Second)

			_, getErr := store.Get(ctx, ref)
			setErr := store.Set(ctx, ref, NewSecret([]byte("pw")))
			for op, err := range map[string]error{"Get": getErr, "Set": setErr} {
				if !errors.Is(err, tc.want) {
					t.Errorf("%s = %v, want %v", op, err, tc.want)
				}
			}

			delErr := store.Delete(ctx, ref)
			availErr := store.Available(ctx)
			if tc.want == ErrNotFound {
				// Delete of an absent item and a probe that finds nothing
				// both mean success.
				if delErr != nil || availErr != nil {
					t.Errorf("Delete = %v, Available = %v, want nil", delErr, availErr)
				}
				return
			}
			for op, err := range map[string]error{"Delete": delErr, "Available": availErr} {
				if !errors.Is(err, tc.want) {
					t.Errorf("%s = %v, want %v", op, err, tc.want)
				}
			}
		})
	}
	if err := mapError(nil); err != nil {
		t.Errorf("mapError(nil) = %v", err)
	}
}

// blockingKeyring blocks every call until release is closed.
func blockingKeyring(t *testing.T) (keyringAPI, *callCounter) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	n := &callCounter{}
	wait := func() { n.add(); <-release }
	return keyringAPI{
		set: func(_, _, _ string) error { wait(); return nil },
		get: func(_, _ string) (string, error) { wait(); return "late", nil },
		del: func(_, _ string) error { wait(); return nil },
	}, n
}

type callCounter struct {
	mu sync.Mutex
	n  int
}

func (c *callCounter) add() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *callCounter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestOSStore_ContextTimeout(t *testing.T) {
	ref := Ref{IdentityID: testID, Field: FieldPassword}

	t.Run("store timeout", func(t *testing.T) {
		kr, _ := blockingKeyring(t)
		store := newOSStore(testService, kr, 50*time.Millisecond)
		ops := map[string]func() error{
			"Get":       func() error { _, err := store.Get(context.Background(), ref); return err },
			"Set":       func() error { return store.Set(context.Background(), ref, NewSecret([]byte("pw"))) },
			"Delete":    func() error { return store.Delete(context.Background(), ref) },
			"Available": func() error { return store.Available(context.Background()) },
		}
		for name, op := range ops {
			start := time.Now()
			err := op()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("%s = %v, want DeadlineExceeded", name, err)
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Errorf("%s took %v", name, d)
			}
		}
	})

	t.Run("caller cancels", func(t *testing.T) {
		kr, n := blockingKeyring(t)
		store := newOSStore(testService, kr, time.Hour)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for n.get() == 0 {
				time.Sleep(time.Millisecond)
			}
			cancel()
		}()
		if _, err := store.Get(ctx, ref); !errors.Is(err, context.Canceled) {
			t.Fatalf("Get = %v, want Canceled", err)
		}
	})

	t.Run("already cancelled", func(t *testing.T) {
		kr, n := blockingKeyring(t)
		store := newOSStore(testService, kr, time.Hour)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := store.Get(ctx, ref); !errors.Is(err, context.Canceled) {
			t.Fatalf("Get = %v, want Canceled", err)
		}
		if n.get() != 0 {
			t.Fatal("keyring called with a cancelled context")
		}
	})
}
