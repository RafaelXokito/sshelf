package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/session"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// endTab ends fake session s of tab num and delivers the end.
func endTab(d *driver, num int, s *apptest.FakeSession, err error) {
	d.t.Helper()
	s.End(err)
	d.pump(num)
}

func TestFR_CONN_005_M2_EndedTabGolden(t *testing.T) {
	s := apptest.NewFakeSession(shellScreen...)
	d := framedDriver(t, testHosts, s)
	connectHost(t, d, "h2")
	endTab(d, 1, s, &session.DisconnectError{Err: errors.New("server shutting down")})
	golden(t, "ended-tab", d.m)
	if got := statusBar(d); got != "✕ connection lost: server shutting down · r reconnect · x close" {
		t.Fatalf("status bar = %q", got)
	}

	s2 := apptest.NewFakeSession(shellScreen...)
	d = framedDriver(t, testHosts, s2)
	connectHost(t, d, "h2")
	endTab(d, 1, s2, &session.ExitError{Status: 3})
	if got := statusBar(d); got != "✕ exited (3) · r reconnect · x close" {
		t.Fatalf("status bar = %q", got)
	}
	if v := plain(d.view()); !strings.Contains(v, "ubuntu@web-prod:~$ uptime") {
		t.Fatalf("the last remote screen is gone:\n%s", v)
	}
}

func TestFR_CONN_005_M2_BackgroundEndKeepsFocus(t *testing.T) {
	d, a, _ := twoSessions(t) // tab 2 focused
	endTab(d, 1, a, nil)
	if d.root().focus != 2 {
		t.Fatalf("focus = %d after a background session ended", d.root().focus)
	}
	if h := headerLine(d); !strings.Contains(h, "1 db-staging ✕") {
		t.Fatalf("header = %q", h)
	}
	d.focus(0)
	endTab(d, 2, d.root().tabs[1].sess.(framedStub).FakeSession, nil)
	if d.root().focus != 0 {
		t.Fatalf("focus = %d: the Hosts tab lost the focus", d.root().focus)
	}
}

func TestFR_CONN_005_M2_CloseSelectsHost(t *testing.T) {
	s := apptest.NewFakeSession()
	d := framedDriver(t, testHosts, s)
	connectHost(t, d, "h1")
	d.focus(0)
	d.press('G', "G") // select zeta on the host list
	d.focus(1)
	endTab(d, 1, s, nil)
	d.press('x', "x")
	if len(d.root().tabs) != 0 || d.root().focus != 0 {
		t.Fatalf("tabs = %d, focus = %d after x", len(d.root().tabs), d.root().focus)
	}
	if h := selected(t, d); h.ID != "h1" {
		t.Fatalf("selected %s after closing the tab, want h1", h.ID)
	}
	if s.Closes() != 1 {
		t.Fatalf("Close calls = %d", s.Closes())
	}

	// With tabs on both sides, x focuses the tab to the left.
	ss := []*apptest.FakeSession{apptest.NewFakeSession(), apptest.NewFakeSession(), apptest.NewFakeSession()}
	d = framedDriver(t, testHosts, ss...)
	for _, id := range []domain.ID{"h1", "h2", "h3"} {
		connectHost(t, d, id)
	}
	d.prefix("2")
	endTab(d, 2, ss[1], nil)
	d.press('x', "x")
	if d.root().focus != 1 || d.root().tabs[0].num != 1 || d.root().tabs[1].num != 3 {
		t.Fatalf("focus = %d, tabs = %d after closing tab 2", d.root().focus, len(d.root().tabs))
	}
}

func TestFR_CONN_005_M2_OnlyRXAndPrefix(t *testing.T) {
	d, _, b := twoSessions(t)
	endTab(d, 2, b, nil)
	for _, k := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}, {Code: 'a', Text: "a"}, {Code: tea.KeyEnter}, {Code: tea.KeyEscape}} {
		d.send(k)
	}
	d.send(tea.PasteMsg{Content: "pasted"})
	if b.Sent() || d.quit || len(d.root().tabs) != 2 || len(d.root().modals) != 0 || len(d.svc.connected) != 2 {
		t.Fatalf("an ended tab acted on a key: sent %v, quit %v, tabs %d, connects %d", b.Keys(), d.quit, len(d.root().tabs), len(d.svc.connected))
	}
	d.prefix("[") // scrollback mode still works
	if !strings.HasPrefix(statusBar(d), "scrollback ") {
		t.Fatalf("status bar = %q", statusBar(d))
	}
	d.press(tea.KeyEscape, "")
	d.prefix("h")
	if d.root().focus != 0 {
		t.Fatalf("focus = %d after Ctrl+] h", d.root().focus)
	}
}

func TestFR_CONN_010_ReconnectSameTab(t *testing.T) {
	old, other, fresh := apptest.NewFakeSession("old screen"), apptest.NewFakeSession(), apptest.NewFakeSession("new screen")
	d := framedDriver(t, testHosts, old, other, fresh)
	connectHost(t, d, "h2")
	connectHost(t, d, "h1")
	d.prefix("1")
	endTab(d, 1, old, &session.ExitError{Status: 0})
	d.clock = d.clock.Add(10 * time.Minute)
	d.press('r', "r")

	if got := d.svc.connected; len(got) != 3 || got[2] != "h2" {
		t.Fatalf("connects = %v", got)
	}
	r := d.root()
	if len(r.tabs) != 2 || r.tabs[0].num != 1 || r.focus != 1 {
		t.Fatalf("tabs = %d, first = %d, focus = %d", len(r.tabs), r.tabs[0].num, r.focus)
	}
	if r.tabs[0].sess.(framedStub).FakeSession != fresh || old.Closes() != 1 {
		t.Fatal("the tab does not run the new session, or the old one is open")
	}
	v := plain(d.view())
	if !strings.Contains(v, "new screen") || strings.Contains(v, "old screen") || strings.Contains(v, "✕") {
		t.Fatalf("view after reconnecting:\n%s", v)
	}
	if got := statusBar(d); !strings.HasPrefix(got, "● connected · ubuntu@10.0.0.10:22 · 00:00:00") {
		t.Fatalf("status bar = %q", got)
	}
	fresh.Output("typed")
	d.pump(1)
	if v := d.view(); !strings.Contains(v, "typed") {
		t.Fatalf("the new session's updates are not listened to:\n%s", v)
	}
}

func TestFR_CONN_010_FailureKeepsEndedTab(t *testing.T) {
	s := apptest.NewFakeSession("last screen")
	svc := &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{s}}
	d := newDriver(t, svc)
	connectHost(t, d, "h2")
	endTab(d, 1, s, &session.ExitError{Status: 3})

	svc.connectErr = app.ErrAuthFailed
	d.press('r', "r")
	if len(d.root().tabs) != 1 || !d.root().tabs[0].done || d.root().focus != 1 {
		t.Fatal("a failed reconnect changed the tab")
	}
	if got := statusBar(d); got != "✕ web-prod: authentication failed · r reconnect · x close" {
		t.Fatalf("status bar = %q", got)
	}
	if v := plain(d.view()); !strings.Contains(v, "last screen") {
		t.Fatalf("the ended screen is gone:\n%s", v)
	}

	// A reconnect cancelled with Esc: the Connecting modal is over the tab,
	// and the tab stays ended.
	svc.connectErr = nil
	m, cmd := d.root().Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	req := cmd().(hostlist.ConnectRequestMsg)
	m, _ = m.Update(req) // the connect command is not run
	if v := plain(m.View().Content); !strings.Contains(v, "Connecting to web-prod") || !strings.HasPrefix(v, "sshelf │ Hosts │[1 web-prod ✕]│") {
		t.Fatalf("Connecting not over the tab:\n%s", v)
	}
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m, _ = m.Update(cmd().(connecting.CancelMsg))
	d.m = m
	if got := statusBar(d); got != "✕ connection cancelled · r reconnect · x close" {
		t.Fatalf("status bar = %q", got)
	}
	if !d.root().tabs[0].done || len(d.root().modals) != 0 {
		t.Fatal("the tab is not ended after the cancel")
	}
	d.press('x', "x")
	if len(d.root().tabs) != 0 {
		t.Fatal("x did not close the tab")
	}
}

func TestARCH_SESS_008_RReconnectRequest(t *testing.T) {
	d, a, _ := twoSessions(t)
	d.prefix("1")
	endTab(d, 1, a, &session.DisconnectError{})
	_, cmd := d.root().Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("r produced no command")
	}
	want := hostlist.ConnectRequestMsg{HostID: "h1", Label: "db-staging", Tab: 1}
	if got := cmd(); got != want {
		t.Fatalf("r produced %#v, want %#v", got, want)
	}
}

func TestFR_TAB_009_ConfirmWithLiveSessions(t *testing.T) {
	d, a, b := twoSessions(t)
	d.focus(0)
	for _, quit := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		for _, no := range []tea.KeyPressMsg{{Code: 'n', Text: "n"}, {Code: tea.KeyEnter}, {Code: tea.KeyEscape}} {
			d.send(quit)
			if v := d.view(); !strings.Contains(v, "Quit and close 2 sessions?") {
				t.Fatalf("no confirmation after %v:\n%s", quit, v)
			}
			d.send(quit) // asked once only
			if n := len(d.root().modals); n != 1 {
				t.Fatalf("%d modals", n)
			}
			d.send(no)
			if d.quit || len(d.root().modals) != 0 || a.Closes()+b.Closes() != 0 {
				t.Fatalf("after %v: quit = %v, closes = %d", no, d.quit, a.Closes()+b.Closes())
			}
		}
	}

	// Ended tabs do not count.
	endTab(d, 1, a, nil)
	d.press('q', "q")
	if v := d.view(); !strings.Contains(v, "Quit and close 1 session?") {
		t.Fatalf("view:\n%s", v)
	}
	d.press('y', "y")
	if !d.quit || b.Closes() != 1 {
		t.Fatalf("quit = %v, closes = %d after y", d.quit, b.Closes())
	}
}

func TestFR_TAB_009_NoConfirmWhenAllEnded(t *testing.T) {
	d, a, b := twoSessions(t)
	endTab(d, 1, a, nil)
	endTab(d, 2, b, nil)
	d.focus(0)
	d.press('q', "q")
	if !d.quit {
		t.Fatalf("q did not quit at once:\n%s", d.view())
	}
}

// scrollSession is a session with 100 lines of scrollback above a
// 3-line screen.
func scrollSession() *apptest.FakeSession {
	s := apptest.NewFakeSession("screen 1", "screen 2", "prompt$ ")
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d", i)
	}
	s.SetScrollback(lines)
	return s
}

func TestFR_TAB_010_ScrollbackKeys(t *testing.T) {
	s := scrollSession()
	s.SetSnapshot(app.Snapshot{Lines: []string{"screen 1", "screen 2", "prompt$ "}, Cursor: &app.Cursor{X: 8, Y: 2}, Seq: 1})
	d := framedDriver(t, testHosts, s)
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24}) // 22 rows
	connectHost(t, d, "h2")
	d.prefix("[")
	if d.m.View().Cursor != nil {
		t.Fatal("the remote cursor is shown in scrollback mode")
	}
	steps := []struct {
		key    tea.KeyPressMsg
		status string
		first  string
	}{
		{tea.KeyPressMsg{Code: tea.KeyUp}, "scrollback 102/103", "line 080"},
		{tea.KeyPressMsg{Code: tea.KeyPgUp}, "scrollback 80/103", "line 058"},
		{tea.KeyPressMsg{Code: 'g', Text: "g"}, "scrollback 22/103", "line 000"},
		{tea.KeyPressMsg{Code: tea.KeyUp}, "scrollback 22/103", "line 000"},
		{tea.KeyPressMsg{Code: tea.KeyDown}, "scrollback 23/103", "line 001"},
		{tea.KeyPressMsg{Code: tea.KeyPgDown}, "scrollback 45/103", "line 023"},
		{tea.KeyPressMsg{Code: 'G', Text: "G"}, "scrollback 103/103", "line 081"},
		{tea.KeyPressMsg{Code: tea.KeyDown}, "scrollback 103/103", "line 081"},
		{tea.KeyPressMsg{Code: 'q', Text: "q"}, "scrollback 103/103", "line 081"},
	}
	for _, st := range steps {
		d.send(st.key)
		lines := strings.Split(plain(d.view()), "\n")
		if want := st.status + " · ↑↓ PgUp PgDn g G · esc back"; lines[len(lines)-1] != want {
			t.Fatalf("after %v: status bar = %q, want %q", st.key, lines[len(lines)-1], want)
		}
		if lines[1] != st.first {
			t.Fatalf("after %v: first line = %q, want %q", st.key, lines[1], st.first)
		}
	}
	if s.Sent() {
		t.Fatalf("scrollback keys reached the remote: %v", s.Keys())
	}
	d.press(tea.KeyEscape, "")
	if c := d.m.View().Cursor; c == nil || c.Y != 3 {
		t.Fatalf("cursor after esc = %+v", c)
	}
	if lines := strings.Split(plain(d.view()), "\n"); lines[1] != "screen 1" {
		t.Fatalf("first line after esc = %q", lines[1])
	}
}

func TestFR_TAB_010_AnchoredWhileOutputArrives(t *testing.T) {
	s := scrollSession()
	d := framedDriver(t, testHosts, s)
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "h2")
	d.prefix("[")
	d.press(tea.KeyPgUp, "")
	before := plain(d.view())

	more := make([]string, 150)
	for i := range more {
		more[i] = fmt.Sprintf("line %03d", i)
	}
	s.SetScrollback(more)
	s.Output("new 1", "new 2", "prompt$ ")
	d.pump(1)
	after := strings.Split(plain(d.view()), "\n")
	if want := strings.Split(before, "\n"); strings.Join(after[1:23], "\n") != strings.Join(want[1:23], "\n") {
		t.Fatalf("the view moved while output arrived:\n%s\n--- before ---\n%s", strings.Join(after, "\n"), before)
	}
	if got := after[len(after)-1]; !strings.HasPrefix(got, "scrollback 81/153") {
		t.Fatalf("status bar = %q", got)
	}
}

func TestFR_TAB_010_Golden(t *testing.T) {
	d := framedDriver(t, testHosts, scrollSession())
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "h2")
	d.prefix("[")
	d.press(tea.KeyPgUp, "") // 22 rows up: lines 59 to 80
	d.press(tea.KeyPgUp, "") // lines 37 to 58
	golden(t, "scrollback", d.m)
}
