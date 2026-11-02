package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/tabs"
)

// clickClose clicks the "✕" of the tab at position num (D-023), checking
// that the header shows the button where the layout puts it.
func clickClose(t *testing.T, d *driver, num int) {
	t.Helper()
	h := []rune(headerLine(d))
	for _, s := range d.root().headerSpans() {
		if s.Kind != tabs.KindTab || s.Tab != num {
			continue
		}
		if !s.Close || s.CloseX >= len(h) || string(h[s.CloseX]) != tabs.CloseButton {
			t.Fatalf("tab %d has no ✕ at %d in %q", num, s.CloseX, string(h))
		}
		d.clickAt(s.CloseX, 0)
		return
	}
	t.Fatalf("tab %d not shown in %q", num, string(h))
}

// question is the top confirmation's text, or "".
func question(d *driver) string {
	if c, ok := top(d).(confirm.Model); ok {
		return plain(c.View())
	}
	return ""
}

// TestFR_TAB_004_D028_ClickMarkerCloses: a click on an ended tab's ✕
// closes it as x does, from the Hosts tab and from a session tab, and the
// tabs to its right renumber (D-023); a click elsewhere on it focuses it.
func TestFR_TAB_004_D028_ClickMarkerCloses(t *testing.T) {
	d, a, b := twoSessions(t)
	a.End(nil)
	d.pump(1)
	d.focus(0)
	if h := headerLine(d); !strings.Contains(h, "1 db-staging ended ✕") {
		t.Fatalf("header = %q", h)
	}
	clickClose(t, d, 1)
	if len(d.root().tabs) != 1 || a.Closes() != 1 || d.root().focus != 0 || len(d.root().modals) != 0 {
		t.Fatalf("tabs %d closes %d focus %d", len(d.root().tabs), a.Closes(), d.root().focus)
	}
	if h := headerLine(d); !strings.Contains(h, "│ 1 web-prod ✕ │") {
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
	clickClose(t, d, 1)
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
	clickClose(t, d, 1)
	if len(d.root().tabs) != 1 || a.Closes() != 1 || d.root().focus != 1 || d.root().focused().sess != b {
		t.Fatalf("tabs %d closes %d focus %d", len(d.root().tabs), a.Closes(), d.root().focus)
	}
	b.End(nil)
	d.pump(2)
	d.prefix("[")
	clickClose(t, d, 1)
	if len(d.root().tabs) != 0 || b.Closes() != 1 {
		t.Fatal("the ✕ did not close in scrollback mode")
	}
}

// TestFR_TAB_004_D030_LiveTabAsks: every session tab shows ✕, the Hosts
// tab none; a click on a live tab's ✕ asks "Disconnect <label>?" as
// Ctrl+] q does there, from any focused tab, while the session is
// focused (D-024) and in scrollback mode; n keeps it, y closes it and
// the tabs renumber (D-023).
func TestFR_TAB_004_D030_LiveTabAsks(t *testing.T) {
	d, a, b := twoSessions(t)
	b.Output("more")
	d.focus(1)
	d.pump(2) // tab 2 shows ●
	if h := headerLine(d); h != "sshelf │ Hosts │[1 db-staging ✕]│ 2 web-prod ● ✕ │" {
		t.Fatalf("header = %q", h)
	}
	for _, s := range d.root().headerSpans() {
		if s.Kind == tabs.KindTab && s.Tab == 0 && s.Close {
			t.Fatal("the Hosts tab has a ✕")
		}
	}

	for _, focus := range []int{0, 1, 2} {
		d.focus(focus)
		clickClose(t, d, 2)
		if q := question(d); !strings.Contains(q, "Disconnect web-prod?") {
			t.Fatalf("focus %d: question %q", focus, q)
		}
		if focus == 2 && (len(b.Mouse()) != 0 || len(b.Keys()) != 0) {
			t.Fatal("the click reached the remote")
		}
		d.keys("n")
		if len(d.root().tabs) != 2 || b.Closes() != 0 || d.root().focus != focus {
			t.Fatalf("focus %d: n closed the tab or moved the focus", focus)
		}
	}

	d.focus(1)
	d.prefix("[")
	clickClose(t, d, 1)
	if q := question(d); !strings.Contains(q, "Disconnect db-staging?") {
		t.Fatalf("scrollback: question %q", q)
	}
	d.keys("y")
	if len(d.root().tabs) != 1 || a.Closes() != 1 {
		t.Fatalf("y: tabs %d closes %d", len(d.root().tabs), a.Closes())
	}
	if h := headerLine(d); !strings.Contains(h, "1 web-prod ✕") {
		t.Fatalf("header = %q: not renumbered", h)
	}
}

// TestFR_TAB_004_D030_LiveTabSharedConnection: a live session's ✕ names
// the SFTP tab that stays connected on its connection, as Ctrl+] q does
// (FR-CONN-014 M3 revision).
func TestFR_TAB_004_D030_LiveTabSharedConnection(t *testing.T) {
	a := apptest.NewFakeSession("A")
	a.SetConnID(7)
	d, _ := sftpDriver(t, testHosts, []*apptest.FakeSession{a}, approvedBrowser(7))
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "h1")
	d.prefix("s")
	clickClose(t, d, 1)
	if q := question(d); !strings.Contains(q, "SFTP tab 2 stays connected.") {
		t.Fatalf("question %q", q)
	}
}

// TestFR_TAB_004_D028_LiveTabsAndModals: with a modal open no click on
// the tab bar closes or asks anything (FR-NAV-016); the title and the
// separators do nothing.
func TestFR_TAB_004_D028_LiveTabsAndModals(t *testing.T) {
	d, a, b := twoSessions(t)
	d.focus(0)
	d.clickAt(0, 0) // the title
	if len(d.root().modals) != 0 {
		t.Fatal("a click on the title opened a modal")
	}
	a.End(nil)
	d.pump(1)
	d.press('q', "q") // the quit confirmation
	clickClose(t, d, 1)
	clickClose(t, d, 2)
	if len(d.root().tabs) != 2 || len(d.root().modals) != 1 || a.Closes()+b.Closes() != 0 {
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
	if h := headerLine(d); !strings.Contains(h, "2 sftp: raspberrypi4 ended ✕") {
		t.Fatalf("header = %q", h)
	}
	clickClose(t, d, 2)
	if len(d.root().tabs) != 1 || b.Closes() != 1 || d.root().focus != 1 {
		t.Fatalf("idle: tabs %d closes %d focus %d", len(d.root().tabs), b.Closes(), d.root().focus)
	}

	d, b = approvedTab(t)
	disconnect(t, d, b)
	clickClose(t, d, 2)
	if q := question(d); !strings.Contains(q, "Close and drop 3 paused transfers?") {
		t.Fatalf("paused: %q", q)
	}
	d.keys("y")
	if len(d.root().tabs) != 1 || b.Closes() != 1 {
		t.Fatalf("paused: tabs %d closes %d", len(d.root().tabs), b.Closes())
	}
}

// TestFR_TAB_004_D030_LiveSFTPAsks: a connected SFTP tab's ✕ asks the
// close question as Ctrl+] q does there (FR-SFTP-025), idle or not.
func TestFR_TAB_004_D030_LiveSFTPAsks(t *testing.T) {
	d, b := approvedTab(t)
	clickClose(t, d, 2)
	if q := question(d); !strings.Contains(q, "Close SFTP tab 2?") {
		t.Fatalf("idle: %q", q)
	}
	d.keys("n")
	pumpTransfers(t, d, b, 2, everyState()...)
	d.focus(0)
	clickClose(t, d, 2)
	if q := question(d); !strings.Contains(q, "Close SFTP tab 2? 4 transfers will stop.") {
		t.Fatalf("busy: %q", q)
	}
	d.keys("y")
	if len(d.root().tabs) != 1 || b.Closes() != 1 {
		t.Fatalf("tabs %d closes %d", len(d.root().tabs), b.Closes())
	}
}
