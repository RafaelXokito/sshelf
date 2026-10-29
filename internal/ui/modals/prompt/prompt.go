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

// maxAnswer bounds what one field accepts, in runes.
const maxAnswer = 1024

// DoneMsg tells the root model that the prompt was answered or cancelled.
// It carries no answer.
type DoneMsg struct{ Cancelled bool }

// field is one answer being typed. bubbles' textinput is not used because
// it imports a clipboard package (SEC-CLIP-005); a prompt needs only
// typing, pasting, backspace and ctrl+u.
type field struct {
	value []rune
	echo  bool
}

func (f *field) insert(s string) {
	for _, r := range domain.Sanitize(s) {
		if r == '\n' || r == '\t' || len(f.value) >= maxAnswer {
			continue
		}
		f.value = append(f.value, r)
	}
}

func (f *field) backspace() {
	if n := len(f.value); n > 0 {
		f.value[n-1] = 0
		f.value = f.value[:n-1]
	}
}

// clear zeroes the typed runes.
func (f *field) clear() {
	clear(f.value)
	f.value = f.value[:0]
}

// view shows the value only when the server allows echo; otherwise
// nothing, so not even the length shows (FR-IDENT-004).
func (f *field) view(focused bool) string {
	var s string
	if f.echo {
		s = string(f.value)
	}
	if focused {
		s += "_"
	}
	return s
}

// Model is the authentication prompt modal (FR-CONN-003, FR-CONN-008,
// SEC-SESSION-002). A password or passphrase prompt names user@host and
// what is needed; a keyboard-interactive prompt is labelled "Prompt from
// <host>" and shows the server's sanitised text. Fields echo nothing, not
// even a mask, unless the server allows echo. Enter moves to the next
// field and submits on the last; Esc cancels.
type Model struct {
	prompt app.AuthPromptMsg
	inputs []*field
	focus  int
}

// New returns the modal for p, with the first field focused.
func New(p app.AuthPromptMsg) Model {
	m := Model{prompt: p}
	for _, q := range p.Questions {
		m.inputs = append(m.inputs, &field{echo: q.Echo})
	}
	return m
}

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	if p, ok := msg.(tea.PasteMsg); ok && len(m.inputs) > 0 {
		m.inputs[m.focus].insert(p.Content)
		return m, nil
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
			answers[i] = string(f.value)
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
	f := m.inputs[m.focus]
	switch k.String() {
	case "backspace", "ctrl+h":
		f.backspace()
	case "ctrl+u":
		f.clear()
	default:
		if k.Text != "" {
			f.insert(k.Text)
		}
	}
	return m, nil
}

func (m *Model) move(d int) {
	if len(m.inputs) > 0 {
		m.focus = (m.focus + d + len(m.inputs)) % len(m.inputs)
	}
}

// clear zeroes what was typed.
func (m *Model) clear() {
	for _, f := range m.inputs {
		f.clear()
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
		fmt.Fprintf(&b, "%s %s\n", strings.TrimRight(domain.Sanitize(q.Text), " "), m.inputs[i].view(i == m.focus))
	}
	help := "enter submit • esc cancel"
	if len(m.inputs) > 1 {
		help = "tab next field • " + help
	}
	b.WriteString("\n" + dimStyle.Render(help))
	return boxStyle.Render(b.String())
}
