// Package conflict is the existing-target question of a transfer
// (FR-SFTP-016): overwrite, skip or keep both, optionally for the rest of
// the same copy.
package conflict

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
)

// Button labels (FR-NAV-016), in the order of the buttons.
const (
	OverwriteButton = "[o] Overwrite"
	SkipButton      = "[s] Skip"
	KeepBothButton  = "[n] Keep both"
	AllButton       = "[a] apply to all"
	EscButton       = "[esc] Skip"
)

// Question is what the modal shows: the name, sanitised, and each side's
// size and modification time, formatted.
type Question struct {
	Name          string
	Remote, Local string // "840 MB, Oct 29 10:12"
}

// Model asks what to do with an existing target. a toggles "apply to
// all" before the choice; Esc skips. Any answer closes it at once
// (components.Closer).
type Model struct {
	q      Question
	all    bool
	done   func(app.Answer, bool) tea.Msg
	closed bool
}

// New returns the question; done is the message delivered with the
// answer and whether it applies to the rest of the copy.
func New(q Question, done func(a app.Answer, all bool) tea.Msg) Model {
	return Model{q: q, done: done}
}

// All reports whether "apply to all" is on.
func (m Model) All() bool { return m.all }

// Closed implements components.Closer.
func (m Model) Closed() bool { return m.closed }

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.ConflictOverwrite):
			return m.answer(app.Overwrite)
		case key.Matches(msg, keys.ConflictSkip):
			return m.answer(app.Skip)
		case key.Matches(msg, keys.ConflictKeepBoth):
			return m.answer(app.KeepBoth)
		case key.Matches(msg, keys.ConflictAll):
			m.all = !m.all
		}
	case components.ClickMsg:
		_, b := m.render()
		switch b.At(msg.X, msg.Y) {
		case 0:
			return m.answer(app.Overwrite)
		case 1, 4:
			return m.answer(app.Skip)
		case 2:
			return m.answer(app.KeepBoth)
		case 3:
			m.all = !m.all
		}
	}
	return m, nil
}

// answer closes the modal and delivers a.
func (m Model) answer(a app.Answer) (components.Modal, tea.Cmd) {
	m.closed = true
	done, all := m.done, m.all
	return m, func() tea.Msg { return done(a, all) }
}

// View implements components.Modal.
func (m Model) View() string {
	v, _ := m.render()
	return v
}

// render builds the View and the place of its buttons together.
func (m Model) render() (string, components.ModalButtons) {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.q.Name+" already exists") + "\n")
	b.WriteString("remote " + m.q.Remote + "\nlocal  " + m.q.Local + "\n\n")
	all := AllButton + ": no"
	if m.all {
		all = AllButton + ": yes"
	}
	btns := components.AddButtons(&b, OverwriteButton, SkipButton, KeepBothButton)
	b.WriteString("\n")
	btns = append(btns, components.AddButtons(&b, all, EscButton)...)
	return boxStyle.Render(b.String()), btns
}

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding {
	return []key.Binding{keys.ConflictOverwrite, keys.ConflictSkip, keys.ConflictKeepBoth, keys.ConflictAll}
}
