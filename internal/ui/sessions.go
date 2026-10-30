package ui

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/help"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/welcome"
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
	failure string // a failed or cancelled reconnect, shown instead of the end cause
	// scroll is the top line of the scrollback view in scrollback mode,
	// counted from the session's first line (top + Dropped()), so the view
	// keeps its place while the cap drops old lines; nil otherwise.
	scroll *int
}

// scrollTop returns the top line of the scrollback view relative to the
// oldest line the session keeps, as Scrollback takes it.
func (t *sessionTab) scrollTop() int { return max(*t.scroll-t.sess.Dropped(), 0) }

// setScrollTop sets the scrollback view's top line, relative to the
// oldest line kept.
func (t *sessionTab) setScrollTop(top int) {
	abs := top + t.sess.Dropped()
	t.scroll = &abs
}

// quitMsg closes every session and quits; the quit confirmation delivers
// it on y (FR-TAB-009).
type quitMsg struct{}

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

// attemptTabPtr is the ended tab the connection attempt in progress
// reconnects, or nil.
func (m Model) attemptTabPtr() *sessionTab {
	if m.attemptTab == 0 {
		return nil
	}
	if i := m.tabIndex(m.attemptTab); i >= 0 {
		return m.tabs[i]
	}
	return nil
}

// reconnected runs s in the ended tab t, keeping its number and position;
// the old screen and scrollback go with the old session (FR-CONN-010).
func (m Model) reconnected(t *sessionTab, s app.FramedSession) (Model, tea.Cmd) {
	old := t.sess
	t.sess, t.started = s, m.now()
	t.done, t.ended, t.failure, t.scroll = false, nil, "", nil
	t.seq, t.seenSeq = 0, 0
	if m.width > 0 {
		s.Resize(m.areaSize())
	}
	m.ticking = m.ticking && m.focused() != t // the clock may have stopped on the ended tab
	m, cmd := m.focusTab(m.tabIndex(t.num) + 1)
	return m, tea.Batch(func() tea.Msg { _ = old.Close(); return nil }, m.listen(t.num, s), cmd)
}

// liveSessions counts the tabs whose session has not ended.
func (m Model) liveSessions() int {
	n := 0
	for _, t := range m.tabs {
		if !t.done {
			n++
		}
	}
	return n
}

// quitKeyQuits reports whether q quits on the Hosts tab's screen: the
// host list outside its search field, and the Welcome screen.
func (m Model) quitKeyQuits() bool {
	switch s := m.screen.(type) {
	case hostlist.Model:
		return !s.Searching()
	case welcome.Model:
		return true
	}
	return false
}

// quit quits sshelf, asking first while sessions are live (FR-TAB-009).
func (m Model) quit() (tea.Model, tea.Cmd) {
	n := m.liveSessions()
	if n == 0 {
		m.cancelAttempt()
		return m, tea.Quit
	}
	if !m.quitAsked {
		m.quitAsked = true
		q := fmt.Sprintf("Quit and close %d session%s?", n, map[bool]string{true: "", false: "s"}[n == 1])
		m.modals = append(m.modals, confirm.New(q, quitMsg{}))
	}
	return m, nil
}

// closeAllAndQuit closes every session off the Update goroutine, then
// quits.
func (m Model) closeAllAndQuit() (tea.Model, tea.Cmd) {
	m.cancelAttempt()
	sessions := make([]app.FramedSession, len(m.tabs))
	for i, t := range m.tabs {
		sessions[i] = t.sess
	}
	return m, func() tea.Msg {
		for _, s := range sessions {
			_ = s.Close()
		}
		return tea.QuitMsg{}
	}
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

	case closeTabMsg:
		m, cmd := m.closeTab(msg.Num)
		return m, cmd, true

	case quitMsg:
		next, cmd := m.closeAllAndQuit()
		return next.(Model), cmd, true

	case tickMsg:
		m.ticking = false
		m, cmd := m.ensureTick()
		return m, cmd, true
	}
	return m, nil, false
}

// closeTabMsg closes tab Num and its session; the Disconnect confirmation
// delivers it on y (FR-CONN-014).
type closeTabMsg struct{ Num int }

// prefixPending is the status text while the prefix key waits for its
// command (FR-TAB-005).
func prefixPending() string { return keys.Prefix.Help().Key + " …" }

// statusLine is the Hosts tab's status line.
func (m Model) statusLine() string {
	if m.prefix {
		return prefixPending()
	}
	return m.status
}

// runPrefix runs a step of the prefix state machine (design §8.5). No
// byte of a prefix command reaches a session, except the literal Ctrl+]
// of Ctrl+] Ctrl+] (ARCH-SESS-006).
func (m Model) runPrefix(r keys.PrefixResult) (tea.Model, tea.Cmd) {
	t := m.focused()
	switch r.Action {
	case keys.PrefixDoLiteral:
		if t != nil && !t.done {
			_ = t.sess.SendBytes([]byte{0x1d})
		}
	case keys.PrefixDoDisconnect:
		if t != nil {
			q := "Disconnect " + domain.Sanitize(t.label) + "?"
			m.modals = append(m.modals, confirm.New(q, closeTabMsg{Num: t.num}))
		}
	case keys.PrefixDoHelp:
		m.modals = append(m.modals, help.New())
	case keys.PrefixDoHosts:
		return m.focusTab(0)
	case keys.PrefixDoTab:
		return m.focusNum(r.Tab)
	case keys.PrefixDoNext:
		return m.focusNext(1)
	case keys.PrefixDoPrev:
		return m.focusNext(-1)
	case keys.PrefixDoScrollback:
		if t != nil && t.scroll == nil {
			_, total := t.sess.Scrollback(0, 0)
			t.setScrollTop(max(total-m.areaRows(t), 0))
		}
	}
	return m, nil
}

// sessionKey handles a key on a session tab that is neither a modal's nor
// the prefix key's. In scrollback mode it moves the view (FR-TAB-010); an
// ended tab takes only r and x (FR-CONN-005 revision); a live session gets
// everything else, encoded by the session (FR-TAB-006).
func (m Model) sessionKey(t *sessionTab, k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case t.scroll != nil:
		m.scrollKey(t, k)
	case t.done && key.Matches(k, keys.Reconnect):
		hostID, label, num := t.hostID, t.label, t.num
		return m, func() tea.Msg { return hostlist.ConnectRequestMsg{HostID: hostID, Label: label, Tab: num} }
	case t.done && key.Matches(k, keys.CloseTab):
		return m.closeTab(t.num)
	case !t.done:
		_ = t.sess.SendKey(toKeyEvent(k)) // never blocks; a full queue drops it (ARCH-SESS-014)
	}
	return m, nil
}

// scrollKey moves the scrollback view; no key reaches the remote.
func (m Model) scrollKey(t *sessionTab, k tea.KeyPressMsg) {
	rows := m.areaRows(t)
	_, total := t.sess.Scrollback(0, 0)
	top := t.scrollTop()
	switch {
	case key.Matches(k, keys.ScrollExit):
		t.scroll = nil
		return
	case key.Matches(k, keys.ScrollUp):
		top--
	case key.Matches(k, keys.ScrollDown):
		top++
	case key.Matches(k, keys.ScrollPageUp):
		top -= rows
	case key.Matches(k, keys.ScrollPageDown):
		top += rows
	case key.Matches(k, keys.ScrollTop):
		top = 0
	case key.Matches(k, keys.ScrollBottom):
		top = total
	}
	t.setScrollTop(min(max(top, 0), max(total-rows, 0)))
}

// areaRows is the number of rows of the session area.
func (m Model) areaRows(t *sessionTab) int {
	if m.height > 0 {
		return max(m.height-2, 1)
	}
	return len(t.sess.Snapshot().Lines)
}

// toKeyEvent converts a Bubble Tea key press to the ultraviolet event the
// session encodes, field by field (design §8.4).
func toKeyEvent(k tea.KeyPressMsg) uv.KeyPressEvent {
	return uv.KeyPressEvent{Text: k.Text, Mod: k.Mod, Code: k.Code, ShiftedCode: k.ShiftedCode, BaseCode: k.BaseCode, IsRepeat: k.IsRepeat}
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
	if m.prefix {
		return prefixPending()
	}
	if n := len(m.modals); n > 0 {
		if h, ok := m.modals[n-1].(components.Hinter); ok {
			return components.Help(h.Hints()...)
		}
	}
	if t.scroll != nil {
		_, total := t.sess.Scrollback(0, 0)
		last := min(t.scrollTop()+m.areaRows(t), total)
		hints := []string{
			keys.ScrollUp.Help().Key + keys.ScrollDown.Help().Key,
			keys.ScrollPageUp.Help().Key, keys.ScrollPageDown.Help().Key,
			keys.ScrollTop.Help().Key, keys.ScrollBottom.Help().Key,
		}
		return tabs.Fit(w, []string{fmt.Sprintf("scrollback %d/%d", last, total)}, strings.Join(hints, " "), hint(keys.ScrollExit))
	}
	if t.done {
		reason := endedReason(t.ended)
		if t.failure != "" {
			reason = t.failure
		}
		return tabs.EndedStatus(w, reason, hint(keys.Reconnect), hint(keys.CloseTab))
	}
	return tabs.LiveStatus(w, t.addr, m.now().Sub(t.started), prefixHint(keys.PrefixHelp), prefixHint(keys.PrefixHosts))
}

// prefixHint is the status-bar hint of a prefix command: "Ctrl+] ? help".
func prefixHint(b key.Binding) string {
	return keys.Prefix.Help().Key + " " + b.Keys()[0] + " " + b.Help().Desc
}

// hint is a status-bar hint: "r reconnect".
func hint(b key.Binding) string { return b.Help().Key + " " + b.Help().Desc }

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
	lines := snap.Lines
	if t.scroll != nil {
		// The view keeps its top line while output arrives (FR-TAB-010).
		_, total := t.sess.Scrollback(0, 0)
		lines, _ = t.sess.Scrollback(min(t.scrollTop(), max(total-rows, 0)), rows)
	}
	var ml []string
	for _, md := range m.modals {
		ml = append(ml, "")
		ml = append(ml, viewLines(md.View())...)
	}
	if m.height == 0 {
		rows = max(rows, len(ml)) // the size is not known yet: show the whole modal
	}
	area := tabs.Area(lines, w, rows)

	var cursor *tea.Cursor
	if len(ml) > 0 {
		ml = ml[max(len(ml)-rows, 0):]
		start := rows - len(ml)
		for i, l := range ml {
			area[start+i] = l
		}
	} else if c := snap.Cursor; c != nil && !t.done && t.scroll == nil && c.Y >= 0 && c.Y < rows && c.X >= 0 && c.X < w {
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
