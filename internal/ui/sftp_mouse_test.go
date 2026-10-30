package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/ui/sftpview"
)

// The approved layout at 80×24 (sftp-approved.80x24.golden): row 0 is the
// header, row 1 the boxes' top borders, rows 2 … 19 the entries, row 20
// the bottom borders, row 22 the progress line and row 23 the footer.
// Local rows: .., photos, notes.txt, report.pdf; remote rows: ..,
// scripts, .bashrc, backup.tar.gz.

// motionTo moves the pointer to column x, row y with the left button held.
func (d *driver) motionTo(x, y int) {
	d.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// releaseAt releases the left button at column x, row y.
func (d *driver) releaseAt(x, y int) {
	d.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// dragTo presses at (x, y), moves to (tx, ty) and releases there.
func (d *driver) dragTo(x, y, tx, ty int) {
	d.clickAt(x, y)
	d.motionTo(tx, ty)
	d.releaseAt(tx, ty)
}

// nameAt is the terminal cell of name's row in pane side, as drawn.
func nameAt(t *testing.T, d *driver, side app.Side, name string) (x, y int) {
	t.Helper()
	r := focusedView(t, d).Layout().Rows[side]
	for i, l := range strings.Split(plain(d.view()), "\n") {
		cells := []rune(l)
		if i < r.Y || i >= r.Y+r.H || len(cells) < r.X+r.W {
			continue
		}
		// The cursor column, the mark column and a space come first.
		if f := strings.Fields(string(cells[r.X+3 : r.X+r.W])); len(f) > 0 && strings.TrimSuffix(f[0], "/") == name {
			return r.X + 4, i
		}
	}
	t.Fatalf("%q is not in the %v pane:\n%s", name, side, plain(d.view()))
	return 0, 0
}

func TestFR_SFTP_021_ClickFocusesAndSelects(t *testing.T) {
	d, _ := approvedTab(t)
	x, y := nameAt(t, d, app.Local, "notes.txt")
	d.clickAt(x, y)
	if side, _, cur := where(t, d); side != app.Local || cur != "notes.txt" {
		t.Fatalf("after a click on notes.txt: %v pane, cursor %q", side, cur)
	}
	// The name's text is not needed: anywhere on the row selects.
	_, y = nameAt(t, d, app.Remote, "backup.tar.gz")
	d.clickAt(78, y)
	if side, _, cur := where(t, d); side != app.Remote || cur != "backup.tar.gz" {
		t.Fatalf("after a click on backup.tar.gz: %v pane, cursor %q", side, cur)
	}
	// A border or a blank row focuses the pane only.
	lay := focusedView(t, d).Layout()
	for _, c := range [][2]int{{lay.Boxes[0].X, 8}, {10, lay.Boxes[0].Y}, {10, 12}} {
		d.clickAt(lay.Boxes[1].X+1, y) // back to the remote pane
		d.clickAt(c[0], c[1])
		v := focusedView(t, d)
		if v.Focus() != app.Local || v.Cursor(app.Local) != "notes.txt" {
			t.Fatalf("click at %v: %v pane, local cursor %q", c, v.Focus(), v.Cursor(app.Local))
		}
	}
	// The footer does nothing.
	before := d.view()
	d.clickAt(5, lay.Footer.Y)
	if d.view() != before {
		t.Fatalf("a click on the footer changed the view:\n%s", plain(d.view()))
	}
}

func TestFR_SFTP_021_DoubleClickOpensDir(t *testing.T) {
	d, _ := approvedTab(t)
	x, y := nameAt(t, d, app.Remote, "scripts")
	d.clickAt(x, y)
	d.releaseAt(x, y)
	d.pressAfter(100*time.Millisecond, x, y)
	if _, dir, _ := where(t, d); dir != "/home/pi/scripts" {
		t.Fatalf("double-click on scripts/: in %q", dir)
	}
	// On a file, nothing.
	x, y = nameAt(t, d, app.Remote, "run.sh")
	d.doubleClick(x, y)
	if _, dir, cur := where(t, d); dir != "/home/pi/scripts" || cur != "run.sh" {
		t.Fatalf("double-click on run.sh: in %q on %q", dir, cur)
	}
	// On "..", the parent.
	x, y = nameAt(t, d, app.Remote, "..")
	d.doubleClick(x, y)
	if _, dir, cur := where(t, d); dir != "/home/pi" || cur != "scripts" {
		t.Fatalf("double-click on ..: in %q on %q", dir, cur)
	}
	// Two quick presses on the same row of different panes are two clicks.
	lx, ly := nameAt(t, d, app.Local, "photos")
	rx, ry := nameAt(t, d, app.Remote, "scripts")
	if ly != ry {
		t.Fatalf("photos on row %d, scripts on row %d", ly, ry)
	}
	d.clickAt(lx, ly)
	d.pressAfter(100*time.Millisecond, rx, ry)
	v := focusedView(t, d)
	if v.Dir(app.Local) != "/Users/me/Downloads" || v.Dir(app.Remote) != "/home/pi" || v.Cursor(app.Remote) != "scripts" {
		t.Fatalf("presses in two panes opened a directory: %q, %q", v.Dir(app.Local), v.Dir(app.Remote))
	}
	// More than 400 ms apart: no double-click.
	d.pressAfter(401*time.Millisecond, rx, ry)
	if _, dir, _ := where(t, d); dir != "/home/pi" {
		t.Fatalf("presses 401 ms apart opened %q", dir)
	}
}

func TestFR_SFTP_021_WheelScrollsPaneUnderPointer(t *testing.T) {
	b := approvedBrowser(1)
	var many []app.Entry
	for i := range 40 {
		many = append(many, file(fmt.Sprintf("f%02d", i), 1))
	}
	b.SetDir(app.Local, "/Users/me/Downloads", "~/Downloads", many...)
	d, _ := sftpOpen(t, b)
	lay := focusedView(t, d).Layout()
	x := lay.Rows[app.Local].X + 2
	d.send(tea.MouseWheelMsg{X: x, Y: 10, Button: tea.MouseWheelDown})
	v := focusedView(t, d)
	if v.Focus() != app.Remote {
		t.Fatalf("the wheel moved the focus to %v", v.Focus())
	}
	// ".." and f00 … f39: three rows down, the first shown is f02.
	if got := rowNames(t, d, app.Local); got[0] != "f02" || v.Cursor(app.Local) != "f02" {
		t.Fatalf("after one notch: rows from %q, cursor %q", got[0], v.Cursor(app.Local))
	}
	d.send(tea.MouseWheelMsg{X: x, Y: 10, Button: tea.MouseWheelDown})
	if got := rowNames(t, d, app.Local); got[0] != "f05" {
		t.Fatalf("after two notches: rows from %q", got[0])
	}
	d.send(tea.MouseWheelMsg{X: x, Y: 10, Button: tea.MouseWheelUp})
	if got := rowNames(t, d, app.Local); got[0] != "f02" {
		t.Fatalf("after a notch up: rows from %q", got[0])
	}
	for range 20 {
		d.send(tea.MouseWheelMsg{X: x, Y: 10, Button: tea.MouseWheelDown})
	}
	if got := rowNames(t, d, app.Local); got[len(got)-1] != "f39" || len(got) != lay.Rows[0].H {
		t.Fatalf("at the end: rows %q", got)
	}
	// The remote pane did not move, and the wheel elsewhere does nothing.
	before := d.view()
	d.send(tea.MouseWheelMsg{X: 3, Y: lay.Footer.Y, Button: tea.MouseWheelUp})
	d.send(tea.MouseWheelMsg{X: 3, Y: 0, Button: tea.MouseWheelUp})
	if d.view() != before || rowNames(t, d, app.Remote)[0] != ".." {
		t.Fatalf("the wheel outside the panes changed the view:\n%s", plain(d.view()))
	}
}

func TestFR_SFTP_021_TabBarClick(t *testing.T) {
	d, _ := approvedTab(t)
	y, x := rowOf(t, d, "Hosts")
	if y != 0 {
		t.Fatalf("Hosts on row %d", y)
	}
	d.clickAt(x, y)
	if d.root().focus != 0 {
		t.Fatalf("focus = %d after a click on Hosts", d.root().focus)
	}
	_, x = rowOf(t, d, "sftp: raspberrypi4")
	d.clickAt(x, 0)
	if d.root().focusedSFTP() == nil {
		t.Fatal("a click on the SFTP tab did not focus it")
	}
	_, x = rowOf(t, d, "1 raspberrypi4")
	d.clickAt(x, 0)
	if d.root().focused() == nil {
		t.Fatal("a click on the session tab from the SFTP tab did not focus it")
	}
}

func TestFR_SFTP_021_ProgressLineClickOpensTransfers(t *testing.T) {
	d, _ := approvedTab(t)
	lay := focusedView(t, d).Layout()
	d.clickAt(3, lay.Footer.Y)
	if focusedView(t, d).ShowingTransfers() {
		t.Fatal("a click on the footer opened the transfers view")
	}
	d.clickAt(3, lay.Progress.Y)
	if !focusedView(t, d).ShowingTransfers() {
		t.Fatal("a click on the progress line did not open the transfers view")
	}
	// Mouse mode stays on in the transfers view; t goes back.
	if mm := d.m.View().MouseMode; mm != tea.MouseModeCellMotion {
		t.Fatalf("mode in the transfers view = %v", mm)
	}
	d.keys("t")
	if focusedView(t, d).ShowingTransfers() {
		t.Fatal("t did not close the transfers view")
	}
}

func TestFR_SFTP_022_DragCopiesMarked(t *testing.T) {
	d, b := approvedTab(t)
	// Mark scripts and backup.tar.gz in the remote pane.
	d.keys("j", "space", "j", "space")
	sx, sy := nameAt(t, d, app.Remote, "backup.tar.gz")
	tx, ty := nameAt(t, d, app.Local, "photos") // a directory row: still the pane's directory
	d.dragTo(sx, sy, tx, ty)
	want := apptest.Copy{From: app.Remote, SrcDir: "/home/pi", Names: []string{"scripts", "backup.tar.gz"}, DstDir: "/Users/me/Downloads"}
	if got := b.Copies(); len(got) != 1 || !copyEqual(got[0], want) {
		t.Fatalf("copies = %+v", got)
	}
	v := focusedView(t, d)
	if len(v.Marked(app.Remote)) != 0 || v.Dragging() || strings.HasPrefix(progressLine(d), "drag:") {
		t.Fatalf("after the drop: marks %q, dragging %v, progress %q", v.Marked(app.Remote), v.Dragging(), progressLine(d))
	}

	// An unmarked entry carries itself only, and the marks stay.
	d.keys("g", "j", "space") // mark scripts
	sx, sy = nameAt(t, d, app.Remote, ".bashrc")
	d.dragTo(sx, sy, 5, 15)
	want = apptest.Copy{From: app.Remote, SrcDir: "/home/pi", Names: []string{".bashrc"}, DstDir: "/Users/me/Downloads"}
	if got := b.Copies(); len(got) != 2 || !copyEqual(got[1], want) {
		t.Fatalf("copies = %+v", got)
	}
	if got := focusedView(t, d).Marked(app.Remote); len(got) != 1 || got[0] != "scripts" {
		t.Fatalf("marks = %q", got)
	}

	// Upload: local to remote.
	sx, sy = nameAt(t, d, app.Local, "report.pdf")
	d.dragTo(sx, sy, 60, 3)
	want = apptest.Copy{From: app.Local, SrcDir: "/Users/me/Downloads", Names: []string{"report.pdf"}, DstDir: "/home/pi"}
	if got := b.Copies(); len(got) != 3 || !copyEqual(got[2], want) {
		t.Fatalf("copies = %+v", got)
	}

	// Dragging ".." does nothing.
	sx, sy = nameAt(t, d, app.Local, "..")
	d.clickAt(sx, sy)
	d.motionTo(60, 5)
	if focusedView(t, d).Dragging() || strings.HasPrefix(progressLine(d), "drag:") {
		t.Fatal("dragging .. shows a hint")
	}
	d.releaseAt(60, 5)
	if len(b.Copies()) != 3 {
		t.Fatalf("dragging .. copied: %+v", b.Copies())
	}
}

func TestFR_SFTP_022_DragHintGolden(t *testing.T) {
	d, _ := approvedTab(t)
	d.keys("tab", "j", "space", "space", "space") // mark photos, notes.txt, report.pdf
	sx, sy := nameAt(t, d, app.Local, "notes.txt")
	d.clickAt(sx, sy)
	d.motionTo(sx, sy) // still on the pressed row: no drag yet
	if focusedView(t, d).Dragging() {
		t.Fatal("motion on the pressed row started a drag")
	}
	d.motionTo(sx+2, sy+1) // still the local pane
	if over, ok := focusedView(t, d).DragTarget(); !ok || over != app.Local {
		t.Fatalf("over %v, %v; want the local pane", over, ok)
	}
	d.motionTo(60, 10)
	const hint = "drag: copy 3 items to Remote: /home/pi · release over the other pane"
	if got := progressLine(d); got != hint {
		t.Fatalf("progress line = %q, want %q", got, hint)
	}
	if over, ok := focusedView(t, d).DragTarget(); !ok || over != app.Remote {
		t.Fatalf("over %v, %v; want the remote pane", over, ok)
	}
	golden(t, "sftp-drag", d.m)
	// The pane under the pointer has its border highlighted: a blank row
	// is drawn the same but styled differently.
	// (golden leaves the terminal at 120×40.)
	over := strings.Split(d.view(), "\n")[10]
	d.motionTo(5, focusedView(t, d).Layout().Progress.Y)
	if _, ok := focusedView(t, d).DragTarget(); ok {
		t.Fatal("a pane is highlighted with the pointer on the progress line")
	}
	off := strings.Split(d.view(), "\n")[10]
	if over == off || plain(over) != plain(off) {
		t.Fatalf("border not highlighted: %q and %q", over, off)
	}
}

func TestFR_SFTP_022_ReleaseSamePaneCancels(t *testing.T) {
	d, b := approvedTab(t)
	lay := focusedView(t, d).Layout()
	sx, sy := nameAt(t, d, app.Remote, "backup.tar.gz")
	for _, at := range []struct {
		name string
		x, y int
	}{
		{"same pane", 60, 15},
		{"same pane's border", lay.Boxes[1].X + lay.Boxes[1].W - 1, 6},
		{"header", 30, 0},
		{"progress line", 30, lay.Progress.Y},
		{"footer", 30, lay.Footer.Y},
	} {
		d.clickAt(sx, sy)
		d.motionTo(at.x, at.y)
		if !strings.HasPrefix(progressLine(d), "drag: copy 1 item to Local: ~/Downloads") {
			t.Fatalf("%s: progress line %q", at.name, progressLine(d))
		}
		d.releaseAt(at.x, at.y)
		if len(b.Copies()) != 0 {
			t.Fatalf("%s: released there copied %+v", at.name, b.Copies())
		}
		if strings.HasPrefix(progressLine(d), "drag:") || focusedView(t, d).Dragging() {
			t.Fatalf("%s: the hint stays: %q", at.name, progressLine(d))
		}
		if focusedView(t, d).ShowingTransfers() || d.root().focusedSFTP() == nil {
			t.Fatalf("%s: a release acted as a click", at.name)
		}
	}
}

func TestFR_SFTP_022_ClickIsNotDrag(t *testing.T) {
	d, b := approvedTab(t)
	x, y := nameAt(t, d, app.Local, "notes.txt")
	// Press and release on the same row, with motion along the row.
	d.clickAt(x, y)
	d.motionTo(x+10, y)
	d.releaseAt(x+10, y)
	if len(b.Copies()) != 0 || focusedView(t, d).Dragging() {
		t.Fatalf("a click copied: %+v", b.Copies())
	}
	if side, _, cur := where(t, d); side != app.Local || cur != "notes.txt" {
		t.Fatalf("the click: %v pane on %q", side, cur)
	}
	// Motion without the button (all-motion terminals) starts nothing.
	d.clickAt(x, y)
	d.send(tea.MouseMotionMsg{X: 60, Y: 5})
	if focusedView(t, d).Dragging() {
		t.Fatal("motion without the button started a drag")
	}
	d.releaseAt(60, 5)
	// Once off the row, coming back keeps the drag; releasing there is
	// over the same pane and copies nothing.
	d.clickAt(x, y)
	d.motionTo(x, y+1)
	d.motionTo(x, y)
	if !focusedView(t, d).Dragging() {
		t.Fatal("coming back to the row ended the drag")
	}
	d.releaseAt(x, y)
	if len(b.Copies()) != 0 {
		t.Fatalf("copies = %+v", b.Copies())
	}
}

func TestARCH_UI_006_M3_SftpCellMotion(t *testing.T) {
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
		{"sftp tab", mouseState{sftp: true}, cell},
		{"sftp tab, modal", mouseState{sftp: true, modal: true}, cell},
		{"sftp tab, remote mode of another tab", mouseState{sftp: true, requested: all}, cell},
		{"sftp tab, stale requested none", mouseState{sftp: true, requested: none}, cell},
	} {
		if got := mouseModeFor(tc.in); got != tc.want {
			t.Errorf("%s: mode %v, want %v", tc.name, got, tc.want)
		}
	}

	// Through the root model: shell tab, SFTP tab, shell tab; then the
	// SFTP tab's states.
	d, b := approvedTab(t)
	mode := func() tea.MouseMode { return d.m.View().MouseMode }
	if mode() != cell {
		t.Fatalf("sftp tab: mode %v", mode())
	}
	d.focus(1)
	if d.root().focused() == nil || mode() != none {
		t.Fatalf("plain shell tab: mode %v", mode())
	}
	d.focus(2)
	if mode() != cell {
		t.Fatalf("back on the sftp tab: mode %v", mode())
	}
	d.keys("t")
	if !focusedView(t, d).ShowingTransfers() || mode() != cell {
		t.Fatalf("transfers view: mode %v", mode())
	}
	d.keys("t", "?")
	if len(d.root().modals) != 1 || mode() != cell {
		t.Fatalf("modal: mode %v", mode())
	}
	d.keys("esc")
	disconnect(t, d, b)
	if !sftpTabAt(t, d, 2).disconnected || mode() != cell {
		t.Fatalf("disconnected: mode %v", mode())
	}
	d.focus(1)
	if mode() != none {
		t.Fatalf("shell tab again: mode %v", mode())
	}
}

func TestARCH_UI_007_M3_LayoutMatchesView(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{80, 24}, {120, 40}, {81, 24}} {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			d, b := approvedTab(t)
			d.send(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			lay := sftpview.Layout(sz.w, sz.h)
			if lay != focusedView(t, d).Layout() {
				t.Fatal("the view's layout is not sftpview.Layout")
			}
			lines := strings.Split(plain(d.view()), "\n")
			cell := func(x, y int) string {
				r := []rune(lines[y])
				if x >= len(r) {
					return ""
				}
				return string(r[x])
			}
			for side, box := range lay.Boxes {
				corners := map[[2]int]string{
					{box.X, box.Y}:                         "┌",
					{box.X + box.W - 1, box.Y}:             "┐",
					{box.X, box.Y + box.H - 1}:             "└",
					{box.X + box.W - 1, box.Y + box.H - 1}: "┘",
				}
				for at, want := range corners {
					if got := cell(at[0], at[1]); got != want {
						t.Errorf("box %d: %q at %v, want %q", side, got, at, want)
					}
				}
			}
			if !strings.HasPrefix(strings.TrimSpace(lines[lay.Footer.Y]), "tab switch pane") {
				t.Errorf("footer row %d = %q", lay.Footer.Y, lines[lay.Footer.Y])
			}
			// Each name is clicked where the view draws it.
			for _, side := range []app.Side{app.Local, app.Remote} {
				for _, name := range rowNames(t, d, side) {
					name = strings.TrimSuffix(name, "/")
					x, y := nameAt(t, d, side, name)
					d.clickAt(x, y)
					if s, _, cur := where(t, d); s != side || cur != name {
						t.Fatalf("click on %q at %d,%d: %v pane on %q", name, x, y, s, cur)
					}
				}
			}
			// A press on a local row, motion over the remote pane and a
			// release there queue one copy.
			x, y := nameAt(t, d, app.Local, "notes.txt")
			d.dragTo(x, y, lay.Rows[1].X+3, lay.Rows[1].Y+lay.Rows[1].H-1)
			if got := b.Copies(); len(got) != 1 || got[0].Names[0] != "notes.txt" || got[0].DstDir != "/home/pi" {
				t.Fatalf("copies = %+v", got)
			}
			// The progress line is the row Layout says.
			d.clickAt(0, lay.Progress.Y)
			if !focusedView(t, d).ShowingTransfers() {
				t.Fatalf("a click on row %d did not open the transfers view", lay.Progress.Y)
			}
		})
	}
}

func TestARCH_UI_007_M3_MotionIgnoredElsewhere(t *testing.T) {
	gesture := func(d *driver, x, y int) {
		d.send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
		d.send(tea.MouseMotionMsg{X: x + 30, Y: y + 1, Button: tea.MouseLeft})
		d.send(tea.MouseReleaseMsg{X: x + 30, Y: y + 1, Button: tea.MouseLeft})
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, d *driver)
	}{
		{"host list", func(t *testing.T, d *driver) { d.focus(0) }},
		{"host form", func(t *testing.T, d *driver) {
			d.focus(0)
			d.press('a', "a")
			if !d.inForm() {
				t.Fatal("no form")
			}
		}},
		{"modal on the Hosts tab", func(t *testing.T, d *driver) {
			d.focus(0)
			d.press('d', "d")
			if len(d.root().modals) != 1 {
				t.Fatal("no modal")
			}
		}},
		{"modal on the sftp tab", func(t *testing.T, d *driver) { d.keys("?") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b := approvedTab(t)
			tc.setup(t, d)
			before := d.view()
			gesture(d, 5, 4)
			gesture(d, 45, 4)
			if d.view() != before || len(b.Copies()) != 0 {
				t.Fatalf("motion and release changed the view or copied:\n%s", plain(d.view()))
			}
		})
	}

	// A drag under way when a modal opens ends without copying.
	d, b := approvedTab(t)
	x, y := nameAt(t, d, app.Remote, "backup.tar.gz")
	d.clickAt(x, y)
	d.motionTo(10, 5)
	d.keys("?")
	d.motionTo(12, 6)
	d.releaseAt(12, 6)
	d.keys("esc")
	if len(b.Copies()) != 0 || focusedView(t, d).Dragging() || strings.HasPrefix(progressLine(d), "drag:") {
		t.Fatalf("a drag under a modal: copies %+v, progress %q", b.Copies(), progressLine(d))
	}
}
