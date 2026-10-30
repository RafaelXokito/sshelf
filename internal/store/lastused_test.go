package store

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// TestFR_NAV_018_StoreRoundTrip saves a host's last connection and loads
// it again: RFC 3339 in UTC with whole seconds, and the version stays 1.
func TestFR_NAV_018_StoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	when := time.Date(2026, 10, 30, 10, 15, 42, 987_000_000, time.FixedZone("CET", 3600))
	d := domain.Data{Hosts: []domain.Host{
		{Meta: domain.Meta{ID: "h1"}, Label: "web", Hostname: "w", LastConnectedAt: &when},
		{Meta: domain.Meta{ID: "h2"}, Label: "never", Hostname: "n"},
	}}
	if err := Save(path, &Snapshot{Data: d}); err != nil {
		t.Fatal(err)
	}
	b := readFile(t, path)
	if !bytes.Contains(b, []byte(`"last_connected_at": "2026-10-30T09:15:42Z"`)) || !bytes.Contains(b, []byte(`"version": 1`)) {
		t.Fatalf("saved:\n%s", b)
	}
	if n := bytes.Count(b, []byte("last_connected_at")); n != 1 {
		t.Fatalf("last_connected_at written %d times; the never-connected host has none:\n%s", n, b)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Data.Hosts
	want := time.Date(2026, 10, 30, 9, 15, 42, 0, time.UTC)
	if got[0].LastConnectedAt == nil || !got[0].LastConnectedAt.Equal(want) || got[0].LastConnectedAt.Location() != time.UTC {
		t.Fatalf("loaded %v, want %v", got[0].LastConnectedAt, want)
	}
	if got[1].LastConnectedAt != nil || len(s.Warnings) != 0 {
		t.Fatalf("never-connected host = %v, warnings %q", got[1].LastConnectedAt, s.Warnings)
	}
	// Saving what was loaded writes the same bytes.
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if b2 := readFile(t, path); !bytes.Equal(b, b2) {
		t.Fatalf("second save differs:\n%s\n%s", b, b2)
	}
}

// TestFR_NAV_018_OldFileLoads loads a config.json written before the
// field existed: every host is never connected, and saving it adds
// nothing.
func TestFR_NAV_018_OldFileLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	const old = `{"version": 1, "settings": {}, "groups": [], "identities": [],
  "hosts": [{"id": "h1", "label": "db", "hostname": "db.example.com", "port": 22, "user": "",
             "identity_id": "", "group_id": "", "tags": [], "jump_host_ids": [], "forward_agent": false}]}`
	writeTestFile(t, path, old)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Data.Hosts) != 1 || s.Data.Hosts[0].LastConnectedAt != nil || len(s.Warnings) != 0 {
		t.Fatalf("snapshot = %+v", s)
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if b := readFile(t, path); bytes.Contains(b, []byte("last_connected_at")) {
		t.Fatalf("saved an old file with the field:\n%s", b)
	}
}

// TestFR_NAV_018_InvalidTimeWarns: a value that is not an RFC 3339 time
// is ignored with a one-line warning; the file still loads.
func TestFR_NAV_018_InvalidTimeWarns(t *testing.T) {
	for _, v := range []string{"yesterday", "2026-10-30", "2026-10-30 09:00:00", "0000-01-01T00:30:00+01:00"} {
		path := filepath.Join(t.TempDir(), "config.json")
		writeTestFile(t, path, `{"version": 1, "hosts": [{"id": "h1", "label": "db", "hostname": "d", "last_connected_at": "`+v+`"}]}`)
		s, err := Load(path)
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		if s.Data.Hosts[0].LastConnectedAt != nil {
			t.Errorf("%q: loaded as %v", v, s.Data.Hosts[0].LastConnectedAt)
		}
		if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], `host "db": last_connected_at`) || strings.Contains(s.Warnings[0], "\n") {
			t.Errorf("%q: warnings = %q", v, s.Warnings)
		}
	}
}
