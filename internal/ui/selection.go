package ui

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
)

// sshelf's own text selection in session tabs (FR-TAB-012, FR-TAB-013,
// SEC-CLIP-006, D-024, ADR-014 v1 amendment). The text always comes from
// the session's own lines (its snapshot or scrollback), never from the
// rendered frame, so nothing of sshelf's header, status bar or modals can
// be copied.

// point is a cell of a session's text: column x of line y. On the live
// screen y is the screen row; in scrollback mode it is the line counted
// from the session's first line (top + Dropped()), so a selection stays on
// its text while the view moves.
type point struct{ x, y int }

// selection is a session tab's selection.
type selection struct {
	anchor, end point
	scroll      bool // made in scrollback mode (lines are absolute)
	dragging    bool // a plain press: the left button is held
	moved       bool // the pointer moved since the press
}

// span returns the selection's first and last cells in reading order.
func (s *selection) span() (a, b point) {
	a, b = s.anchor, s.end
	if b.y < a.y || b.y == a.y && b.x < a.x {
		a, b = b, a
	}
	return a, b
}

// cols returns the columns l to r (inclusive) the selection covers on
// line y of a session w cells wide: linewise, like a terminal.
func (s *selection) cols(y, w int) (l, r int, ok bool) {
	a, b := s.span()
	if y < a.y || y > b.y {
		return 0, 0, false
	}
	l, r = 0, w-1
	if y == a.y {
		l = a.x
	}
	if y == b.y {
		r = min(b.x, w-1)
	}
	return l, r, l <= r
}

// multiClick counts left presses on the same row, each within
// components.DoubleClickInterval of the previous one: 1, 2 (a
// double-click) or 3 (a triple-click); a fourth starts over.
type multiClick struct {
	at  time.Time
	row int
	n   int
}

// press records a left press on row at now and returns its count.
func (c *multiClick) press(now time.Time, row int) int {
	gap := now.Sub(c.at)
	if c.n > 0 && c.n < 3 && row == c.row && gap >= 0 && gap <= components.DoubleClickInterval {
		c.n++
	} else {
		c.n = 1
	}
	c.at, c.row = now, row
	return c.n
}

// cellLine parses a session line (text with SGR styles) into w cells, with
// the emulator's width method, so double-width characters take two.
func cellLine(line string, w int) uv.ScreenBuffer {
	sb := uv.NewScreenBuffer(w, 1)
	sb.Method = ansi.GraphemeWidth
	uv.NewStyledString(line).Draw(sb, uv.Rect(0, 0, w, 1))
	return sb
}

// widen moves l left onto the first cell of a double-width character it
// falls inside, so the character is selected whole.
func widen(sb uv.ScreenBuffer, l int) int {
	for l > 0 {
		if c := sb.CellAt(l, 0); c == nil || c.Width != 0 {
			break
		}
		l--
	}
	return l
}

// cellsText is the text of cells l to r of sb, without styles and
// trailing spaces; a double-width character appears once.
func cellsText(sb uv.ScreenBuffer, l, r int) string {
	var b strings.Builder
	for x := widen(sb, l); x <= r && x < sb.Width(); x++ {
		c := sb.CellAt(x, 0)
		switch {
		case c == nil || c.Content == "" && c.Width > 0:
			b.WriteByte(' ')
		case c.Width == 0:
			// the second cell of a double-width character
		default:
			b.WriteString(c.Content)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// highlight draws cells l to r of line in reverse video over its own
// styles (FR-TAB-012).
func highlight(line string, w, l, r int) string {
	sb := cellLine(line, w)
	for x := widen(sb, l); x <= r && x < w; x++ {
		c := sb.CellAt(x, 0)
		if c == nil || c.Width == 0 {
			continue
		}
		cc := *c
		cc.Style.Attrs |= uv.AttrReverse
		sb.SetCell(x, 0, &cc)
	}
	return sb.Line(0).Render()
}

// wordDelims are the characters that end a word for a double-click,
// besides blanks.
const wordDelims = "()[]{}<>\"'`|,;"

// isWordCell reports whether c is part of a word.
func isWordCell(c *uv.Cell) bool {
	if c == nil || c.Width == 0 {
		return c != nil // the second cell of a double-width character
	}
	return strings.TrimSpace(c.Content) != "" && !strings.Contains(wordDelims, c.Content)
}

// wordAt returns the columns of the word around column x of line, or
// false on a blank or delimiter cell.
func wordAt(line string, w, x int) (l, r int, ok bool) {
	sb := cellLine(line, w)
	x = widen(sb, x)
	if x >= w || !isWordCell(sb.CellAt(x, 0)) {
		return 0, 0, false
	}
	l, r = x, x
	for l > 0 && isWordCell(sb.CellAt(l-1, 0)) {
		l--
	}
	for r+1 < w && isWordCell(sb.CellAt(r+1, 0)) {
		r++
	}
	return widen(sb, l), r, true
}

// areaCols is the session area's width, or line's own width before the
// terminal size is known.
func (m Model) areaCols(line string) int {
	if m.width > 0 {
		return m.width
	}
	return max(ansi.StringWidth(line), 1)
}

// viewLines returns the lines the session area of t shows now, from snap,
// and the line number of the top one: 0 on the live screen, top +
// Dropped() in scrollback mode. sessionFrame and the selection share it.
func (m Model) viewLines(t *sessionTab, snap app.Snapshot) ([]string, int) {
	if t.scroll == nil {
		return snap.Lines, 0
	}
	rows := len(snap.Lines)
	if m.height > 0 {
		rows = max(m.height-2, 1)
	}
	// The view keeps its top line while output arrives (FR-TAB-010).
	_, total := t.sess.Scrollback(0, 0)
	top := min(t.scrollTop(), max(total-rows, 0))
	lines, _ := t.sess.Scrollback(top, rows)
	return lines, top + t.sess.Dropped()
}

// selectionValid reports whether t's selection belongs to the view t
// shows now (it was made in the same mode).
func (t *sessionTab) selectionValid() bool {
	return t.sel != nil && t.sel.scroll == (t.scroll != nil)
}

// highlightLines draws t's selection over lines, whose top line is
// origin.
func (m Model) highlightLines(t *sessionTab, lines []string, origin int) []string {
	if !t.selectionValid() || len(m.modals) > 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line
		w := m.areaCols(line)
		if l, r, ok := t.sel.cols(origin+i, w); ok {
			out[i] = highlight(line, w, l, r)
		}
	}
	return out
}

// selectionText is the text of t's selection: one line per row, each
// without its trailing spaces, joined with "\n" (FR-TAB-013).
func (m Model) selectionText(t *sessionTab) string {
	if !t.selectionValid() {
		return ""
	}
	a, b := t.sel.span()
	var lines []string
	first := a.y
	if t.sel.scroll {
		d := t.sess.Dropped()
		from := max(a.y-d, 0) // lines that left the scrollback are gone
		first = from + d
		lines, _ = t.sess.Scrollback(from, b.y-first+1)
	} else {
		snap := t.sess.Snapshot().Lines
		lines = snap[min(max(a.y, 0), len(snap)):min(max(b.y+1, 0), len(snap))]
	}
	rows := make([]string, 0, len(lines))
	for i, line := range lines {
		w := m.areaCols(line)
		if l, r, ok := t.sel.cols(first+i, w); ok {
			rows = append(rows, cellsText(cellLine(line, w), l, r))
		}
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}

// clearSelections drops every tab's selection: a key press, a tab switch
// or a resize (FR-TAB-012).
func (m Model) clearSelections() {
	for _, t := range m.tabs {
		t.sel = nil
	}
}

// remoteWantsMouse reports whether the mouse in t's session area goes to
// the remote (FR-TAB-011 v1 revision): a live session, not in scrollback
// mode, whose remote has mouse reporting on.
func (m Model) remoteWantsMouse(t *sessionTab) bool {
	return !t.done && t.scroll == nil && t.sess.Snapshot().Mouse != app.MouseNone
}

// sessionMouse handles a mouse event on the focused session tab t with no
// modal open (design §9.5, routing from v1): the header row takes tab
// clicks, the status bar nothing, and the session area goes to the remote
// while it asked for the mouse, or to sshelf's selection otherwise.
func (m Model) sessionMouse(t *sessionTab, msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, ok := msg.(tea.MouseMsg)
	if !ok {
		return m, nil
	}
	ms := mm.Mouse()
	area := m.sessionArea()
	if t.sel != nil && t.sel.dragging {
		switch msg.(type) {
		case tea.MouseMotionMsg, tea.MouseReleaseMsg:
			// A drag keeps going over the frame, clamped to the area.
			return m.selectMouse(t, msg, ms)
		}
	}
	if ms.Y < area.Y {
		if c, ok := msg.(tea.MouseClickMsg); ok && c.Button == tea.MouseLeft {
			return m.tabBarClick(ms.X) // FR-NAV-017 v1 revision, D-028
		}
		return m, nil
	}
	if ms.Y >= area.Y+area.H || ms.X >= area.W {
		return m, nil // the status bar, or outside the area
	}
	if m.remoteWantsMouse(t) {
		m.forwardMouse(t, msg)
		return m, nil
	}
	return m.selectMouse(t, msg, ms)
}

// selectMouse runs sshelf's selection for a mouse event in t's session
// area (FR-TAB-012, FR-TAB-013) and the wheel (FR-TAB-010 v1 revision).
func (m Model) selectMouse(t *sessionTab, msg tea.Msg, ms tea.Mouse) (tea.Model, tea.Cmd) {
	area := m.sessionArea()
	x := min(max(ms.X-area.X, 0), max(area.W-1, 0))
	row := min(max(ms.Y-area.Y, 0), max(area.H-1, 0))
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m.wheelScroll(t, ms.Button)
		return m, nil
	case tea.MouseClickMsg:
		if ms.Button != tea.MouseLeft {
			return m, nil
		}
		t.sel = nil
		lines, origin := m.viewLines(t, t.sess.Snapshot())
		p := point{x, origin + row}
		scroll := t.scroll != nil
		switch m.selClicks.press(m.now(), ms.Y) {
		case 2:
			if row < len(lines) {
				if l, r, ok := wordAt(lines[row], m.areaCols(lines[row]), x); ok {
					t.sel = &selection{anchor: point{l, p.y}, end: point{r, p.y}, scroll: scroll}
				}
			}
		case 3:
			t.sel = &selection{anchor: point{0, p.y}, end: point{area.W - 1, p.y}, scroll: scroll}
		default:
			t.sel = &selection{anchor: p, end: p, scroll: scroll, dragging: true}
		}
	case tea.MouseMotionMsg:
		if t.sel == nil || !t.sel.dragging {
			return m, nil
		}
		_, origin := m.viewLines(t, t.sess.Snapshot())
		if p := (point{x, origin + row}); p != t.sel.end {
			t.sel.end, t.sel.moved = p, true
		}
	case tea.MouseReleaseMsg:
		if t.sel == nil {
			return m, nil
		}
		if t.sel.dragging {
			t.sel.dragging = false
			if !t.sel.moved {
				t.sel = nil // a plain click selects nothing
				return m, nil
			}
		}
		return m.copySelection(t)
	}
	return m, nil
}

// copySelection copies t's selection with an OSC 52 write and shows how
// much was copied (FR-TAB-013). An empty text copies nothing.
func (m Model) copySelection(t *sessionTab) (tea.Model, tea.Cmd) {
	text := m.selectionText(t)
	if text == "" {
		return m, nil
	}
	t.copied = "copied " + plural(utf8.RuneCountInString(text), "character")
	t.copiedAt = m.now()
	return m, tea.SetClipboard(text)
}

// wheelNotch is how many lines one wheel notch scrolls.
const wheelNotch = 3

// wheelScroll enters and moves scrollback mode with the wheel
// (FR-TAB-010 v1 revision): up enters it when there are lines above the
// screen, down past the last line leaves it.
func (m Model) wheelScroll(t *sessionTab, b tea.MouseButton) {
	rows := m.areaRows(t)
	_, total := t.sess.Scrollback(0, 0)
	last := max(total-rows, 0)
	switch b {
	case tea.MouseWheelUp:
		if t.scroll == nil {
			if last == 0 {
				return
			}
			t.sel = nil
			t.setScrollTop(last)
		}
		t.setScrollTop(max(min(t.scrollTop(), last)-wheelNotch, 0))
	case tea.MouseWheelDown:
		if t.scroll == nil {
			return
		}
		top := min(t.scrollTop(), last)
		if top >= last {
			t.scroll, t.sel = nil, nil
			return
		}
		t.setScrollTop(min(top+wheelNotch, last))
	}
}
