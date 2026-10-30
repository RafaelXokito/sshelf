package sftpview

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
)

// WheelRows is how many rows one wheel notch scrolls a pane
// (FR-SFTP-021).
const WheelRows = 3

// press is the last left press on a row (design §9.6): where it was and
// which entry it hit. The pressed entry is what a drag carries.
type press struct {
	side   app.Side
	idx    int // index in the pane's rows
	name   string
	parent bool // the ".." row: dragging it does nothing (FR-SFTP-022)
}

// drag is the mouse state of the tab: the press a drag may start from,
// and whether the pointer has left that row with the button held.
type drag struct {
	from     *press
	active   bool     // the pointer left the pressed row: the hint is shown
	over     app.Side // the pane under the pointer, if overPane
	overPane bool
}

// DragTarget is the pane whose border a drag highlights, and whether a
// drag is under way over a pane (FR-SFTP-022).
func (m Model) DragTarget() (app.Side, bool) {
	return m.drag.over, m.drag.active && m.drag.overPane
}

// Dragging reports whether a drag is under way.
func (m Model) Dragging() bool { return m.drag.active }

// CancelDrag ends a press or drag without copying, such as when a modal
// takes the mouse.
func (m Model) CancelDrag() Model {
	m.drag = drag{}
	return m
}

// paneAt is the pane whose box holds the cell at column x, row y, in the
// frame's coordinates (Layout's, the header row included).
func (m Model) paneAt(lay Regions, x, y int) (app.Side, bool) {
	for _, side := range []app.Side{app.Local, app.Remote} {
		if lay.Boxes[side].Contains(x, y) {
			return side, true
		}
	}
	return 0, false
}

// rowAt is the index in pane side's rows drawn at row y, if one is.
func (m Model) rowAt(lay Regions, side app.Side, x, y int) (int, bool) {
	if !lay.Rows[side].Contains(x, y) {
		return 0, false
	}
	p := m.panes[side]
	idx := p.top + y - lay.Rows[side].Y
	return idx, idx < len(p.rows)
}

// click handles a left press at column x, row y of the frame
// (FR-SFTP-021, design §9.5): on a row it focuses the pane and moves the
// cursor there, and a double-click on the same row is enter; on a pane's
// border or blank rows it focuses the pane; on the progress line it
// opens the transfers view; on the footer it does nothing. The root
// model handles the header.
func (m Model) click(c components.ClickMsg) (Model, tea.Cmd) {
	last := m.lastPress
	m.drag, m.lastPress = drag{}, nil
	lay := m.Layout()
	if lay.Progress.Contains(c.X, c.Y) {
		m.showTransfers = true // as t (FR-SFTP-015)
		return m, nil
	}
	if m.showTransfers {
		return m, nil
	}
	side, ok := m.paneAt(lay, c.X, c.Y)
	if !ok {
		return m, nil
	}
	m.endFilter()
	m.focus = side
	idx, ok := m.rowAt(lay, side, c.X, c.Y)
	if !ok || (m.disconnected && side == app.Remote) {
		return m, nil // the border or a blank row: focus only
	}
	p := m.panes[side]
	p.cursor = idx
	r := p.rows[idx]
	pr := &press{side: side, idx: idx, name: r.e.Name, parent: r.parent}
	m.drag.from, m.lastPress = pr, pr
	if c.Double && last != nil && *last == *pr {
		m.expireMessage()
		return m.open(p)
	}
	return m, nil
}

// endFilter gives the keyboard back to the panes, keeping the filter's
// text, as enter does (FR-SFTP-008).
func (m Model) endFilter() {
	if p := m.panes[m.focus]; p.filtering {
		p.filtering = false
		p.filter.Blur()
	}
}

// wheel scrolls the pane under the pointer by WheelRows rows a notch,
// keeping its cursor on screen and the focus where it is (FR-SFTP-021).
func (m Model) wheel(w components.WheelMsg) Model {
	if m.showTransfers {
		return m
	}
	lay := m.Layout()
	side, ok := m.paneAt(lay, w.X, w.Y)
	if !ok {
		return m
	}
	p := m.panes[side]
	vis := m.visible(side)
	p.top = min(max(p.top+w.Delta*WheelRows, 0), max(len(p.rows)-vis, 0))
	p.cursor = min(max(p.cursor, p.top), p.top+vis-1)
	p.cursor = min(p.cursor, max(len(p.rows)-1, 0))
	return m
}

// motion follows the pointer with the left button held (FR-SFTP-022):
// the first motion off the pressed row starts a drag; from then on the
// pane under the pointer is highlighted.
func (m Model) motion(ms tea.Mouse) Model {
	from := m.drag.from
	if from == nil || ms.Button != tea.MouseLeft {
		return m
	}
	lay := m.Layout()
	if !m.drag.active {
		if idx, ok := m.rowAt(lay, from.side, ms.X, ms.Y); ok && idx == from.idx {
			return m // still on the pressed row: a click so far
		}
		if from.parent {
			m.drag.from = nil // dragging ".." does nothing
			return m
		}
		m.drag.active = true
	}
	m.drag.over, m.drag.overPane = m.paneAt(lay, ms.X, ms.Y)
	return m
}

// release ends a press (FR-SFTP-022): a drag released over the other
// pane's box copies the dragged entries into that pane's directory;
// anything else copies nothing.
func (m Model) release(ms tea.Mouse) (Model, tea.Cmd) {
	d := m.drag
	m.drag = drag{}
	if !d.active {
		return m, nil
	}
	side, ok := m.paneAt(m.Layout(), ms.X, ms.Y)
	if !ok || side == d.from.side {
		return m, nil
	}
	m.expireMessage()
	if m.disconnected {
		m.setMessage(ErrorMark + " not connected")
		return m, nil
	}
	p := m.panes[d.from.side]
	cmd := m.copyNames(p, p.dragged(d.from.name))
	if cmd != nil && p.marks[d.from.name] {
		clear(p.marks) // as c does (FR-SFTP-009)
	}
	return m, cmd
}

// dragged are the names a drag from name carries: the marked entries if
// name is marked, otherwise name alone (FR-SFTP-022).
func (p *pane) dragged(name string) []string {
	if p.marks[name] {
		return p.marked()
	}
	return []string{name}
}

// dragHint is the progress line during a drag (FR-SFTP-022): "drag:
// copy 3 items to Remote: /home/pi · release over the other pane".
func (m Model) dragHint() string {
	from := m.drag.from
	n := len(m.panes[from.side].dragged(from.name))
	to := from.side.Other()
	return "drag: copy " + strconv.Itoa(n) + " " + plural(n, "item", "items") + " to " +
		to.String() + ": " + m.panes[to].title + hintJoiner + "release over the other pane"
}
