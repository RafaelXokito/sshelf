// Package ui is the root Bubble Tea model: it routes messages to one active
// screen and a stack of modals (design §9.2), and runs every I/O operation
// through app.Service inside tea.Cmd functions (ARCH-UI-002).
package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/keychanged"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/tofu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/loaderror"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).MarginBottom(1)
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// LoadedMsg carries the result of loading config.json.
type LoadedMsg struct {
	Hosts   []domain.Host
	Notices []string // shown once in the status bar
	Err     error
}

// ConnectedMsg carries an authenticated session, ready for tea.Exec.
type ConnectedMsg struct {
	attempt int
	HostID  domain.ID
	Label   string
	Session app.Session
}

// ConnectFailedMsg reports a connection attempt that did not get a session.
type ConnectFailedMsg struct {
	attempt int
	HostID  domain.ID
	Label   string
	Err     error
}

// SessionEndedMsg reports that an attached session returned the terminal.
// Err is nil for exit status 0, a *session.ExitError, a
// *session.DisconnectError, or another error.
type SessionEndedMsg struct {
	HostID domain.ID
	Label  string
	Err    error
}

// execFunc is tea.Exec; tests replace it.
type execFunc func(tea.ExecCommand, tea.ExecCallback) tea.Cmd

// Model is the root Bubble Tea model.
type Model struct {
	svc    app.Service
	screen components.Screen // nil while loading
	modals []components.Modal
	status string
	exec   execFunc

	attempt int                // id of the latest connection attempt
	cancel  context.CancelFunc // cancels the attempt in progress; nil if none
}

// New returns the root model. It does no I/O; Init loads the config.
func New(svc app.Service) Model {
	return Model{svc: svc, exec: tea.Exec}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		l, err := svc.Load(context.Background())
		return LoadedMsg{Hosts: l.Hosts, Notices: l.Notices, Err: err}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, keys.ForceQuit) {
			m.cancelAttempt()
			return m, tea.Quit
		}
		if n := len(m.modals); n > 0 {
			var cmd tea.Cmd
			m.modals[n-1], cmd = m.modals[n-1].Update(msg)
			return m, cmd
		}
		if m.screen == nil {
			if key.Matches(msg, keys.Quit) {
				return m, tea.Quit
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.screen, cmd = m.screen.Update(msg)
		return m, cmd

	case LoadedMsg:
		m.status = strings.Join(msg.Notices, "; ")
		if msg.Err != nil {
			le, ok := errors.AsType[*app.LoadError](msg.Err)
			if !ok {
				le = &app.LoadError{Path: "config.json", Problem: msg.Err.Error(), Err: msg.Err}
			}
			m.screen = loaderror.New(le)
			return m, nil
		}
		m.screen = hostlist.New(msg.Hosts)
		return m, nil

	case hostlist.ConnectRequestMsg:
		if m.cancel != nil {
			return m, nil // one attempt at a time
		}
		return m.startConnect(msg.HostID, msg.Label)

	case connecting.CancelMsg:
		if m.cancel == nil {
			return m, nil
		}
		m.cancelAttempt()
		m.modals = nil
		m.status = "connection cancelled"
		return m, nil

	case app.HostKeyPromptMsg:
		if m.cancel == nil {
			msg.Answer(false) // the attempt is already over
			return m, nil
		}
		m.modals = append(m.modals, tofu.New(msg))
		return m, nil

	case tofu.DoneMsg:
		// The connection goes on (trusted) or fails with
		// ErrHostKeyRejected; Connecting stays up until then.
		m.popModal()
		return m, nil

	case ConnectedMsg:
		if msg.attempt != m.attempt || m.cancel == nil {
			// The user cancelled meanwhile: release the session off the
			// Update goroutine.
			s := msg.Session
			return m, func() tea.Msg { _ = s.Close(); return nil }
		}
		m.cancelAttempt()
		m.modals = nil
		m.status = ""
		hostID, label := msg.HostID, msg.Label
		return m, m.exec(msg.Session, func(err error) tea.Msg {
			return SessionEndedMsg{HostID: hostID, Label: label, Err: err}
		})

	case ConnectFailedMsg:
		if msg.attempt != m.attempt || m.cancel == nil {
			return m, nil
		}
		m.cancelAttempt()
		m.modals = nil
		m.status = failedStatus(msg.Label, msg.Err)
		if me, ok := errors.AsType[*app.HostKeyMismatchError](msg.Err); ok {
			m.modals = append(m.modals, keychanged.New(msg.Label, me))
		}
		return m, nil

	case keychanged.CloseMsg:
		m.popModal()
		return m, nil

	case SessionEndedMsg:
		m.status = endedStatus(msg.Label, msg.Err)
		if l, ok := m.screen.(hostlist.Model); ok {
			m.screen = l.Select(msg.HostID)
		}
		return m, nil
	}

	if m.screen != nil {
		var cmd tea.Cmd
		m.screen, cmd = m.screen.Update(msg)
		return m, cmd
	}
	return m, nil
}

// startConnect pushes the Connecting modal and starts the connect command.
func (m Model) startConnect(hostID domain.ID, label string) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.attempt++
	m.cancel = cancel
	m.modals = append(m.modals, connecting.New(label))
	m.status = ""
	svc, attempt := m.svc, m.attempt
	return m, func() tea.Msg {
		s, err := svc.Connect(ctx, hostID)
		if err != nil {
			return ConnectFailedMsg{attempt: attempt, HostID: hostID, Label: label, Err: err}
		}
		return ConnectedMsg{attempt: attempt, HostID: hostID, Label: label, Session: s}
	}
}

func (m *Model) cancelAttempt() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Model) popModal() {
	if n := len(m.modals); n > 0 {
		m.modals = m.modals[:n-1]
	}
}

// failedStatus is the status-bar text after a connection attempt failed.
func failedStatus(label string, err error) string {
	if errors.Is(err, app.ErrHostKeyRejected) {
		return label + ": host key not trusted; connection cancelled"
	}
	if errors.Is(err, app.ErrHostKeyMismatch) {
		return label + ": host key changed; connection blocked"
	}
	if re, ok := errors.AsType[*app.HostKeyRevokedError](err); ok {
		return fmt.Sprintf("%s: host key revoked (%s line %d); connection refused", label, re.Entry.File, re.Entry.Line)
	}
	return label + ": " + err.Error()
}

// endedStatus is the status-bar text after a session (FR-CONN-005), e.g.
// "web-prod: exited (3)" or "web-prod: connection lost: <reason>".
func endedStatus(label string, err error) string {
	if err == nil {
		return label + ": exited (0)"
	}
	return label + ": " + err.Error()
}

// View implements tea.Model.
func (m Model) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render("sshelf"))
	b.WriteString("\n")
	if m.screen == nil {
		b.WriteString(dimStyle.Render("Loading…"))
		b.WriteString("\n")
	} else {
		b.WriteString(m.screen.View())
	}
	for _, md := range m.modals {
		b.WriteString("\n")
		b.WriteString(md.View())
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(statusStyle.Render(m.status))
		b.WriteString("\n")
	}
	if _, ok := m.screen.(hostlist.Model); ok {
		b.WriteString(dimStyle.Render("↑/↓ move • enter connect • q quit"))
		b.WriteString("\n")
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}
