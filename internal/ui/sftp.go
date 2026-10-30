package ui

import (
	"context"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/sftpview"
	"github.com/RafaelXokito/sshelf/internal/ui/tabs"
)

// sftpTab is the SFTP part of a tab (design §9.6, ADR-020): the tab's
// number, host and label are its sessionTab's, whose sess is nil.
type sftpTab struct {
	browser      app.Browser
	view         sftpview.Model
	disconnected bool
	seenDone     int  // finished transfers when last seen focused; activity = more since
	ticking      bool // a progress tick is on its way
	gen          int  // the listener generation; a reconnect starts a new one
	stop         chan struct{}
}

// ConnectedSFTPMsg carries a browser opened by OpenSFTP (through the
// connect flow) or OpenSFTPOn (on a session's connection, attempt 0).
type ConnectedSFTPMsg struct {
	attempt int
	HostID  domain.ID
	Label   string
	Browser app.Browser
}

// sftpOnFailedMsg reports that OpenSFTPOn failed for session tab num.
type sftpOnFailedMsg struct {
	num int
	err error
}

// sftpNoticeHold is how long a session tab's status bar shows that SFTP
// could not be opened on it (FR-SFTP-003).
const sftpNoticeHold = 5 * time.Second

// focusedSFTP returns the focused tab if it is an SFTP tab.
func (m Model) focusedSFTP() *sessionTab {
	if m.focus <= 0 || m.focus > len(m.tabs) {
		return nil
	}
	if t := m.tabs[m.focus-1]; t.sftp != nil {
		return t
	}
	return nil
}

// localHome is where ~ goes in a local pane.
func localHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// startSFTPConnect opens an SFTP tab for a host through the normal
// connect flow (FR-SFTP-002): the Connecting modal, host-key checks and
// prompts as for a session.
func (m Model) startSFTPConnect(hostID domain.ID, label string) (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		return m, nil // one attempt at a time
	}
	svc := m.svc
	ctx, cancel := context.WithCancel(context.Background())
	m.attempt++
	m.attemptHost, m.attemptTab, m.attemptAddr = hostID, 0, ""
	m.cancel = cancel
	m.modals = append(m.modals, connecting.New(label))
	m.status = ""
	attempt := m.attempt
	return m, func() tea.Msg {
		b, err := svc.OpenSFTP(ctx, hostID)
		if err != nil {
			return ConnectFailedMsg{attempt: attempt, HostID: hostID, Label: label, Err: err}
		}
		return ConnectedSFTPMsg{attempt: attempt, HostID: hostID, Label: label, Browser: b}
	}
}

// openSFTPOn opens an SFTP tab on session tab t's connection, or focuses
// the one already there (FR-SFTP-003).
func (m Model) openSFTPOn(t *sessionTab) (tea.Model, tea.Cmd) {
	if num := m.sftpTabOn(t.sess.ConnID()); num > 0 {
		return m.focusNum(num)
	}
	svc, s, num, hostID, label := m.svc, t.sess, t.num, t.hostID, t.label
	return m, func() tea.Msg {
		b, err := svc.OpenSFTPOn(s, hostID)
		if err != nil {
			return sftpOnFailedMsg{num: num, err: err}
		}
		return ConnectedSFTPMsg{HostID: hostID, Label: label, Browser: b}
	}
}

// openSFTPTab adds an SFTP tab for b after the last tab, focuses it and
// lists both panes (FR-SFTP-002, FR-SFTP-005).
func (m Model) openSFTPTab(hostID domain.ID, label string, b app.Browser) (Model, tea.Cmd) {
	m.nextNum++
	v := sftpview.New(m.nextNum, b, sftpview.Options{Home: m.home, Now: m.now})
	if m.width > 0 {
		v = v.SetSize(m.width, m.height)
	}
	t := &sessionTab{num: m.nextNum, hostID: hostID, label: label, started: m.now(), sftp: &sftpTab{browser: b, view: v, stop: make(chan struct{})}}
	m.tabs = append(m.tabs, t)
	m, cmd := m.focusTab(len(m.tabs))
	return m, tea.Batch(v.Init(), m.listenTab(t), cmd)
}

// updateSFTP handles the SFTP messages; ok is false for others.
func (m Model) updateSFTP(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case hostlist.SFTPRequestMsg:
		next, cmd := m.startSFTPConnect(msg.HostID, msg.Label)
		return next.(Model), cmd, true

	case ConnectedSFTPMsg:
		if msg.attempt != 0 {
			if msg.attempt != m.attempt || m.cancel == nil {
				b := msg.Browser // cancelled meanwhile
				return m, func() tea.Msg { _ = b.Close(); return nil }, true
			}
			m.cancelAttempt()
			m.modals = nil
			m.status = ""
		}
		m, cmd := m.openSFTPTab(msg.HostID, msg.Label, msg.Browser)
		return m, cmd, true

	case sftpOnFailedMsg:
		if i := m.tabIndex(msg.num); i >= 0 {
			m.tabs[i].notice = failedStatusNoLabel(msg.err)
			m.tabs[i].noticeAt = m.now()
		}
		return m, nil, true

	case sftpview.TabMsg:
		i := m.tabIndex(msg.TabNum())
		if i < 0 || m.tabs[i].sftp == nil {
			return m, nil, true
		}
		st := m.tabs[i].sftp
		var cmd tea.Cmd
		st.view, cmd = st.view.Update(msg)
		return m, cmd, true
	}
	return m, nil, false
}

// failedStatusNoLabel is why opening SFTP failed, without a host label.
func failedStatusNoLabel(err error) string {
	return strings.TrimPrefix(failedStatus("", err), ": ")
}

// sftpKey handles a key on an SFTP tab that is neither a modal's nor the
// prefix key's (design §9.6).
func (m Model) sftpKey(t *sessionTab, k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	st := t.sftp
	if st.disconnected && !st.view.Filtering() && !st.view.ShowingTransfers() {
		switch {
		case key.Matches(k, keys.Reconnect):
			return m.reconnectSFTP(t)
		case key.Matches(k, keys.CloseTab):
			return m.closeSFTPAsk(t, true)
		}
	}
	var cmd tea.Cmd
	st.view, cmd = st.view.Update(k)
	return m, cmd
}

// sftpView draws an SFTP tab: the header, the panes with the modals over
// their lower part, the progress line and the footer (ARCH-UI-005).
func (m Model) sftpView(t *sessionTab) tea.View {
	out, _ := m.sftpFrame(t)
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	v.MouseMode = m.mouseMode(app.MouseNone)
	return v
}

// sftpFrame lays out an SFTP tab: the lines of the frame and where each
// modal sits. View and hit-testing share it (ARCH-UI-007).
func (m Model) sftpFrame(t *sessionTab) ([]string, frameLayout) {
	var lay frameLayout
	w := m.viewWidth()
	lines := t.sftp.view.Lines()
	body := lines[:len(lines)-2] // the pane boxes
	tail := lines[len(lines)-2:] // the progress line and the footer
	if m.prefix {
		tail[1] = prefixPending()
	} else if n := len(m.modals); n > 0 {
		if h, ok := m.modals[n-1].(components.Hinter); ok {
			tail[1] = components.Help(h.Hints()...)
		}
	}
	if n := len(m.modals); n > 0 {
		var ml []string
		tops := make([]int, n)
		for i, md := range m.modals {
			ml = append(ml, "")
			box := viewLines(md.View())
			tops[i] = len(ml)
			lay.modals = append(lay.modals, region{W: linesWidth(box), H: len(box)})
			ml = append(ml, box...)
		}
		rows := len(body)
		cut := max(len(ml)-rows, 0)
		ml = ml[cut:]
		start := rows - len(ml)
		body = append([]string(nil), body...)
		for i, l := range ml {
			body[start+i] = l
		}
		for i := range lay.modals {
			lay.modals[i].Y = 1 + start + tops[i] - cut // below the header
		}
		lay = lay.clip(1, 1+rows)
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, m.header())
	out = append(out, body...)
	out = append(out, tail...)
	for i, l := range out {
		out[i] = truncateLine(l, w)
	}
	return out, lay
}

// sftpEntry is an SFTP tab's entry in the tab bar (FR-TAB-003 M3
// revision).
func sftpEntry(t *sessionTab, focused bool) tabs.Entry {
	return tabs.SFTPEntry(t.num, t.label, !focused && t.sftp.activity(), t.sftp.disconnected)
}

// activity reports a transfer that finished or failed while the tab was
// in the background (FR-TAB-003 M3 revision).
func (s *sftpTab) activity() bool { return s.view.Finished() > s.seenDone }

// seen clears the activity marker while the tab is focused.
func (s *sftpTab) seen() { s.seenDone = s.view.Finished() }
