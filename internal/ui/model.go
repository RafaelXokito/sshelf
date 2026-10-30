// Package ui is the root Bubble Tea model: it routes messages to one active
// screen and a stack of modals (design §9.2), and runs every I/O operation
// through app.Service inside tea.Cmd functions (ARCH-UI-002).
package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/editor"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/keychanged"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/prompt"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/tofu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostedit"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/identities"
	importscreen "github.com/RafaelXokito/sshelf/internal/ui/screens/import"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/loaderror"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/welcome"
)

var (
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

// ImportedMsg carries the result of importing ~/.ssh/config.
type ImportedMsg struct{ Report app.ImportReport }

// ImportFailedMsg reports that ~/.ssh/config could not be imported.
type ImportFailedMsg struct{ Err error }

// HostDeletedMsg reports that a host was deleted; Hosts are the rest.
type HostDeletedMsg struct {
	Label      string
	Hosts      []domain.Host
	Identities []domain.Identity
	Warnings   []string // stored items of its hidden identity left behind
}

// HostDeleteFailedMsg reports that a host was not deleted.
type HostDeleteFailedMsg struct {
	Label string
	Err   error
}

// ConnectedMsg carries an authenticated session; it opens a tab, or
// reconnects an ended one (FR-TAB-002, FR-CONN-010).
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

// Model is the root Bubble Tea model.
type Model struct {
	svc    app.Service
	screen components.Screen // nil while loading
	modals []components.Modal
	status string

	identities []domain.Identity // for the host form's picker
	prev       components.Screen // the screen to return to while a form is open
	idView     identities.Model  // the identities view to return to from its form
	noStore    *app.StoreError   // set when no credential store is available (D-101)

	width, height int // terminal size; 0 until the first tea.WindowSizeMsg

	attempt     int                // id of the latest connection attempt
	attemptHost domain.ID          // host of the attempt in progress
	attemptAddr string             // user@host:port of that host
	cancel      context.CancelFunc // cancels the attempt in progress; nil if none

	// Session tabs (design §9.4, ADR-015).
	tabs    []*sessionTab // in opening order
	focus   int           // 0 = Hosts tab, i = tabs[i-1]
	nextNum int           // the last tab number given; numbers are never reused
	ticking bool          // a tickMsg is on its way
	prefix  bool          // ctrl+] seen, waiting for the command key (design §8.5)

	attemptTab int  // the ended tab the attempt in progress reconnects; 0 for a new tab
	quitAsked  bool // the "Quit and close N tabs?" confirmation is open

	// fg and bg are the terminal's default colours once Bubble Tea
	// reports them; every session answers OSC 10/11 with them
	// (ARCH-SESS-016). nil until reported.
	fg, bg color.Color

	clicks components.ClickTracker // recognises double-clicks (ARCH-UI-007)

	home string // the local home directory, where ~ goes in a local pane

	now    func() time.Time                     // injectable clock
	tick   func() tea.Cmd                       // the one-second clock; tests replace it
	listen func(num int, s app.Session) tea.Cmd // waits for a session's next update; tests replace it
	// listenSFTP waits for an SFTP tab's next queue change or drop, and
	// progressTick is its 250 ms redraw; tests replace both.
	listenSFTP   func(num, gen int, b app.Browser, watchDone bool, stop <-chan struct{}) tea.Cmd
	progressTick func(num int, b app.Browser) tea.Cmd

	// editing is set from e until the edit ends: one edit at a time
	// (FR-SFTP-019). runEditor runs the editor (editor.Run); tests
	// replace it and never start a program.
	editing   bool
	runEditor func(path string, done func(error) tea.Msg) tea.Cmd
}

// New returns the root model. It does no I/O; Init loads the config.
func New(svc app.Service) Model {
	return Model{svc: svc, now: time.Now, tick: tickEverySecond, listen: listenSession, home: localHome(),
		listenSFTP: listenBrowser, progressTick: progressTick, runEditor: editor.Run}
}

// Init implements tea.Model. Besides loading the config it asks the
// terminal for its default colours, for the sessions' emulators
// (ARCH-SESS-016).
func (m Model) Init() tea.Cmd {
	svc := m.svc
	return tea.Batch(func() tea.Msg {
		l, err := svc.Load(context.Background())
		return LoadedMsg{Hosts: l.Hosts, Identities: l.Identities, Notices: l.Notices, Err: err}
	}, probeStore(svc), tea.RequestBackgroundColor, tea.RequestForegroundColor)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.updateMouse(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.updateIdentities(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.updateSessions(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.updateSFTP(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.updateTransfers(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.updateEdit(msg); ok {
		return next, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.screen = m.sized(m.screen)
		for _, t := range m.tabs {
			switch {
			case t.sftp != nil:
				t.sftp.view = t.sftp.view.SetSize(m.width, m.height)
			case !t.done:
				t.sess.Resize(m.areaSize())
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		// Key routing, in order (design §9.4): Ctrl+C on the Hosts tab,
		// the top modal, the prefix key, the focused tab.
		if m.focus == 0 && key.Matches(msg, keys.ForceQuit) {
			m.prefix = false
			if fs, ok := m.screen.(components.FormScreen); ok {
				// Ctrl+C cancels a form instead of quitting (FR-NAV-003).
				m.modals = nil
				return m.Update(fs.Cancel())
			}
			return m.quit()
		}
		if len(m.modals) > 0 {
			m.prefix = false // the prefix key is off while a modal is open
			return m.updateTopModal(msg)
		}
		if armed, r := keys.PrefixStep(m.prefix, msg); r.Action != keys.PrefixNone {
			m.prefix = armed
			return m.runPrefix(r)
		}
		if t := m.focused(); t != nil {
			return m.sessionKey(t, msg)
		}
		if t := m.focusedSFTP(); t != nil {
			return m.sftpKey(t, msg)
		}
		if tabs, transfers := m.openWork(); key.Matches(msg, keys.Quit) && m.quitKeyQuits() && tabs+transfers > 0 {
			return m.quit() // q on the host list or Welcome asks first (FR-TAB-009)
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
		m.screen = m.sized(m.hostsScreen(msg.Hosts))
		return m, nil

	case hostlist.AddHostMsg, welcome.AddHostMsg:
		return m.openForm(domain.Host{}), nil

	case hostlist.EditHostMsg:
		return m.openForm(msg.Host), nil

	case hostedit.SaveMsg:
		svc, h, c := m.svc, msg.Host, msg.Creds
		return m, func() tea.Msg {
			saved, err := svc.SaveHostWithCredentials(context.Background(), h, c)
			if err != nil {
				return HostSaveFailedMsg{Err: err}
			}
			return HostSavedMsg{Saved: saved}
		}

	case HostSavedMsg:
		// Saved even if the form was cancelled meanwhile: show and select
		// the host (FR-HOST-001).
		h := msg.Saved.Host
		if f, ok := m.screen.(hostedit.Model); ok {
			f.Destroy()
		}
		if msg.Saved.Identities != nil {
			m.identities = msg.Saved.Identities
		}
		m.modals = nil
		m.screen = m.sized(hostlist.New(msg.Saved.Hosts).WithClock(m.now).Select(h.ID))
		m.status = domain.Sanitize(strings.Join(append([]string{h.Label + ": saved"}, msg.Saved.Warnings...), "; "))
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

	case hostlist.ImportMsg, welcome.ImportMsg:
		svc := m.svc
		m.status = "importing ~/.ssh/config…"
		return m, func() tea.Msg {
			rep, err := svc.ImportSSHConfig(context.Background())
			if err != nil {
				return ImportFailedMsg{Err: err}
			}
			return ImportedMsg{Report: rep}
		}

	case ImportedMsg:
		m.identities = msg.Report.AllIdentities
		m.modals = nil
		m.status = ""
		m.screen = m.sized(importscreen.New(msg.Report))
		return m, nil

	case ImportFailedMsg:
		m.status = components.ErrorPrefix + "import failed: " + domain.Sanitize(msg.Err.Error())
		return m, nil

	case importscreen.DoneMsg:
		if r, ok := m.screen.(importscreen.Model); ok {
			m.screen = m.sized(m.hostsScreen(r.Report().AllHosts))
		}
		return m, nil

	case hostlist.DeleteHostMsg:
		svc, id, label := m.svc, msg.HostID, msg.Label
		return m, func() tea.Msg {
			del, err := svc.DeleteHost(context.Background(), id)
			if err != nil {
				return HostDeleteFailedMsg{Label: label, Err: err}
			}
			return HostDeletedMsg{Label: label, Hosts: del.Hosts, Identities: del.Identities, Warnings: del.Warnings}
		}

	case HostDeletedMsg:
		row := 0
		if l, ok := m.screen.(hostlist.Model); ok {
			row = l.Cursor()
		}
		m.screen = m.sized(m.hostsScreen(msg.Hosts))
		if l, ok := m.screen.(hostlist.Model); ok {
			m.screen = l.SelectRow(row)
		}
		if msg.Identities != nil {
			m.identities = msg.Identities
		}
		m.status = domain.Sanitize(strings.Join(append([]string{msg.Label + ": deleted"}, msg.Warnings...), "; "))
		return m, nil

	case HostDeleteFailedMsg:
		m.status = components.ErrorPrefix + domain.Sanitize(msg.Label) + ": not deleted: " + domain.Sanitize(msg.Err.Error())
		return m, nil

	case components.ShowModalMsg:
		m.modals = append(m.modals, msg.Modal)
		return m, nil

	case confirm.DoneMsg:
		m.popModal()
		m.quitAsked = false
		if msg.Then != nil {
			then := msg.Then
			return m, func() tea.Msg { return then }
		}
		return m, nil

	case hostlist.ConnectRequestMsg:
		if m.cancel != nil {
			return m, nil // one attempt at a time
		}
		m.attemptAddr, m.attemptTab = "", msg.Tab
		if t := m.attemptTabPtr(); t != nil {
			m.attemptAddr = t.addr
		}
		if h, ok := m.hostByID(msg.HostID); ok {
			m.attemptAddr = sessionAddr(h)
		}
		return m.startConnect(msg.HostID, msg.Label)

	case connecting.CancelMsg:
		if m.cancel == nil {
			return m, nil
		}
		m.cancelAttempt()
		m.modals = nil
		if t := m.attemptTabPtr(); t != nil {
			t.failure = "connection cancelled" // the tab stays ended (FR-CONN-010)
		} else {
			m.status = "connection cancelled"
		}
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
		// The prompt was popped when it was answered (components.Closer).
		// The connection goes on, or fails with ErrAuthCancelled;
		// Connecting stays up until then.
		return m, nil

	case tea.PasteMsg:
		if len(m.modals) > 0 {
			return m.updateTopModal(msg)
		}
		if t := m.focused(); t != nil {
			if !t.done {
				_ = t.sess.Paste(msg.Content) // never blocks; a full queue drops it (ARCH-SESS-014)
			}
			return m, nil
		}
		if t := m.focusedSFTP(); t != nil {
			var cmd tea.Cmd
			t.sftp.view, cmd = t.sftp.view.Update(msg)
			return m, cmd
		}

	case tofu.DoneMsg:
		// The modal was popped when it was answered (components.Closer).
		// The connection goes on (trusted) or fails with
		// ErrHostKeyRejected; Connecting stays up until then.
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
		m = m.markConnected(msg.HostID)
		if t := m.attemptTabPtr(); t != nil {
			return m.reconnected(t, msg.Session)
		}
		return m.openTab(msg.HostID, msg.Label, m.attemptAddr, msg.Session)

	case ConnectFailedMsg:
		if msg.attempt != m.attempt || m.cancel == nil {
			return m, nil
		}
		m.cancelAttempt()
		m.modals = nil
		if t := m.attemptTabPtr(); t != nil {
			t.failure = failedStatus(msg.Label, msg.Err) // the tab stays ended (FR-CONN-010)
		} else {
			m.status = components.ErrorPrefix + failedStatus(msg.Label, msg.Err)
		}
		if me, ok := errors.AsType[*app.HostKeyMismatchError](msg.Err); ok {
			m.modals = append(m.modals, keychanged.New(msg.Label, me))
		}
		return m, nil

	case keychanged.CloseMsg:
		m.popModal()
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
	f := hostedit.New(h, hosts, m.identities)
	if m.noStore != nil {
		f = f.WithoutStore(m.noStore.Store)
	}
	m.screen = m.sized(f)
	return m
}

// hostsScreen is the host list, or the Welcome screen when there are no
// hosts (FR-NAV-013).
func (m Model) hostsScreen(hosts []domain.Host) components.Screen {
	if len(hosts) == 0 {
		return welcome.New()
	}
	return hostlist.New(hosts).WithClock(m.now)
}

// markConnected re-sorts the host list, shown or kept behind a form, after
// a session to the host with id was established (FR-NAV-018). The app has
// recorded and saved the time; the list mirrors it with the UI's clock, so
// it is in order by the time the user returns to the Hosts tab.
func (m Model) markConnected(id domain.ID) Model {
	now := m.now()
	if l, ok := m.screen.(hostlist.Model); ok {
		m.screen = l.Connected(id, now)
	}
	if l, ok := m.prev.(hostlist.Model); ok {
		m.prev = l.Connected(id, now)
	}
	return m
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
	var size app.Size // 80×24 until the terminal size is known
	if m.width > 0 {
		size = m.areaSize()
	}
	return m, func() tea.Msg {
		s, err := svc.Connect(ctx, hostID, size)
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

// updateTopModal sends msg to the top modal and pops it at once if that
// answered it (components.Closer).
func (m Model) updateTopModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	n := len(m.modals)
	var cmd tea.Cmd
	m.modals[n-1], cmd = m.modals[n-1].Update(msg)
	if c, ok := m.modals[n-1].(components.Closer); ok && c.Closed() {
		m.popModal()
	}
	return m, cmd
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

// View implements tea.Model. The first line is the header with the tab
// bar (FR-TAB-003). On the Hosts tab the layout below it is the MVP one:
// a blank line, the active screen with the modals under it, the status
// line and the footer; a session tab is drawn by sessionView
// (ARCH-UI-005). Once the terminal size is known the view fills it
// exactly: the body is cut or padded so that the footer is always the last
// line, and every line is truncated to the width with "…" (NFR-UX-002).
func (m Model) View() tea.View {
	if t := m.focused(); t != nil {
		return m.sessionView(t)
	}
	if t := m.focusedSFTP(); t != nil {
		return m.sftpView(t)
	}
	out, _ := m.hostsFrame()
	if m.width > 0 {
		for i, l := range out {
			out[i] = truncateLine(l, m.width)
		}
	}

	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	v.MouseMode = m.mouseMode(app.MouseNone)
	return v
}

// hostsFrameTop is the first row of the Hosts tab's body: below the
// header and a blank row.
const hostsFrameTop = 2

// hostsFrame lays out the Hosts tab: the lines of the frame, before
// truncation to the width, and where the screen and each modal sit in
// them. View and hit-testing share it (ARCH-UI-007).
func (m Model) hostsFrame() ([]string, frameLayout) {
	var lay frameLayout
	body, foot := m.screenLines()
	banner := len(m.withBanner(nil))
	body = m.withBanner(body)
	lay.screen = region{Y: hostsFrameTop + banner, W: m.viewWidth(), H: len(body) - banner}
	if n := len(m.modals); n > 0 {
		var ml []string
		tops := make([]int, n)
		for i, md := range m.modals {
			if i > 0 {
				ml = append(ml, "")
			}
			lines := viewLines(md.View())
			tops[i] = len(ml)
			lay.modals = append(lay.modals, region{W: linesWidth(lines), H: len(lines)})
			ml = append(ml, lines...)
		}
		if h, ok := m.modals[n-1].(components.Hinter); ok {
			foot = components.Help(h.Hints()...)
		}
		start := len(body) + 1
		if m.height == 0 || len(body)+1+len(ml) <= m.bodyHeight() {
			body = append(append(body, ""), ml...)
		} else {
			// The modals have the keyboard; the screen gives way, and if
			// the stack is still too tall its bottom, with the top
			// modal's buttons, stays in view.
			cut := max(len(ml)-m.bodyHeight(), 0)
			body = ml[cut:]
			start, lay.screen.H = -cut, 0
		}
		for i := range lay.modals {
			lay.modals[i].Y = hostsFrameTop + start + tops[i]
		}
	}
	if m.height > 0 {
		lay = lay.clip(hostsFrameTop, hostsFrameTop+m.bodyHeight())
	}

	out := []string{m.header(), ""}
	if m.height > 0 {
		h := m.bodyHeight()
		body = body[:min(len(body), h)]
		for len(body) < h {
			body = append(body, "")
		}
		out = append(out, body...)
		out = append(out, statusStyle.Render(m.statusLine()))
	} else {
		out = append(out, body...)
		if m.statusLine() != "" {
			out = append(out, "", statusStyle.Render(m.statusLine()))
		}
	}
	out = append(out, foot)
	return out, lay
}

// truncateLine cuts l to width columns with "…" (NFR-UX-002).
func truncateLine(l string, width int) string { return ansi.Truncate(l, width, "…") }

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
