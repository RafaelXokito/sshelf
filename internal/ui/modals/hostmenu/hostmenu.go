// Package hostmenu is the context menu of a host row, opened by a right
// click on the host list (FR-NAV-019, D-025, design §9.8).
package hostmenu

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	keyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// Item is one entry of the menu: what it shows and the host-list key it
// stands for.
type Item struct {
	Label string
	Key   key.Binding
	press tea.KeyPressMsg
}

// Items are the menu's entries, in order. Each acts as its key on the
// host list.
var Items = []Item{
	{"Connect (SSH)", keys.Connect, tea.KeyPressMsg{Code: tea.KeyEnter}},
	{"Open SFTP", keys.OpenSFTP, tea.KeyPressMsg{Code: 's', Text: "s"}},
	{"Edit", keys.EditHost, tea.KeyPressMsg{Code: 'e', Text: "e"}},
	{"Duplicate", keys.DuplicateHost, tea.KeyPressMsg{Code: 'c', Text: "c"}},
	{"Delete", keys.DeleteHost, tea.KeyPressMsg{Code: 'd', Text: "d"}},
}

// ChosenMsg reports the item chosen: Key is that item's key press, which
// the root model sends to the host list.
type ChosenMsg struct{ Key tea.KeyPressMsg }

// Model is the menu. It is popped at once when an item is chosen or it
// is closed (components.Closer).
type Model struct {
	host   domain.Host
	x, y   int
	cursor int
	closed bool
}

// New returns the menu of host h, opened at column x, row y of the
// terminal.
func New(h domain.Host, x, y int) Model { return Model{host: h, x: x, y: y} }

// Host returns the host the menu was opened on.
func (m Model) Host() domain.Host { return m.host }

// Cursor returns the highlighted item.
func (m Model) Cursor() int { return m.cursor }

// Anchor implements components.Popup.
func (m Model) Anchor() (int, int) { return m.x, m.y }

// Closed implements components.Closer.
func (m Model) Closed() bool { return m.closed }

// Hints implements components.Hinter.
func (m Model) Hints() []key.Binding {
	return []key.Binding{keys.Up, keys.Down, keys.MenuChoose, keys.MenuClose}
}

// Update implements components.Modal.
func (m Model) Update(msg tea.Msg) (components.Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.Up):
			m.cursor = max(m.cursor-1, 0)
		case key.Matches(msg, keys.Down):
			m.cursor = min(m.cursor+1, len(Items)-1)
		case key.Matches(msg, keys.MenuChoose):
			return m.choose(m.cursor)
		case key.Matches(msg, keys.MenuClose):
			m.closed = true
		}
	case components.ClickMsg:
		if i := msg.Y - components.BoxInsetY; i >= 0 && i < len(Items) && msg.X >= 1 && msg.X < m.width()-1 {
			return m.choose(i)
		}
	}
	return m, nil
}

func (m Model) choose(i int) (components.Modal, tea.Cmd) {
	m.cursor, m.closed = i, true
	k := Items[i].press
	return m, func() tea.Msg { return ChosenMsg{Key: k} }
}

// width is the width of View.
func (m Model) width() int { return lipgloss.Width(m.View()) }

// View implements components.Modal: one line per item, the item's key
// right-aligned, the highlighted one marked with "> " (NFR-UX-003).
func (m Model) View() string {
	lw, kw := 0, 0
	for _, it := range Items {
		lw = max(lw, lipgloss.Width(it.Label))
		kw = max(kw, lipgloss.Width(it.Key.Help().Key))
	}
	lines := make([]string, len(Items))
	for i, it := range Items {
		k := it.Key.Help().Key
		label := it.Label + strings.Repeat(" ", lw-lipgloss.Width(it.Label))
		pad := strings.Repeat(" ", kw-lipgloss.Width(k))
		if i == m.cursor {
			lines[i] = selectedStyle.Render("> "+label) + "  " + pad + keyStyle.Render(k)
		} else {
			lines[i] = "  " + label + "  " + pad + keyStyle.Render(k)
		}
	}
	return boxStyle.Render(strings.Join(lines, "\n"))
}
