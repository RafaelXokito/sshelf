package keys

import "charm.land/bubbles/v2/key"

// The keymap (ARCH-UI-004). Only the bindings of the MVP slice exist so
// far; T-016 completes it and generates the footer from it.
var (
	Up        = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up"))
	Down      = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down"))
	Connect   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect"))
	Quit      = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	Cancel    = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	ForceQuit = key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit"))
)
