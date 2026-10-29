package confirm

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
)

// DoneMsg tells the root model that the question was answered. The root
// pops the modal and, when Then is not nil (the answer was yes), delivers
// Then as a message.
type DoneMsg struct{ Then tea.Msg }

// Model asks a yes/no question. Only y answers yes; n, enter and esc
// answer no, so the default is "No".
type Model struct {
	question string
	then     tea.Msg
}

// New returns the modal. then is delivered when the user answers yes.
func New(question string, then tea.Msg) Model { return Model{question: question, then: then} }

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(k, keys.ConfirmYes):
		then := m.then
		return m, func() tea.Msg { return DoneMsg{Then: then} }
	case key.Matches(k, keys.ConfirmNo):
		return m, func() tea.Msg { return DoneMsg{} }
	}
	return m, nil
}

// View implements components.Modal.
func (m Model) View() string {
	return boxStyle.Render(titleStyle.Render(m.question))
}

// Hints implements components.Hinter: the footer shows the answers.
func (m Model) Hints() []key.Binding { return []key.Binding{keys.ConfirmYes, keys.ConfirmNo} }
