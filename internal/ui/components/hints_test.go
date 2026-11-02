package components

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

func TestFR_NAV_020_FitHints(t *testing.T) {
	b := func(k, d string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, d)) }
	// Widths: "a add" 5, "l a-long-entry" 14, "c cut" 5, "q quit" 6,
	// "? help" 6, separators 3.
	a, long, c, q, more := b("a", "add"), b("l", "a-long-entry"), b("c", "cut"), b("q", "quit"), b("?", "help")
	shown := []key.Binding{a, long, c, q}
	prio := []key.Binding{q, long, a, c}
	names := func(bs []key.Binding) string {
		var out []string
		for _, b := range bs {
			out = append(out, b.Help().Key)
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct {
		width int
		want  string
	}{
		{0, "a l c q ?"},  // unknown width: everything
		{6, "?"},          // only ? help fits
		{15, "q ?"},       // "q quit • ? help"
		{24, "a q ?"},     // the long entry does not fit, a still does
		{31, "a c q ?"},   // and c
		{32, "l q ?"},     // the long one comes before a and c in priority
		{40, "a l q ?"},   // then a
		{47, "a l q ?"},   // one short of everything
		{48, "a l c q ?"}, // everything, in shown's order
	} {
		got := FitHints(tc.width, shown, prio, more)
		if names(got) != tc.want {
			t.Errorf("width %d: %q, want %q", tc.width, names(got), tc.want)
		}
		if w := ansi.StringWidth(Help(got...)); tc.width >= 6 && w > tc.width {
			t.Errorf("width %d: footer is %d wide", tc.width, w)
		}
	}
}
