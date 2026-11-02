package components

import (
	"slices"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// helpSep separates the entries of a footer (Help).
const helpSep = " • "

// FitHints picks the bindings of a footer width columns wide (FR-NAV-020,
// design §9.8). It walks priority and keeps each binding that still fits
// together with those kept before it and more; a binding left out does
// not stop a later, shorter one. It returns the kept bindings in shown's
// order, then more. A width of 0 or less keeps every binding of shown.
// Bindings are told apart by their help key.
func FitHints(width int, shown, priority []key.Binding, more key.Binding) []key.Binding {
	entry := func(b key.Binding) int {
		h := b.Help()
		return ansi.StringWidth(h.Key + " " + h.Desc)
	}
	kept := map[string]bool{}
	if width <= 0 {
		for _, b := range shown {
			kept[b.Help().Key] = true
		}
	} else {
		used := entry(more)
		for _, b := range priority {
			if w := used + ansi.StringWidth(helpSep) + entry(b); w <= width {
				kept[b.Help().Key], used = true, w
			}
		}
	}
	out := slices.DeleteFunc(slices.Clone(shown), func(b key.Binding) bool { return !kept[b.Help().Key] })
	return append(out, more)
}
