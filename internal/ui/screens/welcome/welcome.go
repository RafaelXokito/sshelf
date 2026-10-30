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
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.WelcomeImport):
			return m, choose(choiceImport)
		case key.Matches(msg, keys.WelcomeAdd):
			return m, choose(choiceAdd)
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		}
	case components.ClickMsg:
		// A click anywhere on a choice's line acts as its key
		// (FR-NAV-014).
		_, rows := render()
		for c, y := range rows {
			if y == msg.Y {
				return m, choose(c)
			}
		}
	}
	return m, nil
}

// choice is one of the Welcome screen's choices.
type choice int

const (
	choiceImport choice = iota
	choiceAdd
)

// choose returns the command that runs choice c.
func choose(c choice) tea.Cmd {
	if c == choiceImport {
		return func() tea.Msg { return ImportMsg{} }
	}
	return func() tea.Msg { return AddHostMsg{} }
}

// View implements components.Screen.
func (Model) View() string {
	v, _ := render()
	return v
}

// render builds the View and the line of each choice in it, so that
// clicks and the View agree (ARCH-UI-007).
func render() (string, map[choice]int) {
	lines := []string{
		headStyle.Render("Welcome to sshelf"), "",
		"You have no saved hosts yet.", "",
	}
	rows := map[choice]int{}
	rows[choiceImport] = len(lines)
	lines = append(lines, "  "+keyStyle.Render("[I]")+" Import from ~/.ssh/config")
	rows[choiceAdd] = len(lines)
	lines = append(lines, "  "+keyStyle.Render("[a]")+" Add host", "",
		dimStyle.Render("Import copies your hosts once and references key files by path."),
		dimStyle.Render("sshelf never changes ~/.ssh/config."))
	return strings.Join(lines, "\n") + "\n", rows
}
