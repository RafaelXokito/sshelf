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
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// sftpService is a fakeService that also opens SFTP tabs: OpenSFTP and
// OpenSFTPOn return the next of browsers, or sftpErr.
type sftpService struct {
	*fakeService
	browsers  []*apptest.FakeBrowser
	sftpErr   error
	opened    []domain.ID   // hosts passed to OpenSFTP
	openedOn  []app.Session // sessions passed to OpenSFTPOn
	reconnErr error
	reconns   int
}

func (s *sftpService) next() (app.Browser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sftpErr != nil {
		return nil, s.sftpErr
	}
	if len(s.browsers) == 0 {
		panic("no fake browser left")
	}
	b := s.browsers[0]
	s.browsers = s.browsers[1:]
	b.Check = s.checkGoroutine
	return b, nil
}

func (s *sftpService) OpenSFTP(_ context.Context, id domain.ID) (app.Browser, error) {
	s.checkGoroutine("OpenSFTP")
	s.mu.Lock()
	s.opened = append(s.opened, id)
	s.mu.Unlock()
	return s.next()
}

func (s *sftpService) OpenSFTPOn(sess app.Session, _ domain.ID) (app.Browser, error) {
	s.checkGoroutine("OpenSFTPOn")
	s.mu.Lock()
	s.openedOn = append(s.openedOn, sess)
	s.mu.Unlock()
	return s.next()
}

func (s *sftpService) ReconnectSFTP(_ context.Context, b app.Browser) error {
	s.checkGoroutine("ReconnectSFTP")
	s.mu.Lock()
	s.reconns++
	err := s.reconnErr
	s.mu.Unlock()
	if err == nil {
		b.(*apptest.FakeBrowser).Reattach()
	}
	return err
}

var _ app.Service = (*sftpService)(nil)

// piHosts is the approved layout's host.
var piHosts = []domain.Host{{Meta: domain.Meta{ID: "pi"}, Label: "raspberrypi4", Hostname: "192.168.1.20", User: "pi"}}

// fileTime is the fake files' modification time.
var fileTime = time.Date(2026, 10, 29, 10, 12, 0, 0, time.UTC)

func file(name string, size int64) app.Entry {
	return app.Entry{Name: name, Display: name, Kind: app.KindFile, Size: size, Mode: 0o644, ModTime: fileTime}
}

func dir(name string) app.Entry {
	return app.Entry{Name: name, Display: name, Kind: app.KindDir, Size: 4096, Mode: 0o755, ModTime: fileTime}
}

func link(name string) app.Entry {
	return app.Entry{Name: name, Display: name, Kind: app.KindSymlink, Size: 11, Mode: 0o777, ModTime: fileTime}
}

// approvedBrowser is the approved layout's file systems (FR-SFTP-023):
// ~/Downloads locally, /home/pi remotely.
func approvedBrowser(connID uint64) *apptest.FakeBrowser {
	b := apptest.NewFakeBrowser(connID, "/Users/me/Downloads", "/home/pi")
	b.SetDir(app.Local, "/Users/me/Downloads", "~/Downloads", dir("photos"), file("report.pdf", 1_200_000), file("notes.txt", 3_000))
	b.SetDir(app.Local, "/Users/me/Downloads/photos", "~/Downloads/photos", file("a.jpg", 1), file("b.jpg", 2), file("c.jpg", 3), file("d.jpg", 4))
	b.SetDir(app.Local, "/Users/me", "~", dir("Downloads"), dir("Documents"), file(".zshrc", 900))
	b.SetDir(app.Local, "/Users/me/Documents", "~/Documents")
	b.SetDir(app.Remote, "/home/pi", "/home/pi", dir("scripts"), file("backup.tar.gz", 840_000_000), file(".bashrc", 4_000))
	b.SetCount(app.Remote, "/home/pi/scripts", 12)
	b.SetDir(app.Remote, "/home/pi/scripts", "/home/pi/scripts", file("run.sh", 120))
	b.SetDir(app.Remote, "/home", "/home", dir("pi"))
	b.SetDir(app.Remote, "/", "/", dir("home"), dir("srv"))
	return b
}

// sftpDriver is a driver over an sftpService serving browsers.
func sftpDriver(t *testing.T, hosts []domain.Host, sessions []*apptest.FakeSession, browsers ...*apptest.FakeBrowser) (*driver, *sftpService) {
	t.Helper()
	svc := &sftpService{fakeService: &fakeService{hosts: hosts, framed: sessions}, browsers: browsers}
	d := &driver{t: t, svc: svc.fakeService, svcSFTP: svc, listeners: map[int]tea.Cmd{}, clock: testEpoch}
	m := New(svc)
	m.home = "/Users/me"
	m.now = func() time.Time { return d.clock }
	m.tick = func() tea.Cmd { d.ticks++; return nil }
	m.listen = func(num int, s app.Session) tea.Cmd {
		d.listeners[num] = listenSession(num, s)
		return nil
	}
	m.listenSFTP = func(num, gen int, b app.Browser, watchDone bool, stop <-chan struct{}) tea.Cmd {
		d.listeners[num] = listenBrowser(num, gen, b, watchDone, stop)
		return nil
	}
	m.progressTick = func(num int, b app.Browser) tea.Cmd {
		d.progressTicks++
		d.pendingTick = func() tea.Msg { return progressTickMsg{num: num, transfers: b.Transfers()} }
		return nil
	}
	d.m = m
	d.run(m.Init())
	return d, svc
}

// openSFTP presses s on host id in the host list.
func openSFTP(t *testing.T, d *driver, id domain.ID) {
	t.Helper()
	d.focus(0)
	l, ok := d.root().screen.(hostlist.Model)
	if !ok {
		t.Fatalf("the Hosts tab shows %T", d.root().screen)
	}
	m := d.root()
	m.screen = l.Select(id)
	d.m = m
	d.press('s', "s")
}

// keys presses each key: a rune, or a named key such as "tab".
func (d *driver) keys(ks ...string) {
	d.t.Helper()
	named := map[string]rune{"tab": tea.KeyTab, "enter": tea.KeyEnter, "esc": tea.KeyEscape, "backspace": tea.KeyBackspace,
		"up": tea.KeyUp, "down": tea.KeyDown, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd, "space": tea.KeySpace}
	for _, k := range ks {
		switch {
		case k == "ctrl+]":
			d.send(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
		case k == "space":
			d.send(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
		case named[k] != 0:
			d.press(named[k], "")
		default:
			r := []rune(k)[0]
			d.press(r, k)
		}
	}
}

// sftpTabAt returns tab number num's SFTP state.
func sftpTabAt(t *testing.T, d *driver, num int) *sftpTab {
	t.Helper()
	m := d.root()
	i := m.tabIndex(num)
	if i < 0 || m.tabs[i].sftp == nil {
		t.Fatalf("tab %d is not an SFTP tab", num)
	}
	return m.tabs[i].sftp
}

// viewLine returns line i of the plain view.
func viewLine(d *driver, i int) string {
	return strings.Split(plain(d.view()), "\n")[i]
}
