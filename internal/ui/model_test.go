package ui

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/session"
	"github.com/RafaelXokito/sshelf/internal/testutil"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }

// goroutineID returns the current goroutine's ID. Tests only.
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	f := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))
	id, _ := strconv.ParseUint(f[0], 10, 64)
	return id
}

// fakeService is an app.Service that panics when it is called on the
// goroutine that runs Update (ARCH-UI-002).
type fakeService struct {
	hosts      []domain.Host
	notices    []string
	loadErr    error
	connectErr error
	updateGID  atomic.Uint64

	identities []domain.Identity
	saveErr    error // returned by SaveHost instead of saving

	fakeIdentities // identities_test.go

	// framed are the sessions the next Connect calls return, in order;
	// after them Connect returns a new FakeSession showing a prompt.
	framed []*apptest.FakeSession

	mu        sync.Mutex
	connected []domain.ID
	sizes     []app.Size    // the session area passed to each Connect
	saves     []domain.Host // hosts passed to SaveHost
}

// SaveHost mimics app.App.SaveHost in memory.
func (f *fakeService) SaveHost(_ context.Context, h domain.Host) (app.SavedHost, error) {
	f.checkGoroutine("SaveHost")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves = append(f.saves, h)
	if f.saveErr != nil {
		return app.SavedHost{}, f.saveErr
	}
	hosts := slices.Clone(f.hosts)
	if h.ID == "" {
		h.ID = domain.NewID()
		hosts = append(hosts, h)
	} else {
		i := slices.IndexFunc(hosts, func(o domain.Host) bool { return o.ID == h.ID })
		if i < 0 {
			return app.SavedHost{}, app.ErrHostNotFound
		}
		hosts[i] = h
	}
	if err := domain.ValidateHost(h, domain.Data{Hosts: hosts, Identities: f.identities}); err != nil {
		return app.SavedHost{}, err
	}
	slices.SortFunc(hosts, func(a, b domain.Host) int { return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label)) })
	f.hosts = hosts
	return app.SavedHost{Host: h, Hosts: slices.Clone(hosts)}, nil
}

func (f *fakeService) checkGoroutine(method string) {
	if goroutineID() == f.updateGID.Load() {
		panic("app.Service." + method + " called on the Update goroutine")
	}
}

func (f *fakeService) Load(context.Context) (app.Loaded, error) {
	f.checkGoroutine("Load")
	if f.loadErr != nil {
		return app.Loaded{}, f.loadErr
	}
	return app.Loaded{Hosts: f.hosts, Identities: f.identities, Notices: f.notices}, nil
}

func (f *fakeService) Connect(_ context.Context, id domain.ID, size app.Size) (app.Session, error) {
	f.checkGoroutine("Connect")
	f.mu.Lock()
	f.connected = append(f.connected, id)
	f.sizes = append(f.sizes, size)
	var framed *apptest.FakeSession
	if len(f.framed) > 0 {
		framed, f.framed = f.framed[0], f.framed[1:]
	}
	f.mu.Unlock()
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	if framed == nil {
		framed = apptest.NewFakeSession("$ ")
	}
	return framed, nil
}

// OpenSFTP is refused: sftpService opens SFTP tabs.
func (f *fakeService) OpenSFTP(context.Context, domain.ID) (app.Browser, error) {
	f.checkGoroutine("OpenSFTP")
	return nil, app.ErrNoSFTP
}

// OpenSFTPOn is refused: sftpService opens SFTP tabs.
func (f *fakeService) OpenSFTPOn(app.Session, domain.ID) (app.Browser, error) {
	f.checkGoroutine("OpenSFTPOn")
	return nil, app.ErrNoSFTP
}

// ReconnectSFTP is refused: sftpService opens SFTP tabs.
func (f *fakeService) ReconnectSFTP(context.Context, app.Browser) error {
	f.checkGoroutine("ReconnectSFTP")
	return app.ErrNoSFTP
}

// driver runs the root model the way tea.Program does: Update on one
// goroutine, every command on another.
type driver struct {
	t    testing.TB
	svc  *fakeService
	m    tea.Model
	quit bool

	// Session listeners and the status-bar clock would block a
	// synchronous driver, so they are parked: pump runs a tab's pending
	// listener once its fake session has output or has ended, and ticks
	// counts the clock ticks asked for.
	listeners map[int]tea.Cmd
	ticks     int
	// progressTicks counts the SFTP progress ticks asked for; pendingTick
	// is the last one, delivered by fireTick.
	progressTicks int
	pendingTick   tea.Cmd
	svcSFTP       *sftpService // set by sftpDriver
	clock         time.Time
}

// testEpoch is the driver's clock at the start of every test.
var testEpoch = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

func newDriver(t *testing.T, svc *fakeService) *driver {
	d := &driver{t: t, svc: svc, listeners: map[int]tea.Cmd{}, clock: testEpoch}
	m := New(svc)
	m.now = func() time.Time { return d.clock }
	m.tick = func() tea.Cmd { d.ticks++; return nil }
	m.listen = func(num int, s app.Session) tea.Cmd {
		d.listeners[num] = listenSession(num, s)
		return nil
	}
	d.m = m
	d.run(m.Init())
	return d
}

// pump delivers the pending update or end of session tab num.
func (d *driver) pump(num int) {
	d.t.Helper()
	cmd, ok := d.listeners[num]
	if !ok {
		d.t.Fatalf("no listener for tab %d", num)
	}
	delete(d.listeners, num)
	d.send(cmd())
}

// focus focuses tab i (0 = Hosts) through the model method.
func (d *driver) focus(i int) {
	m, cmd := d.root().focusTab(i)
	d.m = m
	d.run(cmd)
}

// send delivers msg to Update and runs the resulting commands to
// completion.
func (d *driver) send(msg tea.Msg) {
	d.svc.updateGID.Store(goroutineID())
	var cmd tea.Cmd
	d.m, cmd = d.m.Update(msg)
	d.run(cmd)
}

func (d *driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msgs := make(chan tea.Msg, 1)
	go func() { msgs <- cmd() }() // never on the Update goroutine
	switch msg := (<-msgs).(type) {
	case nil:
	case tea.QuitMsg:
		d.quit = true
	case tea.BatchMsg:
		for _, c := range msg {
			d.run(c)
		}
	default:
		d.send(msg)
	}
}

func (d *driver) press(code rune, text string) { d.send(tea.KeyPressMsg{Code: code, Text: text}) }

func (d *driver) view() string { return d.m.View().Content }

func (d *driver) root() Model { return d.m.(Model) }

var testHosts = []domain.Host{
	{Meta: domain.Meta{ID: "h1"}, Label: "db-staging", Hostname: "10.0.1.20", Port: 2222, User: "admin"},
	{Meta: domain.Meta{ID: "h2"}, Label: "web-prod", Hostname: "10.0.0.10", User: "ubuntu"},
	{Meta: domain.Meta{ID: "h3"}, Label: "zeta", Hostname: "10.0.0.30"},
}

func selected(t *testing.T, d *driver) domain.Host {
	t.Helper()
	h, ok := d.root().screen.(hostlist.Model).Selected()
	if !ok {
		t.Fatal("no host selected")
	}
	return h
}

func TestModelNavigatesAndQuits(t *testing.T) {
	d := newDriver(t, &fakeService{hosts: testHosts})
	view := d.view()
	for _, want := range []string{"> db-staging", "admin@10.0.1.20:2222", "ubuntu@10.0.0.10\n"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not contain %q:\n%s", want, view)
		}
	}
	d.press('j', "j")
	d.press(tea.KeyDown, "")
	d.press(tea.KeyDown, "") // already at the bottom
	if h := selected(t, d); h.ID != "h3" {
		t.Fatalf("selected %s after j, down, down; want h3", h.ID)
	}
	d.press('k', "k")
	if h := selected(t, d); h.ID != "h2" {
		t.Fatalf("selected %s after k; want h2", h.ID)
	}
	d.press('q', "q")
	if !d.quit {
		t.Fatal("q did not quit")
	}
}

// connectSecond selects web-prod and presses Enter.
func connectSecond(d *driver) {
	d.press('j', "j")
	d.press(tea.KeyEnter, "")
}

func TestFR_CONN_005_ExitStatusShown(t *testing.T) {
	sess := apptest.NewFakeSession("$ exit 3")
	d := newDriver(t, &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{sess}})
	connectSecond(d)
	sess.End(&session.ExitError{Status: 3})
	d.pump(1)
	if v := plain(d.view()); !strings.Contains(v, "✕ exited (3)") || !strings.Contains(v, "$ exit 3") {
		t.Fatalf("view lacks the exit status or the last screen:\n%s", v)
	}
}

func TestFR_CONN_005_ConnectionLostShown(t *testing.T) {
	sess := apptest.NewFakeSession()
	d := newDriver(t, &fakeService{hosts: testHosts, framed: []*apptest.FakeSession{sess}})
	connectSecond(d)
	sess.End(&session.DisconnectError{Err: errors.New("connection reset by peer")})
	d.pump(1)
	if v := plain(d.view()); !strings.Contains(v, "✕ connection lost: connection reset by peer") {
		t.Fatalf("view lacks the disconnect reason:\n%s", v)
	}
}

func TestARCH_SESS_002_AuthFailureOpensNoTab(t *testing.T) {
	svc := &fakeService{hosts: testHosts, connectErr: errors.New("ssh: handshake failed: ssh: unable to authenticate")}
	d := newDriver(t, svc)
	connectSecond(d)
	if len(d.root().tabs) != 0 {
		t.Fatalf("%d tabs opened after a failed connect", len(d.root().tabs))
	}
	if len(svc.connected) != 1 || svc.connected[0] != "h2" {
		t.Fatalf("Connect calls = %v", svc.connected)
	}
	v := d.view()
	if !strings.Contains(v, "web-prod: ssh: handshake failed: ssh: unable to authenticate") {
		t.Fatalf("view lacks the error:\n%s", v)
	}
	if strings.Contains(v, "Connecting to") || len(d.root().modals) != 0 {
		t.Fatalf("Connecting modal still open:\n%s", v)
	}
	if h := selected(t, d); h.ID != "h2" {
		t.Fatalf("selected %s, want h2", h.ID)
	}
}

func TestARCH_UI_002_NoIOInUpdate(t *testing.T) {
	// The fake panics if Load or Connect runs on the Update goroutine; the
	// driver runs Update on the test goroutine and commands elsewhere.
	svc := &fakeService{hosts: testHosts}
	d := newDriver(t, svc)
	connectSecond(d)
	d.focus(0)
	d.press(tea.KeyEnter, "")
	if len(svc.connected) != 2 || len(d.root().tabs) != 2 {
		t.Fatalf("connects = %d, tabs = %d", len(svc.connected), len(d.root().tabs))
	}

	// And the fake really does panic when called from Update's goroutine.
	defer func() {
		if recover() == nil {
			t.Fatal("fake did not detect a call on the Update goroutine")
		}
	}()
	svc.updateGID.Store(goroutineID())
	_, _ = svc.Load(context.Background())
}

func TestConnectingModalCancel(t *testing.T) {
	block := make(chan struct{})
	svc := &blockingService{fakeService: fakeService{hosts: testHosts}, release: block}
	m := New(svc)
	var tm tea.Model = m
	tm, _ = tm.Update(LoadedMsg{Hosts: testHosts})
	tm, cmd := tm.Update(hostlist.ConnectRequestMsg{HostID: "h2", Label: "web-prod"})
	if !strings.Contains(tm.View().Content, "Connecting to web-prod") {
		t.Fatalf("no Connecting modal:\n%s", tm.View().Content)
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()

	tm, cancelCmd := tm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	tm, _ = tm.Update(cancelCmd())
	if strings.Contains(tm.View().Content, "Connecting to") {
		t.Fatal("modal still open after esc")
	}
	msg := <-result // Connect saw the cancelled context
	failed, ok := msg.(ConnectFailedMsg)
	if !ok || !errors.Is(failed.Err, context.Canceled) {
		t.Fatalf("connect result = %#v", msg)
	}
	tm, _ = tm.Update(msg)
	if !strings.Contains(tm.View().Content, "connection cancelled") {
		t.Fatalf("view:\n%s", tm.View().Content)
	}
	close(block)
}

func TestSEC_FILE_002_NoticeInStatusBar(t *testing.T) {
	notice := "permissions of config.json were too open; set to 0700/0600"
	d := newDriver(t, &fakeService{hosts: testHosts, notices: []string{notice}})
	if v := d.view(); !strings.Contains(v, notice) {
		t.Fatalf("view lacks the notice:\n%s", v)
	}
	connectSecond(d)
	if v := d.view(); strings.Contains(v, notice) {
		t.Fatalf("notice still shown after the next action:\n%s", v)
	}
}

// blockingService's Connect waits for its context to end.
type blockingService struct {
	fakeService
	release chan struct{}
}

func (b *blockingService) Connect(ctx context.Context, _ domain.ID, _ app.Size) (app.Session, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.release:
		return nil, errors.New("released")
	}
}
