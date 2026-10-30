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
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/tofu"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// walkSFTPService is the M3 walk-through's service: OpenSFTP asks the
// TOFU question first, as app.App does for a host whose key is unknown.
type walkSFTPService struct {
	*sftpService
	send func(tea.Msg)
}

func (w *walkSFTPService) OpenSFTP(ctx context.Context, id domain.ID) (app.Browser, error) {
	w.checkGoroutine("OpenSFTP")
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
	return w.sftpService.OpenSFTP(ctx, id)
}

// sftpAsyncDriver is an asyncDriver over walkSFTPService.
func sftpAsyncDriver(t *testing.T, svc *sftpService) *asyncDriver {
	t.Helper()
	d := &asyncDriver{t: t, svc: svc.fakeService, msgs: make(chan tea.Msg, 64)}
	ws := &walkSFTPService{sftpService: svc, send: func(msg tea.Msg) { d.msgs <- msg }}
	m := New(ws)
	m.home = "/Users/me"
	m.now = func() time.Time { return testEpoch }
	m.tick = func() tea.Cmd { return nil }
	d.m = m
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	d.dispatch(m.Init())
	return d
}

// selectHost selects host id in the host list.
func (d *asyncDriver) selectHost(id domain.ID) {
	m := d.m.(Model)
	m.screen = m.screen.(hostlist.Model).Select(id)
	d.m = m
}

// prefix sends Ctrl+] and then text.
func (d *asyncDriver) prefix(text string) {
	d.send(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	d.press([]rune(text)[0], text)
}

// shows waits until the plain view holds every text.
func (d *asyncDriver) shows(texts ...string) {
	d.t.Helper()
	d.until(strings.Join(texts, ", "), func(m Model) bool {
		v := plain(m.View().Content)
		for _, s := range texts {
			if !strings.Contains(v, s) {
				return false
			}
		}
		return true
	})
}

// TestARCH_UI_001_M3_Walkthrough is the M3 walk-through (ARCH-UI-001 M3
// revision): the host list, s on a host, TOFU, the SFTP tab opens, tab,
// enter into a directory, c, the progress line, t, Esc, Ctrl+] h, enter
// on another host, Ctrl+] s (a new SFTP tab: the connection differs),
// Ctrl+] q on that SFTP tab and y; every step matches its goldens.
func TestARCH_UI_001_M3_Walkthrough(t *testing.T) {
	first, second := approvedBrowser(1), approvedBrowser(2)
	sess := apptest.NewFakeSession("admin@db-staging:~$ ")
	sess.SetConnID(2)
	svc := &sftpService{fakeService: &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{sess}}, browsers: []*apptest.FakeBrowser{first, second}}
	d := sftpAsyncDriver(t, svc)
	d.until("the host list", func(m Model) bool { _, ok := m.screen.(hostlist.Model); return ok })

	// s on web-prod: the TOFU question over Connecting.
	d.selectHost("h2")
	d.press('s', "s")
	d.until("the TOFU modal", func(m Model) bool { _, ok := topModal(m).(tofu.Model); return ok })
	golden(t, "walk-m3-1-tofu", d.m)

	// y: the SFTP tab opens, focused, both panes listed.
	d.press('y', "y")
	d.until("the SFTP tab", func(m Model) bool { st := m.focusedSFTP(); return st != nil && st.num == 1 })
	d.shows("12 items", "4 items", "backup.tar.gz")
	if m := d.m.(Model); len(m.modals) != 0 || m.cancel != nil || len(svc.opened) != 1 || svc.opened[0] != "h2" {
		t.Fatalf("modals %d, attempt running %v, opened %v", len(m.modals), m.cancel != nil, svc.opened)
	}
	golden(t, "walk-m3-2-sftp", d.m)

	// tab, then enter on photos/ in the local pane.
	d.press(tea.KeyTab, "")
	d.press('j', "j")
	d.press(tea.KeyEnter, "")
	d.shows("Local: ~/Downloads/photos", "a.jpg")
	golden(t, "walk-m3-3-dir", d.m)

	// c on a.jpg queues an upload; the progress line shows it.
	d.press('j', "j")
	d.press('c', "c")
	d.until("the copy", func(Model) bool { return len(first.Copies()) == 1 })
	if c := first.Copies()[0]; c.From != app.Local || c.SrcDir != "/Users/me/Downloads/photos" || c.DstDir != "/home/pi" || len(c.Names) != 1 || c.Names[0] != "a.jpg" {
		t.Fatalf("copy = %+v", c)
	}
	first.SetTransfers(app.Transfer{ID: 1, Batch: 1, From: app.Local, Name: "a.jpg", State: app.Running, Done: 400_000, Total: 1_000_000, Rate: 200_000, ETA: 3 * time.Second, DstDir: "/home/pi"})
	d.shows("↑ a.jpg")
	golden(t, "walk-m3-4-progress", d.m)

	// t shows the transfers view; Esc returns to the panes.
	d.press('t', "t")
	d.shows("Transfers (1)")
	golden(t, "walk-m3-5-transfers", d.m)
	d.press(tea.KeyEscape, "")
	d.until("the panes", func(m Model) bool { return !m.focusedSFTP().sftp.view.ShowingTransfers() })

	// Ctrl+] h, enter on db-staging: a session tab on connection 2.
	d.prefix("h")
	d.until("the Hosts tab", func(m Model) bool { return m.focus == 0 })
	d.selectHost("h1")
	d.press(tea.KeyEnter, "")
	d.until("the session tab", func(m Model) bool { t := m.focused(); return t != nil && t.num == 2 })

	// Ctrl+] s: the SFTP tab of connection 1 is not reused.
	d.prefix("s")
	d.until("a second SFTP tab", func(m Model) bool { st := m.focusedSFTP(); return st != nil && st.num == 3 })
	d.shows("12 items", "4 items")
	if len(svc.openedOn) != 1 || svc.openedOn[0] != app.Session(sess) {
		t.Fatalf("OpenSFTPOn %v", svc.openedOn)
	}
	golden(t, "walk-m3-6-second", d.m)

	// Ctrl+] q on the SFTP tab and y: it closes, focus moves left.
	d.prefix("q")
	d.until("the close question", func(m Model) bool { _, ok := topModal(m).(confirm.Model); return ok })
	golden(t, "walk-m3-7-close", d.m)
	d.press('y', "y")
	d.until("the closed tab", func(m Model) bool { return len(m.tabs) == 2 && m.focus == 2 && second.Closes() == 1 })
	if first.Closes() != 0 || sess.Closes() != 0 {
		t.Fatalf("closes: first %d, session %d", first.Closes(), sess.Closes())
	}
	golden(t, "walk-m3-8-closed", d.m)

	// Clean up: end the session and close the first SFTP tab, so their
	// listeners return.
	sess.End(nil)
	d.until("the end of the session", func(m Model) bool { return m.tabs[1].done })
	d.prefix("1")
	d.prefix("q")
	d.until("the close question", func(m Model) bool { _, ok := topModal(m).(confirm.Model); return ok })
	d.press('y', "y")
	d.until("the closed tab", func(m Model) bool { return len(m.tabs) == 1 && first.Closes() == 1 })
}
