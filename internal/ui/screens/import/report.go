package importscreen

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
	titleStyle = lipgloss.NewStyle().Bold(true)
	headStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// DoneMsg asks the root model to leave the report for the host list.
type DoneMsg struct{}

// Model is the import report (FR-IMPORT-003): how many hosts and
// identities were created, the aliases skipped and why, and the
// directives not imported per host. ↑/↓ scroll a report longer than the
// screen; enter or esc goes to the host list.
type Model struct {
	report app.ImportReport
	lines  []string
	offset int
	height int // 0 = unknown: show everything
}

// New returns the report screen for r.
func New(r app.ImportReport) Model {
	return Model{report: r, lines: render(r)}
}

// Report returns the report shown.
func (m Model) Report() app.ImportReport { return m.report }

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding {
	if m.scrollable() {
		return []key.Binding{keys.Back, keys.Up, keys.Down}
	}
	return []key.Binding{keys.Back}
}

func (m Model) scrollable() bool { return m.height > 0 && len(m.lines) > m.height }

// Update implements components.Screen.
func (m Model) Update(msg tea.Msg) (components.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.offset = m.clamp(m.offset)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.Back):
			return m, func() tea.Msg { return DoneMsg{} }
		case key.Matches(msg, keys.Up):
			m.offset = m.clamp(m.offset - 1)
		case key.Matches(msg, keys.Down):
			m.offset = m.clamp(m.offset + 1)
		}
	}
	return m, nil
}

func (m Model) clamp(off int) int {
	if m.height <= 0 {
		return 0
	}
	return max(min(off, len(m.lines)-m.height), 0)
}

// View implements components.Screen.
func (m Model) View() string {
	lines := m.lines
	if m.height > 0 {
		lines = lines[m.offset:min(m.offset+m.height, len(lines))]
	}
	return strings.Join(lines, "\n") + "\n"
}

// render lays the report out, one line per entry.
func render(r app.ImportReport) []string {
	out := []string{
		titleStyle.Render("Import from " + domain.Sanitize(r.File)),
		"",
		fmt.Sprintf("Created %s and %s.", plural(len(r.Created), "host"), plural(len(r.Identities), "identity")),
		dimStyle.Render("Key files are referenced by path; nothing was copied into the credential store."),
	}
	if len(r.Skipped) > 0 {
		out = append(out, "", headStyle.Render(fmt.Sprintf("Skipped (%d):", len(r.Skipped))))
		for _, s := range r.Skipped {
			out = append(out, "  "+domain.Sanitize(s.Alias)+": "+domain.Sanitize(s.Reason))
		}
	}
	if len(r.NotImported) > 0 {
		out = append(out, "", headStyle.Render("Not imported:"))
		for _, h := range r.NotImported {
			ds := make([]string, len(h.Directives))
			for i, d := range h.Directives {
				ds[i] = domain.Sanitize(d)
			}
			out = append(out, "  "+domain.Sanitize(h.Label)+": "+strings.Join(ds, ", "))
		}
	}
	return out
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	if strings.HasSuffix(noun, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(noun, "y"))
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
