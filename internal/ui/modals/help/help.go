// Package help is the prefix-key help overlay (FR-TAB-005): every command
// after Ctrl+], generated from ui/keys (ARCH-UI-004).
package help

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true)
	keyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
)

// Model is the help overlay. ? or Esc closes it; it is popped at once
// (components.Closer).
type Model struct{ closed bool }

// New returns the overlay.
func New() Model { return Model{} }

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && key.Matches(k, keys.CloseHelp) {
		m.closed = true
	}
	return m, nil
}

// Closed implements components.Closer.
func (m Model) Closed() bool { return m.closed }

// View implements components.Modal.
func (m Model) View() string {
	cmds := keys.PrefixCommands()
	width := 0
	for _, b := range cmds {
		width = max(width, len(b.Help().Key))
	}
	prefix := keys.Prefix.Help().Key
	lines := []string{titleStyle.Render("Press " + prefix + ", then:"), ""}
	for _, b := range cmds {
		h := b.Help()
		lines = append(lines, keyStyle.Render(h.Key+strings.Repeat(" ", width-len(h.Key)))+"  "+h.Desc)
	}
	return boxStyle.Render(strings.Join(lines, "\n"))
}

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding { return []key.Binding{keys.CloseHelp} }
