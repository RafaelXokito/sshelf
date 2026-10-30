package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/help"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/keychanged"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/prompt"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/tofu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// clickText clicks the middle of the first occurrence of s in the view.
func (d *driver) clickText(t *testing.T, s string) {
	t.Helper()
	y, x := rowOf(t, d, s)
	d.clickAt(x+len([]rune(s))/2, y)
}

// clickModel sends a left press at the middle of the last occurrence of
// s in m's view (the top modal is drawn last), runs the command it
// returns once and delivers its message, as tofuModel and promptModel
// tests do with keys.
func clickModel(t *testing.T, m tea.Model, s string) (tea.Model, tea.Msg) {
	t.Helper()
	y, x := -1, 0
	for i, l := range strings.Split(plain(m.View().Content), "\n") {
		if j := strings.Index(l, s); j >= 0 {
			y, x = i, len([]rune(l[:j]))
		}
	}
	if y < 0 {
		t.Fatalf("%q is not in the view:\n%s", s, plain(m.View().Content))
	}
	m, cmd := m.Update(tea.MouseClickMsg{X: x + len([]rune(s))/2, Y: y, Button: tea.MouseLeft})
	if cmd == nil {
		return m, nil
	}
	msg := cmd()
	m, _ = m.Update(msg)
	return m, msg
}

// sized gives m the 80×24 terminal.
func sized(m tea.Model) tea.Model {
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// focusedLabel is the label the form's focus glyph is on.
func focusedLabel(t *testing.T, d *driver) string {
	t.Helper()
	for _, l := range strings.Split(plain(d.view()), "\n") {
		if rest, ok := strings.CutPrefix(l, components.FocusGlyph); ok {
			return strings.Fields(rest)[0]
		}
	}
	t.Fatalf("no focused field:\n%s", plain(d.view()))
	return ""
}

func TestFR_NAV_015_ClickFocusesField(t *testing.T) {
	t.Run("host form", func(t *testing.T) {
		d := newDriver(t, &fakeService{hosts: testHosts})
		d.resize(80, 24)
		d.press('e', "e") // db-staging
		if !d.inForm() {
			t.Fatal("no form")
		}
		for _, label := range []string{"Hostname", "Username", "Port"} {
			d.clickText(t, label)
			if got := focusedLabel(t, d); got != label {
				t.Fatalf("click on %q focused %q", label, got)
			}
		}
		// A click on the value, even at its start, focuses the field
		// with the cursor at the end.
		y, _ := rowOf(t, d, "Label")
		x := len([]rune(strings.Split(lineWith(plain(d.view()), "Label"), "db-staging")[0]))
		d.clickAt(x, y)
		if got := focusedLabel(t, d); got != "Label" {
			t.Fatalf("click on the label's value focused %q", got)
		}
		d.typeText("X")
		if !strings.Contains(lineWith(plain(d.view()), "Label"), "db-stagingX") {
			t.Fatalf("typed text not at the end:\n%s", plain(d.view()))
		}
		// The title, the blank line under the form and the help line
		// focus nothing.
		for _, s := range []string{"Edit host", "ctrl+s save"} {
			d.clickText(t, s)
			if got := focusedLabel(t, d); got != "Label" {
				t.Fatalf("click on %q moved focus to %q", s, got)
			}
		}
	})
	t.Run("identity form", func(t *testing.T) {
		d := newDriver(t, identityService())
		d.resize(80, 24)
		d.press('i', "i")
		d.press('a', "a")
		if !d.inIdentityForm() {
			t.Fatalf("no identity form:\n%s", plain(d.view()))
		}
		d.clickText(t, "Username")
		if got := focusedLabel(t, d); got != "Username" {
			t.Fatalf("focused %q", got)
		}
		d.typeText("bob")
		if !strings.Contains(lineWith(plain(d.view()), "Username"), "bob") {
			t.Fatalf("typing after the click:\n%s", plain(d.view()))
		}
	})
}

func TestFR_NAV_015_ClickChoiceArrows(t *testing.T) {
	d := newDriver(t, identityService())
	d.resize(80, 24)
	d.press('a', "a")
	// The first click focuses the choice; its arrows appear.
	d.clickText(t, "Identity")
	if got := focusedLabel(t, d); got != "Identity" {
		t.Fatalf("focused %q", got)
	}
	choice := func() string {
		return strings.TrimSpace(strings.SplitN(lineWith(plain(d.view()), "Identity"), "Identity", 2)[1])
	}
	if choice() != "‹ (none) ›" {
		t.Fatalf("choice shows %q", choice())
	}
	y, x := rowOf(t, d, "›")
	d.clickAt(x, y)
	if choice() != "‹ ops ›" {
		t.Fatalf("after ›: %q", choice())
	}
	// A click elsewhere on the choice only focuses it.
	d.clickText(t, "ops")
	if choice() != "‹ ops ›" {
		t.Fatalf("a click on the option changed it: %q", choice())
	}
	y, x = rowOf(t, d, "‹")
	d.clickAt(x, y)
	if choice() != "‹ (none) ›" {
		t.Fatalf("after ‹: %q", choice())
	}
	d.clickAt(x, y) // wraps
	if choice() != "‹ synced ›" {
		t.Fatalf("after ‹ again: %q", choice())
	}
}

func TestFR_NAV_015_ClickSaveValidates(t *testing.T) {
	svc := &fakeService{hosts: testHosts}
	d := newDriver(t, svc)
	d.resize(80, 24)
	d.press('a', "a")
	d.clickText(t, components.SaveButton)
	v := plain(d.view())
	if !d.inForm() || !strings.Contains(lineWith(v, "Label"), components.ErrorPrefix) || len(svc.saves) != 0 {
		t.Fatalf("[ Save ] on an empty form did not show errors:\n%s", v)
	}
	if got := focusedLabel(t, d); got != "Label" {
		t.Fatalf("focus on %q, want the first error", got)
	}
	d.typeText("mouse-host")
	d.clickText(t, "Hostname")
	d.typeText("10.0.0.99")
	d.clickText(t, components.SaveButton)
	if d.inForm() || len(svc.saves) != 1 || svc.saves[0].Label != "mouse-host" {
		t.Fatalf("[ Save ] did not save like ctrl+s: saves %+v\n%s", svc.saves, plain(d.view()))
	}
}

func TestFR_NAV_015_ClickCancelConfirmsDirty(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	d.resize(80, 24)
	// A clean form closes at once.
	d.press('e', "e")
	d.clickText(t, components.CancelButton)
	if d.inForm() || len(d.root().modals) != 0 {
		t.Fatalf("[ Cancel ] on a clean form:\n%s", plain(d.view()))
	}
	// A dirty form asks first, like esc; [n] No keeps it.
	d.press('e', "e")
	d.typeText("Z")
	d.clickText(t, components.CancelButton)
	if !d.inForm() || !strings.Contains(d.view(), "Discard unsaved changes?") {
		t.Fatalf("no confirmation:\n%s", plain(d.view()))
	}
	d.clickText(t, confirm.NoButton)
	if !d.inForm() || len(d.root().modals) != 0 {
		t.Fatalf("[n] No closed the form:\n%s", plain(d.view()))
	}
	d.clickText(t, components.CancelButton)
	d.clickText(t, confirm.YesButton)
	if d.inForm() {
		t.Fatalf("[y] Yes kept the form:\n%s", plain(d.view()))
	}
}

func TestFR_NAV_015_ReadOnlyFieldNotChanged(t *testing.T) {
	svc := identityService()
	svc.storeErr = noSecretService
	d := newDriver(t, svc)
	d.resize(80, 24)
	d.press('a', "a")
	before := lineWith(plain(d.view()), "Password")
	if !strings.Contains(before, "(disabled: no Secret Service)") {
		t.Fatalf("Password is not read-only:\n%s", plain(d.view()))
	}
	d.clickText(t, "disabled: no Secret Service")
	if got := focusedLabel(t, d); got != "Password" {
		t.Fatalf("click focused %q", got)
	}
	d.typeText("secret")
	d.send(tea.PasteMsg{Content: "pasted"})
	after := lineWith(plain(d.view()), "Password")
	if !strings.Contains(after, "(disabled: no Secret Service)") || strings.Contains(after, "secret") || strings.Contains(after, "pasted") || strings.Contains(after, "•") {
		t.Fatalf("read-only field took input: %q", after)
	}
}

func TestFR_NAV_016_ClickButtons(t *testing.T) {
	t.Run("delete confirmation", func(t *testing.T) {
		svc := &fakeService{hosts: testHosts}
		d := newDriver(t, svc)
		d.resize(80, 24)
		d.press('d', "d")
		d.clickText(t, confirm.NoButton)
		if len(d.root().modals) != 0 || len(d.list().Hosts()) != 3 {
			t.Fatal("[n] No did not just close the confirmation")
		}
		d.press('d', "d")
		d.clickText(t, confirm.YesButton)
		if len(d.root().modals) != 0 || len(d.list().Hosts()) != 2 {
			t.Fatalf("[y] Yes did not delete:\n%s", plain(d.view()))
		}
	})
	t.Run("connecting", func(t *testing.T) {
		svc := &blockingService{fakeService: fakeService{hosts: testHosts}, release: make(chan struct{})}
		defer close(svc.release)
		m := sized(New(svc))
		m, _ = m.Update(LoadedMsg{Hosts: testHosts})
		m, _ = m.Update(hostlist.ConnectRequestMsg{HostID: "h2", Label: "web-prod"}) // the connect cmd is not run
		m, msg := clickModel(t, m, connecting.CancelButton)
		if _, ok := msg.(connecting.CancelMsg); !ok || len(m.(Model).modals) != 0 {
			t.Fatalf("[esc] Cancel produced %#v", msg)
		}
		if !strings.Contains(m.View().Content, "connection cancelled") {
			t.Fatalf("view:\n%s", plain(m.View().Content))
		}
	})
	t.Run("host key prompt", func(t *testing.T) {
		for _, tc := range []struct {
			button string
			trust  bool
		}{{tofu.TrustButton, true}, {tofu.CancelButton, false}} {
			m, reply := tofuModel(t)
			m, msg := clickModel(t, sized(m), tc.button)
			if done, ok := msg.(tofu.DoneMsg); !ok || done.Trusted != tc.trust {
				t.Fatalf("%s produced %#v", tc.button, msg)
			}
			if got := <-reply; got != tc.trust {
				t.Fatalf("%s answered %v", tc.button, got)
			}
			if n := len(m.(Model).modals); n != 1 {
				t.Fatalf("%s: %d modals, want Connecting only", tc.button, n)
			}
		}
	})
	t.Run("authentication prompt", func(t *testing.T) {
		twoQuestions := app.AuthPromptMsg{Kind: app.PromptKeyboardInteractive, Questions: []app.Question{{Text: "Code:", Echo: true}, {Text: "PIN:"}}}
		m, reply := promptModel(t, twoQuestions)
		m = sized(m)
		// A click on a question's line focuses its field, as in a form.
		m, _ = clickModel(t, m, "PIN:")
		m = typeText(m, "77")
		m, _ = clickModel(t, m, "Code:")
		m = typeText(m, "123")
		m, msg := clickModel(t, m, prompt.SubmitButton)
		if _, ok := msg.(prompt.DoneMsg); !ok {
			t.Fatalf("[enter] Submit produced %#v", msg)
		}
		if r := <-reply; r.Cancelled || strings.Join(r.Answers, ",") != "123,77" {
			t.Fatalf("answers %+v", r)
		}
		if n := len(m.(Model).modals); n != 1 {
			t.Fatalf("%d modals after submit", n)
		}

		m, reply = promptModel(t, passwordPrompt)
		m, msg = clickModel(t, sized(m), prompt.CancelButton)
		if done, ok := msg.(prompt.DoneMsg); !ok || !done.Cancelled || len(m.(Model).modals) != 1 {
			t.Fatalf("[esc] Cancel produced %#v", msg)
		}
		if r := <-reply; !r.Cancelled {
			t.Fatalf("reply %+v", r)
		}
	})
	t.Run("changed host key", func(t *testing.T) {
		d := newDriver(t, &fakeService{hosts: testHosts, connectErr: mismatchErr()})
		d.resize(80, 24)
		connectSecond(d)
		if _, ok := d.root().modals[0].(keychanged.Model); !ok {
			t.Fatal("no block screen")
		}
		d.clickText(t, keychanged.CloseButton)
		if len(d.root().modals) != 0 {
			t.Fatal("[esc] Close did not close the block screen")
		}
	})
	t.Run("prefix help", func(t *testing.T) {
		d, _, _ := twoSessions(t)
		d.prefix("?")
		if _, ok := d.root().modals[0].(help.Model); !ok {
			t.Fatal("no help overlay")
		}
		if got := d.m.View().MouseMode; got != tea.MouseModeCellMotion {
			t.Fatalf("mode %v over a session with a modal", got)
		}
		d.clickText(t, help.CloseButton)
		if len(d.root().modals) != 0 {
			t.Fatal("[esc] Close did not close the overlay")
		}
	})
	t.Run("disconnect confirmation", func(t *testing.T) {
		d, _, b := twoSessions(t)
		d.prefix("q")
		d.clickText(t, confirm.NoButton)
		if len(d.root().modals) != 0 || len(d.root().tabs) != 2 {
			t.Fatal("[n] No did not keep the tab")
		}
		d.prefix("q")
		d.clickText(t, confirm.YesButton)
		if len(d.root().tabs) != 1 || b.Closes() != 1 {
			t.Fatalf("[y] Yes: %d tabs, %d closes", len(d.root().tabs), b.Closes())
		}
	})
	t.Run("quit confirmation", func(t *testing.T) {
		d, a, b := twoSessions(t)
		d.focus(0)
		d.press('q', "q")
		d.clickText(t, confirm.NoButton)
		if d.quit || len(d.root().modals) != 0 {
			t.Fatal("[n] No quit")
		}
		d.press('q', "q")
		d.clickText(t, confirm.YesButton)
		if !d.quit || a.Closes()+b.Closes() != 2 {
			t.Fatalf("[y] Yes: quit %v, closes %d", d.quit, a.Closes()+b.Closes())
		}
	})
}

func TestFR_NAV_016_ClickOutsideIgnored(t *testing.T) {
	svc := &fakeService{hosts: testHosts}
	d := newDriver(t, svc)
	d.resize(80, 24)
	d.press('d', "d")
	// The screen below, the header, the footer and the cells right of
	// the box on the button row do nothing.
	zy, zx := rowOf(t, d, "zeta")
	by, bx := rowOf(t, d, confirm.NoButton)
	for _, c := range [][2]int{{zx, zy}, {5, 0}, {5, 23}, {bx + 30, by}, {bx - 2, by}} {
		d.clickAt(c[0], c[1])
		d.doubleClick(c[0], c[1])
		if len(d.root().modals) != 1 || selected(t, d).ID != "h1" || len(d.list().Hosts()) != 3 {
			t.Fatalf("a click at %v acted:\n%s", c, plain(d.view()))
		}
	}
	// The wheel does nothing either.
	d.wheelAt(zy, tea.MouseWheelDown)
	if selected(t, d).ID != "h1" {
		t.Fatal("the wheel moved the list under the modal")
	}

	// Only the top modal takes clicks: Connecting's Cancel under the TOFU
	// modal does nothing.
	m, reply := tofuModel(t)
	m = sized(m)
	m, msg := clickModel(t, m, connecting.CancelButton)
	if msg != nil || len(m.(Model).modals) != 2 || len(reply) != 0 {
		t.Fatalf("a click on the modal below acted: %#v", msg)
	}
}

func TestFR_NAV_016_TofuTrustsOnlyOnButton(t *testing.T) {
	m, reply := tofuModel(t)
	m = sized(m)
	view := plain(m.View().Content)
	ty, tx := textAt(t, view, tofu.TrustButton)
	cy, cx := textAt(t, view, tofu.CancelButton)
	onButton := func(x, y int) bool {
		return y == ty && x >= tx && x < tx+len(tofu.TrustButton) || y == cy && x >= cx && x < cx+len(tofu.CancelButton)
	}
	// Every other cell of the terminal, single or double clicked, leaves
	// the prompt unanswered.
	for y := range 24 {
		for x := range 80 {
			if onButton(x, y) {
				continue
			}
			var cmd tea.Cmd
			m, cmd = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if cmd != nil {
				if _, ok := cmd().(tofu.DoneMsg); ok {
					t.Fatalf("a click at %d,%d answered the prompt", x, y)
				}
			}
		}
	}
	if len(reply) != 0 || len(m.(Model).modals) != 2 {
		t.Fatal("the prompt was answered")
	}
	// Only a click on [y] Trust and save trusts.
	_, msg := clickModel(t, m, tofu.TrustButton)
	if done, ok := msg.(tofu.DoneMsg); !ok || !done.Trusted || !<-reply {
		t.Fatalf("[y] Trust and save produced %#v", msg)
	}
}

func TestFR_NAV_017_ClickTabFocuses(t *testing.T) {
	d, _, _ := twoSessions(t)
	d.focus(0)
	d.clickText(t, "2 web-prod")
	if d.root().focus != 2 {
		t.Fatalf("focus %d after clicking 2 web-prod, want 2", d.root().focus)
	}
	// On a session tab tracking is off; any header click that still
	// arrives is ignored (D-012).
	d.clickText(t, "1 db-staging")
	if d.root().focus != 2 {
		t.Fatal("a header click switched tabs while a session was focused")
	}
	d.focus(0)
	d.clickText(t, "1 db-staging")
	if d.root().focus != 1 {
		t.Fatalf("focus %d, want 1", d.root().focus)
	}
	d.focus(0)
	d.clickText(t, "Hosts")
	if d.root().focus != 0 {
		t.Fatal("a click on Hosts left the Hosts tab")
	}
}

func TestFR_NAV_017_ClickSeparatorIgnored(t *testing.T) {
	d, _, _ := twoSessions(t)
	d.focus(0)
	header := headerLine(d)
	for x, r := range []rune(header) {
		if r == '│' || x < len("sshelf ") {
			d.clickAt(x, 0)
			if d.root().focus != 0 {
				t.Fatalf("a click at column %d (%q) of %q focused tab %d", x, r, header, d.root().focus)
			}
		}
	}
	// With a modal open, the tab bar does nothing.
	d.press('q', "q")
	d.clickText(t, "2 web-prod")
	if d.root().focus != 0 || len(d.root().modals) != 1 {
		t.Fatal("a tab click went through the quit confirmation")
	}
}
