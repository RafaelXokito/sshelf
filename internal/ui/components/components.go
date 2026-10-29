package components

import tea "charm.land/bubbletea/v2"

// Screen is one full-screen view. The root model routes messages to the
// active screen (design §9.2).
type Screen interface {
	Update(msg tea.Msg) (Screen, tea.Cmd)
	View() string
}

// Modal is an overlay stacked over the screen. The top modal receives input
// first.
type Modal interface {
	Update(msg tea.Msg) (Modal, tea.Cmd)
	View() string
}
