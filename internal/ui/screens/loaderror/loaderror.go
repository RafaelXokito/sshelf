// Package loaderror is the LoadError screen: config.json could not be
// loaded, so sshelf shows where and why and only lets the user quit
// (FR-CFG-003, design §9.1).
package loaderror

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
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// Model is the LoadError screen.
type Model struct {
	err *app.LoadError
}

// New returns the screen for err.
func New(err *app.LoadError) Model { return Model{err: err} }

// Update implements components.Screen: q quits; nothing else happens, so
// the file is never touched.
func (m Model) Update(msg tea.Msg) (components.Screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && key.Matches(k, keys.Quit) {
		return m, tea.Quit
	}
	return m, nil
}

// View implements components.Screen.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(errStyle.Render("sshelf cannot load its configuration"))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "  File:     %s\n", m.err.Path)
	if m.err.Line > 0 {
		fmt.Fprintf(&b, "  Line:     %d, column %d\n", m.err.Line, m.err.Column)
	}
	fmt.Fprintf(&b, "  Problem:  %s\n\n", m.err.Problem)
	b.WriteString("sshelf has not changed this file. Fix it and start sshelf again.\n\n")
	b.WriteString(dimStyle.Render("q quit"))
	b.WriteString("\n")
	return b.String()
}
