package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/store"
)

// fakeClock is the App's clock in these tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

var lastUsedEpoch = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

// withClock returns a sliceFixture App whose clock is c, loaded.
func withClock(t *testing.T, authorize bool, c *fakeClock) *App {
	t.Helper()
	a, _ := sliceFixture(t, authorize)
	a.cfg.Now = c.now
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a
}

// savedLastUse reads config.json from disk and returns the last
// connection of the host with id.
func savedLastUse(t *testing.T, a *App, id domain.ID) *time.Time {
	t.Helper()
	s, err := store.Load(paths.ConfigFile(a.cfg.Dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range s.Data.Hosts {
		if h.ID == id {
			return h.LastConnectedAt
		}
	}
	t.Fatalf("host %s not in config.json", id)
	return nil
}

func connectAndClose(t *testing.T, a *App, id domain.ID) {
	t.Helper()
	s, err := a.Connect(context.Background(), id, Size{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
}

// TestFR_NAV_018_RecordedOnEstablished: an established session stamps
// the host with the clock's time, in UTC to the second, and writes
// config.json; connecting again (a reconnect) stamps it again; Load
// returns the hosts in last-use order.
func TestFR_NAV_018_RecordedOnEstablished(t *testing.T) {
	c := &fakeClock{t: lastUsedEpoch.Add(123 * time.Millisecond).In(time.FixedZone("CET", 3600))}
	a := withClock(t, true, c)
	connectAndClose(t, a, "h1")
	if got := savedLastUse(t, a, "h1"); got == nil || !got.Equal(lastUsedEpoch) {
		t.Fatalf("saved last use = %v, want %v", got, lastUsedEpoch)
	}
	if got := savedLastUse(t, a, "h2"); got != nil {
		t.Fatalf("the other host was stamped: %v", got)
	}

	c.t = lastUsedEpoch.Add(2 * time.Hour)
	connectAndClose(t, a, "h1")
	if got := savedLastUse(t, a, "h1"); got == nil || !got.Equal(c.t) {
		t.Fatalf("after reconnecting, saved last use = %v, want %v", got, c.t)
	}

	// After a restart the connected host comes first, whatever its label.
	l, err := New(Config{Dir: a.cfg.Dir}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Hosts) != 2 || l.Hosts[0].ID != "h1" || l.Hosts[0].LastConnectedAt == nil {
		t.Fatalf("hosts after restart = %+v", l.Hosts)
	}
}

// TestFR_NAV_018_NotRecordedOnFailure: a refused dial, an authentication
// failure and a cancelled TOFU prompt write nothing and stamp nothing.
func TestFR_NAV_018_NotRecordedOnFailure(t *testing.T) {
	c := &fakeClock{t: lastUsedEpoch}
	unchanged := func(t *testing.T, a *App, id domain.ID, before []byte) {
		t.Helper()
		after, err := os.ReadFile(paths.ConfigFile(a.cfg.Dir))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Fatalf("config.json changed:\n%s", after)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, h := range a.data.Hosts {
			if h.ID == id && h.LastConnectedAt != nil {
				t.Fatalf("host stamped in memory: %v", h.LastConnectedAt)
			}
		}
	}

	t.Run("refused", func(t *testing.T) {
		a := withClock(t, true, c)
		before, _ := os.ReadFile(paths.ConfigFile(a.cfg.Dir))
		if _, err := a.Connect(context.Background(), "h2", Size{}); err == nil { // zeta: 127.0.0.1:1
			t.Fatal("Connect to port 1 succeeded")
		}
		unchanged(t, a, "h2", before)
	})
	t.Run("auth failure", func(t *testing.T) {
		a := withClock(t, false, c)
		before, _ := os.ReadFile(paths.ConfigFile(a.cfg.Dir))
		if _, err := a.Connect(context.Background(), "h1", Size{}); err == nil {
			t.Fatal("Connect with an unauthorised key succeeded")
		}
		unchanged(t, a, "h1", before)
	})
	t.Run("TOFU cancel", func(t *testing.T) {
		f := newTrustFixture(t)
		a := f.app()
		a.cfg.Now = c.now
		before, _ := os.ReadFile(paths.ConfigFile(f.dir))
		if err := f.connectAnswering(a, false); err == nil {
			t.Fatal("Connect after a cancelled TOFU prompt succeeded")
		}
		unchanged(t, a, "h1", before)
	})
}

// TestFR_NAV_018_SaveFailureKeepsSession: when config.json cannot be
// written, the session still opens and the time is kept in memory, so the
// next save writes it.
func TestFR_NAV_018_SaveFailureKeepsSession(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	c := &fakeClock{t: lastUsedEpoch}
	a := withClock(t, true, c)
	if err := os.Chmod(a.cfg.Dir, 0o500); err != nil { //nolint:gosec // makes the save fail
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(a.cfg.Dir, 0o700) }) //nolint:gosec // restores the test dir
	s, err := a.Connect(context.Background(), "h1", Size{})
	if err != nil {
		t.Fatalf("Connect failed when config.json could not be written: %v", err)
	}
	_ = s.Close()
	if got := savedLastUse(t, a, "h1"); got != nil {
		t.Fatalf("config.json was written: %v", got)
	}
	if err := os.Chmod(a.cfg.Dir, 0o700); err != nil { //nolint:gosec // the config dir mode
		t.Fatal(err)
	}
	h := a.data.Hosts[0]
	if h.ID != "h2" {
		h = a.data.Hosts[1]
	}
	if _, err := a.SaveHost(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if got := savedLastUse(t, a, "h1"); got == nil || !got.Equal(lastUsedEpoch) {
		t.Fatalf("the next save wrote %v, want %v", got, lastUsedEpoch)
	}
}

// TestFR_NAV_018_EditKeepsTime: saving a host from the form, which does
// not carry the time, keeps it.
func TestFR_NAV_018_EditKeepsTime(t *testing.T) {
	c := &fakeClock{t: lastUsedEpoch}
	a := withClock(t, true, c)
	connectAndClose(t, a, "h1")
	saved, err := a.SaveHost(context.Background(), domain.Host{Meta: domain.Meta{ID: "h1"}, Label: "web-prod-2", Hostname: "127.0.0.1", Port: 2})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Host.LastConnectedAt == nil || !saved.Host.LastConnectedAt.Equal(lastUsedEpoch) {
		t.Fatalf("saved host = %v", saved.Host.LastConnectedAt)
	}
	if got := savedLastUse(t, a, "h1"); got == nil || !got.Equal(lastUsedEpoch) {
		t.Fatalf("config.json has %v", got)
	}
	if saved.Hosts[0].ID != "h1" {
		t.Fatalf("SavedHost.Hosts not in last-use order: %+v", saved.Hosts)
	}
}

// TestFR_NAV_018_ImportHasNoTime: hosts imported from ~/.ssh/config have
// never been connected.
func TestFR_NAV_018_ImportHasNoTime(t *testing.T) {
	a, _, _ := importApp(t, importFixture)
	rep, err := a.ImportSSHConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.AllHosts) == 0 {
		t.Fatal("nothing imported")
	}
	for _, h := range rep.AllHosts {
		if h.LastConnectedAt != nil {
			t.Errorf("imported %s has a last use %v", h.Label, h.LastConnectedAt)
		}
	}
	b, err := os.ReadFile(filepath.Join(a.cfg.Dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "last_connected_at") {
		t.Fatalf("config.json after import:\n%s", b)
	}
}
