package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// A config.json as a newer sshelf might write it: the same schema
// version with fields this build does not know, everywhere.
const newerFile = `{
  "version": 1,
  "settings": {"theme": "dark", "font": "mono"},
  "groups": [{"id": "g1", "name": "G", "parent_id": null, "colour": "red"}],
  "identities": [
    {"id": "i1", "name": "I", "username": "", "key_path": "", "has_password": true,
     "has_private_key": false, "has_key_passphrase": false, "use_agent": false, "fido": {"slot": 2}},
    {"id": "i2", "name": "J", "username": "", "key_path": "", "has_password": false,
     "has_private_key": false, "has_key_passphrase": false, "use_agent": false}
  ],
  "hosts": [
    {"id": "h1", "label": "a", "hostname": "a.example", "port": 22, "user": "", "identity_id": "i1",
     "group_id": "g1", "tags": [], "jump_host_ids": [], "forward_agent": false,
     "notes": "rack 4 <b>", "favourite": true},
    {"id": "h2", "label": "b", "hostname": "b.example", "port": 22, "user": "", "identity_id": "",
     "group_id": "", "tags": [], "jump_host_ids": [], "forward_agent": false, "keepalive": 15}
  ],
  "snippets": [{"id": "s1", "body": "uptime"}]
}`

func TestFR_CFG_011_UnknownFieldsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, path, newerFile)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Edit as the app does: a fresh snapshot with changed data; drop h2.
	d := s.Data
	d.Hosts[0].Label = "renamed"
	d.Hosts = d.Hosts[:1]
	d.Hosts = append(d.Hosts, domain.Host{Meta: domain.Meta{ID: "h3"}, Label: "new", Hostname: "c.example", Port: 22})
	if err := Save(path, &Snapshot{Data: d}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(readFile(t, path), &got); err != nil {
		t.Fatal(err)
	}
	hosts := got["hosts"].([]any)
	h1 := hosts[0].(map[string]any)
	if h1["label"] != "renamed" || h1["notes"] != "rack 4 <b>" || h1["favourite"] != true {
		t.Errorf("h1 = %v; want the edit and its unknown fields", h1)
	}
	if h3 := hosts[1].(map[string]any); h3["keepalive"] != nil || h3["id"] != "h3" {
		t.Errorf("h3 = %v; a deleted host's fields must not move to another", h3)
	}
	for _, c := range []struct {
		name string
		got  any
		want any
	}{
		{"snippets", got["snippets"], []any{map[string]any{"id": "s1", "body": "uptime"}}},
		{"settings.font", got["settings"].(map[string]any)["font"], "mono"},
		{"groups[0].colour", got["groups"].([]any)[0].(map[string]any)["colour"], "red"},
		{"identities[0].fido", got["identities"].([]any)[0].(map[string]any)["fido"], map[string]any{"slot": float64(2)}},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	// The file still loads, with the edit.
	s2, err := Load(path)
	if err != nil || s2.Data.Hosts[0].Label != "renamed" || len(s2.Data.Hosts) != 2 {
		t.Fatalf("reload: %v, %+v", err, s2)
	}
}

func TestFR_CFG_011_NoExtrasSameBytes(t *testing.T) {
	// A file this build wrote is written back byte for byte.
	golden := readFile(t, filepath.Join("testdata", "config_v1.json"))
	ex := parseExtras(golden)
	if ex.top != nil || ex.settings != nil || len(ex.groups)+len(ex.identities)+len(ex.hosts) != 0 {
		t.Fatalf("extras in a file this build wrote: %+v", ex)
	}
}
