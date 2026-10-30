package ui

import (
	"net"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/tabs"
)

// SessionUpdateMsg reports that session tab Num has new output
// (design §9.4).
type SessionUpdateMsg struct {
	Num  int
	sess app.FramedSession
}

// SessionEndedMsg reports that the session of tab Num ended. Err is nil
// for exit status 0, a *session.ExitError, a *session.DisconnectError, or
// another error.
type SessionEndedMsg struct {
	Num  int
	Err  error
	sess app.FramedSession
}

// tickMsg redraws the focused session's duration once a second.
type tickMsg struct{}

// sessionTab is one session tab (design §9.4, ADR-015).
type sessionTab struct {
	num     int
	hostID  domain.ID
	label   string // the host label as typed; sanitised when drawn
	addr    string // user@host:port
	sess    app.FramedSession
	started time.Time
	seq     uint64 // Snapshot.Seq when last seen
	seenSeq uint64 // Snapshot.Seq when last focused; activity = seq > seenSeq
	ended   error  // the end cause; meaningful once done
	done    bool
}

// activity reports output that arrived while the tab was in the
// background (FR-TAB-004).
func (t *sessionTab) activity() bool { return t.seq > t.seenSeq }

// listenSession waits for the next update of s and reports it as a
// SessionUpdateMsg, or as a SessionEndedMsg once Updates is closed.
func listenSession(num int, s app.FramedSession) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-s.Updates(); ok {
			return SessionUpdateMsg{Num: num, sess: s}
		}
		return SessionEndedMsg{Num: num, Err: s.Err(), sess: s}
	}
}

// tickEverySecond is the status-bar clock (FR-TAB-007).
func tickEverySecond() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// sessionAddr is the user@host:port of h shown in the status bar.
func sessionAddr(h domain.Host) string {
	port := h.Port
	if port == 0 {
		port = domain.DefaultPort
	}
	addr := net.JoinHostPort(h.Hostname, strconv.Itoa(port))
	if h.User != "" {
		addr = h.User + "@" + addr
	}
	return addr
}

// hostByID finds a host in the host list, shown or kept behind a form.
func (m Model) hostByID(id domain.ID) (domain.Host, bool) {
	for _, s := range []components.Screen{m.screen, m.prev} {
		if l, ok := s.(hostlist.Model); ok {
			for _, h := range l.Hosts() {
				if h.ID == id {
					return h, true
				}
			}
		}
	}
	return domain.Host{}, false
}

// tabIndex returns the index in m.tabs of tab number num.
func (m Model) tabIndex(num int) int {
	for i, t := range m.tabs {
		if t.num == num {
			return i
		}
	}
	return -1
}

// focused returns the focused session tab, or nil on the Hosts tab.
func (m Model) focused() *sessionTab {
	if m.focus <= 0 || m.focus > len(m.tabs) {
		return nil
	}
	return m.tabs[m.focus-1]
}

// areaSize is the session area: the terminal minus the header and the
// status bar (FR-CONN-001 revision).
func (m Model) areaSize() app.Size {
	return app.Size{Cols: m.width, Rows: max(m.height-2, 1)}
}

// openTab adds a tab for s after the last one, with the next unused number,
// and focuses it (FR-TAB-002).
func (m Model) openTab(hostID domain.ID, label, addr string, s app.FramedSession) (Model, tea.Cmd) {
	m.nextNum++
	t := &sessionTab{num: m.nextNum, hostID: hostID, label: label, addr: addr, sess: s, started: m.now()}
	if m.width > 0 {
		s.Resize(m.areaSize())
	}
	m.tabs = append(m.tabs, t)
	m, cmd := m.focusTab(len(m.tabs))
	return m, tea.Batch(m.listen(t.num, s), cmd)
}

// focusTab focuses tab i: 0 is the Hosts tab, i is m.tabs[i-1]. Focusing
// a session clears its activity marker (FR-TAB-001, FR-TAB-004).
func (m Model) focusTab(i int) (Model, tea.Cmd) {
	if i < 0 || i > len(m.tabs) {
		return m, nil
	}
	m.focus = i
	if t := m.focused(); t != nil {
		t.seq = t.sess.Snapshot().Seq
		t.seenSeq = t.seq
	}
	return m.ensureTick()
}

// focusNum focuses session tab number num, if there is one.
func (m Model) focusNum(num int) (Model, tea.Cmd) {
	if i := m.tabIndex(num); i >= 0 {
		return m.focusTab(i + 1)
	}
	return m, nil
}

// focusNext focuses the next tab (delta 1) or the previous one (-1),
// the Hosts tab included, wrapping at both ends.
func (m Model) focusNext(delta int) (Model, tea.Cmd) {
	n := len(m.tabs) + 1
	return m.focusTab(((m.focus+delta)%n + n) % n)
}

// closeTab closes session tab number num and its session, and focuses the
// tab to its left when it was focused; on the Hosts tab, the session's
// host is selected (FR-CONN-005 revision). Close runs off the Update
// goroutine, since it may wait on the network.
func (m Model) closeTab(num int) (Model, tea.Cmd) {
	i := m.tabIndex(num)
	if i < 0 {
		return m, nil
	}
	t := m.tabs[i]
	m.tabs = append(m.tabs[:i:i], m.tabs[i+1:]...)
	closeCmd := func() tea.Msg { _ = t.sess.Close(); return nil }
	switch {
	case m.focus == i+1:
		m.focus = i
	case m.focus > i+1:
		m.focus--
		return m, closeCmd
	default:
		return m, closeCmd
	}
	if m.focus == 0 {
		if l, ok := m.screen.(hostlist.Model); ok {
			m.screen = l.Select(t.hostID)
		}
	}
	m, cmd := m.focusTab(m.focus)
	return m, tea.Batch(closeCmd, cmd)
}

// ensureTick starts the one-second clock while a live session is focused.
func (m Model) ensureTick() (Model, tea.Cmd) {
	if t := m.focused(); t == nil || t.done || m.ticking {
		return m, nil
	}
	m.ticking = true
	return m, m.tick()
}

// updateSessions handles the session-tab messages. ok is false for
// messages it does not handle.
func (m Model) updateSessions(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case SessionUpdateMsg:
		i := m.tabIndex(msg.Num)
		if i < 0 || m.tabs[i].sess != msg.sess {
			return m, nil, true // a closed tab, or a replaced session
		}
		t := m.tabs[i]
		t.seq = t.sess.Snapshot().Seq
		if m.focus == i+1 {
			t.seenSeq = t.seq
		}
		return m, m.listen(t.num, t.sess), true

	case SessionEndedMsg:
		i := m.tabIndex(msg.Num)
		if i < 0 || m.tabs[i].sess != msg.sess {
			return m, nil, true
		}
		t := m.tabs[i]
		t.done, t.ended = true, msg.Err
		return m, nil, true

	case tickMsg:
		m.ticking = false
		m, cmd := m.ensureTick()
		return m, cmd, true
	}
	return m, nil, false
}

// header is the first line of every frame: the title and the tab bar
// (FR-TAB-003).
func (m Model) header() string {
	return tabs.Header(m.headerSpans())
}

// headerSpans lays out the header; View and hit-testing share it.
func (m Model) headerSpans() []tabs.Span {
	entries := make([]tabs.Entry, 0, len(m.tabs)+1)
	entries = append(entries, tabs.HostsEntry())
	for i, t := range m.tabs {
		entries = append(entries, tabs.SessionEntry(t.num, t.label, m.focus != i+1 && t.activity(), t.done))
	}
	return tabs.Layout(m.viewWidth(), entries, m.focus)
}

// viewWidth is the width lines are cut to; very large until the terminal
// size is known.
func (m Model) viewWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 1 << 15
}

// sessionStatus is the focused session's status bar (FR-TAB-007).
func (m Model) sessionStatus(t *sessionTab) string {
	w := m.viewWidth()
	if n := len(m.modals); n > 0 {
		if h, ok := m.modals[n-1].(components.Hinter); ok {
			return components.Help(h.Hints()...)
		}
	}
	if t.done {
		return tabs.EndedStatus(w, endedReason(t.ended))
	}
	return tabs.LiveStatus(w, t.addr, m.now().Sub(t.started), prefixHint(keys.PrefixHelp), prefixHint(keys.PrefixHosts))
}

// prefixHint is the status-bar hint of a prefix command: "Ctrl+] ? help".
func prefixHint(b key.Binding) string {
	h := b.Help()
	return keys.Prefix.Help().Key + " " + h.Key + " " + h.Desc
}

// endedReason is why a session ended, as its tab shows it: "exited (3)"
// or "connection lost: <reason>" (FR-CONN-005 revision).
func endedReason(err error) string {
	if err == nil {
		return "exited (0)"
	}
	return domain.Sanitize(err.Error()) // may carry server text
}

// sessionView draws a session tab: the header, the session area with
// the modals over its lower part, and the status bar (ARCH-UI-005).
func (m Model) sessionView(t *sessionTab) tea.View {
	w := m.viewWidth()
	snap := t.sess.Snapshot()
	rows := len(snap.Lines)
	if m.height > 0 {
		rows = max(m.height-2, 1)
	}
	area := tabs.Area(snap.Lines, w, rows)

	var cursor *tea.Cursor
	if len(m.modals) > 0 {
		var ml []string
		for _, md := range m.modals {
			ml = append(ml, "")
			ml = append(ml, viewLines(md.View())...)
		}
		ml = ml[max(len(ml)-rows, 0):]
		start := rows - len(ml)
		for i, l := range ml {
			area[start+i] = l
		}
	} else if c := snap.Cursor; c != nil && !t.done && c.Y >= 0 && c.Y < rows && c.X >= 0 && c.X < w {
		cursor = tea.NewCursor(c.X, c.Y+1)
	}

	out := make([]string, 0, rows+2)
	out = append(out, m.header())
	out = append(out, area...)
	out = append(out, m.sessionStatus(t))
	for i, l := range out {
		out[i] = truncateLine(l, w)
	}
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	v.Cursor = cursor
	return v
}
