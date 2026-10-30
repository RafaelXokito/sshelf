package keys

import "charm.land/bubbles/v2/key"

// The keymap (ARCH-UI-004). Every binding sshelf reacts to is defined here
// with its help text; Maps groups them by view, and footers are generated
// from them (FR-NAV-004).
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

// Host list (FR-NAV-003). The groups key of design §9.3 is v1 and stays
// unassigned; g is First.
var (
	First      = key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "first"))
	Last       = key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "last"))
	Search     = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search"))
	AddHost    = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add"))
	EditHost   = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit"))
	DeleteHost = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	Identities = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "identities"))
	Import     = key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "import"))
)

// Search field (FR-NAV-002): letters type into the query, so only the
// arrows move the selection.
var (
	SearchUp    = key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up"))
	SearchDown  = key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "down"))
	ClearSearch = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear search"))
)

// Welcome screen (FR-NAV-013).
var (
	WelcomeImport = key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "import ~/.ssh/config"))
	WelcomeAdd    = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add host"))
)

// Import report (FR-IMPORT-003): any of these returns to the host list.
var (
	Back = key.NewBinding(key.WithKeys("esc", "enter"), key.WithHelp("enter/esc", "back to hosts"))
)

// Forms (design §9.3): tab/shift+tab move between fields (NextField,
// PrevField), ctrl+s saves, esc cancels (Cancel) and asks first when there
// are unsaved changes. Ctrl+C cancels the form instead of quitting
// (FR-NAV-003).
var (
	Save       = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	ChoicePrev = key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "previous choice"))
	ChoiceNext = key.NewBinding(key.WithKeys("right", "space"), key.WithHelp("→", "next choice"))
	// PressButton presses the focused [ Save ] or [ Cancel ] button of a
	// form (FR-NAV-015).
	PressButton = key.NewBinding(key.WithKeys("enter", "space"), key.WithHelp("enter/space", "press button"))
)

// Confirm modal: only y confirms; the default answer is No.
var (
	ConfirmYes = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes"))
	ConfirmNo  = key.NewBinding(key.WithKeys("n", "enter", "esc"), key.WithHelp("n/enter/esc", "no"))
)

// MouseAction is something the mouse does on sshelf's screens (D-011),
// with the bindings that do the same from the keyboard (NFR-UX-001).
type MouseAction struct {
	Name string
	Keys []key.Binding
}

// MouseActions returns every mouse action of design §9.5 on sshelf's
// screens with its keyboard equivalents.
func MouseActions() []MouseAction {
	return []MouseAction{
		{"host list: click a host", []key.Binding{Up, Down, First, Last}},
		{"host list: double-click a host", []key.Binding{Connect}},
		{"host list: wheel", []key.Binding{Up, Down}},
		{"host list: click the search field", []key.Binding{Search}},
		{"welcome: click Import", []key.Binding{WelcomeImport}},
		{"welcome: click Add host", []key.Binding{WelcomeAdd}},
		{"form: click a field", []key.Binding{NextField, PrevField}},
		{"form: click ‹ or › of a choice", []key.Binding{ChoicePrev, ChoiceNext}},
		{"form: click [ Save ]", []key.Binding{Save, PressButton}},
		{"form: click [ Cancel ]", []key.Binding{Cancel, PressButton}},
		{"confirm: click [y] Yes", []key.Binding{ConfirmYes}},
		{"confirm: click [n] No", []key.Binding{ConfirmNo}},
		{"connecting: click [esc] Cancel", []key.Binding{Cancel}},
		{"host key prompt: click [y] Trust and save", []key.Binding{TrustAndSave}},
		{"host key prompt: click [enter/esc] Cancel", []key.Binding{TOFUCancel}},
		{"authentication prompt: click a field", []key.Binding{NextField, PrevField}},
		{"authentication prompt: click [enter] Submit", []key.Binding{Submit}},
		{"authentication prompt: click [esc] Cancel", []key.Binding{Cancel}},
		{"changed host key: click [esc] Close", []key.Binding{CloseBlock}},
		{"prefix help: click [esc] Close", []key.Binding{CloseHelp}},
		{"tab bar: click a tab", []key.Binding{PrefixHosts, PrefixTab}},
		{"sftp: click a row", []key.Binding{SwitchPane, Up, Down, First, Last}},
		{"sftp: click a pane's border", []key.Binding{SwitchPane}},
		{"sftp: double-click a directory", []key.Binding{OpenDir}},
		{"sftp: wheel over a pane", []key.Binding{PageUp, PageDown, Up, Down}},
		{"sftp: click the progress line", []key.Binding{Transfers}},
		{"sftp: drag to the other pane", []key.Binding{Mark, Copy}},
	}
}

// Map is the keymap of one view: every binding active while it has the
// keyboard. Within a Map no two bindings share a key (ARCH-UI-004).
type Map struct {
	Name     string
	Bindings []key.Binding
}

// textInput are the bindings of a focused components.TextInput.
var textInput = []key.Binding{CursorLeft, CursorRight, CursorHome, CursorEnd, DeleteBack, DeleteForward, DeleteToStart, DeleteToEnd}

// Maps returns the keymap of every view. The help overlay (FR-NAV-006,
// v1) is generated from it; footers show a subset.
func Maps() []Map {
	return append([]Map{
		{Name: "host list", Bindings: []key.Binding{Up, Down, First, Last, Search, Connect, OpenSFTP, AddHost, EditHost, DeleteHost, Identities, Import, Quit, ForceQuit}},
		{Name: "search", Bindings: append([]key.Binding{SearchUp, SearchDown, Connect, ClearSearch, ForceQuit}, textInput...)},
		{Name: "welcome", Bindings: []key.Binding{WelcomeImport, WelcomeAdd, Quit, ForceQuit}},
		{Name: "form", Bindings: []key.Binding{NextField, PrevField, Save, Cancel, ForceQuit}},
		{Name: "form text field", Bindings: textInput},
		{Name: "form choice field", Bindings: []key.Binding{ChoicePrev, ChoiceNext}},
		{Name: "form button", Bindings: []key.Binding{PressButton}},
		{Name: "import report", Bindings: []key.Binding{Back, Up, Down, ForceQuit}},
		{Name: "load error", Bindings: []key.Binding{Quit, ForceQuit}},
		{Name: "confirm", Bindings: []key.Binding{ConfirmYes, ConfirmNo, ForceQuit}},
		{Name: "connecting", Bindings: []key.Binding{Cancel, ForceQuit}},
		{Name: "host key prompt", Bindings: []key.Binding{TrustAndSave, TOFUCancel, ForceQuit}},
		{Name: "authentication prompt", Bindings: append([]key.Binding{Submit, NextField, PrevField, Cancel, ForceQuit}, textInput...)},
		{Name: "changed host key", Bindings: []key.Binding{CloseBlock, ForceQuit}},
		{Name: "identities", Bindings: []key.Binding{Up, Down, AddIdentity, EditIdentity, DeleteIdentity, BackToHosts, ForceQuit}},
		{Name: "form secret field", Bindings: append([]key.Binding{ClearSecret}, textInput...)},
		{Name: "form key file field", Bindings: append([]key.Binding{StoreKeyFile}, textInput...)},
		{Name: "prefix", Bindings: PrefixCommands()},
		{Name: "prefix help", Bindings: []key.Binding{CloseHelp, ForceQuit}},
		{Name: "ended session", Bindings: []key.Binding{Reconnect, CloseTab, Prefix}},
		{Name: "scrollback", Bindings: []key.Binding{ScrollUp, ScrollDown, ScrollPageUp, ScrollPageDown, ScrollTop, ScrollBottom, ScrollExit, Prefix}},
	}, sftpMaps()...)
}
