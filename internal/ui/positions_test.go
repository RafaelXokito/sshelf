package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/session"
)

// threeTabs connects db-staging, web-prod and zeta, in that order, at
// 80x24; tab 3 is focused.
func threeTabs(t *testing.T) (*driver, []*apptest.FakeSession) {
	t.Helper()
	ss := []*apptest.FakeSession{apptest.NewFakeSession("A"), apptest.NewFakeSession("B"), apptest.NewFakeSession("C"), apptest.NewFakeSession("D")}
	d := framedDriver(t, testHosts, ss...)
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, id := range []domain.ID{"h1", "h2", "h3"} {
		connectHost(t, d, id)
	}
	return d, ss
}

// focusedSession is the session of the focused tab, or nil.
func focusedSession(d *driver) *apptest.FakeSession {
	if t := d.root().focused(); t != nil {
		return t.sess.(*apptest.FakeSession)
	}
	return nil
}

// TestFR_TAB_002_NumbersArePositions: closing the first, a middle or the
// last tab renumbers the tabs to its right, Ctrl+] 1-9 address the new
// numbers, and the next tab gets the next position (D-023).
func TestFR_TAB_002_NumbersArePositions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		close  string
		header string // after closing
		left   []int  // the sessions left, by index into the sessions
	}{
		{"first", "1", "sshelf │[Hosts]│ 1 web-prod │ 2 zeta │", []int{1, 2}},
		{"middle", "2", "sshelf │ Hosts │[1 db-staging]│ 2 zeta │", []int{0, 2}},
		{"last", "3", "sshelf │ Hosts │ 1 db-staging │[2 web-prod]│", []int{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, ss := threeTabs(t)
			d.prefix(tc.close)
			d.prefix("q")
			d.keys("y")
			if h := headerLine(d); h != tc.header {
				t.Fatalf("header = %q, want %q", h, tc.header)
			}
			for pos, i := range tc.left {
				d.prefix(string(rune('1' + pos)))
				if focusedSession(d) != ss[i] || d.root().focus != pos+1 {
					t.Fatalf("Ctrl+] %d focused tab %d, not session %d", pos+1, d.root().focus, i)
				}
			}
			before := d.root().focus
			d.prefix("3") // no third tab now
			if d.root().focus != before {
				t.Fatalf("Ctrl+] 3 with two tabs moved focus to %d", d.root().focus)
			}

			connectHost(t, d, "h1")
			if h := headerLine(d); !strings.HasSuffix(h, "│[3 db-staging]│") {
				t.Fatalf("header after a new connection = %q", h)
			}
			d.prefix("1")
			if focusedSession(d) != ss[tc.left[0]] {
				t.Fatal("Ctrl+] 1 does not focus the leftmost tab")
			}
		})
	}
}

// TestFR_TAB_001_NextPrevFollowPositions: Ctrl+] n and p walk the tabs
// left to right after a close (D-023).
func TestFR_TAB_001_NextPrevFollowPositions(t *testing.T) {
	d, ss := threeTabs(t)
	d.prefix("2")
	d.prefix("q")
	d.keys("y") // focus on 1 db-staging
	d.prefix("n")
	if focusedSession(d) != ss[2] || !strings.Contains(headerLine(d), "[2 zeta]") {
		t.Fatalf("Ctrl+] n: header = %q", headerLine(d))
	}
	d.prefix("n")
	if d.root().focus != 0 {
		t.Fatalf("Ctrl+] n from the last tab: focus = %d, want Hosts", d.root().focus)
	}
	d.prefix("p")
	if focusedSession(d) != ss[2] {
		t.Fatal("Ctrl+] p from Hosts did not focus the last tab")
	}
}

// TestFR_TAB_003_ClickAfterRenumbering: a click on a tab's new number
// focuses that tab (D-023, FR-NAV-017).
func TestFR_TAB_003_ClickAfterRenumbering(t *testing.T) {
	d, ss := threeTabs(t)
	d.prefix("1")
	d.prefix("q")
	d.keys("y")
	y, x := textAt(t, d.view(), "2 zeta")
	d.clickAt(x, y)
	if focusedSession(d) != ss[2] || d.root().focus != 2 {
		t.Fatalf("click on 2 zeta: focus = %d", d.root().focus)
	}
}

// TestFR_CONN_010_ReconnectAfterRenumbering: an ended tab that moved left
// keeps its new number and reconnects in place (D-023).
func TestFR_CONN_010_ReconnectAfterRenumbering(t *testing.T) {
	d, ss := threeTabs(t)
	endTab(d, 3, ss[2], &session.ExitError{Status: 0})
	d.prefix("1")
	d.prefix("q")
	d.keys("y")
	if h := headerLine(d); h != "sshelf │[Hosts]│ 1 web-prod │ 2 zeta ✕ │" {
		t.Fatalf("header = %q", h)
	}
	d.prefix("2")
	d.press('r', "r")
	r := d.root()
	if len(r.tabs) != 2 || r.focus != 2 || focusedSession(d) != ss[3] {
		t.Fatalf("tabs = %d, focus = %d after r", len(r.tabs), r.focus)
	}
	if h := headerLine(d); h != "sshelf │ Hosts │ 1 web-prod │[2 zeta]│" {
		t.Fatalf("header after reconnect = %q", h)
	}
}

// TestFR_TAB_003_SFTPTabRenumbered: an SFTP tab moves left like a
// session tab; the questions that name it use its new number, and
// Ctrl+] s from its session focuses it (D-023).
func TestFR_TAB_003_SFTPTabRenumbered(t *testing.T) {
	a, b := apptest.NewFakeSession("A"), apptest.NewFakeSession("B")
	a.SetConnID(7)
	b.SetConnID(8)
	d, svc := sftpDriver(t, testHosts, []*apptest.FakeSession{a, b}, approvedBrowser(7))
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "h1") // 1
	connectHost(t, d, "h2") // 2
	d.prefix("1")
	d.prefix("s") // 3
	d.prefix("2")
	d.prefix("q")
	d.keys("y")
	if h := headerLine(d); h != "sshelf │ Hosts │[1 db-staging]│ 2 sftp: db-staging │" {
		t.Fatalf("header = %q", h)
	}
	d.prefix("1")
	d.prefix("s")
	if d.root().focusedSFTP() == nil || d.root().focus != 2 || len(svc.openedOn) != 1 {
		t.Fatalf("Ctrl+] s: focus = %d, opens = %d", d.root().focus, len(svc.openedOn))
	}
	d.prefix("1")
	d.prefix("q")
	if v := plain(d.view()); !strings.Contains(v, "SFTP tab 2 stays connected.") {
		t.Fatalf("disconnect question:\n%s", v)
	}
	d.press(tea.KeyEscape, "")
	d.prefix("2")
	d.prefix("q")
	if v := plain(d.view()); !strings.Contains(v, "Close SFTP tab 2?") {
		t.Fatalf("close question:\n%s", v)
	}
}

// TestFR_TAB_003_RenumberedGolden: the frame after closing tab 1 of 3.
func TestFR_TAB_003_RenumberedGolden(t *testing.T) {
	d, _ := threeTabs(t)
	d.prefix("1")
	d.prefix("q")
	d.keys("y")
	d.prefix("2")
	golden(t, "tabs-renumbered", d.m)
}
