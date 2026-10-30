package keys

import "charm.land/bubbles/v2/key"

// Host list (M3 addition, FR-SFTP-002).
var OpenSFTP = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sftp"))

// SFTP tab panes (design §9.3, FR-SFTP-007 … FR-SFTP-013). Up, Down,
// First and Last are the host list's.
var (
	PageUp      = key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up"))
	PageDown    = key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("PgDn", "page down"))
	SwitchPane  = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane"))
	OpenDir     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	ParentDir   = key.NewBinding(key.WithKeys("backspace", "h"), key.WithHelp("h", "parent"))
	HomeDir     = key.NewBinding(key.WithKeys("~"), key.WithHelp("~", "home"))
	Mark        = key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "mark"))
	Copy        = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy"))
	Rename      = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename"))
	Delete      = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	Mkdir       = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mkdir"))
	EditLocally = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit locally"))
	Transfers   = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "transfers"))
	Refresh     = key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh"))
	Hidden      = key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden files"))
	Filter      = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter"))
	SFTPHelp    = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help"))
)

// SFTP filter field (FR-SFTP-008): enter keeps the filter, esc clears it.
var (
	FilterKeep  = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "keep filter"))
	FilterClear = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter"))
)

// Transfers view (FR-SFTP-015).
var (
	TransferCancel = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "cancel"))
	TransferRetry  = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry"))
	TransferAnswer = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "answer"))
	TransferClear  = key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "clear finished"))
	TransfersBack  = key.NewBinding(key.WithKeys("t", "esc"), key.WithHelp("t/esc", "back"))
)

// Conflict modal (FR-SFTP-016).
var (
	ConflictOverwrite = key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "overwrite"))
	ConflictSkip      = key.NewBinding(key.WithKeys("s", "esc"), key.WithHelp("s", "skip"))
	ConflictKeepBoth  = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "keep both"))
	ConflictAll       = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "apply to all"))
)

// SFTPPaneKeys are the panes' bindings in help order; the footer shows
// them from the left, as many as fit.
func SFTPPaneKeys() []key.Binding {
	return []key.Binding{SwitchPane, Copy, Rename, Delete, Mkdir, EditLocally, Transfers, OpenDir, ParentDir, HomeDir, Mark, Refresh, Hidden, Filter, Up, Down, PageUp, PageDown, First, Last, SFTPHelp}
}

// SFTPDisconnectedKeys are the bindings of a disconnected SFTP tab
// (FR-SFTP-018), besides the local pane's movement keys.
func SFTPDisconnectedKeys() []key.Binding {
	return []key.Binding{Reconnect, CloseTab, Transfers}
}

// sftpMaps are the keymaps of the SFTP tab.
func sftpMaps() []Map {
	return []Map{
		{Name: "sftp panes", Bindings: append(SFTPPaneKeys(), Prefix)},
		{Name: "sftp filter", Bindings: append([]key.Binding{FilterKeep, FilterClear}, textInput...)},
		{Name: "sftp disconnected", Bindings: append(SFTPDisconnectedKeys(), Up, Down, PageUp, PageDown, First, Last, OpenDir, ParentDir, HomeDir, SwitchPane, Prefix)},
		{Name: "transfers", Bindings: []key.Binding{Up, Down, TransferCancel, TransferRetry, TransferAnswer, TransferClear, TransfersBack, Prefix}},
		{Name: "conflict", Bindings: []key.Binding{ConflictOverwrite, ConflictSkip, ConflictKeepBoth, ConflictAll}},
		{Name: "input", Bindings: append([]key.Binding{Submit, Cancel}, textInput...)},
	}
}
