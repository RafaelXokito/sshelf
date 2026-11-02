package ui

import (
	"encoding/base64"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
)

// sshelf's own selection in sessions (FR-TAB-012, FR-TAB-013,
// SEC-CLIP-006, D-024, T-062). The session area starts on terminal row 1.

// selScreen is a remote screen with plain words, a double-width run,
// trailing spaces and a line the remote wrapped at 80 columns.
var selScreen = []string{
	"alpha beta",
	"gamma delta",
	"epsilon",
	"ab日本語cd",
	"ls -la /var/log(tmp) done   ",
	strings.Repeat("x", 78) + "yz",
	"wrapped-tail",
	"",
	"",
}

// selSession is a driver at 80×24 with tab 1 focused on a plain session
// showing lines.
func selSession(t *testing.T, lines ...string) (*driver, *apptest.FakeSession) {
	t.Helper()
	fs := apptest.NewFakeSession(lines...)
	d := framedDriver(t, testHosts, fs)
	d.resize(80, 24)
	connectHost(t, d, "h2")
	return d, fs
}

// clipboardText returns the text of the clipboard write msg carries, as
// tea.SetClipboard makes it, or false if msg is not one.
func clipboardText(msg tea.Msg) (string, bool) {
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Type().Name() != "setClipboardMsg" || v.Type().PkgPath() != "charm.land/bubbletea/v2" {
		return "", false
	}
	return v.String(), true
}

// release releases the left button at column x, row y and returns what
// it copied, if anything. The release's command is run here so that the
// clipboard write can be seen.
func (d *driver) release(x, y int) (string, bool) {
	d.t.Helper()
	var cmd tea.Cmd
	d.m, cmd = d.m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd == nil {
		return "", false
	}
	msg := cmd()
	text, ok := clipboardText(msg)
	if !ok {
		d.send(msg)
	}
	return text, ok
}

// drag presses at (x, y), moves to (tx, ty) and returns what the release
// there copied.
func (d *driver) drag(x, y, tx, ty int) (string, bool) {
	d.t.Helper()
	d.clickAt(x, y)
	d.motionTo(tx, ty)
	return d.release(tx, ty)
}

// tab1 is tab 1 of d.
func (d *driver) tab1() *sessionTab { return d.root().tabs[0] }

func TestFR_TAB_012_DragSelectsLinewise(t *testing.T) {
	d, fs := selSession(t, selScreen...)
	// From "beta" on row 0 to "epsil" on row 2: the rest of row 0, all of
	// row 1, the start of row 2.
	if got, ok := d.drag(6, 1, 4, 3); !ok || got != "beta\ngamma delta\nepsil" {
		t.Fatalf("drag copied %q (%v)", got, ok)
	}
	// Backwards gives the same.
	if got, _ := d.drag(4, 3, 6, 1); got != "beta\ngamma delta\nepsil" {
		t.Fatalf("backwards drag copied %q", got)
	}
	// One row, right to left.
	if got, _ := d.drag(8, 2, 2, 2); got != "mma del" {
		t.Fatalf("leftwards drag copied %q", got)
	}
	// Past the area: clamped to its last row and the status bar is not
	// selected.
	d.clickAt(0, 9)
	d.motionTo(40, 23)
	if got, _ := d.release(40, 23); got != "" {
		t.Fatalf("a drag over blank rows copied %q", got)
	}
	if len(fs.Mouse()) != 0 {
		t.Fatalf("a plain session got mouse events %v", fs.Mouse())
	}
}

func TestFR_TAB_012_WideCharacters(t *testing.T) {
	d, _ := selSession(t, selScreen...)
	// Row 3 is a b 日日 本本 語語 c d: from the second cell of 日 to the
	// first of 本 selects both whole.
	if got, _ := d.drag(3, 4, 4, 4); got != "日本" {
		t.Fatalf("copied %q, want 日本", got)
	}
	if got, _ := d.drag(1, 4, 8, 4); got != "b日本語c" {
		t.Fatalf("copied %q, want b日本語c", got)
	}
	// Highlighted whole too.
	d.clickAt(3, 4)
	d.motionTo(4, 4)
	line := strings.Split(d.view(), "\n")[4]
	if !strings.Contains(line, "\x1b[7m日本") {
		t.Fatalf("row 3 drawn as %q, want 日本 in reverse video", line)
	}
}

func TestFR_TAB_012_DoubleClickWord(t *testing.T) {
	d, _ := selSession(t, selScreen...)
	for _, tc := range []struct {
		x, y int
		want string
	}{
		{9, 5, "/var/log"}, // ( ends a word
		{17, 5, "tmp"},
		{2, 1, "alpha"},
		{5, 4, "ab日本語cd"}, // the second cell of 日
	} {
		d.clickAt(tc.x, tc.y)
		if _, ok := d.release(tc.x, tc.y); ok {
			t.Fatal("a single click copied")
		}
		d.pressAfter(100*time.Millisecond, tc.x, tc.y)
		if got, _ := d.release(tc.x, tc.y); got != tc.want {
			t.Errorf("double-click at %d,%d copied %q, want %q", tc.x, tc.y, got, tc.want)
		}
	}
	// On a blank cell: nothing.
	d.clickAt(60, 1)
	d.pressAfter(100*time.Millisecond, 60, 1)
	if got, ok := d.release(60, 1); ok {
		t.Fatalf("double-click on a blank copied %q", got)
	}
	// 401 ms apart is not a double-click.
	d.clickAt(2, 1)
	d.pressAfter(401*time.Millisecond, 2, 1)
	if got, ok := d.release(2, 1); ok {
		t.Fatalf("presses 401 ms apart copied %q", got)
	}
}

func TestFR_TAB_012_TripleClickRow(t *testing.T) {
	d, _ := selSession(t, selScreen...)
	d.clickAt(3, 5)
	d.pressAfter(100*time.Millisecond, 3, 5)
	d.pressAfter(100*time.Millisecond, 3, 5)
	if got, _ := d.release(3, 5); got != "ls -la /var/log(tmp) done" {
		t.Fatalf("triple-click copied %q", got)
	}
	// A fourth press starts over: a plain click, nothing copied.
	d.pressAfter(100*time.Millisecond, 3, 5)
	if got, ok := d.release(3, 5); ok {
		t.Fatalf("a fourth press copied %q", got)
	}
}

func TestFR_TAB_012_ReverseVideo(t *testing.T) {
	d, _ := selSession(t, selScreen...)
	before := d.view()
	d.clickAt(6, 1)
	d.motionTo(4, 2) // still dragging: highlighted already
	lines := strings.Split(d.view(), "\n")
	if !strings.Contains(lines[1], "\x1b[7mbeta") || !strings.Contains(lines[2], "\x1b[7mgamma") {
		t.Fatalf("selection not in reverse video:\n%q\n%q", lines[1], lines[2])
	}
	for _, i := range []int{0, 3, len(lines) - 1} { // header, an unselected row, the status bar
		if strings.Contains(lines[i], "\x1b[7m") != strings.Contains(strings.Split(before, "\n")[i], "\x1b[7m") {
			t.Fatalf("row %d changed by the selection: %q", i, lines[i])
		}
	}
	if withoutStatus(plain(d.view())) != withoutStatus(plain(before)) {
		t.Fatalf("the selection changed the text:\n%s", plain(d.view()))
	}
}

func TestFR_TAB_012_KeyAndClickClear(t *testing.T) {
	d, fs := selSession(t, selScreen...)
	before := d.view()
	d.drag(0, 1, 4, 1)
	if d.tab1().sel == nil || d.view() == before {
		t.Fatal("no selection after a drag")
	}
	d.press('a', "a")
	if d.tab1().sel != nil || len(fs.Keys()) != 1 {
		t.Fatalf("after a key: selection %v, keys %v", d.tab1().sel, fs.Keys())
	}
	d.drag(0, 1, 4, 1)
	d.clickAt(10, 10)
	if s := d.tab1().sel; s == nil || s.moved {
		t.Fatal("a click did not replace the selection")
	}
	d.release(10, 10)
	if d.tab1().sel != nil {
		t.Fatal("a plain click left a selection")
	}
	// A tab switch and a resize clear it too.
	d.drag(0, 1, 4, 1)
	d.focus(0)
	d.focus(1)
	if d.tab1().sel != nil {
		t.Fatal("a tab switch kept the selection")
	}
	d.drag(0, 1, 4, 1)
	d.resize(90, 24)
	if d.tab1().sel != nil {
		t.Fatal("a resize kept the selection")
	}
}

// scrolledSession is a plain session with 50 lines of scrollback above
// its screen: "line 0" … "line 49".
func scrolledSession(t *testing.T) (*driver, *apptest.FakeSession) {
	t.Helper()
	d, fs := selSession(t, shellScreen...)
	var sb []string
	for i := range 50 {
		sb = append(sb, "line "+strconv.Itoa(i))
	}
	fs.SetScrollback(sb)
	return d, fs
}

func TestFR_TAB_012_ScrollbackSelection(t *testing.T) {
	d, _ := scrolledSession(t)
	// 53 lines, 22 rows: the view starts at line 31; three notches up
	// show line 22 on top.
	for range 3 {
		d.wheelAt(5, tea.MouseWheelUp)
	}
	if d.tab1().scroll == nil || d.tab1().scrollTop() != 22 {
		t.Fatalf("wheel did not scroll back: %v", d.tab1().scroll)
	}
	if d.m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("tracking off in scrollback mode")
	}
	d.clickAt(5, 1) // "line 22", column 5
	d.motionTo(6, 2)
	// The wheel moves the view under the selection, which stays on its
	// lines.
	d.wheelAt(5, tea.MouseWheelUp)
	if got, _ := d.release(6, 2); got != "22\nline 23" {
		t.Fatalf("copied %q, want the lines selected before the wheel", got)
	}
	d.clickAt(5, 1)
	d.motionTo(6, 2)
	if got, _ := d.release(6, 2); got != "19\nline 20" {
		t.Fatalf("copied %q, want lines 19 and 20", got)
	}
	// Esc leaves scrollback and clears the selection.
	d.esc()
	if d.tab1().scroll != nil || d.tab1().sel != nil {
		t.Fatal("esc left scrollback mode or the selection")
	}
}

func TestFR_TAB_010_V1_WheelScrollback(t *testing.T) {
	d, fs := scrolledSession(t)
	d.wheelAt(5, tea.MouseWheelDown)
	if d.tab1().scroll != nil {
		t.Fatal("wheel down entered scrollback")
	}
	d.wheelAt(5, tea.MouseWheelUp)
	if d.tab1().scroll == nil || d.tab1().scrollTop() != 28 {
		t.Fatalf("wheel up: scroll %v", d.tab1().scroll)
	}
	if !strings.Contains(plain(d.view()), "scrollback") {
		t.Fatal("the status bar does not show scrollback mode")
	}
	d.wheelAt(5, tea.MouseWheelDown)
	if d.tab1().scroll == nil || d.tab1().scrollTop() != 31 {
		t.Fatalf("wheel down: top %d", d.tab1().scrollTop())
	}
	d.wheelAt(5, tea.MouseWheelDown) // past the last line
	if d.tab1().scroll != nil {
		t.Fatal("wheel down at the bottom stayed in scrollback")
	}
	// Nothing above the screen: the wheel does nothing.
	d2, _ := selSession(t, shellScreen...)
	d2.wheelAt(5, tea.MouseWheelUp)
	if d2.tab1().scroll != nil {
		t.Fatal("wheel up entered an empty scrollback")
	}
	// The remote asked for the mouse: the wheel is its.
	remoteMouse(fs, app.MousePress)
	d.wheelAt(5, tea.MouseWheelUp)
	if d.tab1().scroll != nil || len(fs.Mouse()) != 1 {
		t.Fatalf("with remote mouse: scroll %v, forwarded %v", d.tab1().scroll, fs.Mouse())
	}
}

func TestFR_TAB_013_CopyOnRelease(t *testing.T) {
	d, _ := selSession(t, selScreen...)
	// Trailing spaces go; the wrapped line is copied as its two rows.
	got, ok := d.drag(21, 5, 11, 7)
	want := "done\n" + strings.Repeat("x", 78) + "yz\nwrapped-tail"
	if !ok || got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
	status := lastLine(plain(d.view()))
	if !strings.Contains(status, "copied 98 characters") {
		t.Fatalf("status bar %q", status)
	}
	// One character, singular; then the message goes after 5 s.
	d.clickAt(0, 1)
	d.motionTo(1, 1)
	d.motionTo(0, 1)
	if got, _ := d.release(0, 1); got != "a" || !strings.Contains(lastLine(plain(d.view())), "copied 1 character") {
		t.Fatalf("copied %q, status %q", got, lastLine(plain(d.view())))
	}
	d.clock = d.clock.Add(sftpNoticeHold)
	if strings.Contains(lastLine(plain(d.view())), "copied") {
		t.Fatal("the message stayed after 5 s")
	}
}

// lastLine is the last line of s.
func lastLine(s string) string { return s[strings.LastIndex(s, "\n")+1:] }

func TestFR_TAB_013_EmptySelectionCopiesNothing(t *testing.T) {
	d, _ := selSession(t, selScreen...)
	if got, ok := d.drag(0, 8, 40, 9); ok {
		t.Fatalf("blank rows copied %q", got)
	}
	d.clickAt(3, 1)
	if got, ok := d.release(3, 1); ok {
		t.Fatalf("a plain click copied %q", got)
	}
	if d.tab1().copied != "" {
		t.Fatalf("status %q", d.tab1().copied)
	}
}

func TestFR_TAB_013_SelectionOnEndedTab(t *testing.T) {
	d, fs := selSession(t, selScreen...)
	fs.End(nil)
	d.pump(1)
	if got, _ := d.drag(0, 1, 4, 1); got != "alpha" {
		t.Fatalf("ended tab copied %q", got)
	}
	if !strings.Contains(lastLine(plain(d.view())), "copied 5 characters") {
		t.Fatalf("status %q", lastLine(plain(d.view())))
	}
}

func TestFR_NAV_017_V1_TabClicksFromSession(t *testing.T) {
	for _, state := range []string{"plain", "remote mouse", "scrollback", "ended"} {
		t.Run(state, func(t *testing.T) {
			d, fs := scrolledSession(t)
			switch state {
			case "remote mouse":
				remoteMouse(fs, app.MouseAny)
			case "scrollback":
				d.prefix("[")
			case "ended":
				fs.End(nil)
				d.pump(1)
			}
			_, x := textAt(t, d.view(), "Hosts")
			d.clickAt(x, 0)
			if _, ok := d.release(x, 0); ok || d.root().focus != 0 {
				t.Fatalf("focus %d after clicking Hosts", d.root().focus)
			}
			_, x = textAt(t, d.view(), "1 web-prod")
			d.clickAt(x, 0)
			if d.root().focus != 1 || len(fs.Mouse()) != 0 {
				t.Fatalf("focus %d, forwarded %v", d.root().focus, fs.Mouse())
			}
		})
	}
}

func TestFR_TAB_011_V1_RemoteMouseStillForwarded(t *testing.T) {
	for _, mode := range []app.MouseMode{app.MousePress, app.MouseDrag, app.MouseAny} {
		d, fs := mouseSession(t, mode)
		got, ok := d.drag(0, 1, 5, 2)
		if ok || d.tab1().sel != nil {
			t.Fatalf("mode %v: copied %q, selection %v", mode, got, d.tab1().sel)
		}
		if len(fs.Mouse()) != 3 {
			t.Fatalf("mode %v: forwarded %v", mode, fs.Mouse())
		}
	}
}

func TestSEC_CLIP_006_NoCopyUnderModal(t *testing.T) {
	d, _ := selSession(t, selScreen...)
	d.clickAt(0, 1)
	d.motionTo(4, 1) // a drag in progress when a modal opens
	d.prefix("q")    // the disconnect confirmation
	if len(d.root().modals) != 1 {
		t.Fatal("the disconnect confirmation did not open")
	}
	for _, y := range []int{1, 20, 21, 22} { // the area, and the modal at the bottom
		d.clickAt(0, y)
		d.motionTo(30, y)
		if got, ok := d.release(30, y); ok {
			t.Fatalf("copied %q under a modal", got)
		}
	}
	if d.tab1().sel != nil || d.tab1().copied != "" || strings.Contains(d.view(), "\x1b[7malpha") {
		t.Fatal("a selection or copy under a modal")
	}
}

func TestSEC_CLIP_006_OnlyOnRelease(t *testing.T) {
	d, fs := selSession(t, selScreen...)
	// Output alone, an OSC 52 write by the remote included (the emulator
	// drops it; the fake shows it as text), never copies.
	fs.Output("\x1b]52;c;SGVsbG8=\a", "more output")
	d.pump(1)
	d.clickAt(0, 1)
	d.motionTo(4, 1)
	if d.tab1().copied != "" {
		t.Fatal("copied before the release")
	}
	// Drags over the header and status bar copy nothing.
	for _, y := range []int{0, 23} {
		d.clickAt(30, y)
		d.motionTo(40, y)
		if got, ok := d.release(40, y); ok {
			t.Fatalf("a drag on row %d copied %q", y, got)
		}
	}
	// Nor on the Hosts tab.
	d.focus(0)
	d.clickAt(4, 3)
	d.motionTo(8, 3)
	if got, ok := d.release(8, 3); ok {
		t.Fatalf("a drag on the Hosts tab copied %q", got)
	}
}

func TestSEC_CLIP_006_PayloadIsBase64(t *testing.T) {
	d, _ := selSession(t, selScreen...)
	text, ok := d.drag(0, 4, 9, 4)
	if !ok || text != "ab日本語cd" {
		t.Fatalf("copied %q", text)
	}
	// What Bubble Tea writes for it: OSC 52 with the text in base64.
	seq := ansi.SetSystemClipboard(text)
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	if seq != want || strings.Contains(seq, "日") {
		t.Fatalf("OSC 52 is %q, want %q", seq, want)
	}
}
