package identities

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// OpenMsg asks the root model to open the identities view from the host
// list (FR-NAV-003 `i`).
type OpenMsg struct{}

// AddMsg asks the root model to open the identity form for a new identity.
type AddMsg struct{}

// EditMsg asks the root model to open the identity form for Info.
type EditMsg struct{ Info app.IdentityInfo }

// BackMsg asks the root model to return to the host list.
type BackMsg struct{}

// Model is the identities view.
type Model struct {
	list    []app.IdentityInfo
	cursor  int
	loading bool
}

// Loading returns the view while the identities are being read.
func Loading() Model { return Model{loading: true} }

// New returns the view of list, already without hidden identities, with
// the first one selected.
func New(list []app.IdentityInfo) Model { return Model{list: list} }

// IsLoading reports whether the view waits for its identities.
func (m Model) IsLoading() bool { return m.loading }

// List returns the listed identities. The caller must not modify them.
func (m Model) List() []app.IdentityInfo { return m.list }

// Selected returns the identity under the cursor.
func (m Model) Selected() (app.IdentityInfo, bool) {
	if m.cursor < 0 || m.cursor >= len(m.list) {
		return app.IdentityInfo{}, false
	}
	return m.list[m.cursor], true
}

// Select moves the cursor to the identity with id, if it is listed.
func (m Model) Select(id domain.ID) Model {
	for i, info := range m.list {
		if info.Identity.ID == id {
			m.cursor = i
		}
	}
	return m
}

// Update implements components.Screen.
func (m Model) Update(msg tea.Msg) (components.Screen, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(k, keys.Back):
		return m, func() tea.Msg { return BackMsg{} }
	case m.loading:
	case key.Matches(k, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(k, keys.Down):
		if m.cursor < len(m.list)-1 {
			m.cursor++
		}
	case key.Matches(k, keys.AddIdentity):
		return m, func() tea.Msg { return AddMsg{} }
	case key.Matches(k, keys.EditIdentity):
		if info, ok := m.Selected(); ok {
			return m, func() tea.Msg { return EditMsg{Info: info} }
		}
	}
	return m, nil
}

// View implements components.Screen.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Identities") + "\n\n")
	switch {
	case m.loading:
		b.WriteString(dimStyle.Render("Loading…") + "\n")
	case len(m.list) == 0:
		b.WriteString(dimStyle.Render("No identities yet.") + "\n")
	default:
		for i, info := range m.list {
			line := Line(info)
			if i == m.cursor {
				b.WriteString(selectedStyle.Render("> " + line))
			} else {
				b.WriteString("  " + line)
			}
			b.WriteString("\n")
		}
		if info, ok := m.Selected(); ok {
			b.WriteString("\n" + Details(info))
		}
	}
	b.WriteString("\n" + components.Help(keys.Up, keys.Down, keys.AddIdentity, keys.EditIdentity, keys.Back) + "\n")
	return b.String()
}

// Line renders one identity: its name, username and number of hosts.
func Line(info app.IdentityInfo) string {
	id := info.Identity
	user := id.Username
	if user == "" {
		user = "-"
	}
	hosts := strconv.Itoa(info.LinkedHosts) + " hosts"
	if info.LinkedHosts == 1 {
		hosts = "1 host"
	}
	return pad(domain.Sanitize(id.Label), 20) + " " + pad(domain.Sanitize(user), 12) + " " + hosts
}

// Details renders the selected identity's credentials: each secret only
// as "set", "not set" or "not set on this device" (SEC-LOG-005), and the
// key file path.
func Details(info app.IdentityInfo) string {
	id := info.Identity
	rows := [][2]string{
		{"Password", info.Password.String()},
		{"Key file", keyFile(id)},
		{"Stored key", info.Key.String()},
		{"Passphrase", info.Passphrase.String()},
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString("  " + dimStyle.Render(pad(r[0], 11)) + " " + r[1] + "\n")
	}
	return b.String()
}

func keyFile(id domain.Identity) string {
	if id.KeyPath == "" {
		return "none"
	}
	return domain.Sanitize(id.KeyPath)
}

func pad(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
