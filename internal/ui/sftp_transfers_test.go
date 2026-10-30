package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/conflict"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/connecting"
)

// running is the approved layout's download.
var running = app.Transfer{ID: 1, Batch: 1, From: app.Remote, Name: "backup.tar.gz", State: app.Running,
	Done: 412_000_000, Total: 840_000_000, Rate: 12_000_000, ETA: 35 * time.Second, DstDir: "/Users/me/Downloads"}

// everyState is one transfer in each state (FR-SFTP-015).
func everyState() []app.Transfer {
	q := &app.Conflict{Remote: app.FileInfo{Size: 840_000_000, ModTime: fileTime}, Local: app.FileInfo{Size: 812_000_000, ModTime: fileTime.Add(-25 * time.Hour)}}
	return []app.Transfer{
		running,
		{ID: 2, Batch: 1, From: app.Remote, Name: "photos/a.jpg", State: app.Queued, Total: 2_000_000},
		{ID: 3, Batch: 2, From: app.Remote, Name: "notes.txt", State: app.Waiting, Total: 3_000, Conflict: q},
		{ID: 4, Batch: 3, From: app.Local, Name: "report.pdf", State: app.Paused, Done: 600_000, Total: 1_200_000},
		{ID: 5, Batch: 3, From: app.Local, Name: "done.txt", State: app.Done, Done: 5_000, Total: 5_000},
		{ID: 6, Batch: 3, From: app.Local, Name: "secret.key", State: app.Failed, Reason: "permission denied", Total: 400},
		{ID: 7, Batch: 4, From: app.Remote, Name: "big.iso", State: app.Cancelled, Done: 1_000_000, Total: 4_000_000_000},
		{ID: 8, Batch: 4, From: app.Remote, Name: "link@", State: app.Skipped, Reason: "symlink"},
	}
}

// pumpTransfers delivers the pending queue update of tab num.
func pumpTransfers(t *testing.T, d *driver, b *apptest.FakeBrowser, num int, ts ...app.Transfer) {
	t.Helper()
	b.SetTransfers(ts...)
	d.pump(num)
}

func TestFR_SFTP_014_ProgressLineGolden(t *testing.T) {
	d, b := approvedTab(t)
	d.keys("j", "j", "j")
	second := running
	second.ID, second.Name = 9, "other.bin"
	q := app.Transfer{ID: 10, State: app.Queued}
	pumpTransfers(t, d, b, 2, running, second, q, q, q)
	if p := progressLine(d); p != "↓ backup.tar.gz  412/840 MB  █████░░░░░ 49%  12 MB/s  +1 running, 3 queued" {
		t.Fatalf("progress line = %q", p)
	}
	golden(t, "sftp-progress", d.m)

	// Paused and waiting texts.
	paused := running
	paused.State = app.Paused
	pumpTransfers(t, d, b, 2, paused, paused, paused)
	if p := progressLine(d); p != "⏸ 3 transfers paused: connection lost · r reconnect" {
		t.Fatalf("paused line = %q", p)
	}
	waiting := everyState()[2]
	pumpTransfers(t, d, b, 2, waiting)
	if p := progressLine(d); p != "? notes.txt exists · t to answer" {
		t.Fatalf("waiting line = %q", p)
	}
	// A finished copy says how it went.
	done := running
	done.State, done.Done = app.Done, done.Total
	skipped := app.Transfer{ID: 11, Batch: 1, State: app.Skipped}
	pumpTransfers(t, d, b, 2, done, skipped)
	if p := progressLine(d); p != "copied 1 file, skipped 1" {
		t.Fatalf("summary = %q", p)
	}
}

func TestFR_SFTP_014_RedrawAtMost4Hz(t *testing.T) {
	if sftpProgressEvery := ProgressEvery; sftpProgressEvery != 250*time.Millisecond {
		t.Fatalf("tick = %v", sftpProgressEvery)
	}
	d, b := approvedTab(t)
	for range 20 {
		pumpTransfers(t, d, b, 2, running)
	}
	if d.progressTicks != 1 {
		t.Fatalf("%d ticks in flight after 20 updates, want 1", d.progressTicks)
	}
	tick := d.pendingTick
	d.run(tick) // fires: still running, so one more
	if d.progressTicks != 2 {
		t.Fatalf("ticks = %d after a tick", d.progressTicks)
	}
	done := running
	done.State = app.Done
	b.SetTransfers(done)
	d.run(d.pendingTick) // nothing runs: the tick stops
	pumpTransfers(t, d, b, 2, done)
	if d.progressTicks != 2 {
		t.Fatalf("ticks = %d with nothing running", d.progressTicks)
	}
	// No tick for a tab in the background.
	d.focus(1)
	pumpTransfers(t, d, b, 2, running)
	if d.progressTicks != 2 {
		t.Fatalf("background tab ticks: %d", d.progressTicks)
	}
	d.prefix("2") // focusing it starts one
	if d.progressTicks != 3 {
		t.Fatalf("ticks = %d after focusing", d.progressTicks)
	}
}

func TestFR_SFTP_015_ViewGolden(t *testing.T) {
	d, b := approvedTab(t)
	pumpTransfers(t, d, b, 2, everyState()...)
	d.keys("t")
	if !focusedView(t, d).ShowingTransfers() {
		t.Fatal("t did not open the transfers view")
	}
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40}) // the reasons fit at 120 columns
	v := plain(d.view())
	for _, s := range []string{"running", "queued", "waiting", "paused", "done", "failed: permission denied", "cancelled", "skipped: symlink", "412/840 MB", "12 MB/s", "0:35", "↑ report.pdf"} {
		if !strings.Contains(v, s) {
			t.Errorf("view lacks %q", s)
		}
	}
	golden(t, "sftp-transfers", d.m)
	// ETA is "—" while the rate is 0.
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	stalled := running
	stalled.Rate, stalled.ETA = 0, 0
	pumpTransfers(t, d, b, 2, stalled)
	if v := plain(d.view()); !strings.Contains(v, "—") {
		t.Fatal("no — for a zero rate")
	}
}

func TestFR_SFTP_015_Keys(t *testing.T) {
	d, b := approvedTab(t)
	pumpTransfers(t, d, b, 2, everyState()...)
	d.keys("t", "x")                                    // cancel the running one
	d.keys("down", "down", "down", "down", "down", "r") // retry the failed one
	d.keys("down", "r")                                 // and the cancelled one
	d.keys("down", "x")                                 // skipped: nothing to cancel
	if got := b.Cancels(); !slices.Equal(got, []app.TransferID{1}) {
		t.Fatalf("cancels = %v", got)
	}
	if got := b.Retries(); !slices.Equal(got, []app.TransferID{6, 7}) {
		t.Fatalf("retries = %v", got)
	}
	d.keys("C")
	if b.Clears() != 1 || len(focusedView(t, d).Transfers()) != 4 {
		t.Fatalf("clears %d, rows %d", b.Clears(), len(focusedView(t, d).Transfers()))
	}
	d.keys("g") // not a transfers key; up to the waiting one
	d.keys("up", "up", "up", "down", "down", "enter")
	if _, ok := top(d).(conflict.Model); !ok {
		t.Fatalf("enter on waiting: modal %#v", top(d))
	}
	d.keys("esc")
	d.keys("t")
	if focusedView(t, d).ShowingTransfers() {
		t.Fatal("t did not return to the panes")
	}
	d.keys("t", "esc")
	if focusedView(t, d).ShowingTransfers() {
		t.Fatal("esc did not return to the panes")
	}
}

func TestFR_SFTP_016_ConflictModalGolden(t *testing.T) {
	d, b := approvedTab(t)
	pumpTransfers(t, d, b, 2, everyState()[2])
	d.keys("t", "enter")
	golden(t, "sftp-conflict", d.m)
	v := plain(d.view())
	for _, s := range []string{"notes.txt already exists", "remote 840 MB, Oct 29 10:12", "local  812 MB, Oct 28 09:12", "[o] Overwrite", "[s] Skip", "[n] Keep both", "[a] apply to all: no", "[esc] Skip"} {
		if !strings.Contains(v, s) {
			t.Errorf("modal lacks %q", s)
		}
	}
	d.keys("a")
	if !strings.Contains(plain(d.view()), "[a] apply to all: yes") {
		t.Fatal("a did not toggle")
	}
	d.keys("n")
	if got := b.Answers(); !slices.Equal(got, []apptest.Answered{{ID: 3, Answer: app.KeepBoth, All: true}}) {
		t.Fatalf("answers = %v", got)
	}
	d.keys("enter", "o")
	if got := b.Answers(); got[1] != (apptest.Answered{ID: 3, Answer: app.Overwrite}) {
		t.Fatalf("answers = %v", got)
	}
}

func TestFR_SFTP_016_EscSkips(t *testing.T) {
	d, b := approvedTab(t)
	pumpTransfers(t, d, b, 2, everyState()[2])
	d.keys("t", "enter", "esc")
	if top(d) != nil {
		t.Fatal("modal still open")
	}
	d.keys("enter", "s")
	if got := b.Answers(); !slices.Equal(got, []apptest.Answered{{ID: 3, Answer: app.Skip}, {ID: 3, Answer: app.Skip}}) {
		t.Fatalf("answers = %v", got)
	}
}

// disconnect drops tab 2's connection with paused transfers.
func disconnect(t *testing.T, d *driver, b *apptest.FakeBrowser) {
	t.Helper()
	q := running
	q.State = app.Queued
	b.SetTransfers(running, q, q)
	<-b.Updates() // drained by the drop's listener instead
	b.Drop()
	d.pump(2)
}

func TestFR_SFTP_018_DisconnectedGolden(t *testing.T) {
	d, b := approvedTab(t)
	disconnect(t, d, b)
	if !sftpTabAt(t, d, 2).disconnected {
		t.Fatal("not disconnected")
	}
	if h := headerLine(d); !strings.Contains(h, "[2 sftp: raspberrypi4 ✕]") {
		t.Fatalf("header = %q", h)
	}
	if p := progressLine(d); p != "⏸ 3 transfers paused: connection lost · r reconnect" {
		t.Fatalf("progress line = %q", p)
	}
	golden(t, "sftp-disconnected", d.m)
}

func TestFR_SFTP_018_RefusedWhileDisconnected(t *testing.T) {
	d, b := approvedTab(t)
	disconnect(t, d, b)
	lists := b.Lists()
	for _, k := range []string{"j", "enter", "c", "d", "m", "R"} {
		d.keys(k)
		if p := progressLine(d); p != "✕ not connected" {
			t.Fatalf("%s on the remote pane: %q", k, p)
		}
	}
	if b.Lists() != lists || len(b.Copies()) != 0 || top(d) != nil {
		t.Fatal("an operation ran")
	}
	// The local pane still browses.
	d.keys("tab", "j", "enter")
	if _, dir, _ := where(t, d); dir != "/Users/me/Downloads/photos" {
		t.Fatalf("local browsing: %q", dir)
	}
	d.keys("t")
	if !focusedView(t, d).ShowingTransfers() {
		t.Fatal("t refused")
	}
	d.keys("esc")
	// x asks before dropping paused transfers.
	d.keys("x")
	c, ok := top(d).(confirm.Model)
	if !ok || !strings.Contains(c.View(), "Close and drop 3 paused transfers?") {
		t.Fatalf("x: %#v", top(d))
	}
	d.keys("y")
	if len(d.root().tabs) != 1 || b.Closes() != 1 || d.root().focus != 1 {
		t.Fatalf("tabs %d closes %d focus %d", len(d.root().tabs), b.Closes(), d.root().focus)
	}
}

func TestFR_SFTP_018_ReconnectSameTab(t *testing.T) {
	d, b := approvedTab(t)
	svc := d.svcSFTP
	disconnect(t, d, b)
	// A failed reconnect leaves the tab disconnected with the error.
	svc.reconnErr = app.ErrAuthFailed
	d.keys("r")
	if top(d) != nil || !sftpTabAt(t, d, 2).disconnected {
		t.Fatal("failed reconnect")
	}
	if p := progressLine(d); p != "✗ raspberrypi4: authentication failed" {
		t.Fatalf("progress line = %q", p)
	}
	// A cancelled one too.
	svc.reconnErr = nil
	m := d.root()
	next, cmd := m.reconnectSFTP(m.tabs[1])
	d.m = next
	if _, ok := top(d).(connecting.Model); !ok {
		t.Fatal("no Connecting modal over the tab")
	}
	_ = cmd // never run: the user cancels first
	d.keys("esc")
	if !sftpTabAt(t, d, 2).disconnected || !strings.Contains(progressLine(d), "connection cancelled") {
		t.Fatalf("cancel: %q", progressLine(d))
	}
	lists := b.Lists()
	d.keys("r")
	st := sftpTabAt(t, d, 2)
	if st.disconnected || svc.reconns != 2 || len(d.root().tabs) != 2 {
		t.Fatalf("disconnected %v, reconnects %d, tabs %d", st.disconnected, svc.reconns, len(d.root().tabs))
	}
	if b.Lists()-lists != 2 {
		t.Fatalf("panes not refreshed: %d lists", b.Lists()-lists)
	}
	if h := headerLine(d); strings.Contains(h, "✕") {
		t.Fatalf("header = %q", h)
	}
	// The new connection's drop is watched again.
	b.Drop()
	d.pump(2)
	if !sftpTabAt(t, d, 2).disconnected {
		t.Fatal("second drop not seen")
	}
}

func TestFR_SFTP_025_CloseConfirmCountsTransfers(t *testing.T) {
	d, b := approvedTab(t)
	pumpTransfers(t, d, b, 2, everyState()...)
	d.prefix("q")
	c, ok := top(d).(confirm.Model)
	if !ok || !strings.Contains(c.View(), "Close SFTP tab 2? 4 transfers will stop.") {
		t.Fatalf("question: %#v", top(d))
	}
	for _, k := range []string{"n", "enter", "esc"} {
		if k != "n" {
			d.prefix("q")
		}
		d.keys(k)
		if len(d.root().tabs) != 2 || b.Closes() != 0 {
			t.Fatalf("%s closed the tab", k)
		}
	}
	d.prefix("q")
	d.keys("y")
	if len(d.root().tabs) != 1 || b.Closes() != 1 || d.root().focus != 1 {
		t.Fatalf("tabs %d closes %d focus %d", len(d.root().tabs), b.Closes(), d.root().focus)
	}
	// With nothing pending the question has no count.
	d2, _ := approvedTab(t)
	d2.prefix("q")
	if c, ok := top(d2).(confirm.Model); !ok || strings.Contains(c.View(), "will stop") {
		t.Fatalf("question: %#v", top(d2))
	}
}

// TestFR_SFTP_001_CopiesKeepKeyOrder: Bubble Tea runs commands
// concurrently, yet copies queued by quick key presses are queued in the
// order of the presses, so the transfers view lists them that way.
func TestFR_SFTP_001_CopiesKeepKeyOrder(t *testing.T) {
	d, b := approvedTab(t)
	d.keys("g")
	var cmds []tea.Cmd
	for range 3 {
		d.keys("j")
		var cmd tea.Cmd
		d.m, cmd = d.m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
		cmds = append(cmds, cmd)
	}
	done := make(chan struct{})
	for _, cmd := range slices.Backward(cmds) { // the last press runs first
		go func() { cmd(); done <- struct{}{} }()
		time.Sleep(10 * time.Millisecond)
	}
	for range cmds {
		<-done
	}
	var got []string
	for _, c := range b.Copies() {
		got = append(got, c.Names...)
	}
	if want := []string{"scripts", ".bashrc", "backup.tar.gz"}; !slices.Equal(got, want) {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

func TestFR_SFTP_001_CopyQueuesAndClearsMarks(t *testing.T) {
	d, b := approvedTab(t)
	d.keys("g", "c") // c on .. does nothing
	if len(b.Copies()) != 0 {
		t.Fatal("c on .. copied")
	}
	d.keys("j", "space", "j", "space", "c")
	want := apptest.Copy{From: app.Remote, SrcDir: "/home/pi", Names: []string{"scripts", "backup.tar.gz"}, DstDir: "/Users/me/Downloads"}
	if got := b.Copies(); len(got) != 1 || !copyEqual(got[0], want) {
		t.Fatalf("copies = %+v", got)
	}
	if got := focusedView(t, d).Marked(app.Remote); len(got) != 0 {
		t.Fatalf("marks not cleared: %q", got)
	}
	// Upload of the cursor entry.
	d.keys("tab", "G", "c")
	if got := b.Copies(); len(got) != 2 || !copyEqual(got[1], apptest.Copy{From: app.Local, SrcDir: "/Users/me/Downloads", Names: []string{"report.pdf"}, DstDir: "/home/pi"}) {
		t.Fatalf("copies = %+v", got)
	}
	// A finished download refreshes the local pane showing its target.
	b.SetDir(app.Local, "/Users/me/Downloads", "~/Downloads", dir("photos"), file("report.pdf", 1), file("notes.txt", 1), file("backup.tar.gz", 840_000_000))
	done := running
	done.State, done.Done = app.Done, done.Total
	pumpTransfers(t, d, b, 2, done)
	if got := rowNames(t, d, app.Local); !slices.Contains(got, "backup.tar.gz") {
		t.Fatalf("local pane not refreshed: %q", got)
	}
	if _, _, cur := where(t, d); cur != "report.pdf" {
		t.Fatalf("cursor moved to %q", cur)
	}
}

func copyEqual(a, b apptest.Copy) bool {
	return a.From == b.From && a.SrcDir == b.SrcDir && a.DstDir == b.DstDir && slices.Equal(a.Names, b.Names)
}

func TestFR_TAB_003_M3_ActivityMarker(t *testing.T) {
	d, b := approvedTab(t)
	d.focus(1)
	pumpTransfers(t, d, b, 2, running)
	if h := headerLine(d); strings.Contains(h, "●") {
		t.Fatalf("marker for a running transfer: %q", h)
	}
	failed := running
	failed.State = app.Failed
	pumpTransfers(t, d, b, 2, failed)
	if h := headerLine(d); !strings.Contains(h, " 2 sftp: raspberrypi4 ● ") {
		t.Fatalf("no marker: %q", h)
	}
	d.prefix("2")
	d.prefix("1")
	if h := headerLine(d); strings.Contains(h, "●") {
		t.Fatalf("marker not cleared by focusing: %q", h)
	}
}
