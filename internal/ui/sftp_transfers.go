package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/keychanged"
	"github.com/RafaelXokito/sshelf/internal/ui/sftpview"
)

// The SFTP tab's link to its transfer queue and connection (design §9.6):
// a listener on Browser.Updates and Browser.Done, the 4 Hz progress tick,
// the disconnected state and the reconnect.

// TransfersMsg reports that tab Num's queue changed (design §9.6), with a
// snapshot taken in the listener's command.
type TransfersMsg struct {
	Num       int
	gen       int
	transfers []app.Transfer
}

// SFTPDoneMsg reports that tab Num's connection dropped (FR-SFTP-018).
type SFTPDoneMsg struct {
	Num       int
	gen       int
	transfers []app.Transfer
}

// progressTickMsg redraws a focused tab's progress line (ARCH-SFTP-003).
type progressTickMsg struct {
	num       int
	transfers []app.Transfer
}

// sftpReconnectedMsg reports that ReconnectSFTP gave tab num a new
// connection.
type sftpReconnectedMsg struct {
	attempt int
	num     int
}

// ProgressEvery is the progress tick: at most 4 redraws a second
// (FR-SFTP-014).
const ProgressEvery = 250 * time.Millisecond

// listenBrowser waits for tab num's next queue change, or for its
// connection to drop when watchDone is set, and takes a snapshot. It
// returns nil once stop is closed (the tab closed).
func listenBrowser(num, gen int, b app.Browser, watchDone bool, stop <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		var done <-chan struct{}
		if watchDone {
			done = b.Done()
		}
		select {
		case <-stop:
			return nil
		case <-b.Updates():
			return TransfersMsg{Num: num, gen: gen, transfers: b.Transfers()}
		case <-done:
			return SFTPDoneMsg{Num: num, gen: gen, transfers: b.Transfers()}
		}
	}
}

// progressTick is the 250 ms tick, with a snapshot taken when it fires.
func progressTick(num int, b app.Browser) tea.Cmd {
	return tea.Tick(ProgressEvery, func(time.Time) tea.Msg {
		return progressTickMsg{num: num, transfers: b.Transfers()}
	})
}

// listenTab arms tab t's listener.
func (m Model) listenTab(t *sessionTab) tea.Cmd {
	st := t.sftp
	return m.listenSFTP(t.id, st.gen, st.browser, !st.disconnected, st.stop)
}

// snapshot gives tab t's view a snapshot; a focused tab has seen what
// finished, and starts its progress tick if a transfer runs.
func (m Model) snapshot(t *sessionTab, ts []app.Transfer) tea.Cmd {
	st := t.sftp
	var cmd tea.Cmd
	st.view, cmd = st.view.Update(sftpview.SnapshotMsg{Tab: t.id, Transfers: ts})
	if m.focusedSFTP() == t {
		st.seen()
	}
	return tea.Batch(cmd, m.ensureProgress(t))
}

// ensureProgress starts the progress tick of the focused tab t while a
// transfer runs; one tick at a time (ARCH-SFTP-003).
func (m Model) ensureProgress(t *sessionTab) tea.Cmd {
	st := t.sftp
	if st.ticking || m.focusedSFTP() != t || !st.view.Running() {
		return nil
	}
	st.ticking = true
	return m.progressTick(t.id, st.browser)
}

// sftpTabNum returns the SFTP tab with id id, or nil.
func (m Model) sftpTabNum(id int) *sessionTab {
	if i := m.tabIndex(id); i >= 0 && m.tabs[i].sftp != nil {
		return m.tabs[i]
	}
	return nil
}

// updateTransfers handles the queue and connection messages of SFTP
// tabs; ok is false for others.
func (m Model) updateTransfers(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case TransfersMsg:
		t := m.sftpTabNum(msg.Num)
		if t == nil {
			return m, nil, true
		}
		var again tea.Cmd
		if msg.gen == t.sftp.gen {
			again = m.listenTab(t)
		}
		return m, tea.Batch(again, m.snapshot(t, msg.transfers)), true

	case SFTPDoneMsg:
		t := m.sftpTabNum(msg.Num)
		if t == nil || msg.gen != t.sftp.gen {
			return m, nil, true
		}
		st := t.sftp
		st.disconnected = true
		st.view = st.view.SetDisconnected(true)
		return m, tea.Batch(m.listenTab(t), m.snapshot(t, msg.transfers)), true

	case progressTickMsg:
		t := m.sftpTabNum(msg.num)
		if t == nil {
			return m, nil, true
		}
		t.sftp.ticking = false
		return m, m.snapshot(t, msg.transfers), true

	case sftpReconnectedMsg:
		if msg.attempt != m.attempt || m.cancel == nil {
			return m, nil, true
		}
		m.cancelAttempt()
		m.modals = nil
		t := m.sftpTabNum(msg.num)
		if t == nil {
			return m, nil, true
		}
		st := t.sftp
		st.disconnected, t.failure = false, ""
		st.gen++
		st.view = st.view.SetDisconnected(false)
		m, focus := m.focusTab(m.tabIndex(t.id) + 1)
		return m, tea.Batch(st.view.Refresh(), m.listenTab(t), focus), true

	case ConnectFailedMsg:
		t := m.attemptTabPtr()
		if t == nil || t.sftp == nil || msg.attempt != m.attempt || m.cancel == nil {
			return m, nil, false // a session's attempt
		}
		m.cancelAttempt()
		m.modals = nil
		t.failure = failedStatus(msg.Label, msg.Err)
		t.sftp.view = t.sftp.view.WithMessage(components.ErrorPrefix + t.failure)
		if me, ok := errors.AsType[*app.HostKeyMismatchError](msg.Err); ok {
			m.modals = append(m.modals, keychanged.New(msg.Label, me))
		}
		return m, nil, true

	case connecting.CancelMsg:
		t := m.attemptTabPtr()
		if t == nil || t.sftp == nil || m.cancel == nil {
			return m, nil, false
		}
		m.cancelAttempt()
		m.modals = nil
		t.failure = "connection cancelled"
		t.sftp.view = t.sftp.view.WithMessage(t.failure)
		return m, nil, true
	}
	return m, nil, false
}

// reconnectSFTP reconnects disconnected tab t through the normal connect
// flow (FR-SFTP-018): the Connecting modal over the tab, then
// ReconnectSFTP, which resumes the paused transfers.
func (m Model) reconnectSFTP(t *sessionTab) (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		return m, nil // one attempt at a time
	}
	svc := m.svc
	ctx, cancel := context.WithCancel(context.Background())
	m.attempt++
	m.attemptHost, m.attemptTab = t.hostID, t.id
	m.cancel = cancel
	m.modals = append(m.modals, connecting.New(t.label))
	attempt, num, hostID, label, b := m.attempt, t.id, t.hostID, t.label, t.sftp.browser
	return m, func() tea.Msg {
		if err := svc.ReconnectSFTP(ctx, b); err != nil {
			return ConnectFailedMsg{attempt: attempt, HostID: hostID, Label: label, Err: err}
		}
		return sftpReconnectedMsg{attempt: attempt, num: num}
	}
}

// closeQuestion is the close confirmation of SFTP tab t at position pos
// (FR-SFTP-025, D-023): "Close SFTP tab 2? 3 transfers will stop.", or for
// a disconnected tab "Close and drop 3 paused transfers?" (FR-SFTP-018).
func closeQuestion(t *sessionTab, pos int) string {
	n := t.sftp.view.Pending()
	if t.sftp.disconnected && n > 0 {
		return fmt.Sprintf("Close and drop %d paused %s?", n, map[bool]string{true: "transfer", false: "transfers"}[n == 1])
	}
	q := fmt.Sprintf("Close SFTP tab %d?", pos)
	if n > 0 {
		q += fmt.Sprintf(" %d %s will stop.", n, map[bool]string{true: "transfer", false: "transfers"}[n == 1])
	}
	return q
}

// closeSFTPAsk asks before closing SFTP tab t (FR-SFTP-025); x on a
// disconnected tab with nothing paused closes it at once (FR-SFTP-018).
func (m Model) closeSFTPAsk(t *sessionTab, immediateIfIdle bool) (tea.Model, tea.Cmd) {
	if immediateIfIdle && t.sftp.view.Pending() == 0 {
		return m.closeTab(t.id)
	}
	m.modals = append(m.modals, confirm.New(closeQuestion(t, m.tabIndex(t.id)+1), closeTabMsg{Num: t.id}))
	return m, nil
}
