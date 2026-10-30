// Package keychanged is the block screen for a changed host key (FR-KH-002,
// SEC-HOSTKEY-005, ARCH-CONN-004). It shows both fingerprints and how to
// remove the stale entry, and offers no way to connect anyway.
package keychanged

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("203")).Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// CloseMsg tells the root model to close the block screen.
type CloseMsg struct{}

// Model is the block screen.
type Model struct {
	label string
	err   *app.HostKeyMismatchError
}

// New returns the block screen for the host labelled label.
func New(label string, err *app.HostKeyMismatchError) Model { return Model{label: label, err: err} }

// Update implements components.Modal. The only action is closing it.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	closeCmd := func() tea.Msg { return CloseMsg{} }
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, keys.CloseBlock) {
			return m, closeCmd
		}
	case components.ClickMsg:
		if _, b := m.render(); b.At(msg.X, msg.Y) == 0 {
			return m, closeCmd
		}
	}
	return m, nil
}

// CloseButton is the block screen's only button (FR-NAV-016).
const CloseButton = "[esc] Close"

// View implements components.Modal.
func (m Model) View() string {
	v, _ := m.render()
	return v
}

// render builds the View and the place of its button together.
func (m Model) render() (string, components.ModalButtons) {
	e := m.err
	var b strings.Builder
	b.WriteString(titleStyle.Render("WARNING: HOST KEY HAS CHANGED"))
	// The button shares the title's line: the box already fills 80×24.
	btns := components.AddButtons(&b, CloseButton)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "%s (%s) presented a different key from the one you\n", m.label, e.Name)
	b.WriteString("trust. Someone could be intercepting the connection\n")
	b.WriteString("(a man-in-the-middle attack), or the server's key was\n")
	b.WriteString("replaced. sshelf did not connect and sent no credentials.\n\n")
	fmt.Fprintf(&b, "  Key type:       %s\n", e.KeyType)
	fmt.Fprintf(&b, "  Presented key:  %s\n", e.Fingerprint)
	for _, s := range e.Stale {
		fmt.Fprintf(&b, "  Trusted key:    %s", s.Fingerprint)
		if s.KeyType != e.KeyType {
			fmt.Fprintf(&b, " (%s)", s.KeyType)
		}
		fmt.Fprintf(&b, "\n                  %s line %d\n", s.File, s.Line)
	}
	b.WriteString("\nIf you know the key was replaced, remove the stale entry\n")
	b.WriteString("and connect again to review the new key:\n")
	files := map[string]bool{}
	for _, s := range e.Stale {
		fmt.Fprintf(&b, "  delete line %d of %s\n", s.Line, s.File)
		files[s.File] = true
	}
	b.WriteString("or run:\n")
	for _, s := range e.Stale {
		if files[s.File] {
			fmt.Fprintf(&b, "  ssh-keygen -R %s -f %s\n", quote(e.Name), quote(s.File))
			files[s.File] = false
		}
	}
	return boxStyle.Render(strings.TrimSuffix(b.String(), "\n")), btns
}

// quote quotes s for a POSIX shell when it needs it.
func quote(s string) string {
	safe := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:@%+=,", r)
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !safe(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding { return []key.Binding{keys.CloseBlock} }
