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
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
	"github.com/RafaelXokito/sshelf/internal/ui/tabs"
)

// shellScreen is a canned remote screen: a prompt after a command.
var shellScreen = []string{
	"ubuntu@web-prod:~$ uptime",
	" 09:00:00 up 12 days,  3:04,  1 user,  load average: 0.08, 0.03, 0.01",
	"ubuntu@web-prod:~$ ",
}

// framedDriver is a driver whose next connections return sessions.
func framedDriver(t *testing.T, hosts []domain.Host, sessions ...*apptest.FakeSession) *driver {
	t.Helper()
	return newDriver(t, &fakeService{hosts: hosts, framed: sessions})
}

// connectHost focuses the Hosts tab, selects the host with id and presses
// Enter.
func connectHost(t *testing.T, d *driver, id domain.ID) {
	t.Helper()
	d.focus(0)
	l, ok := d.root().screen.(hostlist.Model)
	if !ok {
		t.Fatalf("the Hosts tab shows %T, not the host list", d.root().screen)
	}
	m := d.root()
	m.screen = l.Select(id)
	d.m = m
	d.press(tea.KeyEnter, "")
}

// headerLine is the plain first line of the view.
func headerLine(d *driver) string {
	return strings.Split(plain(d.view()), "\n")[0]
}

func TestARCH_UI_005_FrameGoldens(t *testing.T) {
	t.Run("hosts", func(t *testing.T) {
		d := framedDriver(t, testHosts)
		golden(t, "frame-hosts", d.m)
	})

	t.Run("hosts with 3 tabs", func(t *testing.T) {
		d := framedDriver(t, testHosts, apptest.NewFakeSession(shellScreen...), apptest.NewFakeSession(), apptest.NewFakeSession())
		connectHost(t, d, "h2")
		connectHost(t, d, "h1")
		connectHost(t, d, "h2")
		d.focus(0)
		golden(t, "frame-hosts-tabs", d.m)
	})

	t.Run("live session", func(t *testing.T) {
		s := apptest.NewFakeSession(shellScreen...)
		d := framedDriver(t, testHosts, s)
		connectHost(t, d, "h2")
		d.clock = d.clock.Add(time.Hour + 2*time.Minute + 3*time.Second)
		golden(t, "frame-live", d.m)
	})

	t.Run("modal over a session", func(t *testing.T) {
		d := framedDriver(t, testHosts, apptest.NewFakeSession(shellScreen...))
		connectHost(t, d, "h2")
		d.send(components.ShowModalMsg{Modal: confirm.New("Disconnect web-prod?", nil)})
		golden(t, "frame-modal", d.m)
	})

	t.Run("ended session", func(t *testing.T) {
		s := apptest.NewFakeSession(shellScreen...)
		d := framedDriver(t, testHosts, s)
		connectHost(t, d, "h2")
		s.End(&session.ExitError{Status: 3})
		d.pump(1)
		golden(t, "frame-ended", d.m)
	})

	t.Run("10 tabs", func(t *testing.T) {
		hosts := make([]domain.Host, 10)
		sessions := make([]*apptest.FakeSession, 10)
		for i := range hosts {
			hosts[i] = domain.Host{Meta: domain.Meta{ID: domain.ID(fmt.Sprint("t", i))}, Label: fmt.Sprintf("server-%06d", i), Hostname: "10.0.0.1"}
			sessions[i] = apptest.NewFakeSession()
		}
		d := framedDriver(t, hosts, sessions...)
		for _, h := range hosts {
			connectHost(t, d, h.ID)
		}
		d.focus(5)
		golden(t, "frame-overflow", d.m)
	})
}

func TestARCH_UI_005_CursorShifted(t *testing.T) {
	s := apptest.NewFakeSession(shellScreen...)
	s.SetSnapshot(app.Snapshot{Lines: shellScreen, Cursor: &app.Cursor{X: 19, Y: 2}, Seq: 1})
	d := framedDriver(t, testHosts, s)
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "h2")
	c := d.m.View().Cursor
	if c == nil || c.X != 19 || c.Y != 3 {
		t.Fatalf("cursor = %+v, want (19, 3): the remote cursor one row down", c)
	}

	s.SetSnapshot(app.Snapshot{Lines: shellScreen, Seq: 2}) // hidden by the remote
	if c := d.m.View().Cursor; c != nil {
		t.Fatalf("cursor = %+v with the remote cursor hidden", c)
	}

	s.SetSnapshot(app.Snapshot{Lines: shellScreen, Cursor: &app.Cursor{X: 19, Y: 2}, Seq: 3})
	d.send(components.ShowModalMsg{Modal: confirm.New("Disconnect web-prod?", nil)})
	if c := d.m.View().Cursor; c != nil {
		t.Fatalf("cursor = %+v with a modal open", c)
	}

	d.focus(0)
	if c := d.m.View().Cursor; c != nil {
		t.Fatalf("cursor = %+v on the Hosts tab", c)
	}
}

func TestFR_TAB_002_SameHostTwice(t *testing.T) {
	svc := &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{apptest.NewFakeSession(), apptest.NewFakeSession()}}
	d := newDriver(t, svc)
	connectHost(t, d, "h2")
	if d.root().focus != 1 {
		t.Fatalf("focus = %d after the first connect, want tab 1", d.root().focus)
	}
	connectHost(t, d, "h2")
	if h := headerLine(d); !strings.Contains(h, "│ 1 web-prod │[2 web-prod]│") {
		t.Fatalf("header = %q", h)
	}
	if len(svc.connected) != 2 || d.execs != 0 {
		t.Fatalf("connects = %v, execs = %d", svc.connected, d.execs)
	}
	if a, b := d.root().tabs[0].sess, d.root().tabs[1].sess; a == b {
		t.Fatal("both tabs share one session")
	}
}

func TestFR_TAB_002_NumbersNotReused(t *testing.T) {
	d := framedDriver(t, testHosts, apptest.NewFakeSession(), apptest.NewFakeSession(), apptest.NewFakeSession(), apptest.NewFakeSession())
	for _, id := range []domain.ID{"h1", "h2", "h3"} {
		connectHost(t, d, id)
	}
	first := d.root().tabs[0].sess.(framedStub)
	m, cmd := d.root().closeTab(1)
	d.m = m
	d.run(cmd)
	if first.Closes() != 1 {
		t.Fatalf("closed tab's session Close calls = %d", first.Closes())
	}
	connectHost(t, d, "h1")
	var nums []int
	for _, tab := range d.root().tabs {
		nums = append(nums, tab.num)
	}
	if fmt.Sprint(nums) != "[2 3 4]" {
		t.Fatalf("tab numbers = %v, want [2 3 4]", nums)
	}
	if h := headerLine(d); !strings.Contains(h, "[4 db-staging]") {
		t.Fatalf("header = %q", h)
	}
}

func TestFR_TAB_002_FailedConnectOpensNoTab(t *testing.T) {
	svc := &fakeService{hosts: testHosts, connectErr: app.ErrAuthFailed, framed: []*apptest.FakeSession{apptest.NewFakeSession()}}
	d := newDriver(t, svc)
	connectSecond(d)
	if n := len(d.root().tabs); n != 0 || d.root().focus != 0 {
		t.Fatalf("tabs = %d, focus = %d after a failed connect", n, d.root().focus)
	}
	if h := headerLine(d); h != "sshelf │[Hosts]│" {
		t.Fatalf("header = %q", h)
	}
	if v := d.view(); !strings.Contains(v, "web-prod: authentication failed") {
		t.Fatalf("view lacks the error:\n%s", v)
	}
}

func TestARCH_SESS_002_M2_PromptsOverCurrentTab(t *testing.T) {
	d := framedDriver(t, testHosts, apptest.NewFakeSession(shellScreen...))
	connectHost(t, d, "h2")
	d.focus(0)

	// Start a second connection without running it, and let it ask for a
	// password: the prompt opens over the Hosts tab, and no tab opens.
	m, _ := d.root().Update(hostlist.ConnectRequestMsg{HostID: "h1", Label: "db-staging"})
	m, _ = m.Update(app.AuthPromptMsg{Kind: app.PromptPassword, Label: "db-staging", User: "admin", Host: "10.0.1.20", Questions: []app.Question{{Text: "Password:"}}})
	root := m.(Model)
	v := plain(m.View().Content)
	if root.focus != 0 || len(root.tabs) != 1 {
		t.Fatalf("focus = %d, tabs = %d while authenticating", root.focus, len(root.tabs))
	}
	if !strings.Contains(v, "Password for admin@10.0.1.20") || !strings.HasPrefix(v, "sshelf │[Hosts]│ 1 web-prod │") {
		t.Fatalf("prompt not over the Hosts tab:\n%s", v)
	}

	// Authentication fails: still no new tab, and the error is shown.
	m, _ = m.Update(ConnectFailedMsg{attempt: root.attempt, HostID: "h1", Label: "db-staging", Err: app.ErrAuthFailed})
	root = m.(Model)
	if len(root.tabs) != 1 || len(root.modals) != 0 {
		t.Fatalf("tabs = %d, modals = %d after the failure", len(root.tabs), len(root.modals))
	}
	if v := m.View().Content; !strings.Contains(v, "db-staging: authentication failed") {
		t.Fatalf("view lacks the error:\n%s", v)
	}
}

func TestFR_TAB_003_FocusBracketsInASCIIProfile(t *testing.T) {
	d := framedDriver(t, testHosts, apptest.NewFakeSession(), apptest.NewFakeSession())
	connectHost(t, d, "h3")
	connectHost(t, d, "h2")
	if h := headerLine(d); h != "sshelf │ Hosts │ 1 zeta │[2 web-prod]│" {
		t.Fatalf("header = %q", h)
	}
	d.focus(0)
	if h := headerLine(d); h != "sshelf │[Hosts]│ 1 zeta │ 2 web-prod │" {
		t.Fatalf("header = %q", h)
	}
	// Reverse video is extra: the brackets alone show focus.
	if raw := strings.Split(d.view(), "\n")[0]; !strings.Contains(raw, "\x1b[") {
		t.Fatalf("header has no styling at all: %q", raw)
	}
}

func TestFR_TAB_003_LabelSanitisedAndCut(t *testing.T) {
	hosts := []domain.Host{{Meta: domain.Meta{ID: "x"}, Label: "evil\x1b]0;pwned\x07-host-with-a-very-long-name", Hostname: "10.0.0.9"}}
	d := framedDriver(t, hosts, apptest.NewFakeSession())
	connectHost(t, d, "x")
	raw := strings.Split(d.view(), "\n")[0]
	if strings.Contains(raw, "\x1b]") || strings.Contains(raw, "\x07") {
		t.Fatalf("header carries the label's control sequence: %q", raw)
	}
	h := headerLine(d)
	want := "[1 " + tabs.SessionEntry(1, hosts[0].Label, false, false).Text[2:] + "]"
	if !strings.Contains(h, want) {
		t.Fatalf("header = %q, want %q", h, want)
	}
	label := strings.TrimSuffix(strings.TrimPrefix(want, "[1 "), "]")
	if w := len([]rune(label)); w != tabs.LabelWidth || !strings.HasSuffix(label, "…") {
		t.Fatalf("label %q is %d columns, want %d ending in …", label, w, tabs.LabelWidth)
	}

	// D-014: what the remote prints never names the tab.
	s := d.root().tabs[0].sess.(framedStub)
	s.Output("\x1b]0;remote title\x07prompt$ ")
	d.pump(1)
	if h := headerLine(d); strings.Contains(h, "remote title") {
		t.Fatalf("header shows a remote title: %q", h)
	}
}

func TestFR_TAB_003_OverflowKeepsFocusVisible(t *testing.T) {
	entries := []tabs.Entry{tabs.HostsEntry()}
	for i := 1; i <= 10; i++ {
		entries = append(entries, tabs.SessionEntry(i, fmt.Sprintf("label-%06d", i), false, false))
	}
	for focus := range entries {
		spans := tabs.Layout(80, entries, focus)
		width, shown, hiddenL, hiddenR, focused := 0, 0, 0, 0, false
		for _, s := range spans {
			width += s.Width
			switch s.Kind {
			case tabs.KindTab:
				shown++
				focused = focused || (s.Focused && s.Tab == focus)
			case tabs.KindMoreLeft:
				_, _ = fmt.Sscanf(s.Text, "‹+%d", &hiddenL)
			case tabs.KindMoreRight:
				_, _ = fmt.Sscanf(s.Text, "+%d›", &hiddenR)
			}
		}
		if width > 80 {
			t.Errorf("focus %d: header is %d columns wide", focus, width)
		}
		if !focused {
			t.Errorf("focus %d: the focused tab is not shown", focus)
		}
		if shown+hiddenL+hiddenR != len(entries) {
			t.Errorf("focus %d: %d shown + %d + %d hidden != %d tabs", focus, shown, hiddenL, hiddenR, len(entries))
		}
		for _, s := range spans {
			if s.Kind != tabs.KindTab {
				continue
			}
			if got, ok := tabs.TabAt(spans, s.X+1); !ok || got != s.Tab {
				t.Errorf("focus %d: TabAt(%d) = %d, %v, want %d", focus, s.X+1, got, ok, s.Tab)
			}
		}
	}
	if _, ok := tabs.TabAt(tabs.Layout(80, entries, 0), 2); ok {
		t.Error("TabAt reports a tab on the title")
	}
}

func TestFR_TAB_004_ActivityOnlyInBackground(t *testing.T) {
	bg, fg := apptest.NewFakeSession(), apptest.NewFakeSession()
	d := framedDriver(t, testHosts, bg, fg)
	connectHost(t, d, "h1")
	connectHost(t, d, "h2") // tab 2 focused

	bg.Output("new output")
	d.pump(1)
	if h := headerLine(d); !strings.Contains(h, "│ 1 db-staging ● │") {
		t.Fatalf("header = %q, want the activity marker on tab 1", h)
	}
	fg.Output("more")
	d.pump(2)
	if h := headerLine(d); strings.Contains(h, "web-prod ●") {
		t.Fatalf("header = %q: output in the focused tab set the marker", h)
	}

	d.focus(1)
	if h := headerLine(d); strings.Contains(h, "●") {
		t.Fatalf("header = %q after focusing tab 1", h)
	}

	// Bytes the emulator sends to the remote do not change Seq, so an
	// update without new output sets nothing.
	d.focus(2)
	bg.SetSnapshot(bg.Snapshot()) // same Seq
	d.send(SessionUpdateMsg{Num: 1, sess: d.root().tabs[0].sess})
	if h := headerLine(d); strings.Contains(h, "●") {
		t.Fatalf("header = %q after an update with no new output", h)
	}
}

func TestFR_TAB_004_EndedMarker(t *testing.T) {
	a, b := apptest.NewFakeSession(), apptest.NewFakeSession()
	d := framedDriver(t, testHosts, a, b)
	connectHost(t, d, "h1")
	connectHost(t, d, "h2")

	a.Output("x")
	d.pump(1)
	a.End(&session.DisconnectError{Err: errors.New("reset")})
	d.pump(1)
	if h := headerLine(d); !strings.Contains(h, "│ 1 db-staging ✕ │") || strings.Contains(h, "●") {
		t.Fatalf("header = %q, want ✕ instead of ●", h)
	}
	b.End(nil)
	d.pump(2)
	if h := headerLine(d); !strings.Contains(h, "[2 web-prod ✕]") {
		t.Fatalf("header = %q, want ✕ on the focused tab", h)
	}
}

func TestFR_TAB_007_StatusBarDurationTicks(t *testing.T) {
	d := framedDriver(t, testHosts, apptest.NewFakeSession(shellScreen...))
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	connectHost(t, d, "h2")
	if d.ticks != 1 {
		t.Fatalf("ticks asked for = %d, want 1", d.ticks)
	}
	last := func() string {
		lines := strings.Split(plain(d.view()), "\n")
		return lines[len(lines)-1]
	}
	want := "● connected · ubuntu@10.0.0.10:22 · 00:00:00 · Ctrl+] ? help · Ctrl+] h hosts"
	if got := last(); got != want {
		t.Fatalf("status bar = %q, want %q", got, want)
	}
	d.clock = d.clock.Add(time.Hour + 2*time.Minute + 3*time.Second)
	d.send(tickMsg{})
	if got := last(); !strings.Contains(got, "· 01:02:03 ·") {
		t.Fatalf("status bar = %q after the tick", got)
	}
	if d.ticks != 2 {
		t.Fatalf("the tick was not re-armed: %d", d.ticks)
	}

	// On the Hosts tab the clock stops.
	d.focus(0)
	d.send(tickMsg{})
	if d.ticks != 2 {
		t.Fatalf("ticks = %d on the Hosts tab", d.ticks)
	}
	d.focus(1)
	if d.ticks != 3 {
		t.Fatalf("ticks = %d after focusing the session again", d.ticks)
	}
}

func TestFR_TAB_007_DropsHintsFirst(t *testing.T) {
	const addr = "ubuntu@10.0.0.10:22"
	d := time.Hour + 2*time.Minute + 3*time.Second
	for _, tc := range []struct {
		width int
		want  string
	}{
		{80, "● connected · ubuntu@10.0.0.10:22 · 01:02:03 · Ctrl+] ? help · Ctrl+] h hosts"},
		{70, "● connected · ubuntu@10.0.0.10:22 · 01:02:03 · Ctrl+] ? help"},
		{50, "● connected · ubuntu@10.0.0.10:22 · 01:02:03"},
		{40, "● connected · ubuntu@10.0.0.10:22"},
		{20, "● connected · ubunt…"},
	} {
		if got := plain(tabs.LiveStatus(tc.width, addr, d, "Ctrl+] ? help", "Ctrl+] h hosts")); got != tc.want {
			t.Errorf("width %d: %q, want %q", tc.width, got, tc.want)
		}
	}
}

func TestFR_TAB_001_ScreenStateKeptAcrossSwitch(t *testing.T) {
	a, b := apptest.NewFakeSession("screen of A"), apptest.NewFakeSession("screen of B")
	d := framedDriver(t, testHosts, a, b)
	connectHost(t, d, "h1")
	connectHost(t, d, "h2")

	// The Hosts tab keeps its screen: a search in progress survives.
	d.focus(0)
	d.press('/', "/")
	d.press('z', "z")
	d.focus(1)
	d.focus(0)
	l := d.root().screen.(hostlist.Model)
	if !l.Searching() || l.Query() != "z" {
		t.Fatalf("search = %v %q after switching away and back", l.Searching(), l.Query())
	}

	// Each session tab shows its own screen, plus output that arrived in
	// the background.
	d.focus(1)
	if v := d.view(); !strings.Contains(v, "screen of A") {
		t.Fatalf("tab 1:\n%s", v)
	}
	b.Output("screen of B", "background output")
	d.pump(2)
	d.focus(2)
	if v := d.view(); !strings.Contains(v, "screen of B\nbackground output") {
		t.Fatalf("tab 2:\n%s", v)
	}
	d.focus(1)
	if v := d.view(); !strings.Contains(v, "screen of A") || strings.Contains(v, "screen of B") {
		t.Fatalf("tab 1 after switching back:\n%s", v)
	}
}

func TestFR_CONN_001_M2_TabOpensWithSessionArea(t *testing.T) {
	s := apptest.NewFakeSession()
	d := framedDriver(t, testHosts, s)
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	connectHost(t, d, "h2")
	if got := s.Sizes(); len(got) == 0 || got[0] != (app.Size{Cols: 120, Rows: 38}) {
		t.Fatalf("sizes = %v, want the session area 120×38 first", got)
	}
	d.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	if got := s.Sizes(); got[len(got)-1] != (app.Size{Cols: 100, Rows: 28}) {
		t.Fatalf("sizes = %v after a resize", got)
	}
}

func TestARCH_UI_005_ListenerReportsUpdatesAndEnd(t *testing.T) {
	s := apptest.NewFakeSession()
	s.Output("x")
	if msg := listenSession(3, s)(); msg != (SessionUpdateMsg{Num: 3, sess: s}) {
		t.Fatalf("listener after output = %#v", msg)
	}
	want := &session.ExitError{Status: 3}
	s.End(want)
	msg, ok := listenSession(3, s)().(SessionEndedMsg)
	if !ok || msg.Num != 3 || msg.Err != error(want) {
		t.Fatalf("listener after the end = %#v", msg)
	}
}
