package hostlist

import (
	"cmp"
	"net"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/domain"
	searchpkg "github.com/RafaelXokito/sshelf/internal/search"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
)

// LabelWidth is the column the address starts after, unless a label is
// longer.
const LabelWidth = 20

// SelectGlyph marks the selected row, so selection does not rely on colour
// alone (NFR-UX-003).
const SelectGlyph = "> "

var (
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	searchStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
)

// ConnectRequestMsg asks the root model to connect to a host.
type ConnectRequestMsg struct {
	HostID domain.ID
	Label  string
}

// AddHostMsg asks the root model to open the host form for a new host.
type AddHostMsg struct{}

// EditHostMsg asks the root model to open the host form for Host.
type EditHostMsg struct{ Host domain.Host }

// DeleteHostMsg asks the root model to delete a host. The list sends it
// only after the user confirmed (FR-HOST-002).
type DeleteHostMsg struct {
	HostID domain.ID
	Label  string
}

// IdentitiesMsg asks the root model to open the identities view (i).
type IdentitiesMsg struct{}

// ImportMsg asks the root model to import ~/.ssh/config (I).
type ImportMsg struct{}

// Model is the host list (FR-NAV-001, FR-NAV-003): one line per host,
// sorted by label, with a cursor and a search field opened with /. While
// the query is not empty only the hosts that fuzzy-match it are shown,
// best match first (FR-NAV-002).
type Model struct {
	hosts  []domain.Host // sorted by label
	shown  []int         // indexes into hosts, in row order
	cursor int           // row
	offset int           // first row shown
	width  int           // 0 = unknown: nothing is truncated
	height int           // rows available, search line included; 0 = unknown

	search    *components.TextInput
	searching bool // the search field has the keyboard
}

// New returns a list of hosts, sorted by label case-insensitively, with the
// first one selected.
func New(hosts []domain.Host) Model {
	hosts = slices.Clone(hosts)
	slices.SortStableFunc(hosts, func(a, b domain.Host) int {
		return cmp.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	search := components.NewTextInput()
	search.CharLimit = 128
	return Model{hosts: hosts, shown: searchpkg.Filter(hosts, ""), search: search}
}

// Selected returns the host under the cursor.
func (m Model) Selected() (domain.Host, bool) {
	if m.cursor < 0 || m.cursor >= len(m.shown) {
		return domain.Host{}, false
	}
	return m.hosts[m.shown[m.cursor]], true
}

// Hosts returns every host, sorted by label, whether shown or filtered
// out. The caller must not modify them.
func (m Model) Hosts() []domain.Host { return m.hosts }

// Shown returns the hosts shown, in row order.
func (m Model) Shown() []domain.Host {
	out := make([]domain.Host, len(m.shown))
	for i, j := range m.shown {
		out[i] = m.hosts[j]
	}
	return out
}

// Cursor returns the row of the selected host.
func (m Model) Cursor() int { return m.cursor }

// Searching reports whether the search field has the keyboard.
func (m Model) Searching() bool { return m.searching }

// Query returns the search query.
func (m Model) Query() string { return m.search.Value() }

// Select moves the cursor to the host with id, if it is shown.
func (m Model) Select(id domain.ID) Model {
	if i := slices.IndexFunc(m.shown, func(j int) bool { return m.hosts[j].ID == id }); i >= 0 {
		m.cursor = i
		m.scroll()
	}
	return m
}

// SelectRow moves the cursor to row i, clamped to the list.
func (m Model) SelectRow(i int) Model {
	m.cursor = min(max(i, 0), max(len(m.shown)-1, 0))
	m.scroll()
	return m
}

// refilter applies the query and selects the top result.
func (m *Model) refilter() {
	m.shown = searchpkg.Filter(m.hosts, m.search.Value())
	m.cursor, m.offset = 0, 0
	m.scroll()
}

// Hints implements components.Hinter (FR-NAV-004).
func (m Model) Hints() []key.Binding {
	if m.searching {
		return []key.Binding{keys.Connect, keys.ClearSearch, keys.SearchUp, keys.SearchDown}
	}
	return []key.Binding{keys.Connect, keys.Search, keys.AddHost, keys.EditHost, keys.DeleteHost, keys.Import, keys.Quit}
}

// Update implements components.Screen.
func (m Model) Update(msg tea.Msg) (components.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scroll()
		return m, nil
	case tea.PasteMsg:
		if m.searching {
			m.search.Update(msg)
			m.refilter()
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m Model) updateList(k tea.KeyPressMsg) (components.Screen, tea.Cmd) {
	switch {
	case key.Matches(k, keys.Up):
		m.move(-1)
	case key.Matches(k, keys.Down):
		m.move(1)
	case key.Matches(k, keys.First):
		m = m.SelectRow(0)
	case key.Matches(k, keys.Last):
		m = m.SelectRow(len(m.shown) - 1)
	case key.Matches(k, keys.Search):
		m.searching = true
		m.search.Focus()
		m.scroll()
	case key.Matches(k, keys.Connect):
		return m, m.connect()
	case key.Matches(k, keys.AddHost):
		return m, func() tea.Msg { return AddHostMsg{} }
	case key.Matches(k, keys.EditHost):
		if h, ok := m.Selected(); ok {
			return m, func() tea.Msg { return EditHostMsg{Host: h} }
		}
	case key.Matches(k, keys.DeleteHost):
		if h, ok := m.Selected(); ok {
			q := "Delete host " + domain.Sanitize(h.Label) + "?"
			then := DeleteHostMsg{HostID: h.ID, Label: h.Label}
			return m, func() tea.Msg { return components.ShowModalMsg{Modal: confirm.New(q, then)} }
		}
	case key.Matches(k, keys.Identities):
		return m, func() tea.Msg { return IdentitiesMsg{} }
	case key.Matches(k, keys.Import):
		return m, func() tea.Msg { return ImportMsg{} }
	case key.Matches(k, keys.Quit):
		return m, tea.Quit
	}
	return m, nil
}

// updateSearch handles a key while the search field has the keyboard:
// arrows move, enter connects to the selected result (the top one unless
// the arrows moved), esc clears the query and returns to the list keeping
// the selected host selected, and every other key goes to the field and
// filters again (FR-NAV-002, FR-NAV-003).
func (m Model) updateSearch(k tea.KeyPressMsg) (components.Screen, tea.Cmd) {
	switch {
	case key.Matches(k, keys.SearchUp):
		m.move(-1)
	case key.Matches(k, keys.SearchDown):
		m.move(1)
	case key.Matches(k, keys.Connect):
		return m, m.connect()
	case key.Matches(k, keys.ClearSearch):
		h, ok := m.Selected()
		m.search.Reset()
		m.search.Blur()
		m.searching = false
		m.refilter()
		if ok {
			m = m.Select(h.ID)
		}
	default:
		before := m.search.Value()
		m.search.Update(k)
		if m.search.Value() != before {
			m.refilter()
		}
	}
	return m, nil
}

func (m Model) connect() tea.Cmd {
	h, ok := m.Selected()
	if !ok {
		return nil
	}
	return func() tea.Msg { return ConnectRequestMsg{HostID: h.ID, Label: h.Label} }
}

func (m *Model) move(d int) {
	m.cursor = min(max(m.cursor+d, 0), max(len(m.shown)-1, 0))
	m.scroll()
}

// rows returns how many host rows fit.
func (m Model) rows() int {
	if m.height <= 0 {
		return len(m.shown)
	}
	n := m.height
	if m.searching {
		n-- // the search line
	}
	return max(n, 1)
}

// scroll keeps the cursor inside the rows shown.
func (m *Model) scroll() {
	rows := m.rows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(min(m.offset, len(m.shown)-rows), 0)
}

// View implements components.Screen.
func (m Model) View() string {
	var b strings.Builder
	if m.searching {
		b.WriteString(m.fit(searchStyle.Render("/ ") + m.search.View()))
		b.WriteString("\n")
	}
	switch {
	case len(m.hosts) == 0:
		b.WriteString(dimStyle.Render("No hosts yet.") + "\n")
		return b.String()
	case len(m.shown) == 0:
		b.WriteString(dimStyle.Render(m.fit("No host matches the search.")) + "\n")
		return b.String()
	}
	end := min(m.offset+m.rows(), len(m.shown))
	for i := m.offset; i < end; i++ {
		h := m.hosts[m.shown[i]]
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(m.fit(SelectGlyph + Line(h))))
		} else {
			b.WriteString(m.fit("  " + Line(h)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// fit truncates s to the width with "…" (FR-NAV-001).
func (m Model) fit(s string) string {
	if m.width <= 0 {
		return s
	}
	return ansi.Truncate(s, m.width, "…")
}

// Line renders one host: its label and user@hostname:port, with port 22
// omitted (FR-NAV-001). The label is sanitised.
func Line(h domain.Host) string {
	addr := h.Hostname
	if h.Port != 0 && h.Port != domain.DefaultPort {
		addr = net.JoinHostPort(h.Hostname, strconv.Itoa(h.Port))
	}
	if h.User != "" {
		addr = h.User + "@" + addr
	}
	return padRight(domain.Sanitize(h.Label), LabelWidth) + " " + domain.Sanitize(addr)
}

func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
