package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
)

// The mouse inside a session (FR-TAB-011, D-012, T-041).

// remoteMouse makes fs report that its remote asked for mode, as the
// emulator would after ?1000h, ?1002h or ?1003h (or after turning them
// off).
func remoteMouse(fs *apptest.FakeSession, mode app.MouseMode) {
	fs.SetSnapshot(app.Snapshot{Lines: shellScreen, Mouse: mode, Seq: 2})
}

// mouseSession is a driver at 80×24 with tab 1 focused on a session
// whose remote asked for mode.
func mouseSession(t *testing.T, mode app.MouseMode) (*driver, *apptest.FakeSession) {
	t.Helper()
	fs := apptest.NewFakeSession(shellScreen...)
	d := framedDriver(t, testHosts, fs)
	d.resize(80, 24)
	connectHost(t, d, "h2")
	remoteMouse(fs, mode)
	return d, fs
}

func TestFR_TAB_011_RemoteModeMapping(t *testing.T) {
	for _, tc := range []struct {
		in   app.MouseMode
		want tea.MouseMode
	}{
		{app.MouseNone, tea.MouseModeNone},
		{app.MousePress, tea.MouseModeCellMotion},
		{app.MouseDrag, tea.MouseModeCellMotion},
		{app.MouseAny, tea.MouseModeAllMotion},
	} {
		d, _ := mouseSession(t, tc.in)
		if got := d.m.View().MouseMode; got != tc.want {
			t.Errorf("remote mode %v: View().MouseMode %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFR_TAB_011_TrackingOffAfterDisable(t *testing.T) {
	d, fs := mouseSession(t, app.MouseNone)
	if got := d.m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("before the remote asks: mode %v", got)
	}
	remoteMouse(fs, app.MousePress) // ?1000h
	if got := d.m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("after ?1000h: mode %v", got)
	}
	remoteMouse(fs, app.MouseNone) // ?1000l
	if got := d.m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("after ?1000l: mode %v, want tracking off in the next frame", got)
	}
	// Presses after that are not forwarded either: the session drops them,
	// and the terminal no longer sends any.
}

func TestFR_TAB_011_ForwardedInSessionCoordinates(t *testing.T) {
	d, fs := mouseSession(t, app.MouseAny)
	d.send(tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft, Mod: tea.ModShift})
	d.send(tea.MouseMotionMsg{X: 11, Y: 5, Button: tea.MouseLeft})
	d.send(tea.MouseReleaseMsg{X: 11, Y: 5, Button: tea.MouseLeft})
	d.send(tea.MouseMotionMsg{X: 0, Y: 1})
	d.send(tea.MouseWheelMsg{X: 79, Y: 22, Button: tea.MouseWheelDown})
	want := []uv.MouseEvent{
		uv.MouseClickEvent{X: 10, Y: 4, Button: uv.MouseLeft, Mod: uv.ModShift},
		uv.MouseMotionEvent{X: 11, Y: 4, Button: uv.MouseLeft},
		uv.MouseReleaseEvent{X: 11, Y: 4, Button: uv.MouseLeft},
		uv.MouseMotionEvent{X: 0, Y: 0},
		uv.MouseWheelEvent{X: 79, Y: 21, Button: uv.MouseWheelDown},
	}
	if got := fs.Mouse(); !slices.Equal(got, want) {
		t.Fatalf("forwarded %v, want %v", got, want)
	}
}

func TestFR_TAB_011_FrameRowsNotForwarded(t *testing.T) {
	d, fs := mouseSession(t, app.MousePress)
	before := d.view()
	_, hostsX := textAt(t, before, "Hosts") // the Hosts tab in the tab bar
	for _, y := range []int{0, 23} {        // the header and the status bar
		for _, x := range []int{0, hostsX, 79} {
			d.clickAt(x, y)
			d.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
			d.send(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
		}
	}
	d.send(tea.MouseClickMsg{X: 80, Y: 5, Button: tea.MouseLeft}) // right of the area
	if got := fs.Mouse(); len(got) != 0 {
		t.Fatalf("frame rows forwarded %v", got)
	}
	// Not acted on either: the tab bar does not switch tabs while a
	// session is focused (D-012), and nothing else changes (but the
	// status bar's clock, which the clicks moved on).
	if d.root().focus != 1 || withoutStatus(d.view()) != withoutStatus(before) {
		t.Fatalf("a click on the frame changed it: focus %d\n%s", d.root().focus, plain(d.view()))
	}
}

// withoutStatus is view without its last line, the status bar.
func withoutStatus(view string) string {
	return view[:strings.LastIndex(view, "\n")]
}

func TestFR_TAB_011_NotForwardedInScrollbackEndedOrModal(t *testing.T) {
	d, fs := mouseSession(t, app.MouseAny)

	d.prefix("[") // scrollback mode
	d.clickAt(10, 5)
	d.send(tea.MouseWheelMsg{X: 10, Y: 5, Button: tea.MouseWheelUp})
	d.esc()

	d.prefix("?") // the prefix help overlay takes the mouse
	if len(d.root().modals) == 0 {
		t.Fatal("the prefix help did not open")
	}
	if got := d.m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("modal over a session: mode %v", got)
	}
	d.clickAt(10, 5)
	d.esc()

	fs.End(nil)
	d.pump(1)
	d.clickAt(10, 5)
	if got := fs.Mouse(); len(got) != 0 {
		t.Fatalf("forwarded %v in scrollback mode, under a modal or after the end", got)
	}
}

func TestARCH_UI_006_SwitchAppliesNextFrame(t *testing.T) {
	plainSess := apptest.NewFakeSession(shellScreen...)
	mouseSess := apptest.NewFakeSession(shellScreen...)
	remoteMouse(mouseSess, app.MouseAny) // ?1003h
	d := framedDriver(t, testHosts, plainSess, mouseSess)
	d.resize(80, 24)
	connectHost(t, d, "h2") // tab 1: a plain shell
	connectHost(t, d, "h1") // tab 2: an app that asked for every motion
	for _, step := range []struct {
		focus int
		want  tea.MouseMode
	}{
		{0, tea.MouseModeCellMotion},
		{1, tea.MouseModeNone},
		{2, tea.MouseModeAllMotion},
		{0, tea.MouseModeCellMotion},
		{2, tea.MouseModeAllMotion},
		{1, tea.MouseModeNone},
	} {
		d.focus(step.focus)
		if got := d.m.View().MouseMode; got != step.want {
			t.Fatalf("focus %d: mode %v, want %v", step.focus, got, step.want)
		}
	}
	// Each session gets only the events of the frames it was focused in.
	d.focus(1)
	d.clickAt(3, 3)
	d.focus(2)
	d.clickAt(4, 4)
	if a, b := plainSess.Mouse(), mouseSess.Mouse(); len(a) != 1 || len(b) != 1 ||
		a[0] != uv.MouseEvent(uv.MouseClickEvent{X: 3, Y: 2, Button: uv.MouseLeft}) ||
		b[0] != uv.MouseEvent(uv.MouseClickEvent{X: 4, Y: 3, Button: uv.MouseLeft}) {
		t.Fatalf("tab 1 got %v, tab 2 got %v", a, b)
	}
}
