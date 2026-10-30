// Package editconflict holds the two questions of edit-locally: the
// remote file changed or was deleted while it was edited (FR-SFTP-020),
// and the upload of the edit failed (FR-SFTP-019).
package editconflict

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

// Button labels (FR-NAV-016) of the remote-changed question.
const (
	OverwriteButton = "[o] Overwrite"
	KeepButton      = "[k] Keep remote"
	SaveMineButton  = "[s] Save mine as " // + the name
)

// Button labels of the upload-failed question.
const (
	RetryButton     = "[r] Retry"
	SaveLocalButton = "[l] Save to " // + the local path
	DiscardButton   = "[x] Discard"
)

// choice is one button: its label, its key and the answer.
type choice struct {
	label  string
	key    key.Binding
	answer app.EditChoice
}

// Model asks what to do with an edit. Any answer closes it at once
// (components.Closer). Enter answers nothing.
type Model struct {
	title   string
	lines   []string
	choices []choice
	done    func(app.EditChoice) tea.Msg
	closed  bool
}

// NewChanged is the question of FR-SFTP-020: name (already sanitised)
// changed on the server, or was deleted when gone, while it was edited.
// Esc keeps the remote file; there is no default.
func NewChanged(name string, gone bool, done func(app.EditChoice) tea.Msg) Model {
	what := " changed on the server while you edited it."
	if gone {
		what = " was deleted on the server while you edited it."
	}
	return Model{
		title: name + what,
		choices: []choice{
			{OverwriteButton, keys.EditOverwrite, app.EditOverwrite},
			{KeepButton, keys.EditKeep, app.EditKeepRemote},
			{SaveMineButton + name + app.LocalEditSuffix, keys.EditSaveMine, app.EditSaveRemote},
		},
		done: done,
	}
}

// NewUploadFailed is the question of FR-SFTP-019 after the upload of name
// (already sanitised) failed with reason; localPath is where "save
// locally" writes, shown as is. The copy is kept until a choice is made,
// so Esc does nothing.
func NewUploadFailed(name, reason, localPath string, done func(app.EditChoice) tea.Msg) Model {
	return Model{
		title: "Upload of " + name + " failed",
		lines: []string{reason},
		choices: []choice{
			{RetryButton, keys.EditRetry, app.EditRetry},
			{SaveLocalButton + localPath, keys.EditSaveLocal, app.EditSaveLocal},
			{DiscardButton, keys.EditDiscard, app.EditDiscard},
		},
		done: done,
	}
}

// Closed implements components.Closer.
func (m Model) Closed() bool { return m.closed }

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		for _, c := range m.choices {
			if key.Matches(msg, c.key) {
				return m.answer(c.answer)
			}
		}
	case components.ClickMsg:
		_, b := m.render()
		if i := b.At(msg.X, msg.Y); i >= 0 {
			return m.answer(m.choices[i].answer)
		}
	}
	return m, nil
}

// answer closes the modal and delivers a.
func (m Model) answer(a app.EditChoice) (components.Modal, tea.Cmd) {
	m.closed = true
	done := m.done
	return m, func() tea.Msg { return done(a) }
}

// View implements components.Modal.
func (m Model) View() string {
	v, _ := m.render()
	return v
}

// render builds the View and the place of its buttons together, one
// button per line so that long names fit.
func (m Model) render() (string, components.ModalButtons) {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title) + "\n")
	for _, l := range m.lines {
		b.WriteString(l + "\n")
	}
	var btns components.ModalButtons
	for _, c := range m.choices {
		b.WriteString("\n") // a blank line before the first button
		btns = append(btns, components.AddButtons(&b, c.label)...)
	}
	return boxStyle.Render(b.String()), btns
}

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding {
	out := make([]key.Binding, len(m.choices))
	for i, c := range m.choices {
		out[i] = c.key
	}
	return out
}
