// Package help is the help overlay, generated from ui/keys (ARCH-UI-004):
// every command after Ctrl+] (FR-TAB-005), or the keys of a view such as
// the SFTP tab (FR-SFTP-007).
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
type Model struct {
	closed   bool
	title    string        // "" for the prefix help
	bindings []key.Binding // the keys listed with NewList
}

// New returns the prefix-key overlay.
func New() Model { return Model{} }

// NewList returns an overlay titled title listing bindings, in two
// columns when there are more than listColumnsFrom.
func NewList(title string, bindings []key.Binding) Model {
	return Model{title: title, bindings: bindings}
}

// listColumnsFrom is the number of bindings from which NewList uses two
// columns, so the overlay fits 24 rows.
const listColumnsFrom = 10

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
	title := "Press " + keys.Prefix.Help().Key + ", then:"
	if m.bindings != nil {
		cmds, title = m.bindings, m.title
	}
	width := 0
	for _, b := range cmds {
		width = max(width, lipgloss.Width(b.Help().Key))
	}
	entry := func(b key.Binding) string {
		h := b.Help()
		return keyStyle.Render(h.Key+strings.Repeat(" ", width-lipgloss.Width(h.Key))) + "  " + h.Desc
	}
	lines := []string{titleStyle.Render(title), ""}
	if m.bindings == nil || len(cmds) <= listColumnsFrom {
		for _, b := range cmds {
			lines = append(lines, entry(b))
		}
	} else {
		half := (len(cmds) + 1) / 2
		col := 0
		for _, b := range cmds[:half] {
			col = max(col, lipgloss.Width(entry(b)))
		}
		for i := range half {
			l := entry(cmds[i])
			if j := i + half; j < len(cmds) {
				l += strings.Repeat(" ", col-lipgloss.Width(l)+3) + entry(cmds[j])
			}
			lines = append(lines, l)
		}
	}
	var b strings.Builder
	b.WriteString(strings.Join(lines, "\n") + "\n\n")
	btns := components.AddButtons(&b, CloseButton)
	return boxStyle.Render(b.String()), btns
}

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding { return []key.Binding{keys.CloseHelp} }
