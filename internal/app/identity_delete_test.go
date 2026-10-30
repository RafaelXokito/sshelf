package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
)

// threeSecrets saves an identity "ops" with all three secrets.
func threeSecrets(t *testing.T, a *App) domain.Identity {
	t.Helper()
	saved, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "ops", Password: secretOf("p"), Key: keySecret(t), Passphrase: secretOf("q")})
	if err != nil {
		t.Fatal(err)
	}
	return saved.Identity
}

func refsWith(store *secrets.MemStore, id domain.ID) []secrets.Ref {
	var out []secrets.Ref
	for _, r := range store.Keys() {
		if r.IdentityID == string(id) {
			out = append(out, r)
		}
	}
	return out
}

func TestSEC_CRED_004_DeleteLeavesNoItems(t *testing.T) {
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	id := threeSecrets(t, a)
	del, err := a.DeleteIdentity(context.Background(), id.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Keys()) != 0 || len(del.Warnings) != 0 || len(del.List) != 0 || len(del.Identities) != 0 {
		t.Fatalf("after delete: keys %v, %+v", store.Keys(), del)
	}
	if _, err := a.DeleteIdentity(context.Background(), id.ID); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestARCH_DATA_004_NoRefsAfterDelete(t *testing.T) {
	store := secrets.NewMemStore()
	a, _ := storeApp(t, store)
	id := threeSecrets(t, a)
	other, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "other", Password: secretOf("o")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteIdentity(context.Background(), id.ID); err != nil {
		t.Fatal(err)
	}
	if refs := refsWith(store, id.ID); len(refs) != 0 {
		t.Fatalf("refs left: %v", refs)
	}
	if refs := refsWith(store, other.Identity.ID); len(refs) != 1 {
		t.Fatalf("the other identity lost its items: %v", store.Keys())
	}
}

func TestSEC_CRED_004_DeleteFailureWarns(t *testing.T) {
	store := secrets.NewMemStore()
	a, dir := storeApp(t, store)
	id := threeSecrets(t, a)
	store.FailNext(secrets.OpDelete, secrets.ErrDenied) // the password
	del, err := a.DeleteIdentity(context.Background(), id.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := `could not delete "sshelf" / "` + string(id.ID) + `/password" from ` + StoreName()
	if len(del.Warnings) != 1 || !strings.HasPrefix(del.Warnings[0], want) {
		t.Fatalf("warnings = %q, want one starting %q", del.Warnings, want)
	}
	// The identity is gone from the config, which records nothing about
	// the orphaned item.
	cfg, err := os.ReadFile(paths.ConfigFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cfg, []byte(id.ID)) {
		t.Fatalf("config.json mentions the identity:\n%s", cfg)
	}
	if refs := refsWith(store, id.ID); len(refs) != 1 || refs[0].Field != secrets.FieldPassword {
		t.Fatalf("refs = %v, want only the password left", refs)
	}
	// With no store at all, an identity with no secrets deletes quietly.
	a2, _ := storeApp(t, nil)
	plain, err := a2.SaveIdentity(context.Background(), IdentityInput{Name: "plain", KeyPath: "~/.ssh/id"})
	if err != nil {
		t.Fatal(err)
	}
	if del, err := a2.DeleteIdentity(context.Background(), plain.Identity.ID); err != nil || len(del.Warnings) != 0 {
		t.Fatalf("delete without a store: %+v, %v", del, err)
	}
}

func TestSEC_CRED_004_ClearedFieldDeleted(t *testing.T) {
	store := secrets.NewMemStore()
	a, dir := storeApp(t, store)
	ctx := context.Background()
	id := threeSecrets(t, a)
	saved, err := a.SaveIdentity(ctx, IdentityInput{ID: id.ID, Name: "ops", ClearPassword: true})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Identity.HasPassword || !saved.Identity.HasKey || len(saved.Warnings) != 0 {
		t.Fatalf("saved = %+v", saved)
	}
	if got, want := refsWith(store, id.ID), refsOf(id.ID, secrets.FieldPrivateKey, secrets.FieldKeyPassphrase); !slices.Equal(got, want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
	// A failed delete warns, and the flag is still cleared.
	store.FailNext(secrets.OpDelete, secrets.ErrDenied)
	saved, err = a.SaveIdentity(ctx, IdentityInput{ID: id.ID, Name: "ops", ClearKey: true})
	if err != nil {
		t.Fatal(err)
	}
	want := `could not delete "sshelf" / "` + string(id.ID) + `/private-key"`
	if saved.Identity.HasKey || len(saved.Warnings) != 1 || !strings.HasPrefix(saved.Warnings[0], want) {
		t.Fatalf("saved = %+v", saved)
	}
	cfg, err := os.ReadFile(paths.ConfigFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cfg, []byte("private-key")) || !bytes.Contains(cfg, []byte(`"has_private_key": false`)) {
		t.Fatalf("config.json:\n%s", cfg)
	}
	// A new value for the same field wins over clearing it.
	saved, err = a.SaveIdentity(ctx, IdentityInput{ID: id.ID, Name: "ops", Password: secretOf("new"), ClearPassword: true})
	if err != nil || !saved.Identity.HasPassword {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
}

// linkedApp has identity "ops" with a password, used by hosts a and b;
// host c has none.
func linkedApp(t *testing.T) (*App, string, domain.Identity) {
	t.Helper()
	a, dir := storeApp(t, secrets.NewMemStore())
	ctx := context.Background()
	ops, err := a.SaveIdentity(ctx, IdentityInput{Name: "ops", Password: secretOf("p")})
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"a", "b", "c"} {
		h := domain.Host{Label: label, Hostname: label + ".example.com"}
		if label != "c" {
			h.IdentityID = ops.Identity.ID
		}
		if _, err := a.SaveHost(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	return a, dir, ops.Identity
}

func TestARCH_DATA_001_DeleteClearsReferences(t *testing.T) {
	a, dir, ops := linkedApp(t)
	list, _ := a.Identities(context.Background())
	if list[0].LinkedHosts != 2 {
		t.Fatalf("linked = %d", list[0].LinkedHosts)
	}
	del, err := a.DeleteIdentity(context.Background(), ops.ID)
	if err != nil {
		t.Fatal(err)
	}
	l, err := New(Config{Dir: dir}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, hosts := range [][]domain.Host{del.Hosts, l.Hosts} {
		if len(hosts) != 3 {
			t.Fatalf("hosts = %+v", hosts)
		}
		for _, h := range hosts {
			if h.IdentityID != "" {
				t.Fatalf("%s still references %s", h.Label, h.IdentityID)
			}
		}
	}
}

func TestARCH_DATA_001_RenameChangesNoHost(t *testing.T) {
	a, _, ops := linkedApp(t)
	a.mu.Lock()
	before := slices.Clone(a.data.Hosts)
	a.mu.Unlock()
	if _, err := a.SaveIdentity(context.Background(), IdentityInput{ID: ops.ID, Name: "operations"}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	after := slices.Clone(a.data.Hosts)
	a.mu.Unlock()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("hosts changed:\n%+v\n%+v", before, after)
	}
}
