package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/welcome"
)

func (d *driver) onWelcome() bool {
	_, ok := d.root().screen.(welcome.Model)
	return ok
}

func TestFR_NAV_013_FirstLaunchShowsWelcome(t *testing.T) {
	d := newDriver(t, &fakeService{})
	if !d.onWelcome() {
		t.Fatalf("no Welcome screen without hosts:\n%s", d.view())
	}
	v := d.view()
	for _, want := range []string{"Import from ~/.ssh/config", "Add host", "I import ~/.ssh/config • a add host • q quit"} {
		if !strings.Contains(plain(v), want) {
			t.Errorf("Welcome lacks %q:\n%s", want, plain(v))
		}
	}
	golden(t, "welcome", d.m)
}

func TestFR_NAV_013_Keys(t *testing.T) {
	// I starts the import.
	if _, cmd := welcome.New().Update(tea.KeyPressMsg{Code: 'I', Text: "I"}); cmd == nil {
		t.Fatal("I did nothing")
	} else if _, ok := cmd().(welcome.ImportMsg); !ok {
		t.Fatal("I did not ask for an import")
	}

	// a opens the host form; esc goes back to Welcome.
	svc := &fakeService{session: &stubSession{}}
	d := newDriver(t, svc)
	d.press('a', "a")
	if !d.inForm() {
		t.Fatalf("a did not open the form:\n%s", d.view())
	}
	d.esc()
	if !d.onWelcome() {
		t.Fatalf("esc did not return to Welcome:\n%s", d.view())
	}
	// After the first host is saved, the host list shows.
	d.press('a', "a")
	d.typeText("first")
	d.tab()
	d.typeText("10.0.0.1")
	d.save()
	if h := selected(t, d); h.Label != "first" {
		t.Fatalf("selected %+v", h)
	}
	// q quits from Welcome.
	d2 := newDriver(t, &fakeService{})
	d2.press('q', "q")
	if !d2.quit {
		t.Fatal("q did not quit from Welcome")
	}
}

func TestFR_NAV_013_LastHostDeletedShowsWelcome(t *testing.T) {
	svc := &fakeService{hosts: slices.Clone(testHosts[:1])}
	d := newDriver(t, svc)
	d.press('d', "d")
	d.press('y', "y")
	if len(svc.hosts) != 0 || !d.onWelcome() {
		t.Fatalf("hosts %d, view:\n%s", len(svc.hosts), d.view())
	}
	if v := d.view(); !strings.Contains(v, "db-staging: deleted") {
		t.Fatalf("no deleted status:\n%s", v)
	}
	// And back to the list once a host exists again.
	d.press('a', "a")
	d.typeText("again")
	d.tab()
	d.typeText("h")
	d.save()
	if l := d.list(); !slices.EqualFunc(l.Hosts(), []domain.Host{{Label: "again"}}, func(a, b domain.Host) bool { return a.Label == b.Label }) {
		t.Fatalf("hosts %+v", l.Hosts())
	}
}
