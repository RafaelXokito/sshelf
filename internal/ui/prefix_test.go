package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/help"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

func (d *driver) ctrlBracket() { d.send(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl}) }

// prefix presses Ctrl+] and then the key typed as text.
func (d *driver) prefix(text string) {
	d.ctrlBracket()
	d.press([]rune(text)[0], text)
}

// statusBar is the plain last line of the view.
func statusBar(d *driver) string {
	lines := strings.Split(plain(d.view()), "\n")
	return lines[len(lines)-1]
}

// twoSessions connects db-staging (tab 1) and web-prod (tab 2, focused).
func twoSessions(t *testing.T) (*driver, *apptest.FakeSession, *apptest.FakeSession) {
	t.Helper()
	a, b := apptest.NewFakeSession("screen of A"), apptest.NewFakeSession("screen of B")
	d := framedDriver(t, testHosts, a, b)
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "h1")
	connectHost(t, d, "h2")
	return d, a, b
}

func TestARCH_SESS_006_PrefixSendsNoBytes(t *testing.T) {
	d, a, b := twoSessions(t)
	for _, k := range []string{"n", "p", "1", "2", "h", "2", "0", "2", "?"} {
		d.prefix(k)
		if k == "?" {
			d.press(tea.KeyEscape, "") // close the help overlay
		}
	}
	if a.Sent() || b.Sent() {
		t.Fatalf("a prefix command reached a session: A %v %q, B %v %q", a.Keys(), a.Bytes(), b.Keys(), b.Bytes())
	}
}

func TestFR_TAB_005_LiteralCtrlBracket(t *testing.T) {
	d, _, b := twoSessions(t)
	d.ctrlBracket()
	if got := statusBar(d); got != "Ctrl+] …" {
		t.Fatalf("status bar after Ctrl+] = %q", got)
	}
	d.ctrlBracket()
	if got := b.Bytes(); string(got) != "\x1d" || len(b.Keys()) != 0 {
		t.Fatalf("bytes = %q, keys = %v; want exactly 0x1d", got, b.Keys())
	}
	if d.root().prefix || strings.Contains(statusBar(d), "…") {
		t.Fatal("the prefix is still pending")
	}
}

func TestFR_TAB_005_UnknownKeyCancels(t *testing.T) {
	d, _, b := twoSessions(t)
	for _, k := range []tea.KeyPressMsg{{Code: 'x', Text: "x"}, {Code: tea.KeyEscape}, {Code: tea.KeyEnter}} {
		d.ctrlBracket()
		d.send(k)
		if b.Sent() || d.root().prefix {
			t.Fatalf("after Ctrl+] %v: sent = %v, prefix = %v", k, b.Keys(), d.root().prefix)
		}
		if got := statusBar(d); !strings.HasPrefix(got, "● connected") {
			t.Fatalf("status bar = %q after the prefix was cancelled", got)
		}
	}
	d.press('x', "x") // no timeout and no state left: the next x is typed
	if keys := b.Keys(); len(keys) != 1 || keys[0].Text != "x" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestFR_TAB_005_HelpOverlayGolden(t *testing.T) {
	d, _, _ := twoSessions(t)
	d.prefix("?")
	if _, ok := d.root().modals[0].(help.Model); !ok {
		t.Fatalf("modals = %v", d.root().modals)
	}
	golden(t, "frame-help", d.m)
	d.press('?', "?")
	if n := len(d.root().modals); n != 0 {
		t.Fatalf("%d modals after ?", n)
	}
	d.prefix("?")
	d.press(tea.KeyEscape, "")
	if n := len(d.root().modals); n != 0 {
		t.Fatalf("%d modals after esc", n)
	}
}

func TestFR_TAB_005_WorksInTextField(t *testing.T) {
	d, _, _ := twoSessions(t)
	d.focus(0)
	d.press('/', "/")
	d.press('w', "w")
	d.ctrlBracket()
	if got := strings.Split(plain(d.view()), "\n"); got[len(got)-2] != "Ctrl+] …" {
		t.Fatalf("status line = %q", got[len(got)-2])
	}
	d.press('1', "1")
	if d.root().focus != 1 {
		t.Fatalf("focus = %d after Ctrl+] 1 in the search field", d.root().focus)
	}
	d.prefix("h")
	l := d.root().screen.(hostlist.Model)
	if l.Query() != "w" || !l.Searching() {
		t.Fatalf("query = %q: a prefix key was typed into the field", l.Query())
	}
	// q and [ do nothing on the Hosts tab.
	d.prefix("q")
	d.prefix("[")
	if d.quit || len(d.root().modals) != 0 || d.root().focus != 0 || l.Query() != "w" {
		t.Fatalf("quit = %v, modals = %d, focus = %d", d.quit, len(d.root().modals), d.root().focus)
	}
}

func TestFR_TAB_001_NumberNextPrevHosts(t *testing.T) {
	d, _, _ := twoSessions(t) // tab 2 focused
	steps := []struct {
		key  string
		want int
	}{
		{"1", 1}, {"2", 2}, {"9", 2}, {"h", 0}, {"2", 2}, {"0", 0},
		{"n", 1}, {"n", 2}, {"n", 0}, // wraps after the last tab
		{"p", 2}, {"p", 1}, {"p", 0}, {"p", 2}, // and before the Hosts tab
	}
	for _, s := range steps {
		d.prefix(s.key)
		if d.root().focus != s.want {
			t.Fatalf("Ctrl+] %s: focus = %d, want %d", s.key, d.root().focus, s.want)
		}
	}
}

func TestFR_TAB_006_SshelfKeysGoToRemote(t *testing.T) {
	d, _, b := twoSessions(t)
	sent := []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
		{Code: 'z', Mod: tea.ModCtrl},
		{Code: '?', Text: "?"},
		{Code: '/', Text: "/"},
		{Code: tea.KeyEscape},
		{Code: 'x', Mod: tea.ModAlt},
		{Code: tea.KeyLeft, Mod: tea.ModCtrl},
		{Code: tea.KeyUp, Mod: tea.ModShift},
		{Code: tea.KeyF5},
		{Code: 'é', Text: "é"},
	}
	for _, k := range sent {
		d.send(k)
	}
	got := b.Keys()
	if len(got) != len(sent) {
		t.Fatalf("%d keys reached the session, want %d: %v", len(got), len(sent), got)
	}
	for i, k := range sent {
		if want := toKeyEvent(k); got[i] != want {
			t.Errorf("key %d = %+v, want %+v", i, got[i], want)
		}
	}
	if d.quit || len(d.root().modals) != 0 || d.root().focus != 2 {
		t.Fatalf("sshelf acted on a key: quit = %v, modals = %d, focus = %d", d.quit, len(d.root().modals), d.root().focus)
	}
	if want := (uv.KeyPressEvent{Code: 'c', Mod: uv.ModCtrl}); got[1] != want {
		t.Fatalf("ctrl+c = %+v", got[1])
	}
}

func TestFR_TAB_006_PasteForwarded(t *testing.T) {
	d, a, b := twoSessions(t)
	text := "echo 'a\tb'\nq ? \x1b[A ~."
	d.send(tea.PasteMsg{Content: text})
	if got := b.Pastes(); len(got) != 1 || got[0] != text {
		t.Fatalf("pastes = %q", got)
	}
	if a.Sent() || len(b.Keys()) != 0 {
		t.Fatal("the paste went elsewhere too")
	}
}

func TestFR_CONN_014_ConfirmDefaultsNo(t *testing.T) {
	d, a, b := twoSessions(t)
	for _, no := range []tea.KeyPressMsg{{Code: 'n', Text: "n"}, {Code: tea.KeyEnter}, {Code: tea.KeyEscape}} {
		d.prefix("q")
		if v := d.view(); !strings.Contains(v, "Disconnect web-prod?") {
			t.Fatalf("no confirmation:\n%s", v)
		}
		d.send(no)
		if len(d.root().tabs) != 2 || b.Closes() != 0 || len(d.root().modals) != 0 {
			t.Fatalf("after %v: tabs = %d, closes = %d", no, len(d.root().tabs), b.Closes())
		}
	}
	if b.Sent() {
		t.Fatalf("the answers reached the session: %v", b.Keys())
	}

	d.prefix("q")
	d.press('y', "y")
	if len(d.root().tabs) != 1 || b.Closes() != 1 || a.Closes() != 0 {
		t.Fatalf("after y: tabs = %d, closes A %d B %d", len(d.root().tabs), a.Closes(), b.Closes())
	}
	if d.root().focus != 1 {
		t.Fatalf("focus = %d, want the tab to the left", d.root().focus)
	}
}

func TestFR_CONN_014_TildeDotIsText(t *testing.T) {
	d, _, b := twoSessions(t)
	d.press(tea.KeyEnter, "")
	d.press('~', "~")
	d.press('.', ".")
	got := b.Keys()
	if len(got) != 3 || got[1].Text != "~" || got[2].Text != "." {
		t.Fatalf("keys = %v", got)
	}
	if len(d.root().tabs) != 2 || b.Closes() != 0 {
		t.Fatal("~. ended the session")
	}
}
