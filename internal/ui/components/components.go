package components

import (
	"errors"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
)

// Screen is one full-screen view. The root model routes messages to the
// active screen (design §9.2). The root model also sends it a
// tea.WindowSizeMsg holding the size of the area it may draw in.
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

// Hinter is a screen or modal whose key hints the root model renders as
// the footer, the last line of the terminal (FR-NAV-004). Hints returns
// the main bindings of its current state, from ui/keys. A screen that is
// not a Hinter ends its View with its own hint line, which the root model
// moves to the footer.
type Hinter interface {
	Hints() []key.Binding
}

// ErrorText is the text a form shows for a failed save. A missing
// credential store gets the D-101 explanation, which names it and says
// how to fix it (FR-CRED-002).
func ErrorText(err error) string {
	if se, ok := errors.AsType[*app.StoreError](err); ok && errors.Is(err, app.ErrStoreUnavailable) {
		return "Not saved. " + se.Explain() + "; passwords and keys cannot be saved until then."
	}
	return err.Error()
}
