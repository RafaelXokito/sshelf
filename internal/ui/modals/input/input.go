// Package input is the one-line text modal of the SFTP tab: rename and
// mkdir (FR-SFTP-010, FR-SFTP-012). It refuses a value its check
// rejects, with the reason, and stays open.
package input

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
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// Button labels (FR-NAV-016).
const (
	OKButton     = "[enter] OK"
	CancelButton = "[esc] Cancel"
)

// fieldWidth is the width of the text field.
const fieldWidth = 40

// Model asks for one line of text. Enter submits it when check accepts
// it; Esc cancels. Either closes the modal at once (components.Closer).
type Model struct {
	title  string
	field  *components.TextInput
	check  func(string) string // the reason a value is refused, or ""
	done   func(string) tea.Msg
	reason string
	closed bool
}

// New returns the modal titled title with value filled in. check returns
// why a value is refused, or ""; done is the message delivered with an
// accepted value.
func New(title, value string, check func(string) string, done func(string) tea.Msg) Model {
	f := components.NewTextInput()
	f.Width = fieldWidth
	f.SetValue(value)
	f.Focus()
	f.End()
	return Model{title: title, field: f, check: check, done: done}
}

// Value is the text typed so far.
func (m Model) Value() string { return m.field.Value() }

// Reason is why the last submitted value was refused, or "".
func (m Model) Reason() string { return m.reason }

// Closed implements components.Closer.
func (m Model) Closed() bool { return m.closed }

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.Submit):
			return m.submit()
		case key.Matches(msg, keys.Cancel):
			m.closed = true
			return m, nil
		}
		m.field.Update(msg)
		m.reason = ""
	case tea.PasteMsg:
		m.field.Update(msg)
		m.reason = ""
	case components.ClickMsg:
		_, b := m.render()
		switch b.At(msg.X, msg.Y) {
		case 0:
			return m.submit()
		case 1:
			m.closed = true
		}
	}
	return m, nil
}

// submit delivers the value if check accepts it.
func (m Model) submit() (components.Modal, tea.Cmd) {
	v := m.field.Value()
	if m.check != nil {
		if r := m.check(v); r != "" {
			m.reason = r
			return m, nil
		}
	}
	m.closed = true
	done := m.done
	return m, func() tea.Msg { return done(v) }
}

// View implements components.Modal.
func (m Model) View() string {
	v, _ := m.render()
	return v
}

// render builds the View and the place of its buttons together.
func (m Model) render() (string, components.ModalButtons) {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title) + "\n\n")
	b.WriteString("> " + m.field.View() + "\n")
	if m.reason != "" {
		b.WriteString(errStyle.Render(components.ErrorPrefix+m.reason) + "\n")
	}
	b.WriteString("\n")
	btns := components.AddButtons(&b, OKButton, CancelButton)
	return boxStyle.Render(b.String()), btns
}

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding { return []key.Binding{keys.Submit, keys.Cancel} }
