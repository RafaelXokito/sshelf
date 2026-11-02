//go:build unix

package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// sgrPress and sgrRelease are what a terminal sends, with SGR mouse
// encoding (?1006), for a left press and release at column x, row y,
// counted from 0.
func sgrPress(x, y int) string   { return fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1) }
func sgrRelease(x, y int) string { return fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1) }

// TestFR_TAB_004_D028_E2EClickMarkerCloses ends a session and closes its
// tab with a real SGR click on the tab's "✕" (D-028), as x does: the
// host list comes back. A click on the tab's label first leaves it open.
func TestFR_TAB_004_D028_E2EClickMarkerCloses(t *testing.T) {
	s := startSlice(t)
	s.waitText(0, "web-prod demo@127.0.0.1")
	off := s.openSession(s.srv, 1)
	s.typeKeys("exit 3\r")
	off = s.waitText(off, "exited (3) · r reconnect · x close")
	// "sshelf │ Hosts │[1 web-prod ended ✕]│": the label at 17, the ✕ at 34.
	s.typeKeys(sgrPress(20, 0) + sgrRelease(20, 0))
	time.Sleep(300 * time.Millisecond)
	if bytes.Contains(s.out.From(off), []byte("enter connect")) {
		t.Fatalf("a click on the label closed the tab; output:\n%q", s.out.From(off))
	}
	s.typeKeys(sgrPress(34, 0) + sgrRelease(34, 0))
	s.tabs--
	s.waitText(off, "enter connect")
	s.quit()
}

// TestFR_TAB_004_D030_E2EClickLiveCloseAsks clicks the "✕" of a live,
// focused session tab with a real SGR click (D-030): it asks as Ctrl+] q
// does, n keeps the session, and a second click then y disconnects it
// and the host list comes back.
func TestFR_TAB_004_D030_E2EClickLiveCloseAsks(t *testing.T) {
	s := startSlice(t)
	s.waitText(0, "web-prod demo@127.0.0.1")
	off := s.openSession(s.srv, 1)
	// "sshelf │ Hosts │[1 web-prod ✕]│": the ✕ at 28.
	s.typeKeys(sgrPress(28, 0) + sgrRelease(28, 0))
	off = s.waitText(off, "Disconnect web-prod?")
	s.typeKeys("n")
	time.Sleep(300 * time.Millisecond)
	if bytes.Contains(s.out.From(off), []byte("enter connect")) {
		t.Fatalf("n closed the tab; output:\n%q", s.out.From(off))
	}
	off = s.out.Len()
	s.typeKeys(sgrPress(28, 0) + sgrRelease(28, 0))
	off = s.waitText(off, "Disconnect web-prod?")
	s.typeKeys("y")
	s.tabs--
	s.waitText(off, "enter connect")
	s.quit()
}

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
	off = s.waitText(off, "[1 web-prod ✕]") // the session's tab
	s.endSession(off, "exit 3", "exited (3)")
	s.quit()
}

// TestFR_TAB_011_V1_E2ERemoteMouse scans what sshelf writes while a
// session is focused (D-024): button-event tracking stays on in a plain
// shell, never all motion; once the remote asks for the mouse, a real SGR
// press in the session area reaches it in session coordinates and a
// press on the header's title does not; and tracking stays on (cell
// motion) after the remote turns reporting off, for sshelf's selection.
func TestFR_TAB_011_V1_E2ERemoteMouse(t *testing.T) {
	s := startSlice(t)
	s.waitText(0, "web-prod demo@127.0.0.1")
	s.waitTracking(true, "on the host list")
	off := s.openSession(s.srv, 1)
	plainFrom := s.out.Len()
	s.typeKeys("echo plain shell\r")
	off = s.waitText(off, "echo plain shell plain shell")
	s.waitTracking(true, "in a plain shell")
	if got := s.out.From(0); bytes.Contains(got, []byte("\x1b[?1003h")) {
		t.Fatalf("all-motion tracking enabled in a plain shell; output:\n%q", got)
	}

	s.typeKeys("modes 1000,1006\r")
	s.waitText(off, "modes 1000,1006")
	time.Sleep(200 * time.Millisecond) // the emulator has seen ?1000h
	have := len(s.srv.Received(1, 1))
	s.typeKeys(sgrPress(0, 0) + sgrRelease(0, 0)) // the title on the header
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
	time.Sleep(200 * time.Millisecond)
	s.waitTracking(true, "after the remote sent ?1000l")
	if got := s.out.From(plainFrom); lastIndex(got, mouseTrackingOff) >= 0 {
		t.Fatalf("mouse tracking turned off in a session; output:\n%q", got)
	}

	s.endSession(off, "exit 3", "exited (3)")
	s.quit()
	if trackingOn(s.out.From(0)) {
		t.Error("mouse tracking is still on after quitting")
	}
}

// osc52 matches the clipboard write sshelf sends for a copy.
var osc52 = regexp.MustCompile(`\x1b\]52;c;([A-Za-z0-9+/=]*)\x07`)

// TestFR_TAB_013_E2ECopyWritesOSC52 triple-clicks rows of a plain shell's
// session area with real SGR sequences until one holds the output of an
// echo, and checks that sshelf wrote it to the terminal as an OSC 52
// clipboard write, base64-encoded.
func TestFR_TAB_013_E2ECopyWritesOSC52(t *testing.T) {
	s := startSlice(t)
	s.waitText(0, "web-prod demo@127.0.0.1")
	off := s.openSession(s.srv, 1)
	s.typeKeys("echo copy-me-123\r")
	off = s.waitText(off, "echo copy-me-123 copy-me-123")
	var copied []string
	found := false
	for y := 1; y <= 8 && !found; y++ {
		from := s.out.Len()
		click := sgrPress(3, y) + sgrRelease(3, y)
		s.typeKeys(click + click + click)
		deadline := time.Now().Add(time.Second)
		for !found && time.Now().Before(deadline) {
			for _, m := range osc52.FindAllSubmatch(s.out.From(from), -1) {
				text, err := base64.StdEncoding.DecodeString(string(m[1]))
				if err != nil {
					t.Fatalf("OSC 52 payload %q: %v", m[1], err)
				}
				copied = append(copied, string(text))
				found = found || string(text) == "copy-me-123"
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !found {
		t.Fatalf("no row copied copy-me-123; copied %q; output:\n%q", copied, s.out.From(0))
	}
	s.endSession(off, "exit 3", "exited (3)")
	s.quit()
}
