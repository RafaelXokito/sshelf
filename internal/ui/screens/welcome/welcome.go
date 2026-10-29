package welcome

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	headStyle = lipgloss.NewStyle().Bold(true)
	keyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// ImportMsg asks the root model to import ~/.ssh/config.
type ImportMsg struct{}

// AddHostMsg asks the root model to open the host form for a new host.
type AddHostMsg struct{}

// Model is the Welcome screen, shown instead of the host list while
// config.json has no hosts (FR-NAV-013). I imports, a adds a host, q
// quits.
type Model struct{}

// New returns the Welcome screen.
func New() Model { return Model{} }

// Hints implements components.Hinter.
func (Model) Hints() []key.Binding {
	return []key.Binding{keys.WelcomeImport, keys.WelcomeAdd, keys.Quit}
}

// Update implements components.Screen.
func (m Model) Update(msg tea.Msg) (components.Screen, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(k, keys.WelcomeImport):
		return m, func() tea.Msg { return ImportMsg{} }
	case key.Matches(k, keys.WelcomeAdd):
		return m, func() tea.Msg { return AddHostMsg{} }
	case key.Matches(k, keys.Quit):
		return m, tea.Quit
	}
	return m, nil
}

// View implements components.Screen.
func (Model) View() string {
	var b strings.Builder
	b.WriteString(headStyle.Render("Welcome to sshelf") + "\n\n")
	b.WriteString("You have no saved hosts yet.\n\n")
	b.WriteString("  " + keyStyle.Render("[I]") + " Import from ~/.ssh/config\n")
	b.WriteString("  " + keyStyle.Render("[a]") + " Add host\n\n")
	b.WriteString(dimStyle.Render("Import copies your hosts once and references key files by path.") + "\n")
	b.WriteString(dimStyle.Render("sshelf never changes ~/.ssh/config.") + "\n")
	return b.String()
}
