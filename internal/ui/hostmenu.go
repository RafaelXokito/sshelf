package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/hostmenu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// otherPress handles a press of a button other than the left one
// (FR-NAV-019, design §9.8): outside a popup it closes it; on a host row of
// the Hosts tab with no modal open, a right press selects the host and
// opens the host menu at the pointer. Anything else does nothing.
func (m Model) otherPress(ms tea.Mouse) (tea.Model, tea.Cmd) {
	lay := m.layout()
	if n := len(m.modals); n > 0 {
		if m.outsidePopup(lay, ms) {
			m.popModal()
		}
		return m, nil
	}
	if ms.Button != tea.MouseRight || m.focus != 0 {
		return m, nil
	}
	l, ok := m.screen.(hostlist.Model)
	if !ok {
		return m, nil
	}
	_, y, ok := lay.screen.local(ms.X, ms.Y)
	if !ok {
		return m, nil
	}
	l, h, ok := l.RightClick(y)
	if !ok {
		return m, nil
	}
	m.screen = l
	m.modals = append(m.modals, hostmenu.New(h, ms.X, ms.Y))
	return m, nil
}

// outsidePopup reports whether the top modal is a popup and ms is outside
// it, which closes it.
func (m Model) outsidePopup(lay frameLayout, ms tea.Mouse) bool {
	n := len(m.modals)
	if n == 0 {
		return false
	}
	if _, ok := m.modals[n-1].(components.Popup); !ok {
		return false
	}
	_, _, in := lay.modals[n-1].local(ms.X, ms.Y)
	return !in
}

// hostMenuChosen sends the key of the item chosen in the host menu to the
// host list, so that it does exactly what that key does (FR-NAV-019).
func (m Model) hostMenuChosen(msg hostmenu.ChosenMsg) (tea.Model, tea.Cmd) {
	l, ok := m.screen.(hostlist.Model)
	if !ok || m.focus != 0 || len(m.modals) > 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.screen, cmd = l.Update(msg.Key)
	return m, cmd
}

// placePopup is where a popup w×h opened at cell (x, y) is drawn: moved
// left and up as far as needed to lie wholly in the body of the frame,
// between the header and the status line (design §9.8).
func (m Model) placePopup(x, y, w, h int) region {
	if m.width > 0 {
		x = min(x, m.width-w)
	}
	if m.height > 0 {
		y = min(y, hostsFrameTop+m.bodyHeight()-h)
	}
	return region{X: max(x, 0), Y: max(y, hostsFrameTop), W: w, H: h}
}

// overlay draws lines over base with their top-left corner at column x,
// row y, keeping what base has to their left and right.
func overlay(base, lines []string, x, y int) []string {
	for i, l := range lines {
		r := y + i
		for len(base) <= r {
			base = append(base, "")
		}
		left := ansi.Truncate(base[r], x, "")
		if w := ansi.StringWidth(left); w < x {
			left += strings.Repeat(" ", x-w)
		}
		right := ansi.TruncateLeft(base[r], x+ansi.StringWidth(l), "")
		base[r] = left + ansi.ResetStyle + l + ansi.ResetStyle + right
	}
	return base
}
