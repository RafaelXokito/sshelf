package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ButtonGap separates the buttons of a modal's button row.
const ButtonGap = "    "

// BoxInsetX and BoxInsetY are the column and line where a modal's content
// starts in its View: inside a one-cell border and one column of padding,
// the box every modal draws (lipgloss Border plus Padding(0, 1)).
const (
	BoxInsetX = 2
	BoxInsetY = 1
)

// ModalButtons is where a modal's bracketed buttons sit in its View, in
// the order they were added (FR-NAV-016).
type ModalButtons []Rect

// AddButtons appends a row of bracketed buttons, such as "[y] Yes" and
// "[n] No", to b, the content of a modal drawn in the standard box, and
// returns where each button lands in the modal's View. The buttons go on
// the line b has reached, after ButtonGap when that line has text.
func AddButtons(b *strings.Builder, labels ...string) ModalButtons {
	s := b.String()
	line := s[strings.LastIndex(s, "\n")+1:]
	y := strings.Count(s, "\n") + BoxInsetY
	x := BoxInsetX + lipgloss.Width(line)
	out := make(ModalButtons, 0, len(labels))
	for i, l := range labels {
		if i > 0 || line != "" {
			b.WriteString(ButtonGap)
			x += len(ButtonGap)
		}
		b.WriteString(l)
		w := lipgloss.Width(l)
		out = append(out, Rect{X: x, Y: y, W: w, H: 1})
		x += w
	}
	return out
}

// At returns the index of the button at column x, line y of the modal's
// View, or -1 when there is none.
func (b ModalButtons) At(x, y int) int {
	for i, r := range b {
		if r.Contains(x, y) {
			return i
		}
	}
	return -1
}
