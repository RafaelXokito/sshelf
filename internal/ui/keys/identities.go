package keys

import "charm.land/bubbles/v2/key"

// Identities view and identity form (FR-IDENT-001); the host list opens
// the view with Identities.
var (
	// BackToHosts leaves the identities view for the host list.
	BackToHosts    = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	AddIdentity    = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add"))
	EditIdentity   = key.NewBinding(key.WithKeys("e", "enter"), key.WithHelp("e", "edit"))
	DeleteIdentity = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))

	// ClearSecret, on a secret field of a form, marks the stored secret
	// to be deleted on save (SEC-CRED-004); pressed again it keeps it.
	ClearSecret = key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "clear secret"))
	// StoreKeyFile, on the key file field, stores the file's key in the
	// credential store on save, leaving the file untouched (ADR-010).
	StoreKeyFile = key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "store key in keychain"))
)
