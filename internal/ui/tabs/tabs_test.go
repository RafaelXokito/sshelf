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

// TestFR_TAB_004_D028_CloseAtMarker: CloseAt finds every tab's "✕" (D-030)
// and the cell after it, focused or not, after a label cut at
// LabelWidth, for session and SFTP tabs, live or ended; nothing else on
// a tab, and not the Hosts tab, is a close button.
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
		if len(closers) != 4 || closers[0] {
			t.Fatalf("focus %d: tabs with a close button %v in %q", focus, closers, h)
		}
		// The first "✕" is tab 1's, after its cut label.
		x := col(t, h, CloseButton)
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

// TestFR_TAB_004_D030_Markers: every session and SFTP tab ends with the
// close button, after "●" or "ended"; the Hosts tab has none. The
// ended marker is plain ASCII, so its width is the same on every
// terminal, and the styled header shows the same text as the layout.
func TestFR_TAB_004_D030_Markers(t *testing.T) {
	entries := []Entry{
		HostsEntry(),
		SessionEntry(1, "web-prod", false, false),
		SessionEntry(2, "db", true, false),
		SessionEntry(3, "pi", true, true),
		SFTPEntry(4, "pi", false, false),
		SFTPEntry(5, "pi", true, true),
	}
	if HostsEntry().Close {
		t.Fatal("the Hosts tab has a close button")
	}
	for _, r := range EndedMarker {
		if r > 0x7e {
			t.Fatalf("EndedMarker %q is not ASCII", EndedMarker)
		}
	}
	want := "sshelf │ Hosts │ 1 web-prod ✕ │ 2 db ● ✕ │ 3 pi ended ✕ │ 4 sftp: pi ✕ │ 5 sftp: pi ended ✕ │"
	for focus := range entries {
		spans := Layout(200, entries, focus)
		h := header(spans)
		if strings.NewReplacer("[", " ", "]", " ").Replace(h) != want {
			t.Fatalf("focus %d: header %q", focus, h)
		}
		if got := ansi.Strip(Header(spans)); got != h {
			t.Fatalf("focus %d: rendered %q, laid out %q", focus, got, h)
		}
	}
}

// TestFR_TAB_004_D030_CloseAtNarrow: at 80 columns the focused tab's
// button stays visible and clickable with labels cut at LabelWidth.
func TestFR_TAB_004_D030_CloseAtNarrow(t *testing.T) {
	entries := []Entry{HostsEntry()}
	for i := 1; i <= 6; i++ {
		entries = append(entries, SessionEntry(i, strings.Repeat("h", 30), false, i%2 == 0))
	}
	for focus := 1; focus < len(entries); focus++ {
		spans := Layout(80, entries, focus)
		h := []rune(header(spans))
		found := false
		for _, s := range spans {
			if s.Kind == KindTab && s.Tab == focus {
				found = s.Close && string(h[s.CloseX]) == CloseButton
				if i, ok := CloseAt(spans, s.CloseX); !ok || i != focus {
					t.Fatalf("focus %d: ✕ at %d gives %d %v", focus, s.CloseX, i, ok)
				}
			}
		}
		if !found || ansi.StringWidth(string(h)) > 80 {
			t.Fatalf("focus %d: %q", focus, string(h))
		}
	}
}
