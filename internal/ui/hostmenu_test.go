package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/hostmenu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// pressAt sends a press of button b at column x, row y.
func (d *driver) pressAt(x, y int, b tea.MouseButton) {
	d.send(tea.MouseClickMsg{X: x, Y: y, Button: b})
}

// hostMenu returns the open host menu and where it is drawn.
func hostMenu(d *driver) (hostmenu.Model, region, bool) {
	m := d.root()
	n := len(m.modals)
	if n == 0 {
		return hostmenu.Model{}, region{}, false
	}
	hm, ok := m.modals[n-1].(hostmenu.Model)
	if !ok {
		return hostmenu.Model{}, region{}, false
	}
	return hm, m.layout().modals[n-1], true
}

// openMenuOn right-clicks the row of label, three columns into it.
func openMenuOn(t *testing.T, d *driver, label string) (x, y int) {
	t.Helper()
	y, x = rowOf(t, d, label)
	x += 3
	d.pressAt(x, y, tea.MouseRight)
	if _, _, ok := hostMenu(d); !ok {
		t.Fatalf("no host menu after a right-click on %s:\n%s", label, plain(d.view()))
	}
	return x, y
}

func TestFR_NAV_019_RightClickOpensAtPointer(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	x, y := openMenuOn(t, d, "web-prod")
	if got := selected(t, d); got.ID != "h2" {
		t.Fatalf("selected %q, want web-prod", got.Label)
	}
	hm, r, _ := hostMenu(d)
	if hm.Host().ID != "h2" {
		t.Fatalf("menu of %q", hm.Host().Label)
	}
	if r.X != x || r.Y != y {
		t.Fatalf("menu at %d,%d, want the pointer %d,%d", r.X, r.Y, x, y)
	}
	if row, col := textAt(t, d.view(), "╭"); row != y || col != x {
		t.Fatalf("menu drawn at %d,%d, want %d,%d:\n%s", col, row, x, y, plain(d.view()))
	}
	for _, it := range hostmenu.Items {
		if !strings.Contains(plain(d.view()), it.Label) {
			t.Errorf("menu lacks %q", it.Label)
		}
	}

	// Clamped at the right edge.
	d.esc()
	d.pressAt(79, y, tea.MouseRight)
	_, r, ok := hostMenu(d)
	if !ok || r.X+r.W != 80 || r.Y != y {
		t.Fatalf("right edge: menu %+v, open %v", r, ok)
	}

	// Clamped at the bottom: above the status line.
	d2 := newDriver(t, &fakeService{hosts: manyHosts(40)})
	d2.resize(80, 24)
	last := hostsFrameTop + d2.root().bodyHeight() - 1
	d2.pressAt(5, last, tea.MouseRight)
	_, r, ok = hostMenu(d2)
	if !ok || r.Y+r.H != last+1 || r.X != 5 {
		t.Fatalf("bottom edge: menu %+v, open %v", r, ok)
	}
	if got := selected(t, d2); got.Label != "host-19" {
		t.Fatalf("bottom edge: selected %q", got.Label)
	}
	lines := strings.Split(plain(d2.view()), "\n")
	if !strings.Contains(lines[r.Y+r.H-1], "╰") || strings.Contains(lines[len(lines)-1], "╰") {
		t.Fatalf("bottom edge drawn wrong:\n%s", plain(d2.view()))
	}
}

func TestFR_NAV_019_RightClickElsewhere(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	for _, y := range []int{0, 1, 10, 22, 23} { // header, blank, blank row, status, footer
		d.pressAt(4, y, tea.MouseRight)
		if _, _, ok := hostMenu(d); ok {
			t.Fatalf("right-click on row %d opened the menu", y)
		}
	}
	if got := selected(t, d); got.ID != "h1" {
		t.Fatalf("selection moved to %q", got.Label)
	}
	// The search line, with a query kept.
	d.press('/', "/")
	d.press('e', "e")
	y, x := rowOf(t, d, "web-prod")
	d.clickAt(x, y)
	sy, _ := rowOf(t, d, "/ e")
	d.pressAt(4, sy, tea.MouseRight)
	if _, _, ok := hostMenu(d); ok {
		t.Fatal("right-click on the search line opened the menu")
	}
	// A right-click on a row while searching selects it, back on the list,
	// keeping the query.
	d.press('/', "/")
	openMenuOn(t, d, "zeta")
	if l := d.list(); l.Searching() || l.Query() != "e" || selected(t, d).ID != "h3" {
		t.Fatalf("searching %v, query %q, selected %q", l.Searching(), l.Query(), selected(t, d).Label)
	}
}

func TestFR_NAV_019_ItemsAct(t *testing.T) {
	checks := []struct {
		item  string
		check func(t *testing.T, d *driver)
	}{
		{"Connect (SSH)", func(t *testing.T, d *driver) {
			if got := d.svc.connections(); !slices.Equal(got, []domain.ID{"h3"}) {
				t.Fatalf("connected %v", got)
			}
		}},
		{"Open SFTP", func(t *testing.T, d *driver) {
			if d.root().focusedSFTP() == nil {
				t.Fatalf("no SFTP tab:\n%s", plain(d.view()))
			}
		}},
		{"Edit", func(t *testing.T, d *driver) {
			if !d.inForm() || !strings.Contains(plain(d.view()), "Edit host zeta") {
				t.Fatalf("no edit form:\n%s", plain(d.view()))
			}
		}},
		{"Duplicate", func(t *testing.T, d *driver) {
			if !d.inForm() || !strings.Contains(plain(d.view()), "Duplicate host zeta") {
				t.Fatalf("no duplicate form:\n%s", plain(d.view()))
			}
		}},
		{"Delete", func(t *testing.T, d *driver) {
			if !strings.Contains(plain(d.view()), "Delete host zeta?") {
				t.Fatalf("no delete confirmation:\n%s", plain(d.view()))
			}
		}},
	}
	for i, c := range checks {
		if hostmenu.Items[i].Label != c.item {
			t.Fatalf("item %d is %q, want %q", i, hostmenu.Items[i].Label, c.item)
		}
		for _, how := range []string{"enter", "click"} {
			t.Run(c.item+" by "+how, func(t *testing.T) {
				d, _ := sftpDriver(t, testHosts, []*apptest.FakeSession{apptest.NewFakeSession(shellScreen...)}, approvedBrowser(1))
				d.resize(80, 24)
				openMenuOn(t, d, "zeta")
				if how == "enter" {
					for range i {
						d.press('j', "j")
					}
					d.enter()
				} else {
					_, r, _ := hostMenu(d)
					d.clickAt(r.X+4, r.Y+1+i)
				}
				if _, _, ok := hostMenu(d); ok {
					t.Fatal("the menu stayed open")
				}
				c.check(t, d)
			})
		}
	}
}

func TestFR_NAV_019_KeyboardNavigation(t *testing.T) {
	svc := &fakeService{hosts: testHosts}
	d := newDriver(t, svc)
	d.resize(80, 24)
	openMenuOn(t, d, "zeta")
	cursor := func() int { hm, _, _ := hostMenu(d); return hm.Cursor() }
	if cursor() != 0 {
		t.Fatalf("opens on item %d", cursor())
	}
	d.press('k', "k")
	d.key(tea.KeyUp, 0)
	if cursor() != 0 {
		t.Fatalf("up from the first item: %d", cursor())
	}
	for range 7 {
		d.press('j', "j")
	}
	if cursor() != len(hostmenu.Items)-1 {
		t.Fatalf("down past the last item: %d", cursor())
	}
	d.key(tea.KeyUp, 0)
	d.press('k', "k")
	d.key(tea.KeyDown, 0)
	if cursor() != len(hostmenu.Items)-2 {
		t.Fatalf("up, k, down: %d", cursor())
	}
	if foot := plain(d.root().footer()); foot != "↑/k up • ↓/j down • enter choose • esc close" {
		t.Fatalf("footer %q", foot)
	}
	// The host-list keys do nothing while the menu is open.
	d.press('a', "a")
	if d.inForm() {
		t.Fatal("a opened the form under the menu")
	}
	d.esc()
	if _, _, ok := hostMenu(d); ok {
		t.Fatal("esc did not close the menu")
	}
	if d.inForm() || len(svc.connections()) != 0 || len(d.root().modals) != 0 || selected(t, d).ID != "h3" {
		t.Fatalf("esc did something:\n%s", plain(d.view()))
	}
}

func TestFR_NAV_019_ClickOutsideCloses(t *testing.T) {
	for _, b := range []tea.MouseButton{tea.MouseLeft, tea.MouseRight, tea.MouseMiddle} {
		t.Run(b.String(), func(t *testing.T) {
			svc := &fakeService{hosts: testHosts}
			d := newDriver(t, svc)
			d.resize(80, 24)
			openMenuOn(t, d, "zeta")
			// The border is the menu's: nothing happens.
			_, r, _ := hostMenu(d)
			d.clickAt(r.X, r.Y)
			d.clickAt(r.X+r.W-1, r.Y+2)
			if _, _, ok := hostMenu(d); !ok {
				t.Fatal("a click on the border closed the menu")
			}
			// A press on another host's row closes the menu and only that.
			y, x := rowOf(t, d, "db-staging")
			d.clock = d.clock.Add(time.Second)
			d.pressAt(x, y, b)
			if _, _, ok := hostMenu(d); ok {
				t.Fatal("a press outside did not close the menu")
			}
			if got := selected(t, d); got.ID != "h3" || len(d.root().modals) != 0 || len(svc.connections()) != 0 {
				t.Fatalf("the press went through: selected %q, modals %d", got.Label, len(d.root().modals))
			}
		})
	}
}

func TestFR_NAV_019_MenuGolden(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	y, _ := rowOf(t, d, "web-prod")
	d.pressAt(70, y, tea.MouseRight) // clamped at 80 columns, not at 120
	golden(t, "hostmenu", d.m)
}

func TestFR_NAV_020_FooterFits(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	if foot := plain(d.root().footer()); foot != "enter connect • s sftp • / search • a add • e edit • d delete • q quit • ? help" {
		t.Fatalf("80 columns: footer %q", foot)
	}
	d.resize(120, 40)
	if foot := plain(d.root().footer()); foot != "enter connect • s sftp • / search • a add • c duplicate • e edit • d delete • i identities • I import • q quit • ? help" {
		t.Fatalf("120 columns: footer %q", foot)
	}
	for w := 40; w <= 130; w++ {
		d.resize(w, 24)
		foot := plain(d.root().footer())
		if len([]rune(foot)) > w || !strings.HasSuffix(foot, "? help") || !strings.HasPrefix(foot, "enter connect") {
			t.Fatalf("%d columns: footer %q", w, foot)
		}
		lines := strings.Split(plain(d.view()), "\n")
		if last := lines[len(lines)-1]; strings.Contains(last, "…") {
			t.Fatalf("%d columns: footer cut: %q", w, last)
		}
	}
	// While searching, the search footer.
	d.resize(80, 24)
	d.press('/', "/")
	if foot := plain(d.root().footer()); strings.Contains(foot, "? help") {
		t.Fatalf("search footer %q", foot)
	}
}

func TestFR_NAV_020_HelpListsEveryKey(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	for _, closeKey := range []string{"?", "esc"} {
		d.press('?', "?")
		v := plain(d.view())
		if !strings.Contains(v, "Host list keys") {
			t.Fatalf("? opened no help:\n%s", v)
		}
		for _, b := range hostlist.HelpKeys() {
			h := b.Help()
			if !strings.Contains(v, h.Key) || !strings.Contains(v, h.Desc) {
				t.Errorf("help lacks %q %q", h.Key, h.Desc)
			}
		}
		if closeKey == "?" {
			d.press('?', "?")
		} else {
			d.esc()
		}
		if len(d.root().modals) != 0 {
			t.Fatalf("%s did not close the help", closeKey)
		}
	}
	for _, want := range []string{"c", "I", "i", "?", "s"} {
		if !slices.ContainsFunc(hostlist.HelpKeys(), func(b key.Binding) bool { return slices.Contains(b.Keys(), want) }) {
			t.Errorf("%s is not in the help", want)
		}
	}
	// While searching, ? types.
	d.press('/', "/")
	d.press('?', "?")
	if len(d.root().modals) != 0 || d.list().Query() != "?" {
		t.Fatalf("? while searching: modals %d, query %q", len(d.root().modals), d.list().Query())
	}
}
