package connecting

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

// CancelMsg asks the root model to cancel the connection attempt.
type CancelMsg struct{}

// Model is the Connecting modal. Connection stages come in T-014.
type Model struct {
	label string
}

// New returns the modal for a connection to the host labelled label.
func New(label string) Model { return Model{label: label} }

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && key.Matches(k, keys.Cancel) {
		return m, func() tea.Msg { return CancelMsg{} }
	}
	return m, nil
}

// View implements components.Modal.
func (m Model) View() string {
	return boxStyle.Render("Connecting to " + m.label + "…\n\nesc cancel")
}
