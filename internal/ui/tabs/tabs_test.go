package tabs

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// header is the plain text of spans.
func header(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// col is the column of the first s in h.
func col(t *testing.T, h, s string) int {
	t.Helper()
	i := strings.Index(h, s)
	if i < 0 {
		t.Fatalf("%q not in %q", s, h)
	}
	return ansi.StringWidth(h[:i])
}

// TestFR_TAB_004_D028_CloseAtMarker: CloseAt finds an ended tab's "✕"
// and the cell after it, focused or not, after a label cut at
// LabelWidth, for session and SFTP tabs; nothing else on a tab, and no
// live tab, is a close button.
func TestFR_TAB_004_D028_CloseAtMarker(t *testing.T) {
	long := strings.Repeat("a", 40)
	entries := []Entry{
		HostsEntry(),
		SessionEntry(1, long, false, true),
		SessionEntry(2, "web-prod", true, false),
		SFTPEntry(3, "pi", false, true),
		SessionEntry(4, "db", false, true),
	}
	for focus := range entries {
		spans := Layout(200, entries, focus)
		h := header(spans)
		runes := []rune(h)
		closers := map[int]bool{}
		for x := range ansi.StringWidth(h) {
			i, ok := CloseAt(spans, x)
			if !ok {
				continue
			}
			closers[i] = true
			if r := runes[x]; r != '✕' && r != ' ' && r != ']' {
				t.Fatalf("focus %d: close hit at column %d (%q) of %q", focus, x, r, h)
			}
			if j, _ := TabAt(spans, x); j != i {
				t.Fatalf("focus %d: column %d closes tab %d but lies on tab %d", focus, x, i, j)
			}
		}
		if len(closers) != 3 || !closers[1] || !closers[3] || !closers[4] {
			t.Fatalf("focus %d: tabs with a close button %v in %q", focus, closers, h)
		}
		// The first "✕" is tab 1's, after its cut label.
		x := col(t, h, EndedMarker)
		if i, ok := CloseAt(spans, x); !ok || i != 1 {
			t.Fatalf("focus %d: ✕ at %d of %q gives %d %v", focus, x, h, i, ok)
		}
		if _, ok := CloseAt(spans, x+1); !ok {
			t.Fatalf("focus %d: the cell after ✕ (a wide ✕) is not a close hit", focus)
		}
		if _, ok := CloseAt(spans, x-1); ok {
			t.Fatalf("focus %d: the space before ✕ closes", focus)
		}
		if _, ok := CloseAt(spans, col(t, h, ActivityMarker)); ok {
			t.Fatalf("focus %d: the ● of a live tab closes", focus)
		}
	}
}

// TestFR_TAB_004_D028_CloseAtOverflow: a hidden ended tab has no close
// button, and the overflow markers only focus (FR-TAB-003).
func TestFR_TAB_004_D028_CloseAtOverflow(t *testing.T) {
	entries := []Entry{HostsEntry()}
	for i := 1; i <= 9; i++ {
		entries = append(entries, SessionEntry(i, "host-"+strings.Repeat("x", i), false, true))
	}
	spans := Layout(60, entries, 9)
	h := header(spans)
	if !strings.Contains(h, "‹+") {
		t.Fatalf("no overflow in %q", h)
	}
	for x := range ansi.StringWidth(h) {
		i, ok := CloseAt(spans, x)
		if !ok {
			continue
		}
		visible := false
		for _, s := range spans {
			visible = visible || (s.Kind == KindTab && s.Tab == i)
		}
		if !visible {
			t.Fatalf("column %d closes hidden tab %d", x, i)
		}
	}
	if _, ok := CloseAt(spans, col(t, h, "‹+")); ok {
		t.Fatal("the overflow marker closes a tab")
	}
}
