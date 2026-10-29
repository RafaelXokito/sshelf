package connecting

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
	boxStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	currentStyle = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// stages are shown in this order (design §9.2).
var stages = []app.Stage{app.StageDialling, app.StageVerifying, app.StageAuthenticating}

// CancelMsg asks the root model to cancel the connection attempt.
type CancelMsg struct{}

// Model is the Connecting modal: it shows "dialling → verifying →
// authenticating" with the current stage marked, and Esc cancels
// (FR-CONN-006, ARCH-CONN-005).
type Model struct {
	label string
	stage int // index in stages
}

// New returns the modal for a connection to the host labelled label.
func New(label string) Model { return Model{label: label} }

// WithStage returns the modal showing stage s. A stage never goes back.
func (m Model) WithStage(s app.Stage) Model {
	for i, st := range stages {
		if st == s && i > m.stage {
			m.stage = i
		}
	}
	return m
}

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && key.Matches(k, keys.Cancel) {
		return m, func() tea.Msg { return CancelMsg{} }
	}
	return m, nil
}

// View implements components.Modal.
func (m Model) View() string {
	parts := make([]string, len(stages))
	for i, s := range stages {
		switch {
		case i < m.stage:
			parts[i] = dimStyle.Render("✓ " + string(s))
		case i == m.stage:
			parts[i] = currentStyle.Render("› " + string(s))
		default:
			parts[i] = dimStyle.Render("· " + string(s))
		}
	}
	return boxStyle.Render("Connecting to " + m.label + "…\n\n" + strings.Join(parts, " → ") + "\n\n" + dimStyle.Render("esc cancel"))
}
