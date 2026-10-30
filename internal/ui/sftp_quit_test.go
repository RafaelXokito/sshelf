package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/session"
)

// dropTab drops SFTP tab num's connection with ts in its queue.
func dropTab(t *testing.T, d *driver, b *apptest.FakeBrowser, num int, ts ...app.Transfer) {
	t.Helper()
	b.SetTransfers(ts...)
	<-b.Updates() // drained by the drop's listener instead
	b.Drop()
	d.pump(num)
	if !sftpTabAt(t, d, num).disconnected {
		t.Fatalf("tab %d not disconnected", num)
	}
}

// TestFR_TAB_009_M3_QuestionCounts: the quit question counts live
// session tabs and SFTP tabs that are connected or have paused transfers,
// and the pending transfers of every SFTP tab; n, Enter and Esc keep
// everything; y closes every tab (the browsers cancel their transfers)
// and quits.
func TestFR_TAB_009_M3_QuestionCounts(t *testing.T) {
	s := apptest.NewFakeSession("$ ")
	b2, b3, b4 := approvedBrowser(1), approvedBrowser(2), approvedBrowser(3)
	d, _ := sftpDriver(t, piHosts, []*apptest.FakeSession{s}, b2, b3, b4)
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "pi") // 1: a live session
	openSFTP(t, d, "pi")    // 2: connected, a running and a queued transfer
	q := running
	q.ID, q.State = 2, app.Queued
	pumpTransfers(t, d, b2, 2, running, q)
	openSFTP(t, d, "pi") // 3: disconnected with a paused transfer
	p := running
	p.ID = 3
	dropTab(t, d, b3, 3, p)
	openSFTP(t, d, "pi") // 4: disconnected with nothing paused: not counted
	done := running
	done.ID, done.State = 4, app.Done
	dropTab(t, d, b4, 4, done)

	if tabs, transfers := d.root().openWork(); tabs != 3 || transfers != 3 {
		t.Fatalf("openWork = %d tabs, %d transfers; want 3, 3", tabs, transfers)
	}
	d.focus(0)
	const want = "Quit and close 3 tabs? 3 transfers will stop."
	for _, quit := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		for _, no := range []tea.KeyPressMsg{{Code: 'n', Text: "n"}, {Code: tea.KeyEnter}, {Code: tea.KeyEscape}} {
			d.send(quit)
			if v := plain(d.view()); !strings.Contains(v, want) {
				t.Fatalf("no question after %v:\n%s", quit, v)
			}
			d.send(no)
			if d.quit || len(d.root().tabs) != 4 || b2.Closes()+b3.Closes()+b4.Closes()+s.Closes() != 0 {
				t.Fatalf("after %v: quit %v, tabs %d", no, d.quit, len(d.root().tabs))
			}
		}
	}

	// Only SFTP work left: the session ended, one transfer still counts.
	s.End(&session.ExitError{Status: 0})
	d.pump(1)
	pumpTransfers(t, d, b2, 2, q)
	d.press('q', "q")
	if v := plain(d.view()); !strings.Contains(v, "Quit and close 2 tabs? 2 transfers will stop.") {
		t.Fatalf("view:\n%s", v)
	}
	d.press('y', "y")
	if !d.quit {
		t.Fatal("y did not quit")
	}
	for i, b := range []*apptest.FakeBrowser{b2, b3, b4} {
		if b.Closes() != 1 {
			t.Errorf("browser of tab %d closed %d times", i+2, b.Closes())
		}
	}
}

// TestFR_TAB_009_M3_NothingLeftQuits: with every SFTP tab disconnected
// and nothing paused, q quits at once.
func TestFR_TAB_009_M3_NothingLeftQuits(t *testing.T) {
	b := approvedBrowser(1)
	d, _ := sftpOpen(t, b)
	dropTab(t, d, b, 1)
	d.focus(0)
	d.press('q', "q")
	if !d.quit {
		t.Fatalf("q did not quit at once:\n%s", plain(d.view()))
	}
}

func TestFR_TAB_009_M3_QuitQuestion(t *testing.T) {
	for _, c := range []struct {
		tabs, transfers int
		want            string
	}{
		{1, 0, "Quit and close 1 tab?"},
		{2, 1, "Quit and close 2 tabs? 1 transfer will stop."},
		{1, 3, "Quit and close 1 tab? 3 transfers will stop."},
	} {
		if got := quitQuestion(c.tabs, c.transfers); got != c.want {
			t.Errorf("quitQuestion(%d, %d) = %q, want %q", c.tabs, c.transfers, got, c.want)
		}
	}
}
