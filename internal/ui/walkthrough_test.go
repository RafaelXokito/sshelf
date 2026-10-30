package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/session"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/tofu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostedit"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// walkService is the fake service of the walkthrough. Connect behaves as
// app.App does for a host whose key is unknown: it sends a
// HostKeyPromptMsg to the program and waits for the answer before it
// returns the session.
type walkService struct {
	*fakeService
	send func(tea.Msg)
}

func (w *walkService) Connect(ctx context.Context, id domain.ID, size app.Size) (app.Session, error) {
	w.checkGoroutine("Connect")
	reply := make(chan bool, 1)
	w.send(app.HostKeyPromptMsg{Host: "10.0.0.10", Port: 22, KeyType: "ssh-ed25519", Fingerprint: testFingerprint, Reply: reply})
	select {
	case trusted := <-reply:
		if !trusted {
			return nil, app.ErrHostKeyRejected
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return w.fakeService.Connect(ctx, id, size)
}

// asyncDriver runs the root model the way tea.Program does, including
// messages that arrive while a command is still running (a prompt from a
// connection attempt): Update runs on the test goroutine, every command
// on its own goroutine, and their messages queue up on msgs.
type asyncDriver struct {
	t    *testing.T
	svc  *fakeService
	m    tea.Model
	msgs chan tea.Msg
}

func newAsyncDriver(t *testing.T, svc *fakeService) *asyncDriver {
	t.Helper()
	d := &asyncDriver{t: t, svc: svc, msgs: make(chan tea.Msg, 64)}
	ws := &walkService{fakeService: svc, send: func(msg tea.Msg) { d.msgs <- msg }}
	m := New(ws)
	m.now = func() time.Time { return testEpoch } // goldens show a fixed duration
	m.tick = func() tea.Cmd { return nil }
	d.m = m
	d.dispatch(m.Init())
	return d
}

func (d *asyncDriver) dispatch(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() { // never on the Update goroutine
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				d.dispatch(c)
			}
		default:
			d.msgs <- msg
		}
	}()
}

func (d *asyncDriver) send(msg tea.Msg) {
	d.svc.updateGID.Store(goroutineID())
	var cmd tea.Cmd
	d.m, cmd = d.m.Update(msg)
	d.dispatch(cmd)
}

func (d *asyncDriver) press(code rune, text string) { d.send(tea.KeyPressMsg{Code: code, Text: text}) }

// until delivers queued messages until cond holds.
func (d *asyncDriver) until(what string, cond func(Model) bool) {
	d.t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond(d.m.(Model)) {
		select {
		case msg := <-d.msgs:
			d.send(msg)
		case <-deadline:
			d.t.Fatalf("waiting for %s; view:\n%s", what, plain(d.m.View().Content))
		}
	}
}

func topModal(m Model) any {
	if n := len(m.modals); n > 0 {
		return m.modals[n-1]
	}
	return nil
}

// TestARCH_UI_001_Walkthrough drives the root model (one screen plus a
// stack of modals, design §9) through start, list, edit, save, connect,
// TOFU, a stub session in its tab, its end, x and back to the list, and
// matches every step with its golden files.
func TestARCH_UI_001_Walkthrough(t *testing.T) {
	sess := apptest.NewFakeSession("$ ")
	d := newAsyncDriver(t, &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{sess}})

	// Start: Load runs in a command and the host list appears.
	d.until("the host list", func(m Model) bool { _, ok := m.screen.(hostlist.Model); return ok })
	golden(t, "walk-1-list", d.m)

	// Edit the selected host (db-staging) and rename it.
	d.press('e', "e")
	d.until("the host form", func(m Model) bool { _, ok := m.screen.(hostedit.Model); return ok })
	d.press(tea.KeyEnd, "")
	for _, r := range "-2" {
		d.press(r, string(r))
	}
	golden(t, "walk-2-edit", d.m)

	// Save: back on the list with the renamed host selected.
	d.send(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	d.until("the saved host", func(m Model) bool { return strings.Contains(m.status, "db-staging-2: saved") })
	if _, ok := d.m.(Model).screen.(hostlist.Model); !ok {
		t.Fatal("not back on the host list after save")
	}
	if h := selected(t, &driver{m: d.m}); h.Label != "db-staging-2" {
		t.Fatalf("selected %q after save, want db-staging-2", h.Label)
	}
	golden(t, "walk-3-saved", d.m)

	// Connect: Connecting, then the TOFU modal on top of it.
	d.press(tea.KeyEnter, "")
	d.until("the TOFU modal", func(m Model) bool { _, ok := topModal(m).(tofu.Model); return ok })
	if _, ok := d.m.(Model).modals[0].(connecting.Model); !ok || len(d.m.(Model).modals) != 2 {
		t.Fatal("the TOFU modal is not stacked over Connecting")
	}
	golden(t, "walk-4-tofu", d.m)

	// Trust: the session opens in a focused tab.
	d.press('y', "y")
	d.until("the session tab", func(m Model) bool { return len(m.tabs) == 1 && m.focus == 1 })
	if m := d.m.(Model); len(m.modals) != 0 || m.cancel != nil {
		t.Fatalf("with the session open: %d modals, attempt still running: %v", len(m.modals), m.cancel != nil)
	}
	golden(t, "walk-5-session", d.m)

	// The remote exits; x closes the ended tab and the list comes back.
	sess.End(nil)
	d.until("the end of the session", func(m Model) bool { return m.tabs[0].done })
	d.press('x', "x")
	d.until("the host list", func(m Model) bool { return len(m.tabs) == 0 && m.focus == 0 })
	if h := selected(t, &driver{m: d.m}); h.Label != "db-staging-2" {
		t.Fatalf("selected %q after the session, want db-staging-2", h.Label)
	}
	golden(t, "walk-6-back", d.m)
}

// TestARCH_UI_001_M2_Walkthrough is the M2 walk-through: start, list,
// connect (tab opens), Ctrl+] h, connect a second host, Ctrl+] 1, remote
// exit 3, the ended tab, x, and the list with the host selected.
func TestARCH_UI_001_M2_Walkthrough(t *testing.T) {
	a, b := apptest.NewFakeSession("ubuntu@web-prod:~$ "), apptest.NewFakeSession("admin@db-staging:~$ ")
	d := framedDriver(t, testHosts, a, b)
	golden(t, "walk-m2-1-list", d.m)

	connectHost(t, d, "h2")
	if r := d.root(); len(r.tabs) != 1 || r.focus != 1 {
		t.Fatalf("tabs = %d, focus = %d after connecting web-prod", len(r.tabs), r.focus)
	}
	golden(t, "walk-m2-2-tab", d.m)

	d.prefix("h")
	if d.root().focus != 0 {
		t.Fatalf("focus = %d after Ctrl+] h", d.root().focus)
	}
	golden(t, "walk-m2-3-hosts", d.m)

	d.press('k', "k") // db-staging, above web-prod
	d.press(tea.KeyEnter, "")
	if r := d.root(); len(r.tabs) != 2 || r.focus != 2 {
		t.Fatalf("tabs = %d, focus = %d after connecting db-staging", len(r.tabs), r.focus)
	}
	golden(t, "walk-m2-4-second", d.m)

	d.prefix("1")
	if d.root().focus != 1 {
		t.Fatalf("focus = %d after Ctrl+] 1", d.root().focus)
	}
	golden(t, "walk-m2-5-first", d.m)

	a.Output("ubuntu@web-prod:~$ exit 3", "logout")
	d.pump(1)
	endTab(d, 1, a, &session.ExitError{Status: 3})
	golden(t, "walk-m2-6-ended", d.m)

	d.press('x', "x")
	if r := d.root(); len(r.tabs) != 1 || r.focus != 0 || a.Closes() != 1 {
		t.Fatalf("tabs = %d, focus = %d, closes = %d after x", len(r.tabs), r.focus, a.Closes())
	}
	if h := selected(t, d); h.ID != "h2" {
		t.Fatalf("selected %s after closing the tab, want h2 (web-prod)", h.ID)
	}
	golden(t, "walk-m2-7-back", d.m)
}

// TestARCH_UI_001_TofuRejectReturnsToList: cancelling the TOFU modal
// fails the attempt and leaves the list with no modal.
func TestARCH_UI_001_TofuRejectReturnsToList(t *testing.T) {
	d := newAsyncDriver(t, &fakeService{hosts: testHosts})
	d.until("the host list", func(m Model) bool { _, ok := m.screen.(hostlist.Model); return ok })
	d.press(tea.KeyEnter, "")
	d.until("the TOFU modal", func(m Model) bool { _, ok := topModal(m).(tofu.Model); return ok })
	d.press(tea.KeyEscape, "")
	d.until("the failed attempt", func(m Model) bool { return m.cancel == nil && len(m.modals) == 0 })
	if !strings.Contains(d.m.(Model).status, "db-staging") {
		t.Fatalf("status %q", d.m.(Model).status)
	}
}
