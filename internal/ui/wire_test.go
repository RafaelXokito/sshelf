package ui

import (
	"fmt"
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
)

// The root model's side of the real sessions (T-037).

func TestARCH_SESS_016_TerminalColoursReachSessions(t *testing.T) {
	solarized := color.RGBA{R: 0xfd, G: 0xf6, B: 0xe3, A: 0xff}
	ink := color.RGBA{R: 0x65, G: 0x7b, B: 0x83, A: 0xff}
	a, b := apptest.NewFakeSession(), apptest.NewFakeSession()
	d := framedDriver(t, testHosts, a, b)

	// Reported before any session: the next session gets them.
	d.send(tea.BackgroundColorMsg{Color: solarized})
	connectHost(t, d, "h2")
	if fg, bg := a.Colors(); bg != color.Color(solarized) || fg != nil {
		t.Fatalf("first session colours fg %v, bg %v", fg, bg)
	}
	// Reported later: every live session gets them, the background kept.
	connectHost(t, d, "h1")
	d.send(tea.ForegroundColorMsg{Color: ink})
	for i, s := range []*apptest.FakeSession{a, b} {
		if fg, bg := s.Colors(); fg != color.Color(ink) || bg != color.Color(solarized) {
			t.Fatalf("session %d colours fg %v, bg %v", i+1, fg, bg)
		}
	}
}

func TestARCH_SESS_016_InitAsksForColours(t *testing.T) {
	m := New(&fakeService{})
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init is not a batch")
	}
	seen := map[string]bool{}
	for _, c := range batch {
		if c != nil {
			seen[fmt.Sprintf("%T", c())] = true
		}
	}
	for _, want := range []string{fmt.Sprintf("%T", tea.RequestBackgroundColor()), fmt.Sprintf("%T", tea.RequestForegroundColor())} {
		if !seen[want] {
			t.Errorf("Init does not send %s; it sends %v", want, seen)
		}
	}
}

func TestFR_CONN_001_M2_ConnectGetsSessionArea(t *testing.T) {
	svc := &fakeService{hosts: testHosts}
	d := newDriver(t, svc)
	connectSecond(d) // before the terminal size is known: the default
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	d.focus(0)
	d.press(tea.KeyEnter, "")
	if len(svc.sizes) != 2 || svc.sizes[0] != (app.Size{}) || svc.sizes[1] != (app.Size{Cols: 120, Rows: 38}) {
		t.Fatalf("Connect sizes = %v, want the zero size, then 120×38", svc.sizes)
	}
}

func TestARCH_SESS_010_CloseSessionsClosesEveryTab(t *testing.T) {
	a, b := apptest.NewFakeSession(), apptest.NewFakeSession()
	d := framedDriver(t, testHosts, a, b)
	connectHost(t, d, "h1")
	connectHost(t, d, "h2")
	endTab(d, 2, b, nil)
	d.root().CloseTabs()
	if a.Closes() != 1 || b.Closes() != 1 {
		t.Fatalf("Close calls: %d, %d", a.Closes(), b.Closes())
	}
}
