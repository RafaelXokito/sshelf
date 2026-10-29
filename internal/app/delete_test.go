package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const (
	sharedIdentityID = "0192a000-0000-7000-8000-000000000001"
	webHostID        = "0192a000-0000-7000-8000-000000000002"
	dbHostID         = "0192a000-0000-7000-8000-000000000003"
)

// appWithConfig is a loaded App over a temp config dir holding cfg.
func appWithConfig(t *testing.T, cfg string) (*App, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sshelf")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Config{Dir: dir, Terminal: stillTerm{}})
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, dir
}

const sharedIdentityConfig = `{"version": 1,
  "identities": [{"id": "` + sharedIdentityID + `", "name": "deploy", "key_path": "~/.ssh/id_ed25519"}],
  "hosts": [
    {"id": "` + webHostID + `", "label": "web", "hostname": "10.0.0.10", "identity_id": "` + sharedIdentityID + `"},
    {"id": "` + dbHostID + `", "label": "db", "hostname": "10.0.0.20", "identity_id": "` + sharedIdentityID + `"}]}`

func TestFR_HOST_002_SharedIdentityKept(t *testing.T) {
	a, dir := appWithConfig(t, sharedIdentityConfig)
	rest, err := a.DeleteHost(context.Background(), webHostID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Hosts) != 1 || rest.Hosts[0].ID != dbHostID {
		t.Fatalf("remaining hosts %+v", rest)
	}
	// The last host using the identity goes too; the identity stays.
	if rest, err = a.DeleteHost(context.Background(), dbHostID); err != nil || len(rest.Hosts) != 0 {
		t.Fatalf("second delete: %+v, %v", rest, err)
	}

	l, err := New(Config{Dir: dir, Terminal: stillTerm{}}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Hosts) != 0 {
		t.Fatalf("hosts after restart %+v", l.Hosts)
	}
	if len(l.Identities) != 1 || l.Identities[0].ID != sharedIdentityID || l.Identities[0].KeyPath != "~/.ssh/id_ed25519" {
		t.Fatalf("identities after restart %+v", l.Identities)
	}
}

func TestFR_HOST_002_DeleteUnknownHost(t *testing.T) {
	a, _ := appWithConfig(t, sharedIdentityConfig)
	if _, err := a.DeleteHost(context.Background(), "nope"); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestFR_HOST_002_DeleteWriteFailureKeepsHost(t *testing.T) {
	a, dir := appWithConfig(t, sharedIdentityConfig)
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // a directory needs its x bit
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // a directory needs its x bit
	if _, err := a.DeleteHost(context.Background(), webHostID); err == nil {
		t.Skip("the directory is writable anyway (running as root?)")
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // a directory needs its x bit
		t.Fatal(err)
	}
	rest, err := a.DeleteHost(context.Background(), dbHostID)
	if err != nil || len(rest.Hosts) != 1 || rest.Hosts[0].ID != webHostID {
		t.Fatalf("the failed delete changed the in-memory config: %+v, %v", rest, err)
	}
}
