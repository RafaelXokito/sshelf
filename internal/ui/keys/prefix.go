package keys

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Prefix key (design §8.5, FR-TAB-005): Ctrl+] then one command key.
// Neither key reaches a session (ARCH-SESS-006).
var (
	Prefix           = key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("Ctrl+]", "prefix"))
	PrefixDisconnect = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "disconnect"))
	PrefixHelp       = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help"))
	PrefixHosts      = key.NewBinding(key.WithKeys("h", "0"), key.WithHelp("h/0", "hosts"))
	PrefixTab        = key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "tab"))
	PrefixNext       = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next tab"))
	PrefixPrev       = key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous tab"))
	PrefixScrollback = key.NewBinding(key.WithKeys("["), key.WithHelp("[", "scrollback"))
	PrefixLiteral    = key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("Ctrl+]", "send Ctrl+]"))
	PrefixCancel     = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))

	// Ended session tab (FR-CONN-005 revision, FR-CONN-010).
	Reconnect = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reconnect"))
	CloseTab  = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "close"))

	// Scrollback mode (FR-TAB-010).
	ScrollUp       = key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "line up"))
	ScrollDown     = key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "line down"))
	ScrollPageUp   = key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up"))
	ScrollPageDown = key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("PgDn", "page down"))
	ScrollTop      = key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top"))
	ScrollBottom   = key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom"))
	ScrollExit     = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))

	// CloseHelp closes the prefix help overlay.
	CloseHelp = key.NewBinding(key.WithKeys("?", "esc"), key.WithHelp("?/esc", "close"))
)

// PrefixCommands are the commands after the prefix key, in help order.
func PrefixCommands() []key.Binding {
	return []key.Binding{PrefixDisconnect, PrefixHelp, PrefixHosts, PrefixTab, PrefixNext, PrefixPrev, PrefixScrollback, PrefixLiteral, PrefixCancel}
}

// PrefixAction is what a key does to the prefix state machine.
type PrefixAction int

// Prefix actions.
const (
	PrefixNone         PrefixAction = iota // not a prefix key: route the key on
	PrefixArmed                            // ctrl+] seen; wait for the command
	PrefixCancelled                        // esc or an unknown key: nothing happens
	PrefixDoLiteral                        // send 0x1d to the focused session
	PrefixDoDisconnect                     // q
	PrefixDoHelp                           // ?
	PrefixDoHosts                          // h or 0
	PrefixDoTab                            // 1-9: Tab holds the number
	PrefixDoNext                           // n
	PrefixDoPrev                           // p
	PrefixDoScrollback                     // [
)

// PrefixResult is one step of the state machine.
type PrefixResult struct {
	Action PrefixAction
	Tab    int // for PrefixDoTab
}

// PrefixStep advances the prefix state machine (design §8.5): armed is
// whether ctrl+] was the previous key. It returns the next state and
// what to do; PrefixNone means the key is not the machine's.
func PrefixStep(armed bool, k tea.KeyPressMsg) (bool, PrefixResult) {
	if !armed {
		if key.Matches(k, Prefix) {
			return true, PrefixResult{Action: PrefixArmed}
		}
		return false, PrefixResult{Action: PrefixNone}
	}
	switch {
	case key.Matches(k, PrefixLiteral):
		return false, PrefixResult{Action: PrefixDoLiteral}
	case key.Matches(k, PrefixDisconnect):
		return false, PrefixResult{Action: PrefixDoDisconnect}
	case key.Matches(k, PrefixHelp):
		return false, PrefixResult{Action: PrefixDoHelp}
	case key.Matches(k, PrefixHosts):
		return false, PrefixResult{Action: PrefixDoHosts}
	case key.Matches(k, PrefixTab):
		return false, PrefixResult{Action: PrefixDoTab, Tab: int(k.String()[0] - '0')}
	case key.Matches(k, PrefixNext):
		return false, PrefixResult{Action: PrefixDoNext}
	case key.Matches(k, PrefixPrev):
		return false, PrefixResult{Action: PrefixDoPrev}
	case key.Matches(k, PrefixScrollback):
		return false, PrefixResult{Action: PrefixDoScrollback}
	}
	return false, PrefixResult{Action: PrefixCancelled}
}
