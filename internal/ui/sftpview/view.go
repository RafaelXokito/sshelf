package sftpview

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

var (
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	msgStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// sizeWidth is the width of the size and item-count column: "10000 items".
const sizeWidth = 11

// timeWidth is the width of the modification-time column: "Oct 30 14:02".
const timeWidth = 12

// hintJoiner separates the footer's hints, as in the approved layout.
const hintJoiner = " · "

// View draws rows 1 to height−1 of the tab: the two panes, the progress
// line and the footer (design §9.6). The root model draws the header.
func (m Model) View() string { return strings.Join(m.Lines(), "\n") }

// Lines are View's lines, each at most the terminal's width.
func (m Model) Lines() []string {
	w, _ := m.size()
	lay := m.Layout()
	left := m.box(app.Local, lay.Boxes[0])
	right := m.box(app.Remote, lay.Boxes[1])
	out := make([]string, 0, len(left)+2)
	for i := range left {
		out = append(out, left[i]+right[i])
	}
	out = append(out, m.ProgressLine(), m.Footer())
	for i, l := range out {
		out[i] = ansi.Truncate(l, w, "…")
	}
	return out
}

// box draws pane side in r: the title border, the rows and the bottom
// border with the filter and the mark count.
func (m Model) box(side app.Side, r Rect) []string {
	p := m.panes[side]
	inner := max(r.W-2, 0)
	out := make([]string, 0, r.H)

	suffix := ""
	if m.hidden {
		suffix = " (hidden off)"
	}
	prefix := side.String() + ": "
	avail := inner - 2 - ansi.StringWidth(prefix) - ansi.StringWidth(suffix)
	title := prefix + cutLeft(p.title, avail) + suffix
	title = cut(title, max(inner-2, 0))
	dashes := max(inner-2-ansi.StringWidth(title), 0)
	ts := title
	if m.focus == side {
		ts = titleStyle.Render(title)
	}
	out = append(out, "┌ "+ts+" "+strings.Repeat("─", dashes)+"┐")

	rows := max(r.H-2, 0)
	for i := range rows {
		idx := p.top + i
		var s string
		switch {
		case idx < len(p.rows):
			s = m.rowText(p, idx, inner)
		case i == 0 && !p.loaded:
			s = dimStyle.Render(padRight("   Loading…", inner))
		default:
			s = strings.Repeat(" ", inner)
		}
		out = append(out, "│"+s+"│")
	}

	var parts []string
	if p.filtering || p.filter.Value() != "" {
		parts = append(parts, "/ "+p.filter.View())
	}
	if n := len(p.marked()); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" marked")
	}
	bottom := ""
	if len(parts) > 0 {
		bottom = " " + strings.Join(parts, hintJoiner) + " "
	}
	bottom = ansi.Truncate(bottom, inner, "…")
	out = append(out, "└"+bottom+strings.Repeat("─", max(inner-ansi.StringWidth(bottom), 0))+"┘")
	return out
}

// rowText draws row idx of p in inner columns (design §9.6): the cursor
// column, the mark column, the name, the size or item count and, from
// MtimeFrom columns, the modification time.
func (m Model) rowText(p *pane, idx, inner int) string {
	r := p.rows[idx]
	cur := " "
	focused := m.focus == p.side && idx == p.cursor
	if focused {
		cur = ">"
	}
	mark := " "
	name, size, mtime := "..", "", ""
	if !r.parent {
		switch {
		case p.marks[r.e.Name]:
			mark = "*"
		case r.e.Kind == app.KindDir:
			mark = "▸"
		}
		name = displayName(r.e)
		if r.e.Kind == app.KindDir {
			n, ok := p.counts[r.e.Name]
			if !ok {
				n = countUnknown
			}
			size = FormatCount(n)
		} else {
			size = FormatSize(r.e.Size)
		}
		mtime = FormatTime(r.e.ModTime, m.opts.Now())
	}
	showTime := inner >= MtimeFrom
	nameW := inner - 3 - 1 - sizeWidth
	if showTime {
		nameW -= 1 + timeWidth
	}
	s := cur + mark + " " + padRight(cut(name, nameW), nameW) + " " + padLeft(size, sizeWidth)
	if showTime {
		s += " " + padRight(mtime, timeWidth)
	}
	s = padRight(ansi.Truncate(s, inner, ""), inner)
	if focused {
		return cursorStyle.Render(s)
	}
	return s
}

// ProgressLine is the line under the panes: the message slot
// (FR-SFTP-014, FR-SFTP-024).
func (m Model) ProgressLine() string {
	if m.msg == "" {
		return ""
	}
	w, _ := m.size()
	return msgStyle.Render(" " + cut(m.msg, w-1))
}

// Footer is the last line: the key hints of the current state, from
// ui/keys, dropping hints from the right to fit (FR-NAV-004).
func (m Model) Footer() string {
	w, _ := m.size()
	return dimStyle.Render(fitHints(w-1, m.Hints()))
}

// Hints are the bindings the footer shows.
func (m Model) Hints() []key.Binding {
	switch {
	case m.panes[m.focus].filtering:
		return []key.Binding{keys.FilterKeep, keys.FilterClear}
	case m.disconnected:
		return keys.SFTPDisconnectedKeys()
	}
	return []key.Binding{keys.SwitchPane, keys.Copy, keys.Rename, keys.Delete, keys.Mkdir, keys.EditLocally, keys.Transfers, keys.Refresh, keys.Hidden, keys.Filter, keys.SFTPHelp}
}

// fitHints joins hints as "key desc" with " · ", leading with a space,
// keeping as many from the left as fit in width columns.
func fitHints(width int, bs []key.Binding) string {
	out := ""
	for _, b := range bs {
		h := b.Help()
		part := h.Key + " " + h.Desc
		next := out + hintJoiner + part
		if out == "" {
			next = part
		}
		if ansi.StringWidth(next) > width {
			break
		}
		out = next
	}
	return " " + out
}
