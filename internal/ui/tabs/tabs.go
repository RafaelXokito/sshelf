// Package tabs lays out the M2 frame (design §9.4): the header with the tab
// bar, the session status bar and the session area. Layout is one pure
// function shared by View and hit-testing (ARCH-UI-007).
package tabs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// LabelWidth is the most columns a host label takes in a tab (FR-TAB-003).
const LabelWidth = 20

// Markers shown after a tab's label (FR-TAB-004).
const (
	ActivityMarker = "●"
	EndedMarker    = "✕"
)

const (
	title    = "sshelf "
	sep      = "│"
	statusOK = "● connected"
	joiner   = " · "
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	focusStyle   = lipgloss.NewStyle().Reverse(true).Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	endedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	activeMarker = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
)

// Entry is one tab of the bar.
type Entry struct {
	Text   string // "Hosts" or "<number> <label>", already sanitised and cut
	Marker string // "", ActivityMarker or EndedMarker
}

// HostsEntry is the Hosts tab.
func HostsEntry() Entry { return Entry{Text: "Hosts"} }

// SessionEntry is the tab of session number num: the host label,
// sanitised (SEC-SESSION-003) and cut to LabelWidth columns, with the end
// marker taking precedence over the activity marker.
func SessionEntry(num int, label string, activity, ended bool) Entry {
	e := Entry{Text: strconv.Itoa(num) + " " + ansi.Truncate(domain.Sanitize(label), LabelWidth, "…")}
	switch {
	case ended:
		e.Marker = EndedMarker
	case activity:
		e.Marker = ActivityMarker
	}
	return e
}

// SFTPEntry is the tab of SFTP tab number num (FR-TAB-003 M3 revision):
// "<num> sftp: <label>", the text after the number sanitised and cut to
// LabelWidth columns, with the disconnected marker taking precedence over
// the activity marker.
func SFTPEntry(num int, label string, activity, disconnected bool) Entry {
	e := Entry{Text: strconv.Itoa(num) + " " + ansi.Truncate("sftp: "+domain.Sanitize(label), LabelWidth, "…")}
	switch {
	case disconnected:
		e.Marker = EndedMarker
	case activity:
		e.Marker = ActivityMarker
	}
	return e
}

// Kind says what a Span shows.
type Kind int

// Span kinds.
const (
	KindTitle     Kind = iota // "sshelf "
	KindSep                   // "│"
	KindTab                   // an entry
	KindMoreLeft              // "‹+N": N tabs hidden on the left
	KindMoreRight             // "+N›": N tabs hidden on the right
)

// Span is one piece of the header, at column X.
type Span struct {
	X, Width int
	Text     string // plain text
	Kind     Kind
	Tab      int  // index into the entries for KindTab; the nearest hidden tab for the overflow markers; -1 otherwise
	Focused  bool // the focused tab
}

// Layout lays out the header for entries at width columns: the title,
// then the visible tabs separated by "│". The focused tab is always
// visible; tabs that do not fit are replaced by "‹+N" and "+N›"
// (FR-TAB-003). The result may be wider than width only when the focused
// tab alone does not fit.
func Layout(width int, entries []Entry, focus int) []Span {
	n := len(entries)
	if n == 0 {
		return []Span{{Text: title, Width: ansi.StringWidth(title), Kind: KindTitle, Tab: -1}}
	}
	focus = min(max(focus, 0), n-1)
	texts := make([]string, n)
	for i, e := range entries {
		texts[i] = entryText(e, i == focus)
	}
	lo, hi := focus, focus+1
	fits := func(lo, hi int) bool { return measure(texts, lo, hi) <= width }
	for {
		grew := false
		if hi < n && fits(lo, hi+1) {
			hi++
			grew = true
		}
		if lo > 0 && fits(lo-1, hi) {
			lo--
			grew = true
		}
		if !grew {
			break
		}
	}

	var spans []Span
	x := 0
	add := func(text string, kind Kind, tab int, focused bool) {
		w := ansi.StringWidth(text)
		spans = append(spans, Span{X: x, Width: w, Text: text, Kind: kind, Tab: tab, Focused: focused})
		x += w
	}
	add(title, KindTitle, -1, false)
	add(sep, KindSep, -1, false)
	if lo > 0 {
		add(moreLeft(lo), KindMoreLeft, lo-1, false)
		add(sep, KindSep, -1, false)
	}
	for i := lo; i < hi; i++ {
		add(texts[i], KindTab, i, i == focus)
		add(sep, KindSep, -1, false)
	}
	if hi < n {
		add(moreRight(n-hi), KindMoreRight, hi, false)
	}
	return spans
}

// entryText is a tab's text: padded with spaces, or with "[" and "]" when
// focused, so focus is not shown by colour alone (NFR-UX-003).
func entryText(e Entry, focused bool) string {
	s := e.Text
	if e.Marker != "" {
		s += " " + e.Marker
	}
	if focused {
		return "[" + s + "]"
	}
	return " " + s + " "
}

func moreLeft(n int) string  { return "‹+" + strconv.Itoa(n) }
func moreRight(n int) string { return "+" + strconv.Itoa(n) + "›" }

// measure is the width of the header showing tabs [lo, hi).
func measure(texts []string, lo, hi int) int {
	w := ansi.StringWidth(title) + 1
	if lo > 0 {
		w += ansi.StringWidth(moreLeft(lo)) + 1
	}
	for i := lo; i < hi; i++ {
		w += ansi.StringWidth(texts[i]) + 1
	}
	if hi < len(texts) {
		w += ansi.StringWidth(moreRight(len(texts) - hi))
	}
	return w
}

// Header renders spans as the styled header line.
func Header(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		switch {
		case s.Kind == KindTitle:
			b.WriteString(titleStyle.Render("sshelf") + " ")
		case s.Focused:
			b.WriteString(focusStyle.Render(s.Text))
		case s.Kind == KindTab && strings.HasSuffix(s.Text, " "+ActivityMarker+" "):
			b.WriteString(strings.TrimSuffix(s.Text, ActivityMarker+" ") + activeMarker.Render(ActivityMarker) + " ")
		case s.Kind == KindSep || s.Kind == KindMoreLeft || s.Kind == KindMoreRight:
			b.WriteString(dimStyle.Render(s.Text))
		default:
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// TabAt returns the tab under column x of the header laid out as spans:
// the entry index for a tab, or the nearest hidden tab for an overflow
// marker. It is the hit-test for clicks on the tab bar (design §9.5).
func TabAt(spans []Span, x int) (int, bool) {
	for _, s := range spans {
		if x >= s.X && x < s.X+s.Width && s.Tab >= 0 {
			return s.Tab, true
		}
	}
	return 0, false
}

// Duration formats d as HH:MM:SS.
func Duration(d time.Duration) string {
	d = max(d, 0).Truncate(time.Second)
	s := int(d / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// Fit joins the fixed parts and then the optional parts with " · ",
// dropping optional parts from the end until the line fits width, and
// cuts it with "…" if even the fixed parts do not fit.
func Fit(width int, fixed []string, optional ...string) string {
	parts := append(append([]string(nil), fixed...), optional...)
	for len(parts) > len(fixed) && ansi.StringWidth(strings.Join(parts, joiner)) > width {
		parts = parts[:len(parts)-1]
	}
	return ansi.Truncate(strings.Join(parts, joiner), width, "…")
}

// LiveStatus is a live session's status bar (FR-TAB-007):
// "● connected · user@host:port · 01:02:03 · <hints>". When it does not
// fit, the hints go first, then the duration.
func LiveStatus(width int, addr string, d time.Duration, hints ...string) string {
	return statusStyle.Render(Fit(width, []string{statusOK, domain.Sanitize(addr)}, append([]string{Duration(d)}, hints...)...))
}

// EndedStatus is an ended session's status bar (FR-CONN-005 revision):
// "✕ <reason> · <hints>".
func EndedStatus(width int, reason string, hints ...string) string {
	return endedStyle.Render(Fit(width, []string{EndedMarker + " " + domain.Sanitize(reason)}, hints...))
}

// Area fits lines to the session area: rows lines, each cut to width
// columns. Missing lines are blank.
func Area(lines []string, width, rows int) []string {
	out := make([]string, max(rows, 0))
	for i := range out {
		if i < len(lines) {
			out[i] = ansi.Truncate(lines[i], width, "")
		}
	}
	return out
}
