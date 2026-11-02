package components

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// noticeGap is the least space between a footer's hints and the update
// notice.
const noticeGap = 2

var noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

// HelpWidth is the width of Help(bs...).
func HelpWidth(bs ...key.Binding) int {
	w := 0
	for i, b := range bs {
		if i > 0 {
			w += ansi.StringWidth(helpSep)
		}
		h := b.Help()
		w += ansi.StringWidth(h.Key + " " + h.Desc)
	}
	return w
}

// NoticeFooter returns a footer width columns wide that ends with the
// update notice for the newer release latest, right-aligned (FR-UPD-002,
// design §9.8): `update available: vX.Y.Z · run sshelf update`, or
// `vX.Y.Z available` when that does not fit. A form is used only when
// essential columns of hints still fit beside it; hints(avail) renders the
// hints for avail columns. ok is false when latest is empty, the width is
// unknown or neither form fits. latest is a version update.Parse accepted,
// so it holds no control characters.
func NoticeFooter(width int, latest string, essential int, hints func(avail int) string) (foot string, ok bool) {
	if latest == "" || width <= 0 {
		return "", false
	}
	forms := []struct{ plain, styled string }{
		{"update available: " + latest + " · run sshelf update",
			helpStyle.Render("update available: ") + noticeStyle.Render(latest) + helpStyle.Render(" · run sshelf ") + noticeStyle.Render("update")},
		{latest + " available", noticeStyle.Render(latest) + helpStyle.Render(" available")},
	}
	for _, f := range forms {
		w := ansi.StringWidth(f.plain)
		if essential+noticeGap+w > width {
			continue
		}
		left := hints(width - noticeGap - w)
		return left + strings.Repeat(" ", max(width-lipgloss.Width(left)-w, noticeGap)) + f.styled, true
	}
	return "", false
}
