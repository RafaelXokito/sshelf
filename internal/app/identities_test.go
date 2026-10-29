package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// storeApp is a loaded App over an empty temp config dir and store.
func storeApp(t *testing.T, store secrets.Store) (*App, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sshelf")
	a := New(Config{Dir: dir, Secrets: store, Home: t.TempDir(), Terminal: stillTerm{}})
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, dir
}

func secretOf(s string) Secret { return NewSecret([]byte(s)) }

// noCanaryUnder fails if any file under dir contains one of the canaries.
func noCanaryUnder(t *testing.T, dir string, canaries ...string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // a test temp dir
		if err != nil {
			return err
		}
		for _, c := range canaries {
			if bytes.Contains(b, []byte(c)) {
				t.Errorf("%s contains a canary secret", p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func refsOf(id domain.ID, fields ...secrets.Field) []secrets.Ref {
	var out []secrets.Ref
	for _, f := range fields {
		out = append(out, secrets.Ref{IdentityID: string(id), Field: f})
	}
	slices.SortFunc(out, func(a, b secrets.Ref) int {
		if a.Account() < b.Account() {
			return -1
		}
		return 1
	})
	return out
}

var allFields = []secrets.Field{secrets.FieldPassword, secrets.FieldPrivateKey, secrets.FieldKeyPassphrase}

func TestFR_IDENT_001_NameUniqueCaseInsensitive(t *testing.T) {
	a, _ := storeApp(t, secrets.NewMemStore())
	ctx := context.Background()
	ops, err := a.SaveIdentity(ctx, IdentityInput{Name: "ops", Username: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"OPS", " Ops ", ""} {
		_, err := a.SaveIdentity(ctx, IdentityInput{Name: name})
		ve, ok := errors.AsType[*domain.ValidationError](err)
		if !ok || ve.Fields[domain.FieldName] == "" {
			t.Fatalf("name %q: err = %v, want a name error", name, err)
		}
	}
	// Renaming to a different case of its own name is fine.
	if _, err := a.SaveIdentity(ctx, IdentityInput{ID: ops.Identity.ID, Name: "OPS"}); err != nil {
		t.Fatal(err)
	}
	// Hidden per-host identities do not take names.
	a.mu.Lock()
	a.data.Identities = append(a.data.Identities, domain.Identity{Meta: domain.Meta{ID: "hid"}, Label: "web", Hidden: true})
	a.mu.Unlock()
	if _, err := a.SaveIdentity(ctx, IdentityInput{Name: "web"}); err != nil {
		t.Fatalf("a hidden identity's name blocks a new one: %v", err)
	}
	list, err := a.Identities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Identity.Label != "OPS" || list[1].Identity.Label != "web" || list[1].Identity.Hidden {
		t.Fatalf("list = %+v", list)
	}
}

func TestFR_IDENT_001_PasswordChangeAppliesToLinkedHosts(t *testing.T) {
	const newPassword = "canary-new-password-55e1"
	srv := sshtest.New(t, sshtest.WithPassword("deploy", newPassword))
	store := newRecStore()
	store.put(t, "i1", secrets.FieldPassword, []byte(testPassword))
	hosts := `[{"id": "h1", "label": "a", "hostname": %[1]q, "port": %[2]d, "identity_id": "i1"},
		{"id": "h2", "label": "b", "hostname": %[1]q, "port": %[2]d, "identity_id": "i1"},
		{"id": "h3", "label": "c", "hostname": %[1]q, "port": %[2]d, "identity_id": "i1"}]`
	a := credApp(t, store, []*sshtest.Server{srv}, `[{"id": "i1", "name": "ops", "username": "deploy", "has_password": true}]`, hosts)
	failOnPrompt(t, a)
	if _, err := a.SaveIdentity(context.Background(), IdentityInput{ID: "i1", Name: "ops", Username: "deploy", Password: secretOf(newPassword)}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"h1", "h2", "h3"} {
		connectOK(t, a, h)
	}
	if got := methods(srv.AuthAttempts()); slices.Index(got, "password") >= 0 {
		t.Fatalf("a host sent the old password: %v", got)
	}
}

func TestFR_CRED_001_ConfigHasNoSecret(t *testing.T) {
	const (
		pw   = "canary-pw-0d1e" //nolint:gosec // a made-up test secret
		pass = "canary-pass-77ab"
	)
	store := secrets.NewMemStore()
	a, dir := storeApp(t, store)
	key, _ := newStoredKey(t, "")
	in := IdentityInput{Name: "ops", Password: secretOf(pw), Passphrase: secretOf(pass)}
	in.Key = NewSecret(bytes.Clone(key))
	saved, err := a.SaveIdentity(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	id := saved.Identity
	if !id.HasPassword || !id.HasKey || !id.HasPassphrase {
		t.Fatalf("flags = %+v", id)
	}
	noCanaryUnder(t, dir, pw, pass, string(key[40:80]))
	if got, want := store.Keys(), refsOf(id.ID, allFields...); !slices.Equal(got, want) {
		t.Fatalf("store keys = %v, want %v", got, want)
	}
	// The input's secrets were destroyed.
	if in.Password.Len() != 0 || in.Key.Len() != 0 || in.Passphrase.Len() != 0 {
		t.Fatal("SaveIdentity did not destroy the input's secrets")
	}
}

func TestSEC_CRED_002_RenameKeepsKeys(t *testing.T) {
	store := secrets.NewMemStore()
	a, dir := storeApp(t, store)
	ctx := context.Background()
	saved, err := a.SaveIdentity(ctx, IdentityInput{Name: "ops", Password: secretOf("p"), Key: keySecret(t), Passphrase: secretOf("q")})
	if err != nil {
		t.Fatal(err)
	}
	id := saved.Identity.ID
	want := []string{string(id) + "/key-passphrase", string(id) + "/password", string(id) + "/private-key"}
	var got []string
	for _, r := range store.Keys() {
		got = append(got, r.Account())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("accounts = %v, want %v", got, want)
	}
	before := store.Keys()
	sets := store.Calls(secrets.OpSet)
	renamed, err := a.SaveIdentity(ctx, IdentityInput{ID: id, Name: "operations"})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Identity.ID != id || !reflect.DeepEqual(store.Keys(), before) || store.Calls(secrets.OpSet) != sets {
		t.Fatalf("rename changed keys: %v -> %v", before, store.Keys())
	}
	if store.Calls(secrets.OpGet) != 0 {
		t.Fatal("an unchanged secret was read back")
	}
	if !renamed.Identity.HasPassword || !renamed.Identity.HasKey || !renamed.Identity.HasPassphrase {
		t.Fatalf("rename lost flags: %+v", renamed.Identity)
	}
	// And after a restart.
	l, err := New(Config{Dir: dir, Terminal: stillTerm{}}).Load(ctx)
	if err != nil || len(l.Identities) != 1 || l.Identities[0].ID != id || l.Identities[0].Label != "operations" {
		t.Fatalf("after restart: %+v, %v", l.Identities, err)
	}
}

func TestFR_IDENT_001_PartialSetFailureRollsBack(t *testing.T) {
	store := secrets.NewMemStore()
	a, dir := storeApp(t, store)
	ctx := context.Background()
	if _, err := a.SaveIdentity(ctx, IdentityInput{Name: "first"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(paths.ConfigFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	// The password is stored, then the key fails.
	store.FailNext(secrets.OpSet, nil)
	store.FailNext(secrets.OpSet, secrets.ErrDenied)
	_, err = a.SaveIdentity(ctx, IdentityInput{Name: "ops", Password: secretOf("p"), Key: keySecret(t), Passphrase: secretOf("q")})
	if se, ok := errors.AsType[*StoreError](err); !ok || !errors.Is(err, secrets.ErrDenied) || se.Error() != "Access to "+StoreName()+" was denied" {
		t.Fatalf("err = %v, want a denied StoreError", err)
	}
	if keys := store.Keys(); len(keys) != 0 {
		t.Fatalf("items left after the rollback: %v", keys)
	}
	after, err := os.ReadFile(paths.ConfigFile(dir))
	if err != nil || !bytes.Equal(after, cfg) {
		t.Fatalf("config.json changed: %v", err)
	}
	list, _ := a.Identities(ctx)
	if len(list) != 1 {
		t.Fatalf("identities = %+v", list)
	}
}

func TestFR_IDENT_001_PartialSetFailureRollsBackOnConfigError(t *testing.T) {
	store := secrets.NewMemStore()
	a, dir := storeApp(t, store)
	// A directory where config.json goes makes the write fail.
	if err := os.Mkdir(paths.ConfigFile(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := a.SaveIdentity(context.Background(), IdentityInput{Name: "ops", Password: secretOf("p")})
	if err == nil {
		t.Fatal("saved")
	}
	if keys := store.Keys(); len(keys) != 0 {
		t.Fatalf("items left after the rollback: %v", keys)
	}
}

func TestFR_IDENT_004_StatusFromFlagsAndMissing(t *testing.T) {
	srv := sshtest.New(t, sshtest.WithPassword("deploy", testPassword))
	store := newRecStore() // empty: the password is "not set on this device"
	a := credApp(t, store, []*sshtest.Server{srv},
		`[{"id": "i1", "name": "ops", "has_password": true, "has_key_passphrase": true}]`, oneHost)
	ctx := context.Background()
	list, _ := a.Identities(ctx)
	if p := list[0]; p.Password != SecretSet || p.Key != SecretNotSet || p.Passphrase != SecretSet || p.LinkedHosts != 1 {
		t.Fatalf("before connect: %+v", p)
	}
	cancelPrompts(a)
	if s, err := a.Connect(ctx, "h1"); err == nil {
		_ = s.Close()
	}
	list, _ = a.Identities(ctx)
	if list[0].Password != SecretNotOnDevice || list[0].Password.String() != "not set on this device" {
		t.Fatalf("after connect: %+v", list[0])
	}
	// Typing it again stores it.
	if _, err := a.SaveIdentity(ctx, IdentityInput{ID: "i1", Name: "ops", Password: secretOf(testPassword)}); err != nil {
		t.Fatal(err)
	}
	list, _ = a.Identities(ctx)
	if list[0].Password != SecretSet {
		t.Fatalf("after save: %+v", list[0])
	}
}
