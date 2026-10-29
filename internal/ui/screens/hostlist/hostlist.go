package hostlist

import (
	"net"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// ConnectRequestMsg asks the root model to connect to a host.
type ConnectRequestMsg struct {
	HostID domain.ID
	Label  string
}

// Model is the host list: one line per host (label and user@hostname:port)
// with a cursor. Sorting, port-22 omission and truncation come in T-016.
type Model struct {
	hosts  []domain.Host
	cursor int
}

// New returns a list of hosts with the first one selected.
func New(hosts []domain.Host) Model { return Model{hosts: hosts} }

// Selected returns the host under the cursor.
func (m Model) Selected() (domain.Host, bool) {
	if m.cursor < 0 || m.cursor >= len(m.hosts) {
		return domain.Host{}, false
	}
	return m.hosts[m.cursor], true
}

// Select moves the cursor to the host with id, if it is listed.
func (m Model) Select(id domain.ID) Model {
	if i := slices.IndexFunc(m.hosts, func(h domain.Host) bool { return h.ID == id }); i >= 0 {
		m.cursor = i
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
	case key.Matches(k, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(k, keys.Down):
		if m.cursor < len(m.hosts)-1 {
			m.cursor++
		}
	case key.Matches(k, keys.Connect):
		if h, ok := m.Selected(); ok {
			return m, func() tea.Msg { return ConnectRequestMsg{HostID: h.ID, Label: h.Label} }
		}
	case key.Matches(k, keys.Quit):
		return m, tea.Quit
	}
	return m, nil
}

// View implements components.Screen.
func (m Model) View() string {
	if len(m.hosts) == 0 {
		return dimStyle.Render("No hosts yet.") + "\n"
	}
	var b strings.Builder
	for i, h := range m.hosts {
		line := Line(h)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Line renders one host: its label and user@hostname:port (FR-NAV-001).
func Line(h domain.Host) string {
	port := h.Port
	if port == 0 {
		port = domain.DefaultPort
	}
	addr := net.JoinHostPort(h.Hostname, strconv.Itoa(port))
	if h.User != "" {
		addr = h.User + "@" + addr
	}
	return padRight(h.Label, 20) + " " + addr
}

func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
