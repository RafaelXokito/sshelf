package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

func TestLoadMissingIsFirstRun(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil || !s.FirstRun || len(s.Data.Hosts) != 0 {
		t.Fatalf("Load(missing) = %+v, %v", s, err)
	}
}

func TestLoadSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	const cfg = `{
  "version": 1,
  "settings": {"theme": "auto"},
  "groups": [],
  "identities": [{"id": "i1", "name": "ops", "username": "deploy", "key_path": "~/.ssh/id_ed25519",
                  "has_password": true, "has_private_key": false, "has_key_passphrase": true, "future": 1}],
  "hosts": [{"id": "h1", "label": "db 1", "hostname": "db1.example.com", "port": 2222, "user": "",
             "identity_id": "i1", "group_id": "", "tags": ["pg"], "jump_host_ids": [], "forward_agent": false}]
}`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path) //nolint:gosec // temp file
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.FirstRun || s.Data.Version != 1 || len(s.Data.Hosts) != 1 {
		t.Fatalf("snapshot = %+v", s)
	}
	wantID := domain.Identity{Meta: domain.Meta{ID: "i1"}, Label: "ops", Username: "deploy",
		KeyPath: "~/.ssh/id_ed25519", HasPassword: true, HasPassphrase: true}
	if len(s.Data.Identities) != 1 || s.Data.Identities[0] != wantID {
		t.Fatalf("identities = %+v", s.Data.Identities)
	}
	h := s.Data.Hosts
	if len(h) != 1 || h[0].ID != "h1" || h[0].Label != "db 1" || h[0].Hostname != "db1.example.com" ||
		h[0].Port != 2222 || h[0].IdentityID != "i1" || len(h[0].Tags) != 1 {
		t.Fatalf("hosts = %+v", h)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) { //nolint:gosec // temp file
		t.Fatal("Load changed the file")
	}
}

func TestLoadSyntaxError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"hosts": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("no error for a broken file")
	}
}
