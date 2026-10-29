// Package ui is the root Bubble Tea model. For now it shows a plain,
// selectable list of hosts; the router and modal stack come later.
package ui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).MarginBottom(1)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

var (
	keyUp   = key.NewBinding(key.WithKeys("up", "k"))
	keyDown = key.NewBinding(key.WithKeys("down", "j"))
	keyQuit = key.NewBinding(key.WithKeys("q", "ctrl+c"))
)

// Model is the root Bubble Tea model.
type Model struct {
	hosts  []domain.Host
	cursor int
}

// New returns a model listing hosts.
func New(hosts []domain.Host) Model {
	return Model{hosts: hosts}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(k, keyQuit):
			return m, tea.Quit
		case key.Matches(k, keyUp):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(k, keyDown):
			if m.cursor < len(m.hosts)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render("sshelf"))
	b.WriteString("\n")

	if len(m.hosts) == 0 {
		b.WriteString(dimStyle.Render("No hosts yet."))
		b.WriteString("\n")
	}
	for i, h := range m.hosts {
		line := fmt.Sprintf("%-20s %s@%s", h.Label, h.User, address(h))
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑/↓ move • q quit"))
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// address returns host:port with port 22 when unset. domain.Resolve replaces
// it once groups and settings exist.
func address(h domain.Host) string {
	port := h.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(h.Hostname, strconv.Itoa(port))
}
