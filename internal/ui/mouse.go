package ui

import (
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/tabs"
)

// region is the rectangle of the frame that one component draws: rows Y
// to Y+H−1 and columns X to X+W−1, counted from 0 at the top left of the
// terminal.
type region struct{ X, Y, W, H int }

// local translates the terminal cell at column x, row y into r's own
// coordinates, which are those of the component's View, and reports
// whether the cell is inside r. It is the one place where frame offsets
// (the header row, the blank row, the banner, the screen above a modal)
// are removed from mouse coordinates (ARCH-UI-007, ADR-014).
func (r region) local(x, y int) (lx, ly int, ok bool) {
	lx, ly = x-r.X, y-r.Y
	if lx < 0 || ly < 0 || lx >= r.W || ly >= r.H {
		return 0, 0, false
	}
	return lx, ly, true
}

// clip cuts r to rows top to bottom−1 of the frame.
func (r region) clip(top, bottom int) region {
	end := min(r.Y+r.H, bottom)
	if r.Y < top {
		r.Y = top
	}
	r.H = max(end-r.Y, 0)
	return r
}

// frameLayout is where a frame puts the active screen and each modal.
type frameLayout struct {
	screen region   // the Hosts tab's screen; H is 0 when it is hidden or on a session tab
	modals []region // one per modal, bottom of the stack first
}

// clip cuts every region to rows top to bottom−1 of the frame.
func (l frameLayout) clip(top, bottom int) frameLayout {
	l.screen = l.screen.clip(top, bottom)
	modals := make([]region, len(l.modals))
	for i, r := range l.modals {
		modals[i] = r.clip(top, bottom)
	}
	l.modals = modals
	return l
}

// linesWidth is the width of the widest line.
func linesWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	return w
}

// layout is the layout of the frame View draws now.
func (m Model) layout() frameLayout {
	if t := m.focused(); t != nil {
		_, _, lay := m.sessionFrame(t, t.sess.Snapshot())
		return lay
	}
	if t := m.focusedSFTP(); t != nil {
		_, lay := m.sftpFrame(t)
		return lay
	}
	_, lay := m.hostsFrame()
	return lay
}

// mouseState is what the mouse mode depends on (ARCH-UI-006).
type mouseState struct {
	session    bool          // a session tab is focused
	modal      bool          // a modal is open
	scrollback bool          // the focused session is in scrollback mode
	ended      bool          // the focused session has ended
	requested  tea.MouseMode // the mode the focused session's remote app asked for
}

// mouseModeFor is the mouse-mode policy (ARCH-UI-006, ADR-014): cell
// motion on the Hosts tab and whenever a modal is open, since those are
// sshelf's screens; on a session tab, none in scrollback mode and on an
// ended session, and otherwise the mode the remote asked for. A plain
// shell asks for none, so the terminal's own selection keeps working
// (D-012).
func mouseModeFor(s mouseState) tea.MouseMode {
	switch {
	case !s.session || s.modal:
		return tea.MouseModeCellMotion
	case s.scrollback || s.ended:
		return tea.MouseModeNone
	}
	return s.requested
}

// mouseMode is the mouse mode of the frame View draws now. requested is
// the mouse reporting the focused session's remote asked for, from the
// snapshot that frame is drawn from; it is ignored on the Hosts tab.
func (m Model) mouseMode(requested app.MouseMode) tea.MouseMode {
	s := mouseState{modal: len(m.modals) > 0}
	if t := m.focused(); t != nil {
		s.session, s.scrollback, s.ended = true, t.scroll != nil, t.done
		s.requested = remoteMouseMode(requested)
	}
	return mouseModeFor(s)
}

// remoteMouseMode is the terminal mouse mode that delivers what a remote
// app asked for (FR-TAB-011, NFR-UX-001 M2 revision): cell motion for
// presses (?9, ?1000) and button motion (?1002), since Bubble Tea has no
// press-only mode and the session drops the motion the app did not ask
// for; all motion for ?1003; none when the app asked for nothing.
func remoteMouseMode(requested app.MouseMode) tea.MouseMode {
	switch requested {
	case app.MousePress, app.MouseDrag:
		return tea.MouseModeCellMotion
	case app.MouseAny:
		return tea.MouseModeAllMotion
	}
	return tea.MouseModeNone
}

// sessionArea is where the focused session's screen sits in the frame:
// below the header row, above the status bar, as wide as the terminal.
// It is the size the session's PTY was given (areaSize).
func (m Model) sessionArea() region {
	sz := m.areaSize()
	return region{Y: 1, W: sz.Cols, H: sz.Rows}
}

// updateMouse handles the mouse messages; ok is false for other messages.
// While a session is focused and no modal is open, every mouse event
// belongs to the session (forwardMouse). Releases and motion are ignored
// on sshelf's screens (ARCH-UI-007).
func (m Model) updateMouse(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	mm, ok := msg.(tea.MouseMsg)
	if !ok {
		return m, nil, false
	}
	if t := m.focused(); t != nil && len(m.modals) == 0 {
		m.forwardMouse(t, msg)
		return m, nil, true
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		next, cmd := m.mouseClick(mm.Mouse())
		return next, cmd, true
	case tea.MouseWheelMsg:
		next, cmd := m.mouseWheel(mm.Mouse())
		return next, cmd, true
	}
	return m, nil, true
}

// forwardMouse sends a mouse event to the focused session t, in the
// coordinates of the session area (FR-TAB-011, ADR-014). Events on the
// header row (the tab bar included, D-012) and on the status bar are
// neither forwarded nor acted on; nothing is forwarded in scrollback mode
// or once the session has ended. The session encodes the event as the
// remote asked (X10 or SGR) and drops what the remote's mode does not
// report, such as motion under ?1000 (ADR-013).
func (m Model) forwardMouse(t *sessionTab, msg tea.Msg) {
	if t.done || t.scroll != nil {
		return
	}
	mm, ok := msg.(tea.MouseMsg)
	if !ok {
		return
	}
	ms := mm.Mouse()
	x, y, ok := m.sessionArea().local(ms.X, ms.Y)
	if !ok {
		return
	}
	in := uv.Mouse{X: x, Y: y, Button: ms.Button, Mod: ms.Mod}
	var ev uv.MouseEvent
	switch msg.(type) {
	case tea.MouseClickMsg:
		ev = uv.MouseClickEvent(in)
	case tea.MouseReleaseMsg:
		ev = uv.MouseReleaseEvent(in)
	case tea.MouseWheelMsg:
		ev = uv.MouseWheelEvent(in)
	case tea.MouseMotionMsg:
		ev = uv.MouseMotionEvent(in)
	default:
		return
	}
	// A full input queue drops the event, as it does a key (ARCH-SESS-014).
	_ = t.sess.SendMouse(ev)
}

// mouseClick sends a left press to the component under it, in its own
// coordinates (ARCH-UI-007, design §9.5).
func (m Model) mouseClick(ms tea.Mouse) (tea.Model, tea.Cmd) {
	if ms.Button != tea.MouseLeft {
		return m, nil
	}
	double := m.clicks.Press(m.now(), ms.Y)
	lay := m.layout()
	if n := len(m.modals); n > 0 {
		// Only the top modal takes clicks; anywhere else, nothing happens
		// (FR-NAV-016).
		x, y, ok := lay.modals[n-1].local(ms.X, ms.Y)
		if !ok {
			return m, nil
		}
		return m.updateTopModal(components.ClickMsg{X: x, Y: y, Double: double})
	}
	if ms.Y == 0 {
		// The tab bar (FR-NAV-017): the title and separators do nothing.
		if i, ok := tabs.TabAt(m.headerSpans(), ms.X); ok {
			return m.focusTab(i)
		}
		return m, nil
	}
	if m.screen == nil || m.focusedSFTP() != nil {
		return m, nil // the SFTP tab's rows are T-056's
	}
	x, y, ok := lay.screen.local(ms.X, ms.Y)
	if !ok {
		return m, nil // the header, the banner, the status line or the footer
	}
	var cmd tea.Cmd
	m.screen, cmd = m.screen.Update(components.ClickMsg{X: x, Y: y, Double: double})
	return m, cmd
}

// mouseWheel sends a wheel notch to the screen under it (design §9.5).
func (m Model) mouseWheel(ms tea.Mouse) (tea.Model, tea.Cmd) {
	var delta int
	switch ms.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return m, nil
	}
	if len(m.modals) > 0 || m.focused() != nil || m.focusedSFTP() != nil || m.screen == nil {
		return m, nil
	}
	x, y, ok := m.layout().screen.local(ms.X, ms.Y)
	if !ok {
		return m, nil
	}
	var cmd tea.Cmd
	m.screen, cmd = m.screen.Update(components.WheelMsg{X: x, Y: y, Delta: delta})
	return m, cmd
}
