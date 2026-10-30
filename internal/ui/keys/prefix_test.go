package keys

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func press(code rune, mod tea.KeyMod, text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod, Text: text}
}

var ctrlBracket = press(']', tea.ModCtrl, "")

func TestARCH_SESS_006_PrefixStateMachine(t *testing.T) {
	ch := func(r rune) tea.KeyPressMsg { return press(r, 0, string(r)) }
	for _, tc := range []struct {
		name   string
		armed  bool
		key    tea.KeyPressMsg
		next   bool
		action PrefixAction
		tab    int
	}{
		{"idle ctrl+]", false, ctrlBracket, true, PrefixArmed, 0},
		{"idle q", false, ch('q'), false, PrefixNone, 0},
		{"idle esc", false, press(tea.KeyEscape, 0, ""), false, PrefixNone, 0},
		{"q", true, ch('q'), false, PrefixDoDisconnect, 0},
		{"?", true, ch('?'), false, PrefixDoHelp, 0},
		{"h", true, ch('h'), false, PrefixDoHosts, 0},
		{"0", true, ch('0'), false, PrefixDoHosts, 0},
		{"1", true, ch('1'), false, PrefixDoTab, 1},
		{"5", true, ch('5'), false, PrefixDoTab, 5},
		{"9", true, ch('9'), false, PrefixDoTab, 9},
		{"n", true, ch('n'), false, PrefixDoNext, 0},
		{"p", true, ch('p'), false, PrefixDoPrev, 0},
		{"[", true, ch('['), false, PrefixDoScrollback, 0},
		{"ctrl+] twice", true, ctrlBracket, false, PrefixDoLiteral, 0},
		{"esc", true, press(tea.KeyEscape, 0, ""), false, PrefixCancelled, 0},
		{"unknown x", true, ch('x'), false, PrefixCancelled, 0},
		{"unknown ctrl+c", true, press('c', tea.ModCtrl, ""), false, PrefixCancelled, 0},
		{"unknown enter", true, press(tea.KeyEnter, 0, ""), false, PrefixCancelled, 0},
	} {
		next, r := PrefixStep(tc.armed, tc.key)
		if next != tc.next || r.Action != tc.action || r.Tab != tc.tab {
			t.Errorf("%s: got (%v, %+v), want (%v, {%v %d})", tc.name, next, r, tc.next, tc.action, tc.tab)
		}
	}
}

func TestARCH_UI_004_PrefixBindingsHaveHelp(t *testing.T) {
	cmds := PrefixCommands()
	if len(cmds) == 0 {
		t.Fatal("no prefix commands")
	}
	for _, b := range append(cmds, Prefix, CloseHelp) {
		if h := b.Help(); h.Key == "" || h.Desc == "" || len(b.Keys()) == 0 {
			t.Errorf("prefix binding %v has no help or no key", b.Keys())
		}
	}
	// Every key the state machine acts on is listed in the help.
	listed := map[string]bool{}
	for _, b := range cmds {
		for _, k := range b.Keys() {
			listed[k] = true
		}
	}
	for _, k := range []string{"q", "?", "h", "0", "1", "9", "n", "p", "[", "ctrl+]", "esc"} {
		if !listed[k] {
			t.Errorf("prefix key %q is not in the help", k)
		}
	}
}
