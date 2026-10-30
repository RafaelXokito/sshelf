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
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, keys.CloseHelp) {
			m.closed = true
		}
	case components.ClickMsg:
		if _, b := m.render(); b.At(msg.X, msg.Y) == 0 {
			m.closed = true
		}
	}
	return m, nil
}

// CloseButton is the overlay's button (FR-NAV-016).
const CloseButton = "[esc] Close"

// Closed implements components.Closer.
func (m Model) Closed() bool { return m.closed }

// View implements components.Modal.
func (m Model) View() string {
	v, _ := m.render()
	return v
}

// render builds the View and the place of its button together.
func (m Model) render() (string, components.ModalButtons) {
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
	var b strings.Builder
	b.WriteString(strings.Join(lines, "\n") + "\n\n")
	btns := components.AddButtons(&b, CloseButton)
	return boxStyle.Render(b.String()), btns
}

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding { return []key.Binding{keys.CloseHelp} }
