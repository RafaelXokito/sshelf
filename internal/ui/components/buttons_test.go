package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestARCH_UI_007_ModalButtonsMatchView checks that the place AddButtons
// reports for each button is where the modal's box draws its label, for a
// row of its own and for buttons after text on the same line.
func TestARCH_UI_007_ModalButtonsMatchView(t *testing.T) {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	thick := lipgloss.NewStyle().Border(lipgloss.ThickBorder()).Padding(0, 1)
	for _, tc := range []struct {
		name   string
		style  lipgloss.Style
		before string
		labels []string
	}{
		{"own row", box, "Delete host web-prod?\n\n", []string{"[y] Yes", "[n] No"}},
		{"after text", box, "Connecting…\n\n› dialling → · verifying", []string{"[esc] Cancel"}},
		{"thick border", thick, "WARNING", []string{"[esc] Close"}},
		{"first line", box, "", []string{"[enter] Submit", "[esc] Cancel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			b.WriteString(tc.before)
			btns := AddButtons(&b, tc.labels...)
			lines := strings.Split(ansi.Strip(tc.style.Render(b.String())), "\n")
			for i, r := range btns {
				row := []rune(lines[r.Y])
				if got := string(row[r.X : r.X+r.W]); got != tc.labels[i] {
					t.Errorf("button %d at %+v covers %q, want %q\n%s", i, r, got, tc.labels[i], strings.Join(lines, "\n"))
				}
				if btns.At(r.X, r.Y) != i || btns.At(r.X+r.W-1, r.Y) != i || btns.At(r.X+r.W, r.Y) == i || btns.At(r.X-1, r.Y) == i {
					t.Errorf("At does not match button %d's edges", i)
				}
			}
		})
	}
}
