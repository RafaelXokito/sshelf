//go:build unix

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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

// TestFR_TAB_011_NoTrackingUntilRequested scans every byte sshelf writes
// while a session is focused: no mouse-tracking sequence until the remote
// asks for the mouse, so the terminal's own selection works (D-012); then
// tracking on, a real SGR press in the session area reaching the remote
// in session coordinates and a press on the header not reaching it; and
// tracking off again once the remote turns reporting off.
func TestFR_TAB_011_NoTrackingUntilRequested(t *testing.T) {
	s := startSlice(t)
	s.waitText(0, "web-prod demo@127.0.0.1")
	s.waitTracking(true, "on the host list")
	off := s.openSession(s.srv, 1)
	s.waitTracking(false, "once the session tab is focused")
	plainFrom := s.out.Len()
	s.typeKeys("echo plain shell\r")
	off = s.waitText(off, "echo plain shell plain shell")
	if got := s.out.From(plainFrom); lastIndex(got, mouseTrackingOn) >= 0 {
		t.Fatalf("mouse tracking enabled in a plain shell; output:\n%q", got)
	}

	s.typeKeys("modes 1000,1006\r")
	s.waitTracking(true, "after the remote sent ?1000h ?1006h")
	if !bytes.Contains(s.out.From(plainFrom), []byte("\x1b[?1002h")) {
		t.Fatalf("cell-motion tracking not enabled for ?1000; output:\n%q", s.out.From(plainFrom))
	}
	have := len(s.srv.Received(1, 1))
	s.typeKeys(sgrPress(10, 0) + sgrRelease(10, 0)) // the header
	s.typeKeys(sgrPress(10, 5) + sgrRelease(10, 5))
	const want = "\x1b[<0;11;5M\x1b[<0;11;5m"
	if !s.srv.WaitFor(10*time.Second, func() bool { return bytes.Contains(s.srv.Received(1, 1)[have:], []byte(want)) }) {
		t.Fatalf("the remote received %q, want %q", s.srv.Received(1, 1)[have:], want)
	}
	if got := s.srv.Received(1, 1)[have:]; !bytes.Equal(got, []byte(want)) {
		t.Fatalf("the remote received %q, want exactly %q", got, want)
	}

	disable := filepath.Join(t.TempDir(), "mouse-off")
	if err := os.WriteFile(disable, []byte("\x1b[?1000l\x1b[?1006l"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.typeKeys("\x03replay " + disable + "\r") // Ctrl+C drops the mouse bytes from the line
	s.waitTracking(false, "after the remote sent ?1000l")

	s.endSession(off, "exit 3", "exited (3)")
	s.quit()
	if trackingOn(s.out.From(0)) {
		t.Error("mouse tracking is still on after quitting")
	}
}
