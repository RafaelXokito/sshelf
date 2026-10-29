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
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/keychanged"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/prompt"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/tofu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostedit"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/loaderror"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/welcome"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// LoadedMsg carries the result of loading config.json.
type LoadedMsg struct {
	Hosts      []domain.Host
	Identities []domain.Identity
	Notices    []string // shown once in the status bar
	Err        error
}

// HostSavedMsg reports that a host was written to config.json.
type HostSavedMsg struct{ Saved app.SavedHost }

// HostSaveFailedMsg reports that a host was not saved.
type HostSaveFailedMsg struct{ Err error }

// HostDeletedMsg reports that a host was deleted; Hosts are the rest.
type HostDeletedMsg struct {
	Label string
	Hosts []domain.Host
}

// HostDeleteFailedMsg reports that a host was not deleted.
type HostDeleteFailedMsg struct {
	Label string
	Err   error
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

	identities []domain.Identity // for the host form's picker
	prev       components.Screen // the screen to return to while a form is open

	width, height int // terminal size; 0 until the first tea.WindowSizeMsg

	attempt     int                // id of the latest connection attempt
	attemptHost domain.ID          // host of the attempt in progress
	cancel      context.CancelFunc // cancels the attempt in progress; nil if none
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
		return LoadedMsg{Hosts: l.Hosts, Identities: l.Identities, Notices: l.Notices, Err: err}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.screen = m.sized(m.screen)
		return m, nil

	case tea.KeyPressMsg:
		if key.Matches(msg, keys.ForceQuit) {
			if fs, ok := m.screen.(components.FormScreen); ok {
				// Ctrl+C cancels a form instead of quitting (FR-NAV-003).
				m.modals = nil
				return m.Update(fs.Cancel())
			}
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
			m.screen = m.sized(loaderror.New(le))
			return m, nil
		}
		m.identities = msg.Identities
		m.screen = m.sized(hostsScreen(msg.Hosts))
		return m, nil

	case hostlist.AddHostMsg, welcome.AddHostMsg:
		return m.openForm(domain.Host{}), nil

	case hostlist.EditHostMsg:
		return m.openForm(msg.Host), nil

	case hostedit.SaveMsg:
		svc, h := m.svc, msg.Host
		return m, func() tea.Msg {
			saved, err := svc.SaveHost(context.Background(), h)
			if err != nil {
				return HostSaveFailedMsg{Err: err}
			}
			return HostSavedMsg{Saved: saved}
		}

	case HostSavedMsg:
		// Saved even if the form was cancelled meanwhile: show and select
		// the host (FR-HOST-001).
		h := msg.Saved.Host
		m.modals = nil
		m.screen = m.sized(hostlist.New(msg.Saved.Hosts).Select(h.ID))
		m.status = domain.Sanitize(h.Label) + ": saved"
		return m, nil

	case HostSaveFailedMsg:
		if f, ok := m.screen.(hostedit.Model); ok {
			m.screen = f.SaveFailed(msg.Err)
		} else {
			m.status = components.ErrorPrefix + "not saved: " + domain.Sanitize(msg.Err.Error())
		}
		return m, nil

	case hostedit.CancelledMsg:
		if _, ok := m.screen.(hostedit.Model); ok && m.prev != nil {
			m.modals = nil
			m.screen = m.sized(m.prev)
		}
		return m, nil

	case hostlist.DeleteHostMsg:
		svc, id, label := m.svc, msg.HostID, msg.Label
		return m, func() tea.Msg {
			hosts, err := svc.DeleteHost(context.Background(), id)
			if err != nil {
				return HostDeleteFailedMsg{Label: label, Err: err}
			}
			return HostDeletedMsg{Label: label, Hosts: hosts}
		}

	case HostDeletedMsg:
		row := 0
		if l, ok := m.screen.(hostlist.Model); ok {
			row = l.Cursor()
		}
		m.screen = m.sized(hostsScreen(msg.Hosts))
		if l, ok := m.screen.(hostlist.Model); ok {
			m.screen = l.SelectRow(row)
		}
		m.status = domain.Sanitize(msg.Label) + ": deleted"
		return m, nil

	case HostDeleteFailedMsg:
		m.status = components.ErrorPrefix + domain.Sanitize(msg.Label) + ": not deleted: " + domain.Sanitize(msg.Err.Error())
		return m, nil

	case components.ShowModalMsg:
		m.modals = append(m.modals, msg.Modal)
		return m, nil

	case confirm.DoneMsg:
		m.popModal()
		if msg.Then != nil {
			then := msg.Then
			return m, func() tea.Msg { return then }
		}
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

	case app.ConnectProgressMsg:
		if m.cancel != nil && msg.HostID == m.attemptHost && len(m.modals) > 0 {
			if c, ok := m.modals[0].(connecting.Model); ok {
				m.modals[0] = c.WithStage(msg.Stage)
			}
		}
		return m, nil

	case app.AuthPromptMsg:
		if m.cancel == nil {
			msg.Cancel() // the attempt is already over
			return m, nil
		}
		m.modals = append(m.modals, prompt.New(msg))
		return m, nil

	case prompt.DoneMsg:
		// The connection goes on, or fails with ErrAuthCancelled;
		// Connecting stays up until then.
		m.popModal()
		return m, nil

	case tea.PasteMsg:
		if n := len(m.modals); n > 0 {
			var cmd tea.Cmd
			m.modals[n-1], cmd = m.modals[n-1].Update(msg)
			return m, cmd
		}

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
		m.status = components.ErrorPrefix + failedStatus(msg.Label, msg.Err)
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

// openForm shows the host form for h (the zero Host to add one) and keeps
// the screen to return to.
func (m Model) openForm(h domain.Host) Model {
	var hosts []domain.Host
	switch s := m.screen.(type) {
	case hostlist.Model:
		hosts = s.Hosts()
	case welcome.Model:
	default:
		return m
	}
	m.prev = m.screen
	m.status = ""
	m.screen = m.sized(hostedit.New(h, hosts, m.identities))
	return m
}

// hostsScreen is the host list, or the Welcome screen when there are no
// hosts (FR-NAV-013).
func hostsScreen(hosts []domain.Host) components.Screen {
	if len(hosts) == 0 {
		return welcome.New()
	}
	return hostlist.New(hosts)
}

// bodyHeight is the number of rows between the title and the status line.
func (m Model) bodyHeight() int { return max(m.height-4, 1) }

// sized tells s the size of the area it draws in, once the terminal size
// is known.
func (m Model) sized(s components.Screen) components.Screen {
	if s == nil || m.width == 0 {
		return s
	}
	s, _ = s.Update(tea.WindowSizeMsg{Width: m.width, Height: m.bodyHeight()})
	return s
}

// startConnect pushes the Connecting modal and starts the connect command.
func (m Model) startConnect(hostID domain.ID, label string) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.attempt++
	m.attemptHost = hostID
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
	label = domain.Sanitize(label)
	if errors.Is(err, app.ErrAuthCancelled) {
		return label + ": authentication cancelled"
	}
	if errors.Is(err, app.ErrAuthFailed) {
		return label + ": authentication failed"
	}
	if errors.Is(err, app.ErrNameResolution) {
		return label + ": name resolution failed"
	}
	if errors.Is(err, app.ErrConnectionRefused) {
		return label + ": connection refused"
	}
	if errors.Is(err, app.ErrTimedOut) {
		return label + ": timed out"
	}
	if se, ok := errors.AsType[*app.StoreError](err); ok {
		return label + ": " + se.Error()
	}
	if errors.Is(err, app.ErrHostKeyRejected) {
		return label + ": host key not trusted; connection cancelled"
	}
	if errors.Is(err, app.ErrHostKeyMismatch) {
		return label + ": host key changed; connection blocked"
	}
	if re, ok := errors.AsType[*app.HostKeyRevokedError](err); ok {
		return fmt.Sprintf("%s: host key revoked (%s line %d); connection refused", label, re.Entry.File, re.Entry.Line)
	}
	return label + ": " + domain.Sanitize(err.Error()) // may carry server text
}

// endedStatus is the status-bar text after a session (FR-CONN-005), e.g.
// "web-prod: exited (3)" or "web-prod: connection lost: <reason>".
func endedStatus(label string, err error) string {
	label = domain.Sanitize(label)
	if err == nil {
		return label + ": exited (0)"
	}
	return label + ": " + domain.Sanitize(err.Error()) // may carry server text
}

// View implements tea.Model. The layout is the title, a blank line, the
// active screen with the modals under it, the status line and the footer.
// Once the terminal size is known the view fills it exactly: the body is
// cut or padded so that the footer is always the last line, and every line
// is truncated to the width with "…" (NFR-UX-002).
func (m Model) View() tea.View {
	body, foot := m.screenLines()
	if n := len(m.modals); n > 0 {
		var ml []string
		for i, md := range m.modals {
			if i > 0 {
				ml = append(ml, "")
			}
			ml = append(ml, viewLines(md.View())...)
		}
		if h, ok := m.modals[n-1].(components.Hinter); ok {
			foot = components.Help(h.Hints()...)
		}
		if m.height == 0 || len(body)+1+len(ml) <= m.bodyHeight() {
			body = append(append(body, ""), ml...)
		} else {
			body = ml // the modals have the keyboard; the screen gives way
		}
	}

	out := []string{titleStyle.Render("sshelf"), ""}
	if m.height > 0 {
		h := m.bodyHeight()
		body = body[:min(len(body), h)]
		for len(body) < h {
			body = append(body, "")
		}
		out = append(out, body...)
		out = append(out, statusStyle.Render(m.status))
	} else {
		out = append(out, body...)
		if m.status != "" {
			out = append(out, "", statusStyle.Render(m.status))
		}
	}
	out = append(out, foot)
	if m.width > 0 {
		for i, l := range out {
			out[i] = ansi.Truncate(l, m.width, "…")
		}
	}

	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	return v
}

// footer returns the footer line the view ends with.
func (m Model) footer() string {
	_, foot := m.screenLines()
	if n := len(m.modals); n > 0 {
		if h, ok := m.modals[n-1].(components.Hinter); ok {
			foot = components.Help(h.Hints()...)
		}
	}
	return foot
}

// screenLines returns the active screen's lines and its footer: the hints
// of a components.Hinter, or else the screen's own last line.
func (m Model) screenLines() (body []string, foot string) {
	if m.screen == nil {
		return []string{dimStyle.Render("Loading…")}, components.Help(keys.Quit)
	}
	body = viewLines(m.screen.View())
	if h, ok := m.screen.(components.Hinter); ok {
		return body, components.Help(h.Hints()...)
	}
	n := len(body)
	foot, body = body[n-1], body[:n-1]
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
	}
	return body, foot
}

// viewLines splits a view into lines, without the trailing newline.
func viewLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}
