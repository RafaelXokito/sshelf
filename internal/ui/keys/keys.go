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

	// TOFU modal (design §9.3): y is the only key that trusts; Enter
	// cancels.
	TrustAndSave = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "trust and save"))
	TOFUCancel   = key.NewBinding(key.WithKeys("enter", "esc"), key.WithHelp("enter/esc", "cancel"))

	// Prompt modal: enter moves to the next field and submits on the last.
	Submit    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit"))
	NextField = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field"))
	PrevField = key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous field"))

	// Changed-host-key block screen: closing is the only action.
	CloseBlock = key.NewBinding(key.WithKeys("esc", "enter", "q"), key.WithHelp("esc", "close"))

	// Text inputs (components.TextInput). Letters always type, so only
	// arrows, editing keys and readline-style control keys are bound.
	CursorLeft    = key.NewBinding(key.WithKeys("left", "ctrl+b"), key.WithHelp("←", "cursor left"))
	CursorRight   = key.NewBinding(key.WithKeys("right", "ctrl+f"), key.WithHelp("→", "cursor right"))
	CursorHome    = key.NewBinding(key.WithKeys("home", "ctrl+a"), key.WithHelp("home", "start of field"))
	CursorEnd     = key.NewBinding(key.WithKeys("end", "ctrl+e"), key.WithHelp("end", "end of field"))
	DeleteBack    = key.NewBinding(key.WithKeys("backspace", "ctrl+h"), key.WithHelp("backspace", "delete before cursor"))
	DeleteForward = key.NewBinding(key.WithKeys("delete", "ctrl+d"), key.WithHelp("delete", "delete at cursor"))
	DeleteToStart = key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "delete to start"))
	DeleteToEnd   = key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "delete to end"))
)
