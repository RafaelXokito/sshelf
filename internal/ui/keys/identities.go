package keys

import "charm.land/bubbles/v2/key"

// Identities (FR-NAV-003 `i`, FR-IDENT-001). These live in their own file
// so the host-list keymap work (T-016) can merge around them.
var (
	// Identities opens the identities view from the host list.
	Identities = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "identities"))
	// Back leaves a view for the one it was opened from.
	Back           = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	AddIdentity    = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add"))
	EditIdentity   = key.NewBinding(key.WithKeys("e", "enter"), key.WithHelp("e", "edit"))
	DeleteIdentity = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
)
