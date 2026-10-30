package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
)

// dupForm is what the host form sends for a duplicate of src left as
// it opened (FR-HOST-004): the copy label, the source's fields, and "" as
// the identity when the source's is hidden ("(none)" in the picker).
func dupForm(t *testing.T, a *App, src domain.Host) (domain.Host, HostCredentials) {
	t.Helper()
	h := domain.Host{Label: domain.CopyLabel(src.Label, a.data.Hosts), Hostname: src.Hostname, Port: src.Port, User: src.User, IdentityID: src.IdentityID}
	c := HostCredentials{CopyOf: src.ID}
	if hidden, ok := hiddenOf(t, a, src.ID); ok {
		h.IdentityID = ""
		c.KeyPath = hidden.KeyPath
	}
	return h, c
}

// hiddenSource saves host "web" with a password, a stored key and a
// passphrase typed in its form, and username deploy on its hidden
// identity.
func hiddenSource(t *testing.T, a *App, pw string) (domain.Host, domain.Identity) {
	t.Helper()
	saved, err := a.SaveHostWithCredentials(context.Background(),
		domain.Host{Label: "web", Hostname: "10.0.0.10", Port: 2222, User: "ubuntu"},
		HostCredentials{KeyPath: "~/.ssh/id_web", Password: secretOf(pw), Key: keySecret(t), Passphrase: secretOf("pass-q")})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	i := slices.IndexFunc(a.data.Identities, func(id domain.Identity) bool { return id.ID == saved.Host.IdentityID })
	a.data.Identities[i].Username = "deploy"
	a.mu.Unlock()
	hidden, _ := hiddenOf(t, a, saved.Host.ID)
	return saved.Host, hidden
}

func storedValue(t *testing.T, store secrets.Store, id domain.ID, f secrets.Field) string {
	t.Helper()
	s, err := store.Get(context.Background(), secrets.Ref{IdentityID: string(id), Field: f})
	if err != nil {
		t.Fatalf("%s/%s: %v", id, f, err)
	}
	defer s.Destroy()
	return string(s.Bytes())
}

func TestFR_HOST_004_CopyKeepsFieldsNotLastUse(t *testing.T) {
	a, _ := storeApp(t, secrets.NewMemStore())
	ctx := context.Background()
	if _, err := a.SaveHostWithCredentials(ctx, domain.Host{Label: "web-prod", Hostname: "10.0.0.10", Port: 2222, User: "ubuntu"}, HostCredentials{}); err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	a.mu.Lock()
	src := &a.data.Hosts[0]
	src.LastConnectedAt = &last
	src.Tags = []string{"prod"}
	src.Notes = "primary"
	src.Favorite = true
	src.Options.SetEnv = map[string]string{"A": "1"}
	src.Import = &domain.ImportSource{Alias: "web-prod"}
	source := *src
	a.mu.Unlock()

	h, c := dupForm(t, a, source)
	dup, err := a.SaveHostWithCredentials(ctx, h, c)
	if err != nil {
		t.Fatal(err)
	}
	got := dup.Host
	if got.ID == "" || got.ID == source.ID || got.Label != "web-prod Copy" || got.Hostname != "10.0.0.10" || got.Port != 2222 || got.User != "ubuntu" {
		t.Fatalf("copy = %+v", got)
	}
	if got.LastConnectedAt != nil || got.Import != nil || got.CreatedAt.IsZero() {
		t.Fatalf("copy kept last use or import source: %+v", got)
	}
	if !slices.Equal(got.Tags, []string{"prod"}) || got.Notes != "primary" || !got.Favorite || got.Options.SetEnv["A"] != "1" || got.IdentityID != "" {
		t.Fatalf("copy lost fields: %+v", got)
	}
	if len(dup.Hosts) != 2 || len(dup.Identities) != 0 {
		t.Fatalf("after duplicate: %+v", dup)
	}
	i := slices.IndexFunc(dup.Hosts, func(o domain.Host) bool { return o.ID == source.ID })
	if i < 0 || dup.Hosts[i].LastConnectedAt == nil || dup.Hosts[i].Label != "web-prod" {
		t.Fatalf("source changed: %+v", dup.Hosts)
	}
	// The form's fields win over the source's.
	h.Label, h.Hostname = "web-eu", "10.0.0.11"
	dup, err = a.SaveHostWithCredentials(ctx, h, c)
	if err != nil || dup.Host.Label != "web-eu" || dup.Host.Hostname != "10.0.0.11" {
		t.Fatalf("edited copy %+v, %v", dup.Host, err)
	}
	// A source that is gone is an error.
	if _, err := a.SaveHostWithCredentials(ctx, h, HostCredentials{CopyOf: "nope"}); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("missing source: %v", err)
	}
}

func TestFR_HOST_011_CopiesHiddenSecrets(t *testing.T) {
	store := secrets.NewMemStore()
	a, dir := storeApp(t, store)
	ctx := context.Background()
	src, srcID := hiddenSource(t, a, "pw-src")
	h, c := dupForm(t, a, src)
	gets, sets := store.Calls(secrets.OpGet), store.Calls(secrets.OpSet)
	dup, err := a.SaveHostWithCredentials(ctx, h, c)
	if err != nil {
		t.Fatal(err)
	}
	if store.Calls(secrets.OpGet)-gets != 3 || store.Calls(secrets.OpSet)-sets != 3 {
		t.Fatalf("%d gets, %d sets, want 3 and 3", store.Calls(secrets.OpGet)-gets, store.Calls(secrets.OpSet)-sets)
	}
	cp, ok := hiddenOf(t, a, dup.Host.ID)
	if !ok || cp.ID == srcID.ID || !cp.Hidden || cp.Label != "web Copy" || cp.Username != "deploy" || cp.KeyPath != "~/.ssh/id_web" ||
		!cp.HasPassword || !cp.HasKey || !cp.HasPassphrase || cp.PublicKey != srcID.PublicKey {
		t.Fatalf("copied identity %+v (source %+v)", cp, srcID)
	}
	if got := refsWith(store, cp.ID); !slices.Equal(got, refsOf(cp.ID, allFields...)) {
		t.Fatalf("copy's items %v", got)
	}
	for _, f := range allFields {
		if storedValue(t, store, cp.ID, f) != storedValue(t, store, srcID.ID, f) {
			t.Errorf("%s differs from the source", f)
		}
	}
	// The copy survives a restart, and the hidden identity is not listed.
	b := New(Config{Dir: dir, Secrets: store, Home: t.TempDir()})
	if l, err := b.Load(ctx); err != nil || len(l.Hosts) != 2 || len(l.Identities) != 2 {
		t.Fatalf("after restart: %+v, %v", l, err)
	}
	if list, _ := a.Identities(ctx); len(list) != 0 {
		t.Fatalf("identities view lists %+v", list)
	}

	// The copies are independent: a new password for the copy leaves the
	// source's, and deleting the source leaves the copy's items.
	if _, err := a.SaveHostWithCredentials(ctx, dup.Host, HostCredentials{KeyPath: cp.KeyPath, Password: secretOf("pw-copy")}); err != nil {
		t.Fatal(err)
	}
	if storedValue(t, store, srcID.ID, secrets.FieldPassword) != "pw-src" || storedValue(t, store, cp.ID, secrets.FieldPassword) != "pw-copy" {
		t.Fatal("editing the copy changed the source's password")
	}
	if _, err := a.DeleteHost(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if got := refsWith(store, cp.ID); len(got) != 3 || len(refsWith(store, srcID.ID)) != 0 {
		t.Fatalf("after deleting the source: %v", store.Keys())
	}
}

func TestFR_HOST_011_TypedAndClearedFields(t *testing.T) {
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	src, srcID := hiddenSource(t, a, "pw-src")
	h, c := dupForm(t, a, src)
	c.Password = secretOf("pw-typed")
	c.ClearPassphrase = true
	gets := store.Calls(secrets.OpGet)
	dup, err := a.SaveHostWithCredentials(context.Background(), h, c)
	if err != nil {
		t.Fatal(err)
	}
	if n := store.Calls(secrets.OpGet) - gets; n != 1 {
		t.Fatalf("%d gets, want 1 (the key)", n)
	}
	cp, _ := hiddenOf(t, a, dup.Host.ID)
	if !cp.HasPassword || !cp.HasKey || cp.HasPassphrase {
		t.Fatalf("copy %+v", cp)
	}
	if storedValue(t, store, cp.ID, secrets.FieldPassword) != "pw-typed" ||
		storedValue(t, store, cp.ID, secrets.FieldPrivateKey) != storedValue(t, store, srcID.ID, secrets.FieldPrivateKey) {
		t.Fatal("typed password not used, or key not copied")
	}
	if len(refsWith(store, cp.ID)) != 2 || len(refsWith(store, srcID.ID)) != 3 {
		t.Fatalf("items %v", store.Keys())
	}
}

func TestFR_HOST_011_SharedIdentityReferenced(t *testing.T) {
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	ctx := context.Background()
	shared, err := a.SaveIdentity(ctx, IdentityInput{Name: "ops", Password: secretOf("shared-pw")})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := a.SaveHostWithCredentials(ctx, domain.Host{Label: "db", Hostname: "db1", IdentityID: shared.Identity.ID}, HostCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	calls := func() int {
		return store.Calls(secrets.OpGet) + store.Calls(secrets.OpSet) + store.Calls(secrets.OpDelete)
	}
	before := calls()
	h, c := dupForm(t, a, saved.Host)
	dup, err := a.SaveHostWithCredentials(ctx, h, c)
	if err != nil {
		t.Fatal(err)
	}
	if dup.Host.IdentityID != shared.Identity.ID || len(dup.Identities) != 1 || calls() != before || len(store.Keys()) != 1 {
		t.Fatalf("copy %+v, identities %+v, %d store calls", dup.Host, dup.Identities, calls()-before)
	}

	// A hidden source whose duplicate picks the shared identity copies
	// nothing.
	src, _ := hiddenSource(t, a, "pw")
	h, c = dupForm(t, a, src)
	h.IdentityID = shared.Identity.ID
	c.KeyPath = ""
	before, keys := calls(), len(store.Keys())
	dup, err = a.SaveHostWithCredentials(ctx, h, c)
	if err != nil {
		t.Fatal(err)
	}
	if dup.Host.IdentityID != shared.Identity.ID || calls() != before || len(store.Keys()) != keys || len(dup.Identities) != 2 {
		t.Fatalf("copy %+v, identities %+v, %d store calls", dup.Host, dup.Identities, calls()-before)
	}
}

func TestFR_HOST_011_StoreErrorLeavesNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, store *secrets.MemStore, src domain.Identity, dir string)
		check func(err error) bool
		want  string
	}{
		{"get denied", func(_ *testing.T, s *secrets.MemStore, _ domain.Identity, _ string) {
			s.FailNext(secrets.OpGet, secrets.ErrDenied)
		}, func(err error) bool {
			_, ok := errors.AsType[*StoreError](err)
			return ok && errors.Is(err, secrets.ErrDenied)
		}, "could not copy the password: Access to " + StoreName() + " was denied"},
		{"second get unavailable", func(_ *testing.T, s *secrets.MemStore, _ domain.Identity, _ string) {
			s.FailNext(secrets.OpGet, nil)
			s.FailNext(secrets.OpGet, secrets.ErrUnavailable)
		}, func(err error) bool { return errors.Is(err, ErrStoreUnavailable) }, "could not copy the key: " + StoreName() + " is not available"},
		{"not on this device", func(t *testing.T, s *secrets.MemStore, src domain.Identity, _ string) {
			if err := s.Delete(context.Background(), secrets.Ref{IdentityID: string(src.ID), Field: secrets.FieldKeyPassphrase}); err != nil {
				t.Fatal(err)
			}
		}, func(err error) bool { return errors.Is(err, secrets.ErrNotFound) }, "could not copy the key passphrase: " + MsgCopyNotOnDevice},
		{"second set denied", func(_ *testing.T, s *secrets.MemStore, _ domain.Identity, _ string) {
			s.FailNext(secrets.OpSet, nil)
			s.FailNext(secrets.OpSet, secrets.ErrDenied)
		}, func(err error) bool { return errors.Is(err, secrets.ErrDenied) }, "Access to " + StoreName() + " was denied"},
		{"config write fails", func(t *testing.T, _ *secrets.MemStore, _ domain.Identity, dir string) {
			file := paths.ConfigFile(dir)
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0o700); err != nil {
				t.Fatal(err)
			}
		}, func(err error) bool { return err != nil }, "could not save config.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := secrets.NewMemStore()
			a, dir := storeApp(t, store)
			src, srcID := hiddenSource(t, a, "pw-src")
			cfg, err := os.ReadFile(paths.ConfigFile(dir))
			if err != nil {
				t.Fatal(err)
			}
			tc.setup(t, store, srcID, dir)
			keys := store.Keys()
			h, c := dupForm(t, a, src)
			_, err = a.SaveHostWithCredentials(context.Background(), h, c)
			if !tc.check(err) || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if got := store.Keys(); !slices.Equal(got, keys) {
				t.Fatalf("items %v, want %v", got, keys)
			}
			if len(a.data.Hosts) != 1 || len(a.data.Identities) != 1 {
				t.Fatalf("in memory: %+v %+v", a.data.Hosts, a.data.Identities)
			}
			if after, err := os.ReadFile(paths.ConfigFile(dir)); err == nil && !bytes.Equal(after, cfg) {
				t.Fatal("config.json changed")
			}
		})
	}
}

func TestFR_HOST_011_NoCanary(t *testing.T) {
	const canary = "canary-dup-pw-4471" //nolint:gosec // a made-up test secret
	store := secrets.NewMemStore()
	a, dir := storeApp(t, store)
	src, _ := hiddenSource(t, a, canary)
	h, c := dupForm(t, a, src)
	dup, err := a.SaveHostWithCredentials(context.Background(), h, c)
	if err != nil {
		t.Fatal(err)
	}
	noCanaryUnder(t, dir, canary)
	if s := fmt.Sprintf("%+v %#v", dup, c); strings.Contains(s, canary) {
		t.Fatal("the result or the credentials print the secret")
	}
}
