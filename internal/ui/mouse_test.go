package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostedit"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// clickAt sends a left press at column x, row y of the terminal, one
// second after the previous event, so that it never completes a
// double-click by accident.
func (d *driver) clickAt(x, y int) {
	d.clock = d.clock.Add(time.Second)
	d.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// pressAfter sends a left press at column x, row y gap after the
// previous one.
func (d *driver) pressAfter(gap time.Duration, x, y int) {
	d.clock = d.clock.Add(gap)
	d.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// doubleClick sends two left presses 100 ms apart at column x, row y.
func (d *driver) doubleClick(x, y int) {
	d.clickAt(x, y)
	d.pressAfter(100*time.Millisecond, x, y)
}

// wheelAt sends one wheel notch at row y.
func (d *driver) wheelAt(y int, b tea.MouseButton) {
	d.send(tea.MouseWheelMsg{X: 4, Y: y, Button: b})
}

// rowOf returns the terminal row of the first line of the view that
// contains s, and the column it starts at.
func rowOf(t *testing.T, d *driver, s string) (y, x int) {
	t.Helper()
	return textAt(t, d.view(), s)
}

// textAt returns the row and column where s first appears in view.
// Every character sshelf draws is one column wide.
func textAt(t *testing.T, view, s string) (y, x int) {
	t.Helper()
	for i, l := range strings.Split(plain(view), "\n") {
		if j := strings.Index(l, s); j >= 0 {
			return i, len([]rune(l[:j]))
		}
	}
	t.Fatalf("%q is not in the view:\n%s", s, plain(view))
	return 0, 0
}

// connections returns the hosts Connect was called for.
func (f *fakeService) connections() []domain.ID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.connected)
}

// manyHosts returns n hosts labelled host-00 … host-(n-1).
func manyHosts(n int) []domain.Host {
	hosts := make([]domain.Host, n)
	for i := range hosts {
		hosts[i] = domain.Host{Meta: domain.Meta{ID: domain.ID(fmt.Sprintf("m%02d", i))}, Label: fmt.Sprintf("host-%02d", i), Hostname: "10.0.0.1"}
	}
	return hosts
}

func TestARCH_UI_006_MouseModePolicy(t *testing.T) {
	const (
		none = tea.MouseModeNone
		cell = tea.MouseModeCellMotion
		all  = tea.MouseModeAllMotion
	)
	for _, tc := range []struct {
		name string
		in   mouseState
		want tea.MouseMode
	}{
		{"hosts tab", mouseState{}, cell},
		{"hosts tab, modal", mouseState{modal: true}, cell},
		{"plain session", mouseState{session: true}, none},
		{"session asking for cell motion", mouseState{session: true, requested: cell}, cell},
		{"session asking for all motion", mouseState{session: true, requested: all}, all},
		{"session, modal", mouseState{session: true, modal: true, requested: all}, cell},
		{"plain session, modal", mouseState{session: true, modal: true}, cell},
		{"scrollback", mouseState{session: true, scrollback: true, requested: all}, none},
		{"ended", mouseState{session: true, ended: true, requested: cell}, none},
		{"ended, scrollback", mouseState{session: true, ended: true, scrollback: true}, none},
		{"ended, modal", mouseState{session: true, ended: true, modal: true}, cell},
	} {
		if got := mouseModeFor(tc.in); got != tc.want {
			t.Errorf("%s: mode %v, want %v", tc.name, got, tc.want)
		}
	}

	// Switching tabs changes the mode of the next View.
	d := framedDriver(t, testHosts, apptest.NewFakeSession(shellScreen...))
	d.resize(80, 24)
	if got := d.m.View().MouseMode; got != cell {
		t.Fatalf("Hosts tab: mode %v", got)
	}
	connectHost(t, d, "h2")
	if got := d.m.View().MouseMode; got != none {
		t.Fatalf("plain shell tab: mode %v", got)
	}
	d.focus(0)
	if got := d.m.View().MouseMode; got != cell {
		t.Fatalf("back on the Hosts tab: mode %v", got)
	}
}

func TestNFR_UX_001_MouseModePolicy(t *testing.T) {
	const (
		none = tea.MouseModeNone
		cell = tea.MouseModeCellMotion
	)
	mode := func(d *driver) tea.MouseMode { return d.m.View().MouseMode }

	// Every sshelf screen and modal: cell motion.
	w := newDriver(t, &fakeService{})
	w.resize(80, 24)
	if !w.onWelcome() || mode(w) != cell {
		t.Fatalf("Welcome: mode %v", mode(w))
	}
	fs := apptest.NewFakeSession(shellScreen...)
	d := framedDriver(t, testHosts, fs)
	d.resize(80, 24)
	if mode(d) != cell {
		t.Fatalf("host list: mode %v", mode(d))
	}
	d.press('a', "a")
	if !d.inForm() || mode(d) != cell {
		t.Fatalf("host form: mode %v", mode(d))
	}
	d.esc()
	d.press('d', "d")
	if len(d.root().modals) != 1 || mode(d) != cell {
		t.Fatalf("confirm modal: mode %v", mode(d))
	}
	d.esc()

	// A focused plain session, scrollback mode and an ended session:
	// none, so the terminal's own selection works (D-012).
	connectHost(t, d, "h2")
	if d.root().focused() == nil || mode(d) != none {
		t.Fatalf("plain session: mode %v", mode(d))
	}
	d.prefix("[")
	if mode(d) != none {
		t.Fatalf("scrollback: mode %v", mode(d))
	}
	d.esc()
	fs.End(nil)
	d.pump(1)
	if !d.root().focused().done || mode(d) != none {
		t.Fatalf("ended session: mode %v", mode(d))
	}
}

func TestARCH_UI_007_DoubleClickThreshold(t *testing.T) {
	var c components.ClickTracker
	at := testEpoch
	if c.Press(at, 5) {
		t.Fatal("a first press is a double-click")
	}
	if !c.Press(at.Add(399*time.Millisecond), 5) {
		t.Fatal("399 ms on the same row is not a double-click")
	}
	at = at.Add(time.Second)
	c.Press(at, 5)
	if c.Press(at.Add(401*time.Millisecond), 5) {
		t.Fatal("401 ms is a double-click")
	}
	at = at.Add(time.Second)
	c.Press(at, 5)
	if c.Press(at.Add(10*time.Millisecond), 6) {
		t.Fatal("a press on another row is a double-click")
	}
	// A third press after a double-click starts over.
	at = at.Add(time.Second)
	c.Press(at, 5)
	if !c.Press(at.Add(100*time.Millisecond), 5) {
		t.Fatal("no double-click")
	}
	if c.Press(at.Add(200*time.Millisecond), 5) {
		t.Fatal("a third press continued the double-click")
	}
	if !c.Press(at.Add(300*time.Millisecond), 5) {
		t.Fatal("the press after the third did not complete a new double-click")
	}

	// Through the root model, with its injected clock.
	svc := &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{apptest.NewFakeSession(shellScreen...)}}
	d := newDriver(t, svc)
	d.resize(80, 24)
	y, x := rowOf(t, d, "zeta")
	d.clickAt(x, y)
	d.pressAfter(401*time.Millisecond, x, y)
	if len(svc.connections()) != 0 {
		t.Fatal("presses 401 ms apart connected")
	}
	d.pressAfter(400*time.Millisecond, x, y)
	if got := svc.connections(); len(got) != 1 || got[0] != "h3" {
		t.Fatalf("presses 400 ms apart: connected %v, want [h3]", got)
	}
}

func TestARCH_UI_007_LayoutMatchesView(t *testing.T) {
	// Each label is clicked where the View draws it, at both sizes and
	// with the "no credential store" banner, which moves the list down.
	for _, tc := range []struct {
		name    string
		noStore bool
		w, h    int
	}{{"80x24", false, 80, 24}, {"120x40", false, 120, 40}, {"banner", true, 80, 24}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{hosts: testHosts}
			if tc.noStore {
				svc.storeErr = noSecretService
			}
			d := newDriver(t, svc)
			d.resize(tc.w, tc.h)
			for _, h := range []domain.Host{testHosts[2], testHosts[0], testHosts[1]} {
				y, x := rowOf(t, d, h.Label)
				d.clickAt(x, y)
				if got := selected(t, d); got.ID != h.ID {
					t.Fatalf("click on %q at row %d selected %q", h.Label, y, got.Label)
				}
			}
		})
	}

	// The region translation is the only offset: a cell is inside a
	// region exactly when its local coordinates are in range.
	r := region{X: 2, Y: 3, W: 10, H: 4}
	for _, tc := range []struct {
		x, y, lx, ly int
		ok           bool
	}{{2, 3, 0, 0, true}, {11, 6, 9, 3, true}, {1, 3, 0, 0, false}, {12, 3, 0, 0, false}, {2, 2, 0, 0, false}, {2, 7, 0, 0, false}} {
		lx, ly, ok := r.local(tc.x, tc.y)
		if ok != tc.ok || lx != tc.lx || ly != tc.ly {
			t.Errorf("local(%d, %d) = %d, %d, %v; want %d, %d, %v", tc.x, tc.y, lx, ly, ok, tc.lx, tc.ly, tc.ok)
		}
	}
}

func TestARCH_UI_007_ReleaseAndMotionIgnored(t *testing.T) {
	svc := &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{apptest.NewFakeSession(shellScreen...)}}
	d := newDriver(t, svc)
	d.resize(80, 24)
	y, x := rowOf(t, d, "zeta")
	before := d.view()
	d.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	d.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	d.send(tea.MouseMotionMsg{X: x, Y: y})
	d.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
	d.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if d.view() != before || selected(t, d).ID != "h1" {
		t.Fatalf("a release, motion or another button changed the list:\n%s", plain(d.view()))
	}
	// Releases between the presses of a double-click do not break it.
	d.clickAt(x, y)
	d.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	d.pressAfter(100*time.Millisecond, x, y)
	if got := svc.connections(); len(got) != 1 || got[0] != "h3" {
		t.Fatalf("double-click with releases: connected %v", got)
	}
}

func TestFR_NAV_012_ClickSelects(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	y, x := rowOf(t, d, "zeta")
	d.clickAt(x, y)
	if got := selected(t, d); got.ID != "h3" {
		t.Fatalf("selected %q, want zeta", got.Label)
	}
	// Anywhere on the row selects, including the address.
	y, _ = rowOf(t, d, "web-prod")
	d.clickAt(60, y)
	if got := selected(t, d); got.ID != "h2" {
		t.Fatalf("selected %q, want web-prod", got.Label)
	}
	// A blank row below the last host, the header, the blank row under
	// it, the status line and the footer do nothing.
	last, _ := rowOf(t, d, "zeta")
	for _, row := range []int{last + 1, 12, 0, 1, 22, 23} {
		d.clickAt(2, row)
		if got := selected(t, d); got.ID != "h2" {
			t.Fatalf("a click on row %d selected %q", row, got.Label)
		}
	}
	if len(d.root().tabs) != 0 || d.root().focus != 0 {
		t.Fatal("a click on the header changed the tabs")
	}
}

func TestFR_NAV_012_DoubleClickConnects(t *testing.T) {
	svc := &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{apptest.NewFakeSession(shellScreen...)}}
	d := newDriver(t, svc)
	d.resize(80, 24)
	db, _ := rowOf(t, d, "db-staging")
	zeta, x := rowOf(t, d, "zeta")
	// Presses on different rows only select.
	d.clickAt(x, db)
	d.pressAfter(100*time.Millisecond, x, zeta)
	if len(svc.connections()) != 0 {
		t.Fatal("presses on different rows connected")
	}
	d.pressAfter(100*time.Millisecond, x, zeta)
	if got := svc.connections(); len(got) != 1 || got[0] != "h3" {
		t.Fatalf("connected %v, want [h3]", got)
	}
	// It connects exactly like Enter: a tab opens for zeta.
	if tb := d.root().focused(); tb == nil || tb.hostID != "h3" {
		t.Fatal("no zeta tab after the double-click")
	}
}

func TestFR_NAV_012_WheelMovesThree(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: manyHosts(20)})
	d.resize(80, 12) // 8 body rows: the list scrolls
	listRow, _ := rowOf(t, d, "host-00")
	cursor := func() int { return d.list().Cursor() }
	d.wheelAt(listRow, tea.MouseWheelDown)
	if cursor() != 3 {
		t.Fatalf("one notch down: row %d, want 3", cursor())
	}
	for range 4 {
		d.wheelAt(listRow, tea.MouseWheelDown)
	}
	if cursor() != 15 || !strings.Contains(plain(d.view()), hostlist.SelectGlyph+"host-15") {
		t.Fatalf("five notches: row %d, want 15 and shown:\n%s", cursor(), plain(d.view()))
	}
	d.wheelAt(listRow, tea.MouseWheelDown)
	d.wheelAt(listRow, tea.MouseWheelDown)
	if cursor() != 19 {
		t.Fatalf("clamped at row %d, want 19", cursor())
	}
	d.wheelAt(listRow, tea.MouseWheelUp)
	if cursor() != 16 {
		t.Fatalf("one notch up: row %d, want 16", cursor())
	}
	for range 10 {
		d.wheelAt(listRow, tea.MouseWheelUp)
	}
	if cursor() != 0 || !strings.Contains(plain(d.view()), hostlist.SelectGlyph+"host-00") {
		t.Fatalf("clamped at row %d, want 0", cursor())
	}
	// The wheel over the header or the footer does nothing.
	d.wheelAt(0, tea.MouseWheelDown)
	d.wheelAt(11, tea.MouseWheelDown)
	if cursor() != 0 {
		t.Fatalf("the wheel outside the list moved it to row %d", cursor())
	}
}

func TestFR_NAV_012_ClickSearchFocuses(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	d.press('/', "/")
	d.typeText("e")
	// A click on a host moves the keyboard to the list, keeping the
	// query; the search line stays.
	y, x := rowOf(t, d, "zeta")
	d.clickAt(x, y)
	if d.list().Searching() || d.list().Query() != "e" {
		t.Fatalf("searching %v, query %q", d.list().Searching(), d.list().Query())
	}
	// A click on the search line focuses the field as / does.
	sy, sx := rowOf(t, d, "/ e")
	d.clickAt(sx+3, sy)
	if !d.list().Searching() || d.list().Query() != "e" {
		t.Fatalf("after clicking the search line: searching %v, query %q", d.list().Searching(), d.list().Query())
	}
	d.typeText("b")
	if d.list().Query() != "eb" {
		t.Fatalf("typing after the click: query %q, want %q", d.list().Query(), "eb")
	}
	if got := selected(t, d); got.ID != "h2" {
		t.Fatalf("selected %q, want web-prod", got.Label)
	}
}

func TestFR_NAV_012_ClickListLeavesSearch(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	d.press('/', "/")
	d.typeText("a")
	if !d.list().Searching() {
		t.Fatal("not searching")
	}
	y, x := rowOf(t, d, "zeta")
	d.clickAt(x, y)
	if d.list().Searching() {
		t.Fatal("still searching after a click on the list")
	}
	if got := selected(t, d); got.ID != "h3" || d.list().Query() != "a" {
		t.Fatalf("selected %q with query %q, want zeta with %q", got.Label, d.list().Query(), "a")
	}
	// The list has the keyboard again: k moves up instead of typing.
	d.press('k', "k")
	if d.list().Query() != "a" || selected(t, d).ID == "h3" {
		t.Fatalf("k went to the search field: query %q", d.list().Query())
	}
	if !strings.Contains(plain(d.view()), "/ a") {
		t.Fatalf("the kept query is not shown:\n%s", plain(d.view()))
	}
}

func TestFR_NAV_014_ClickChoices(t *testing.T) {
	d := newDriver(t, &fakeService{})
	d.resize(80, 24)
	// A click elsewhere does nothing.
	for _, s := range []string{"Welcome to sshelf", "You have no saved hosts", "sshelf never changes"} {
		y, x := rowOf(t, d, s)
		d.clickAt(x, y)
		if !d.onWelcome() || d.root().status != "" {
			t.Fatalf("a click on %q acted", s)
		}
	}
	// Anywhere on the "[a] Add host" line opens the host form.
	y, _ := rowOf(t, d, "[a] Add host")
	d.clickAt(60, y)
	if _, ok := d.root().screen.(hostedit.Model); !ok {
		t.Fatalf("the click did not open the host form:\n%s", plain(d.view()))
	}
	d.esc()
	if !d.onWelcome() {
		t.Fatal("not back on the Welcome screen")
	}
	// The "[I] Import" line starts the import.
	y, x := rowOf(t, d, "[I] Import from ~/.ssh/config")
	d.clickAt(x+1, y)
	if _, ok := d.report(); !ok {
		t.Fatalf("the click did not import:\n%s", plain(d.view()))
	}
}
