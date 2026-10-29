package tofu

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
	boxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// DoneMsg tells the root model that the prompt was answered.
type DoneMsg struct{ Trusted bool }

// Model is the trust-on-first-use modal (FR-KH-001, SEC-HOSTKEY-004): it
// shows the host, port, key type and SHA256 fingerprint and offers exactly
// "Trust and save" (y) and "Cancel" (enter or esc). Enter never trusts a
// key. "Trust once" is v1 (FR-KH-007).
type Model struct {
	prompt app.HostKeyPromptMsg
}

// New returns the modal for p.
func New(p app.HostKeyPromptMsg) Model { return Model{prompt: p} }

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	var trust bool
	switch {
	case key.Matches(k, keys.TrustAndSave):
		trust = true
	case key.Matches(k, keys.TOFUCancel):
	default:
		return m, nil
	}
	m.prompt.Answer(trust) // a buffered channel: never blocks
	return m, func() tea.Msg { return DoneMsg{Trusted: trust} }
}

// View implements components.Modal.
func (m Model) View() string {
	p := m.prompt
	var b strings.Builder
	b.WriteString(titleStyle.Render("Unknown host key"))
	b.WriteString("\n\n")
	b.WriteString("sshelf has never seen this host's key. Check the\n")
	b.WriteString("fingerprint with the server's owner before you trust it.\n\n")
	fmt.Fprintf(&b, "  Host:         %s\n", p.Host)
	fmt.Fprintf(&b, "  Port:         %d\n", p.Port)
	fmt.Fprintf(&b, "  Key type:     %s\n", p.KeyType)
	fmt.Fprintf(&b, "  Fingerprint:  %s\n\n", p.Fingerprint)
	b.WriteString("[y] Trust and save    [enter/esc] Cancel\n")
	b.WriteString(dimStyle.Render("No credentials are sent until you choose."))
	return boxStyle.Render(b.String())
}
