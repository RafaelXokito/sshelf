//go:build unix

package main

import (
	"fmt"
	"testing"
	"time"
)

// sgrPress and sgrRelease are what a terminal sends, with SGR mouse
// encoding (?1006), for a left press and release at column x, row y,
// counted from 0.
func sgrPress(x, y int) string   { return fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1) }
func sgrRelease(x, y int) string { return fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1) }

// TestFR_NAV_012_E2EDoubleClickConnects double-clicks web-prod with real
// SGR mouse sequences through the PTY: a single click only selects, two
// presses on the host row connect to the test server.
func TestFR_NAV_012_E2EDoubleClickConnects(t *testing.T) {
	s := startSlice(t)
	off := s.waitText(0, "web-prod demo@127.0.0.1")
	if !trackingOn(s.out.From(0)) {
		t.Fatalf("mouse tracking is off on the host list; output:\n%q", s.out.From(0))
	}
	// Row 0 is the header, row 1 is blank, row 2 is the first host.
	const x, y = 4, 2
	s.typeKeys(sgrPress(x, y) + sgrRelease(x, y))
	time.Sleep(600 * time.Millisecond) // longer than a double-click
	if n := len(s.srv.RequestsOfType("shell")); n != 0 {
		t.Fatalf("a single click connected (%d shells)", n)
	}
	s.typeKeys(sgrPress(x, y) + sgrRelease(x, y) + sgrPress(x, y) + sgrRelease(x, y))
	if !s.srv.WaitFor(10*time.Second, func() bool { return len(s.srv.RequestsOfType("shell")) == 1 }) {
		t.Fatalf("the double-click did not connect; output:\n%q", s.out.From(0))
	}
	s.tabs = 1
	off = s.waitText(off, "[1 web-prod]") // the session's tab
	s.endSession(off, "exit 3", "exited (3)")
	s.quit()
}
