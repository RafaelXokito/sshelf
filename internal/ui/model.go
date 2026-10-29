// Package ui implements the sshelf terminal interface using Bubble Tea.
package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RafaelXokito/sshelf/internal/hosts"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).MarginBottom(1)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// Model is the root Bubble Tea model: a selectable list of hosts.
type Model struct {
	hosts  []hosts.Host
	cursor int
}

func New(h []hosts.Host) Model {
	return Model{hosts: h}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.hosts)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("sshelf"))
	b.WriteString("\n")

	if len(m.hosts) == 0 {
		b.WriteString(dimStyle.Render("No hosts yet."))
		b.WriteString("\n")
	}
	for i, h := range m.hosts {
		line := fmt.Sprintf("%-20s %s@%s", h.Label, h.User, h.Address())
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
	return b.String()
}
