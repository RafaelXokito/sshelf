package ui

import (
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/tabs"
)

// clickMarker clicks the n-th (from 0) "✕" of the header.
func clickMarker(t *testing.T, d *driver, n int) {
	t.Helper()
	h := []rune(headerLine(d))
	for x, r := range h {
		if r != []rune(tabs.EndedMarker)[0] {
			continue
		}
		if n == 0 {
			d.clickAt(x, 0)
			return
		}
		n--
	}
	t.Fatalf("no ✕ #%d in %q", n, string(h))
}

// TestFR_TAB_004_D028_ClickMarkerCloses: a click on an ended tab's ✕
// closes it as x does, from the Hosts tab and from a session tab, and the
// tabs to its right renumber (D-023); a click elsewhere on it focuses it.
func TestFR_TAB_004_D028_ClickMarkerCloses(t *testing.T) {
	d, a, b := twoSessions(t)
	a.End(nil)
	d.pump(1)
	d.focus(0)
	if h := headerLine(d); !strings.Contains(h, "1 db-staging ✕") {
		t.Fatalf("header = %q", h)
	}
	clickMarker(t, d, 0)
	if len(d.root().tabs) != 1 || a.Closes() != 1 || d.root().focus != 0 {
		t.Fatalf("tabs %d closes %d focus %d", len(d.root().tabs), a.Closes(), d.root().focus)
	}
	if h := headerLine(d); !strings.Contains(h, "│ 1 web-prod │") {
		t.Fatalf("header = %q: not renumbered", h)
	}

	// While the ended tab is focused (D-024): the label focuses, the ✕ closes.
	b.End(nil)
	d.pump(2)
	d.focus(1)
	d.clickText(t, "1 web-prod")
	if len(d.root().tabs) != 1 || d.root().focus != 1 {
		t.Fatal("a click on the label closed the tab")
	}
	clickMarker(t, d, 0)
	if len(d.root().tabs) != 0 || b.Closes() != 1 || d.root().focus != 0 {
		t.Fatalf("tabs %d closes %d focus %d", len(d.root().tabs), b.Closes(), d.root().focus)
	}
}

// TestFR_TAB_004_D028_BackgroundAndScrollback: the ✕ of a background
// ended tab closes it while another session is focused, which keeps the
// focus; the focused ended tab's ✕ closes it in scrollback mode too.
func TestFR_TAB_004_D028_BackgroundAndScrollback(t *testing.T) {
	d, a, b := twoSessions(t)
	a.End(nil)
	d.pump(1)
	d.focus(2)
	clickMarker(t, d, 0)
	if len(d.root().tabs) != 1 || a.Closes() != 1 || d.root().focus != 1 || d.root().focused().sess != b {
		t.Fatalf("tabs %d closes %d focus %d", len(d.root().tabs), a.Closes(), d.root().focus)
	}
	b.End(nil)
	d.pump(2)
	d.prefix("[")
	clickMarker(t, d, 0)
	if len(d.root().tabs) != 0 || b.Closes() != 1 {
		t.Fatal("the ✕ did not close in scrollback mode")
	}
}

// TestFR_TAB_004_D028_LiveTabsAndModals: a live tab has no ✕, so no click
// on the tab bar closes it; with a modal open the ✕ does nothing.
func TestFR_TAB_004_D028_LiveTabsAndModals(t *testing.T) {
	d, a, b := twoSessions(t)
	b.Output("more")
	d.focus(1)
	d.pump(2) // tab 2 shows ●
	for x := range []rune(headerLine(d)) {
		d.clickAt(x, 0)
	}
	if len(d.root().tabs) != 2 || a.Closes()+b.Closes() != 0 {
		t.Fatal("a click closed a live tab")
	}
	a.End(nil)
	d.pump(1)
	d.focus(0)
	d.press('q', "q") // the quit confirmation
	clickMarker(t, d, 0)
	if len(d.root().tabs) != 2 || len(d.root().modals) != 1 {
		t.Fatal("the ✕ worked through a modal")
	}
}

// TestFR_TAB_004_D028_SFTPMarker: a disconnected SFTP tab's ✕ acts as x
// does there: an idle tab closes at once, one with paused transfers asks
// first (FR-SFTP-018, FR-SFTP-025).
func TestFR_TAB_004_D028_SFTPMarker(t *testing.T) {
	d, b := approvedTab(t)
	dropTab(t, d, b, 2)
	d.focus(1)
	clickMarker(t, d, 0)
	if len(d.root().tabs) != 1 || b.Closes() != 1 || d.root().focus != 1 {
		t.Fatalf("idle: tabs %d closes %d focus %d", len(d.root().tabs), b.Closes(), d.root().focus)
	}

	d, b = approvedTab(t)
	disconnect(t, d, b)
	clickMarker(t, d, 0)
	c, ok := top(d).(confirm.Model)
	if !ok || !strings.Contains(c.View(), "Close and drop 3 paused transfers?") {
		t.Fatalf("paused: %#v", top(d))
	}
	d.keys("y")
	if len(d.root().tabs) != 1 || b.Closes() != 1 {
		t.Fatalf("paused: tabs %d closes %d", len(d.root().tabs), b.Closes())
	}
}
