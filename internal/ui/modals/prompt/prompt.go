package prompt

import (
	"fmt"
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
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	noteStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// DoneMsg tells the root model that the prompt was answered or cancelled.
// It carries no answer.
type DoneMsg struct{ Cancelled bool }

// Model is the authentication prompt modal (FR-CONN-003, FR-CONN-008,
// SEC-SESSION-002). A password or passphrase prompt names user@host and
// what is needed; a keyboard-interactive prompt is labelled "Prompt from
// <host>" and shows the server's sanitised text. Fields echo nothing, not
// even a mask, unless the server allows echo. Enter moves to the next
// field and submits on the last; Esc cancels.
type Model struct {
	prompt app.AuthPromptMsg
	inputs []*components.TextInput
	focus  int
}

// New returns the modal for p, with the first field focused.
func New(p app.AuthPromptMsg) Model {
	m := Model{prompt: p}
	for _, q := range p.Questions {
		in := components.NewTextInput()
		in.Echo = components.EchoNone // not even the length shows (FR-IDENT-004)
		if q.Echo {
			in.Echo = components.EchoNormal
		}
		m.inputs = append(m.inputs, in)
	}
	if len(m.inputs) > 0 {
		m.inputs[0].Focus()
	}
	return m
}

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	if _, ok := msg.(tea.PasteMsg); ok && len(m.inputs) > 0 {
		return m, m.inputs[m.focus].Update(msg)
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(k, keys.Cancel):
		m.prompt.Cancel()
		m.clear()
		return m, func() tea.Msg { return DoneMsg{Cancelled: true} }
	case key.Matches(k, keys.Submit):
		if m.focus < len(m.inputs)-1 {
			m.move(1)
			return m, nil
		}
		answers := make([]string, len(m.inputs))
		for i, f := range m.inputs {
			answers[i] = f.Value()
		}
		m.prompt.Answer(answers) // a buffered channel: never blocks
		m.clear()
		return m, func() tea.Msg { return DoneMsg{} }
	case key.Matches(k, keys.NextField):
		m.move(1)
		return m, nil
	case key.Matches(k, keys.PrevField):
		m.move(-1)
		return m, nil
	}
	if len(m.inputs) == 0 {
		return m, nil
	}
	return m, m.inputs[m.focus].Update(k)
}

func (m *Model) move(d int) {
	if len(m.inputs) > 0 {
		m.inputs[m.focus].Blur()
		m.focus = (m.focus + d + len(m.inputs)) % len(m.inputs)
		m.inputs[m.focus].Focus()
	}
}

// clear zeroes what was typed.
func (m *Model) clear() {
	for _, f := range m.inputs {
		f.Reset()
	}
}

// View implements components.Modal.
func (m Model) View() string {
	p := m.prompt
	who := domain.Sanitize(p.User + "@" + p.Host)
	var b strings.Builder
	switch p.Kind {
	case app.PromptPassword:
		b.WriteString(titleStyle.Render("Password for " + who))
		b.WriteString("\n")
	case app.PromptPassphrase:
		b.WriteString(titleStyle.Render("Passphrase for " + domain.Sanitize(p.Key)))
		b.WriteString("\n" + who + "\n")
	default:
		b.WriteString(titleStyle.Render("Prompt from " + domain.Sanitize(p.Host)))
		b.WriteString("\n" + who + "\n")
		if name := strings.TrimSpace(domain.Sanitize(p.Name)); name != "" {
			b.WriteString("\n" + name + "\n")
		}
		if ins := strings.TrimRight(domain.Sanitize(p.Instruction), "\n "); ins != "" {
			b.WriteString("\n" + ins + "\n")
		}
	}
	if p.Note != "" {
		b.WriteString("\n" + noteStyle.Render(domain.Sanitize(p.Note)) + "\n")
	}
	b.WriteString("\n")
	for i, q := range p.Questions {
		fmt.Fprintf(&b, "%s %s\n", strings.TrimRight(domain.Sanitize(q.Text), " "), m.inputs[i].View())
	}
	return boxStyle.Render(strings.TrimSuffix(b.String(), "\n"))
}

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding {
	if len(m.inputs) > 1 {
		return []key.Binding{keys.Submit, keys.NextField, keys.Cancel}
	}
	return []key.Binding{keys.Submit, keys.Cancel}
}
