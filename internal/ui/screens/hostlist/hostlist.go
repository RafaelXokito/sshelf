package hostlist

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

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

// ConnectRequestMsg asks the root model to connect to a host. Tab is the
// id of the ended session tab to reconnect in, or 0 for a new tab
// (FR-CONN-010).
type ConnectRequestMsg struct {
	HostID domain.ID
	Label  string
	Tab    int
}

// SFTPRequestMsg asks the root model to open an SFTP tab for a host
// through the normal connect flow (FR-SFTP-002).
type SFTPRequestMsg struct {
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

// TimeWidth is the width of the last-use column at the end of each row
// (FR-NAV-018).
const TimeWidth = 8

// TimeMinWidth is the narrowest list that shows the last-use column.
const TimeMinWidth = 60

// Model is the host list (FR-NAV-001, FR-NAV-003): one line per host,
// in last-use order (FR-NAV-018), with a cursor and a search field opened
// with /. While the query is not empty only the hosts that fuzzy-match it
// are shown, best match first (FR-NAV-002).
type Model struct {
	hosts  []domain.Host    // in last-use order (domain.CompareLastUse)
	now    func() time.Time // the clock the relative times are measured against
	shown  []int            // indexes into hosts, in row order
	cursor int              // row
	offset int              // first row shown
	width  int              // 0 = unknown: nothing is truncated
	height int              // rows available, search line included; 0 = unknown

	search    *components.TextInput
	searching bool // the search field has the keyboard
}

// New returns a list of hosts in last-use order (domain.CompareLastUse),
// with the first one selected. Relative times are measured against
// time.Now until WithClock replaces it.
func New(hosts []domain.Host) Model {
	hosts = domain.SortByLastUse(hosts)
	search := components.NewTextInput()
	search.CharLimit = 128
	return Model{hosts: hosts, now: time.Now, shown: searchpkg.Filter(hosts, ""), search: search}
}

// WithClock returns m measuring relative times against now.
func (m Model) WithClock(now func() time.Time) Model {
	if now != nil {
		m.now = now
	}
	return m
}

// Connected records that a session to the host with id was established
// at t (FR-NAV-018): the list is sorted again and the search applied
// again, and the selection stays on the host it was on.
func (m Model) Connected(id domain.ID, t time.Time) Model {
	i := slices.IndexFunc(m.hosts, func(h domain.Host) bool { return h.ID == id })
	if i < 0 {
		return m
	}
	sel, ok := m.Selected()
	hosts := slices.Clone(m.hosts)
	t = t.UTC()
	hosts[i].LastConnectedAt = &t
	m.hosts = domain.SortByLastUse(hosts)
	m.shown = searchpkg.Filter(m.hosts, m.search.Value())
	if ok {
		if j := slices.IndexFunc(m.shown, func(j int) bool { return m.hosts[j].ID == sel.ID }); j >= 0 {
			m.cursor = j
		}
	}
	m.cursor = min(max(m.cursor, 0), max(len(m.shown)-1, 0))
	m.scroll()
	return m
}

// Selected returns the host under the cursor.
func (m Model) Selected() (domain.Host, bool) {
	if m.cursor < 0 || m.cursor >= len(m.shown) {
		return domain.Host{}, false
	}
	return m.hosts[m.shown[m.cursor]], true
}

// Hosts returns every host, in last-use order, whether shown or filtered
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
	case components.ClickMsg:
		return m.click(msg)
	case components.WheelMsg:
		m.move(WheelRows * msg.Delta)
		return m, nil
	}
	return m, nil
}

// WheelRows is how many rows one wheel notch moves the selection
// (FR-NAV-012).
const WheelRows = 3

// click handles a left press at line Y of View (FR-NAV-012): on the
// search line it focuses the search field as / does; on a host row it
// selects that host, moving the keyboard back to the list and keeping the
// query, and a double-click connects as Enter does. Blank rows do
// nothing.
func (m Model) click(c components.ClickMsg) (components.Screen, tea.Cmd) {
	row, search := m.rowAt(c.Y)
	switch {
	case search:
		if !m.searching {
			m.searching = true
			m.search.Focus()
			m.search.End()
			m.scroll()
		}
		return m, nil
	case row < 0:
		return m, nil
	}
	if m.searching {
		m.searching = false
		m.search.Blur()
	}
	m.cursor = row
	m.scroll()
	if c.Double {
		return m, m.connect()
	}
	return m, nil
}

// rowAt returns what is at line y of View: the index of a shown host
// (or -1), or search when y is the search line. It follows View's layout.
func (m Model) rowAt(y int) (row int, search bool) {
	if m.searchShown() {
		if y == 0 {
			return -1, true
		}
		y--
	}
	if y < 0 || len(m.shown) == 0 {
		return -1, false
	}
	end := min(m.offset+m.rows(), len(m.shown))
	if i := m.offset + y; i < end {
		return i, false
	}
	return -1, false
}

// searchShown reports whether View shows the search line: while the
// search field has the keyboard, and while a query is kept after a click
// moved the keyboard back to the list.
func (m Model) searchShown() bool { return m.searching || m.search.Value() != "" }

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
	case key.Matches(k, keys.OpenSFTP):
		if h, ok := m.Selected(); ok {
			return m, func() tea.Msg { return SFTPRequestMsg{HostID: h.ID, Label: h.Label} }
		}
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
	if m.searchShown() {
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
	if m.searchShown() {
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
	now := m.now()
	for i := m.offset; i < end; i++ {
		b.WriteString(m.row(m.hosts[m.shown[i]], i == m.cursor, now))
		b.WriteString("\n")
	}
	return b.String()
}

// row renders one host row: the selection glyph, Line, and, when the list
// is at least TimeMinWidth wide and the host was connected, its last use
// right-aligned and dim (FR-NAV-018).
func (m Model) row(h domain.Host, selected bool, now time.Time) string {
	prefix, paint := "  ", func(s string) string { return s }
	if selected {
		prefix, paint = SelectGlyph, func(s string) string { return selectedStyle.Render(s) }
	}
	if m.width < TimeMinWidth || h.LastConnectedAt == nil {
		return paint(m.fit(prefix + Line(h)))
	}
	w := m.width - TimeWidth - 1
	left := padRight(ansi.Truncate(prefix+Line(h), w, "…"), w)
	ago := Ago(*h.LastConnectedAt, now)
	return paint(left) + " " + dimStyle.Render(strings.Repeat(" ", max(TimeWidth-lipgloss.Width(ago), 0))+ago)
}

// Ago is the last-use time t as the host list shows it, relative to now
// (FR-NAV-018): "just now" under a minute (or in the future), "5m ago"
// under an hour, "2h ago" under a day, "3d ago" under 30 days, and then
// the date in now's location: "Oct 12" in now's year, "Oct 2025" before.
// It is at most TimeWidth columns wide.
func Ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", d/time.Minute)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", d/time.Hour)
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", d/(24*time.Hour))
	}
	t = t.In(now.Location())
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2006")
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
