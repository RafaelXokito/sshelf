package confirm

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
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
)

// Button labels (FR-NAV-016).
const (
	YesButton = "[y] Yes"
	NoButton  = "[n] No"
)

// DoneMsg tells the root model that the question was answered. The root
// pops the modal and, when Then is not nil (the answer was yes), delivers
// Then as a message.
type DoneMsg struct{ Then tea.Msg }

// Model asks a yes/no question. Only y or a click on [y] Yes answers
// yes; n, enter, esc and a click on [n] No answer no, so the default is
// "No".
type Model struct {
	question string
	then     tea.Msg
}

// New returns the modal. then is delivered when the user answers yes.
func New(question string, then tea.Msg) Model { return Model{question: question, then: then} }

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.ConfirmYes):
			return m, m.answer(true)
		case key.Matches(msg, keys.ConfirmNo):
			return m, m.answer(false)
		}
	case components.ClickMsg:
		_, b := m.render()
		switch b.At(msg.X, msg.Y) {
		case 0:
			return m, m.answer(true)
		case 1:
			return m, m.answer(false)
		}
	}
	return m, nil
}

// answer returns the command that reports the answer.
func (m Model) answer(yes bool) tea.Cmd {
	if !yes {
		return func() tea.Msg { return DoneMsg{} }
	}
	then := m.then
	return func() tea.Msg { return DoneMsg{Then: then} }
}

// View implements components.Modal.
func (m Model) View() string {
	v, _ := m.render()
	return v
}

// render builds the View and the place of its buttons together.
func (m Model) render() (string, components.ModalButtons) {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.question) + "\n\n")
	btns := components.AddButtons(&b, YesButton, NoButton)
	return boxStyle.Render(b.String()), btns
}

// Hints implements components.Hinter: the footer shows the answers.
func (m Model) Hints() []key.Binding { return []key.Binding{keys.ConfirmYes, keys.ConfirmNo} }
