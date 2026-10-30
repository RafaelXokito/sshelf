package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/session"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/help"
	"github.com/RafaelXokito/sshelf/internal/ui/sftpview"
)

// approvedTab opens the approved layout: a session tab on raspberrypi4,
// then an SFTP tab on the same host.
func approvedTab(t *testing.T) (*driver, *apptest.FakeBrowser) {
	t.Helper()
	b := approvedBrowser(1)
	d, _ := sftpDriver(t, piHosts, []*apptest.FakeSession{apptest.NewFakeSession("pi@raspberrypi4:~ $ ")}, b)
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "pi")
	openSFTP(t, d, "pi")
	return d, b
}

// sftpOpen opens one SFTP tab on browser b with nothing else.
func sftpOpen(t testing.TB, b *apptest.FakeBrowser) (*driver, *sftpService) {
	t.Helper()
	d, svc := sftpDriver(t, piHosts, nil, b)
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	openSFTP(t, d, "pi")
	return d, svc
}

// focusedView is the focused SFTP tab's view.
func focusedView(t *testing.T, d *driver) sftpview.Model {
	t.Helper()
	st := d.root().focusedSFTP()
	if st == nil {
		t.Fatal("no SFTP tab focused")
	}
	return st.sftp.view
}

// rowNames are the names in pane side's rows as shown, in order.
func rowNames(t *testing.T, d *driver, side app.Side) []string {
	t.Helper()
	r := focusedView(t, d).Layout().Rows[side]
	lines := strings.Split(plain(d.view()), "\n")
	var out []string
	for y := r.Y; y < r.Y+r.H; y++ {
		cells := []rune(lines[y])
		if len(cells) < r.X+r.W {
			continue
		}
		if f := strings.Fields(string(cells[r.X+3 : r.X+r.W])); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

// where is the focused pane, its directory and its cursor's name.
func where(t *testing.T, d *driver) (app.Side, string, string) {
	t.Helper()
	v := focusedView(t, d)
	return v.Focus(), v.Dir(v.Focus()), v.Cursor(v.Focus())
}

func TestFR_SFTP_023_ApprovedLayoutGolden(t *testing.T) {
	d, _ := approvedTab(t)
	d.keys("j", "j", "j") // the cursor on backup.tar.gz
	golden(t, "sftp-approved", d.m)

	// With an odd width the extra column goes to the remote pane.
	d.send(tea.WindowSizeMsg{Width: 81, Height: 24})
	view := d.view()
	checkLayout(t, d.root(), view, 81, 24)
	compareGolden(t, "sftp-approved.81x24", plain(view))
	lay := sftpview.Layout(81, 24)
	if lay.Boxes[0].W != 40 || lay.Boxes[1].W != 41 || lay.Rows[0].H < 18 {
		t.Fatalf("layout at 81x24 = %+v", lay)
	}
	if lay := sftpview.Layout(80, 24); lay.Rows[0].H < 18 || lay.Footer.Y != 23 || lay.Progress.Y != 22 {
		t.Fatalf("layout at 80x24 = %+v", lay)
	}
}

func TestARCH_UI_005_M3_FrameGoldens(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		d, _ := approvedTab(t)
		d.keys("?")
		if _, ok := d.root().modals[0].(help.Model); !ok {
			t.Fatalf("modals = %v", d.root().modals)
		}
		golden(t, "sftp-help", d.m)
		d.keys("esc")
		if len(d.root().modals) != 0 {
			t.Fatal("help still open")
		}
	})
	t.Run("filter", func(t *testing.T) {
		d, _ := approvedTab(t)
		d.keys("tab", "/", "r", "e", "p")
		golden(t, "sftp-filter", d.m)
		if v := d.m.View(); v.Cursor != nil {
			t.Fatalf("cursor = %+v, want nil (the field draws its own)", v.Cursor)
		}
	})
	t.Run("close question", func(t *testing.T) {
		d, _ := approvedTab(t)
		d.prefix("q")
		golden(t, "sftp-close", d.m)
	})
	t.Run("mouse mode", func(t *testing.T) {
		d, _ := approvedTab(t)
		if mm := d.m.View().MouseMode; mm != tea.MouseModeCellMotion {
			t.Fatalf("mouse mode on an SFTP tab = %v", mm)
		}
	})
}

func TestFR_SFTP_006_SortDirsFirst(t *testing.T) {
	b := apptest.NewFakeBrowser(1, "/l", "/r")
	b.SetDir(app.Local, "/l", "/l")
	b.SetDir(app.Remote, "/r", "/r",
		file("b.txt", 1), dir("Zeta"), link("a-link"), file("B.txt", 1), dir("alpha"), file("C", 1))
	b.SetDir(app.Remote, "/", "/", dir("home"))
	d, _ := sftpOpen(t, b)
	want := []string{"..", "alpha/", "Zeta/", "a-link@", "B.txt", "b.txt", "C"}
	if got := rowNames(t, d, app.Remote); !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	d.keys("enter") // ..: "/" has no ".." row
	if got := rowNames(t, d, app.Remote); !slices.Equal(got, []string{"home/"}) {
		t.Fatalf("rows at / = %q", got)
	}
}

func TestFR_SFTP_006_SizeUnits(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 999: "999 B", 1000: "1 KB", 3000: "3 KB", 3456: "3.5 KB", 9960: "10 KB",
		1_200_000: "1.2 MB", 840_000_000: "840 MB", 999_600: "1 MB", 12_000_000_000: "12 GB",
	} {
		if got := sftpview.FormatSize(n); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", n, got, want)
		}
	}
	for n, want := range map[int]string{-1: "…", 0: "0 items", 1: "1 item", 12: "12 items", 10_000: "10000 items", 10_001: "10k+ items"} {
		if got := sftpview.FormatCount(n); got != want {
			t.Errorf("FormatCount(%d) = %q, want %q", n, got, want)
		}
	}
	now := time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC)
	if got := sftpview.FormatTime(now.Add(-time.Hour), now); got != "Oct 30 13:02" {
		t.Errorf("recent time = %q", got)
	}
	if got := sftpview.FormatTime(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC), now); got != "2025-03-01" {
		t.Errorf("old time = %q", got)
	}
}

func TestFR_SFTP_006_ItemCountsVisibleOnly(t *testing.T) {
	b := apptest.NewFakeBrowser(1, "/l", "/r")
	b.SetDir(app.Local, "/l", "/l")
	var es []app.Entry
	for i := range 40 {
		es = append(es, dir(string([]rune{rune('a' + i/26), rune('a' + i%26)})))
	}
	b.SetDir(app.Remote, "/r", "/r", es...)
	d, _ := sftpOpen(t, b)
	calls := b.CountCalls()
	// 19 rows on screen at 80x24: ".." and 18 directories.
	if len(calls) != 18 || calls[0] != "/r/aa" || calls[17] != "/r/ar" {
		t.Fatalf("counted %d dirs %q, want the 18 on screen", len(calls), calls)
	}
	if !strings.Contains(viewLine(d, 3), "0 items") {
		t.Fatalf("row 2 lacks its count: %q", viewLine(d, 3))
	}
	d.keys("G") // the last 19 rows come into view; the 3 rows between were never shown
	if calls := b.CountCalls(); len(calls) != 37 || slices.Contains(calls, "/r/as") || calls[36] != "/r/bn" {
		t.Fatalf("after G counted %d: %q", len(calls), calls[len(calls)-3:])
	}
}

func TestFR_SFTP_006_MtimeColumnFrom50(t *testing.T) {
	d, _ := approvedTab(t)
	for _, tc := range []struct {
		width int
		shown bool
	}{{103, false}, {104, true}, {120, true}, {80, false}} {
		d.send(tea.WindowSizeMsg{Width: tc.width, Height: 24})
		lay := focusedView(t, d).Layout()
		local := []rune(viewLine(d, 3))[:lay.Boxes[0].W]
		if got := strings.Contains(string(local), "Oct 29 10:12"); got != tc.shown {
			t.Errorf("width %d (inner %d): mtime shown = %v, want %v: %q", tc.width, lay.Rows[0].W, got, tc.shown, string(local))
		}
	}
}

func TestFR_SFTP_007_Keys(t *testing.T) {
	type want struct {
		side   app.Side
		dir    string
		cursor string
	}
	for _, tc := range []struct {
		name string
		keys []string
		want want
	}{
		{"j", []string{"j"}, want{app.Remote, "/home/pi", "scripts"}},
		{"down", []string{"down"}, want{app.Remote, "/home/pi", "scripts"}},
		{"k", []string{"j", "j", "k"}, want{app.Remote, "/home/pi", "scripts"}},
		{"up", []string{"j", "up"}, want{app.Remote, "/home/pi", ".."}},
		{"G", []string{"G"}, want{app.Remote, "/home/pi", "backup.tar.gz"}},
		{"end", []string{"end"}, want{app.Remote, "/home/pi", "backup.tar.gz"}},
		{"g", []string{"G", "g"}, want{app.Remote, "/home/pi", ".."}},
		{"home", []string{"G", "home"}, want{app.Remote, "/home/pi", ".."}},
		{"pgdown", []string{"pgdown"}, want{app.Remote, "/home/pi", "backup.tar.gz"}},
		{"pgup", []string{"G", "pgup"}, want{app.Remote, "/home/pi", ".."}},
		{"tab", []string{"tab"}, want{app.Local, "/Users/me/Downloads", ".."}},
		{"tab twice", []string{"j", "tab", "tab"}, want{app.Remote, "/home/pi", "scripts"}},
		{"enter dir", []string{"j", "enter"}, want{app.Remote, "/home/pi/scripts", ".."}},
		{"enter ..", []string{"enter"}, want{app.Remote, "/home", "pi"}},
		{"enter file", []string{"G", "enter"}, want{app.Remote, "/home/pi", "backup.tar.gz"}},
		{"h", []string{"j", "enter", "h"}, want{app.Remote, "/home/pi", "scripts"}},
		{"backspace", []string{"j", "enter", "backspace"}, want{app.Remote, "/home/pi", "scripts"}},
		{"~ remote", []string{"j", "enter", "~"}, want{app.Remote, "/home/pi", ".."}},
		{"~ local", []string{"tab", "~"}, want{app.Local, "/Users/me", ".."}},
		{"? help", []string{"?", "esc"}, want{app.Remote, "/home/pi", ".."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := approvedTab(t)
			d.keys(tc.keys...)
			side, dir, cur := where(t, d)
			if got := (want{side, dir, cur}); got != tc.want {
				t.Fatalf("after %q: %+v, want %+v", tc.keys, got, tc.want)
			}
		})
	}

	t.Run("unreadable dir keeps the listing", func(t *testing.T) {
		d, b := approvedTab(t)
		b.SetErr(app.Remote, "/home/pi/scripts", apptest.ErrFake)
		d.keys("j", "enter")
		if _, dir, cur := where(t, d); dir != "/home/pi" || cur != "scripts" {
			t.Fatalf("dir %q cursor %q", dir, cur)
		}
		if got := strings.TrimSpace(viewLine(d, 22)); got != "✕ cannot open /home/pi/scripts: permission denied" {
			t.Fatalf("progress line = %q", got)
		}
	})

	t.Run("each pane keeps its cursor", func(t *testing.T) {
		d, _ := approvedTab(t)
		d.keys("G", "tab", "j", "tab")
		v := focusedView(t, d)
		if v.Cursor(app.Remote) != "backup.tar.gz" || v.Cursor(app.Local) != "photos" {
			t.Fatalf("cursors %q / %q", v.Cursor(app.Local), v.Cursor(app.Remote))
		}
	})
}

func TestFR_SFTP_007_ParentKeepsCursor(t *testing.T) {
	d, _ := approvedTab(t)
	d.keys("tab", "j", "enter") // into ~/Downloads/photos
	if _, dir, _ := where(t, d); dir != "/Users/me/Downloads/photos" {
		t.Fatalf("dir = %q", dir)
	}
	d.keys("h")
	if _, dir, cur := where(t, d); dir != "/Users/me/Downloads" || cur != "photos" {
		t.Fatalf("after h: %q, cursor %q", dir, cur)
	}
}

func TestFR_SFTP_007_SymlinkDirOpensLinkPath(t *testing.T) {
	b := approvedBrowser(1)
	b.SetDir(app.Remote, "/home/pi", "/home/pi", dir("scripts"), link("www"), link("note"))
	b.SetStat(app.Remote, "/home/pi/www", dir("www"))
	b.SetStat(app.Remote, "/home/pi/note", file("note", 5))
	b.SetDir(app.Remote, "/home/pi/www", "/home/pi/www", file("index.html", 10))
	d, _ := sftpOpen(t, b)
	d.keys("G", "enter") // www@, after note@
	if _, dir, _ := where(t, d); dir != "/home/pi/www" {
		t.Fatalf("enter on a link to a dir opened %q, want the link's path", dir)
	}
	d.keys("h")
	if _, dir, cur := where(t, d); dir != "/home/pi" || cur != "www" {
		t.Fatalf("after h: %q, cursor %q", dir, cur)
	}
	d.keys("k", "enter") // note@ points to a file: nothing
	if _, dir, cur := where(t, d); dir != "/home/pi" || cur != "note" {
		t.Fatalf("enter on a link to a file: %q, cursor %q", dir, cur)
	}
}

func TestFR_SFTP_008_HiddenToggle(t *testing.T) {
	d, _ := approvedTab(t)
	b := approvedBrowser(1)
	_ = b
	if got := rowNames(t, d, app.Remote); !slices.Contains(got, ".bashrc") {
		t.Fatalf("hidden files not shown at open: %q", got)
	}
	d.keys(".")
	if got := rowNames(t, d, app.Remote); slices.Contains(got, ".bashrc") {
		t.Fatalf(". did not hide .bashrc: %q", got)
	}
	if l := viewLine(d, 1); strings.Count(l, "(hidden off)") != 2 {
		t.Fatalf("titles lack (hidden off): %q", l)
	}
	d.keys(".")
	if got := rowNames(t, d, app.Remote); !slices.Contains(got, ".bashrc") || strings.Contains(viewLine(d, 1), "hidden off") {
		t.Fatalf("second . did not show them again: %q", got)
	}
}

func TestFR_SFTP_008_FilterSubstring(t *testing.T) {
	d, _ := approvedTab(t)
	d.keys("tab", "j", "j", "space") // mark notes.txt
	d.keys("/", "R", "E", "p")
	if got := rowNames(t, d, app.Local); !slices.Equal(got, []string{"..", "report.pdf"}) {
		t.Fatalf("filter REp shows %q", got)
	}
	bottom := viewLine(d, 21)
	if !strings.Contains(bottom, "/ REp_") || !strings.Contains(bottom, "1 marked") {
		t.Fatalf("bottom border = %q", bottom)
	}
	d.keys("enter", "j") // keep the filter, keys go to the list again
	if _, _, cur := where(t, d); cur != "report.pdf" {
		t.Fatalf("cursor = %q after enter, j", cur)
	}
	if !strings.Contains(viewLine(d, 21), "/ REp ") {
		t.Fatalf("kept filter not shown: %q", viewLine(d, 21))
	}
	d.keys("esc")
	if got := rowNames(t, d, app.Local); len(got) != 4 {
		t.Fatalf("esc did not clear the filter: %q", got)
	}
	d.keys("/", "p", "h", "o", "enter", "g", "j", "enter") // into photos/: the filter is cleared
	if _, dir, _ := where(t, d); dir != "/Users/me/Downloads/photos" || strings.Contains(viewLine(d, 21), "/ ") {
		t.Fatalf("dir %q, bottom %q", dir, viewLine(d, 21))
	}
}

func TestFR_SFTP_009_MarkMovesDown(t *testing.T) {
	d, _ := approvedTab(t)
	d.keys("space") // ".." cannot be marked
	if got := focusedView(t, d).Marked(app.Remote); len(got) != 0 {
		t.Fatalf("marked %q on ..", got)
	}
	d.keys("j", "space")
	v := focusedView(t, d)
	if got := v.Marked(app.Remote); !slices.Equal(got, []string{"scripts"}) || v.Cursor(app.Remote) != ".bashrc" {
		t.Fatalf("marked %q, cursor %q", got, v.Cursor(app.Remote))
	}
	if l := viewLine(d, 3); !strings.Contains(l, "│ * scripts/") {
		t.Fatalf("marked row lacks *: %q", l)
	}
	d.keys("k", "space") // unmark
	if got := focusedView(t, d).Marked(app.Remote); len(got) != 0 {
		t.Fatalf("still marked: %q", got)
	}
}

func TestFR_SFTP_009_MarksDroppedOnCd(t *testing.T) {
	d, b := approvedTab(t)
	d.keys("j", "space", "space") // scripts and .bashrc
	d.keys("g", "j", "enter", "h")
	if got := focusedView(t, d).Marked(app.Remote); len(got) != 0 {
		t.Fatalf("marks kept across a directory change: %q", got)
	}
	d.keys("G", "space") // backup.tar.gz
	b.SetDir(app.Remote, "/home/pi", "/home/pi", dir("scripts"))
	d.keys("R")
	if got := focusedView(t, d).Marked(app.Remote); len(got) != 0 {
		t.Fatalf("mark of a deleted entry kept: %q", got)
	}
}

func TestFR_SFTP_013_RefreshKeepsCursor(t *testing.T) {
	d, b := approvedTab(t)
	d.keys("G")
	b.SetDir(app.Remote, "/home/pi", "/home/pi", dir("scripts"), file("aaa", 1), file("backup.tar.gz", 840_000_000), file(".bashrc", 4_000))
	if got := rowNames(t, d, app.Remote); slices.Contains(got, "aaa") {
		t.Fatal("listing changed without R")
	}
	lists := b.Lists()
	d.keys("R")
	if got := rowNames(t, d, app.Remote); !slices.Contains(got, "aaa") {
		t.Fatalf("R did not show the new file: %q", got)
	}
	if _, _, cur := where(t, d); cur != "backup.tar.gz" {
		t.Fatalf("cursor = %q after R", cur)
	}
	if n := b.Lists() - lists; n != 2 {
		t.Fatalf("R listed %d dirs, want both panes", n)
	}
}

func TestFR_TAB_003_M3_SftpLabel(t *testing.T) {
	d, _ := approvedTab(t)
	if h := headerLine(d); h != "sshelf │ Hosts │ 1 raspberrypi4 │[2 sftp: raspberrypi4]│" {
		t.Fatalf("header = %q", h)
	}
	long := []domain.Host{{Meta: domain.Meta{ID: "pi"}, Label: "a-very-long-host-label\x1b[31m", Hostname: "h"}}
	d2, _ := sftpDriver(t, long, nil, approvedBrowser(2))
	d2.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	openSFTP(t, d2, "pi")
	if h := headerLine(d2); h != "sshelf │ Hosts │[1 sftp: a-very-long-h…]│" {
		t.Fatalf("header = %q", h)
	}
}

func TestFR_TAB_005_M3_PrefixS(t *testing.T) {
	t.Run("live session opens on its connection", func(t *testing.T) {
		s := apptest.NewFakeSession("$ ")
		s.SetConnID(7)
		d, svc := sftpDriver(t, piHosts, []*apptest.FakeSession{s}, approvedBrowser(7))
		d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
		connectHost(t, d, "pi")
		d.prefix("s")
		if len(svc.openedOn) != 1 || svc.openedOn[0] != app.Session(s) || len(svc.opened) != 0 {
			t.Fatalf("OpenSFTPOn %v, OpenSFTP %v", svc.openedOn, svc.opened)
		}
		if len(svc.connected) != 1 {
			t.Fatalf("dialled again: %v", svc.connected)
		}
		if st := d.root().focusedSFTP(); st == nil || st.num != 2 {
			t.Fatal("the new SFTP tab is not focused")
		}
		if s.Sent() {
			t.Fatal("prefix keys reached the session")
		}
		// On an SFTP tab, Ctrl+] s and Ctrl+] [ do nothing.
		d.prefix("s")
		d.prefix("[")
		if len(svc.openedOn) != 1 || len(d.root().tabs) != 2 || d.root().focus != 2 {
			t.Fatalf("prefix on the SFTP tab acted: %d opens, focus %d", len(svc.openedOn), d.root().focus)
		}
	})
	t.Run("hosts tab does nothing", func(t *testing.T) {
		d, svc := sftpDriver(t, piHosts, nil)
		d.prefix("s")
		if len(svc.openedOn)+len(svc.opened) != 0 || len(d.root().tabs) != 0 {
			t.Fatal("Ctrl+] s acted on the Hosts tab")
		}
	})
	t.Run("ended session connects", func(t *testing.T) {
		s := apptest.NewFakeSession("$ ")
		d, svc := sftpDriver(t, piHosts, []*apptest.FakeSession{s}, approvedBrowser(9))
		d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
		connectHost(t, d, "pi")
		s.End(&session.ExitError{Status: 1})
		d.pump(1)
		d.prefix("s")
		if len(svc.opened) != 1 || svc.opened[0] != "pi" || len(svc.openedOn) != 0 {
			t.Fatalf("OpenSFTP %v, OpenSFTPOn %v", svc.opened, svc.openedOn)
		}
		if d.root().focusedSFTP() == nil {
			t.Fatal("no SFTP tab")
		}
	})
	t.Run("refused subsystem", func(t *testing.T) {
		s := apptest.NewFakeSession("$ ")
		d, svc := sftpDriver(t, piHosts, []*apptest.FakeSession{s})
		svc.sftpErr = app.ErrNoSFTP
		d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
		connectHost(t, d, "pi")
		d.prefix("s")
		if len(d.root().tabs) != 1 || d.root().focused() == nil {
			t.Fatal("a tab opened, or the session lost focus")
		}
		if got := statusBar(d); got != "✕ the server does not offer SFTP" {
			t.Fatalf("status bar = %q", got)
		}
		d.clock = d.clock.Add(5 * time.Second)
		if got := statusBar(d); strings.Contains(got, "SFTP") {
			t.Fatalf("notice still shown after 5 s: %q", got)
		}
	})
	t.Run("help lists s", func(t *testing.T) {
		if !strings.Contains(help.New().View(), "sftp tab") {
			t.Fatal("prefix help lacks s")
		}
	})
}

func TestFR_SFTP_003_FocusesExistingTab(t *testing.T) {
	a, b2, c := apptest.NewFakeSession("A"), apptest.NewFakeSession("B"), apptest.NewFakeSession("C")
	a.SetConnID(7)
	b2.SetConnID(7) // another session tab on the same connection
	c.SetConnID(8)
	d, svc := sftpDriver(t, piHosts, []*apptest.FakeSession{a, b2, c}, approvedBrowser(7), approvedBrowser(8))
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "pi") // 1
	connectHost(t, d, "pi") // 2
	connectHost(t, d, "pi") // 3
	d.prefix("1")
	d.prefix("s") // 4: sftp on connection 7
	if st := d.root().focusedSFTP(); st == nil || st.num != 4 {
		t.Fatal("no SFTP tab 4")
	}
	d.prefix("1")
	d.prefix("s")
	d.prefix("2")
	d.prefix("s")
	if len(svc.openedOn) != 1 {
		t.Fatalf("OpenSFTPOn called %d times; the existing tab should be focused", len(svc.openedOn))
	}
	if st := d.root().focusedSFTP(); st == nil || st.num != 4 {
		t.Fatal("the existing SFTP tab is not focused")
	}
	d.prefix("3")
	d.prefix("s") // a different connection gets its own tab
	if st := d.root().focusedSFTP(); st == nil || st.num != 5 || len(svc.openedOn) != 2 {
		t.Fatalf("connection 8: opens %d", len(svc.openedOn))
	}
	// Ctrl+] q on a session whose connection an SFTP tab shares names it
	// (FR-CONN-014 M3 revision).
	d.prefix("2")
	d.prefix("q")
	if v := plain(d.view()); !strings.Contains(v, "Close this raspberrypi4 session? SFTP tab 4 stays connected.") {
		t.Fatalf("question:\n%s", v)
	}
}

func TestARCH_SFTP_007_SharedNumbering(t *testing.T) {
	b := approvedBrowser(1)
	d, _ := sftpDriver(t, testHosts, []*apptest.FakeSession{apptest.NewFakeSession("A"), apptest.NewFakeSession("B")}, b, approvedBrowser(2))
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	connectHost(t, d, "h1") // 1
	openSFTP(t, d, "h2")    // 2
	connectHost(t, d, "h3") // 3
	if h := headerLine(d); !strings.Contains(h, " 1 db-staging │ 2 sftp: web-prod │[3 zeta]│") {
		t.Fatalf("header = %q", h)
	}
	d.prefix("2")
	if st := d.root().focusedSFTP(); st == nil || st.num != 2 {
		t.Fatal("Ctrl+] 2 did not focus the SFTP tab")
	}
	d.prefix("q")
	d.keys("y")
	if b.Closes() != 1 || len(d.root().tabs) != 2 || d.root().focus != 1 {
		t.Fatalf("closes %d, tabs %d, focus %d", b.Closes(), len(d.root().tabs), d.root().focus)
	}
	openSFTP(t, d, "h2")
	if st := d.root().focusedSFTP(); st == nil || st.num != 4 {
		t.Fatal("numbers were reused")
	}
}

func TestARCH_UI_004_SftpBindingsHaveHelp(t *testing.T) {
	overlay := plain(help.NewList("SFTP tab keys", keys.SFTPPaneKeys()).View())
	for _, m := range keys.Maps() {
		for _, b := range m.Bindings {
			if h := b.Help(); h.Key == "" || h.Desc == "" {
				t.Errorf("%s: %v has no help", m.Name, b.Keys())
			}
		}
	}
	for _, b := range keys.SFTPPaneKeys() {
		if !strings.Contains(overlay, b.Help().Desc) {
			t.Errorf("SFTP help lacks %q", b.Help().Desc)
		}
	}
	for _, k := range []string{"up", "k", "down", "j", "pgup", "pgdown", "g", "G", "tab", "enter", "backspace", "h", "~", "space", "c", "r", "d", "m", "e", "t", "R", ".", "/", "?"} {
		if !slices.ContainsFunc(keys.SFTPPaneKeys(), func(b key.Binding) bool { return slices.Contains(b.Keys(), k) }) {
			t.Errorf("no SFTP binding for %q", k)
		}
	}
}
