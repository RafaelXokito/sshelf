//go:build unix

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The mouse inside a real session (FR-TAB-011, T-041): the root model
// over a session connected to the in-process server.

// typeLine types text and Enter into the focused session.
func (l *liveModel) typeLine(text string) {
	for _, r := range text {
		l.m, _ = l.m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	l.m, _ = l.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// waitMouseMode waits until the next View's mouse mode is want.
func (l *liveModel) waitMouseMode(want tea.MouseMode, after string) {
	l.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for l.m.View().MouseMode != want {
		if time.Now().After(deadline) {
			l.t.Fatalf("after %s: View().MouseMode %v, want %v", after, l.m.View().MouseMode, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestFR_TAB_011_SGRClickEncoding: after the remote sends
// ESC[?1000h ESC[?1006h, a left press at terminal cell x=10, y=5 (header
// on row 0) reaches the remote as ESC[<0;11;5M; presses on the header and
// the status bar do not; after ESC[?1000l tracking is off again.
func TestFR_TAB_011_SGRClickEncoding(t *testing.T) {
	l := newLiveModel(t)
	l.m, _ = l.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	l.connect()
	if !l.srv.WaitFor(5*time.Second, func() bool { return len(l.srv.RequestsOfType("shell")) == 1 }) {
		t.Fatal("the shell did not start")
	}
	if got := l.m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("plain shell: mode %v", got)
	}
	l.typeLine("modes 1000,1006")
	l.waitMouseMode(tea.MouseModeCellMotion, "?1000h ?1006h")

	have := len(l.received(1))
	for _, y := range []int{0, 23} { // the header and the status bar
		l.m, _ = l.m.Update(tea.MouseClickMsg{X: 10, Y: y, Button: tea.MouseLeft})
	}
	l.m, _ = l.m.Update(tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft})
	l.m, _ = l.m.Update(tea.MouseMotionMsg{X: 11, Y: 5, Button: tea.MouseLeft}) // ?1000 has no motion
	l.m, _ = l.m.Update(tea.MouseReleaseMsg{X: 11, Y: 5, Button: tea.MouseLeft})
	const want = "\x1b[<0;11;5M\x1b[<0;12;5m"
	if !l.srv.WaitFor(5*time.Second, func() bool { return bytes.Contains(l.received(1)[have:], []byte(want)) }) {
		t.Fatalf("the server received %q, want %q", l.received(1)[have:], want)
	}
	if got := l.received(1)[have:]; !bytes.Equal(got, []byte(want)) {
		t.Fatalf("the server received %q, want exactly %q (no frame rows, no motion)", got, want)
	}

	// The remote turns reporting off: tracking is off in the next frame.
	off := filepath.Join(t.TempDir(), "mouse-off")
	if err := os.WriteFile(off, []byte("\x1b[?1000l\x1b[?1006l"), 0o600); err != nil {
		t.Fatal(err)
	}
	l.m, _ = l.m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) // drop the mouse bytes from the shell's line
	l.typeLine("replay " + off)
	l.waitMouseMode(tea.MouseModeNone, "?1000l")
}
