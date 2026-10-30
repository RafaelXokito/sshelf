package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// emptyApp is a loaded App over an empty temp config dir.
func emptyApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sshelf")
	a := New(Config{Dir: dir})
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, dir
}

func TestFR_HOST_001_SurvivesRestart(t *testing.T) {
	a, dir := emptyApp(t)
	saved, err := a.SaveHost(context.Background(), domain.Host{Label: " Web Prod EU ", Hostname: "2001:db8::1", Port: 2222, User: "ubuntu"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Host.Label != "Web Prod EU" || len(saved.Hosts) != 1 || saved.Hosts[0].ID != saved.Host.ID {
		t.Fatalf("saved = %+v", saved)
	}
	fi, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("config.json mode %v", fi.Mode().Perm())
	}

	restarted := New(Config{Dir: dir})
	l, err := restarted.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := saved.Host
	if len(l.Hosts) != 1 {
		t.Fatalf("hosts after restart = %+v", l.Hosts)
	}
	got := l.Hosts[0]
	if got.ID != want.ID || got.Label != want.Label || got.Hostname != want.Hostname || got.Port != 2222 || got.User != "ubuntu" {
		t.Fatalf("after restart %+v, want %+v", got, want)
	}
}

func TestARCH_DATA_001_NewHostHasUUIDv7(t *testing.T) {
	a, _ := emptyApp(t)
	var ids []domain.ID
	for _, label := range []string{"a", "b"} {
		saved, err := a.SaveHost(context.Background(), domain.Host{Label: label, Hostname: "h"})
		if err != nil {
			t.Fatal(err)
		}
		u, err := uuid.Parse(string(saved.Host.ID))
		if err != nil || u.Version() != 7 || u.String() != string(saved.Host.ID) {
			t.Fatalf("id %q: version %v, %v", saved.Host.ID, u.Version(), err)
		}
		ids = append(ids, saved.Host.ID)
	}
	if ids[0] == ids[1] {
		t.Fatal("two hosts share an ID")
	}
	// Editing keeps the ID.
	saved, err := a.SaveHost(context.Background(), domain.Host{Meta: domain.Meta{ID: ids[0]}, Label: "a2", Hostname: "h"})
	if err != nil || saved.Host.ID != ids[0] {
		t.Fatalf("edit: %+v, %v", saved.Host, err)
	}
}

func TestFR_HOST_001_EditKeepsOtherFields(t *testing.T) {
	a, _ := sliceFixture(t, true)
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.data.Hosts[1].Tags = []string{"prod"}
	a.mu.Unlock()
	saved, err := a.SaveHost(context.Background(), domain.Host{Meta: domain.Meta{ID: "h1"}, Label: "web", Hostname: "127.0.0.1", Port: 2222})
	if err != nil {
		t.Fatal(err)
	}
	h := saved.Host
	if h.Label != "web" || h.Port != 2222 || h.User != "" || h.IdentityID != "" || !slices.Equal(h.Tags, []string{"prod"}) {
		t.Fatalf("edited host %+v", h)
	}
	if _, err := a.SaveHost(context.Background(), domain.Host{Meta: domain.Meta{ID: "gone"}, Label: "x", Hostname: "h"}); !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestFR_HOST_003_InvalidHostNotSaved(t *testing.T) {
	a, _ := sliceFixture(t, true)
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(a.cfg.Dir, "config.json")
	before, err := os.ReadFile(file) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []domain.Host{
		{Label: "Web-Prod", Hostname: "h"},
		{Label: "x", Hostname: "my host"},
		{Label: "x", Hostname: "h", Port: 65536},
		{Label: "x", Hostname: "h", IdentityID: "gone"},
		{Meta: domain.Meta{ID: "h2"}, Label: "WEB-PROD", Hostname: "h"},
	} {
		_, err := a.SaveHost(context.Background(), h)
		if _, ok := errors.AsType[*domain.ValidationError](err); !ok {
			t.Errorf("SaveHost(%+v) = %v, want a validation error", h, err)
		}
	}
	after, err := os.ReadFile(file) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("config.json changed after rejected saves")
	}
	if n := len(a.data.Hosts); n != 2 {
		t.Fatalf("%d hosts in memory", n)
	}
}

func TestFR_HOST_001_FailedWriteKeepsMemory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	a, dir := emptyApp(t)
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // test temp dir
	if _, err := a.SaveHost(context.Background(), domain.Host{Label: "x", Hostname: "h"}); err == nil {
		t.Fatal("save into a read-only dir succeeded")
	}
	if n := len(a.data.Hosts); n != 0 {
		t.Fatalf("%d hosts in memory after a failed save", n)
	}
}

func TestFR_HOST_001_PortChangeUsedOnConnect(t *testing.T) {
	a, srv := sliceFixture(t, true)
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	// zeta points at 127.0.0.1:1; give it the test server's port.
	if _, err := a.SaveHost(context.Background(), domain.Host{
		Meta: domain.Meta{ID: "h2"}, Label: "zeta", Hostname: srv.Host(), Port: srv.Port(), User: "demo", IdentityID: "i1",
	}); err != nil {
		t.Fatal(err)
	}
	s, err := a.Connect(context.Background(), "h2", Size{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	restarted := New(Config{Dir: a.cfg.Dir})
	l, err := restarted.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(l.Hosts, func(h domain.Host) bool { return h.ID == "h2" }); i < 0 || l.Hosts[i].Port != srv.Port() {
		t.Fatalf("hosts after restart %+v", l.Hosts)
	}
	if len(l.Identities) != 1 || l.Identities[0].ID != "i1" {
		t.Fatalf("identities %+v", l.Identities)
	}
}
