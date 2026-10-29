package ui

import (
	"context"
	"errors"
	"io"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
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

// stubSession is an app.Session whose Run returns a fixed result.
type stubSession struct {
	result error
	ran    atomic.Bool
	closed atomic.Bool
}

func (s *stubSession) SetStdin(io.Reader)  {}
func (s *stubSession) SetStdout(io.Writer) {}
func (s *stubSession) SetStderr(io.Writer) {}
func (s *stubSession) Run() error          { s.ran.Store(true); return s.result }
func (s *stubSession) Close() error        { s.closed.Store(true); return nil }

// fakeService is an app.Service that panics when it is called on the
// goroutine that runs Update (ARCH-UI-002).
type fakeService struct {
	hosts      []domain.Host
	notices    []string
	loadErr    error
	connectErr error
	session    *stubSession
	updateGID  atomic.Uint64

	identities []domain.Identity
	saveErr    error // returned by SaveHost instead of saving

	mu        sync.Mutex
	connected []domain.ID
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

func (f *fakeService) Connect(_ context.Context, id domain.ID) (app.Session, error) {
	f.checkGoroutine("Connect")
	f.mu.Lock()
	f.connected = append(f.connected, id)
	f.mu.Unlock()
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	return f.session, nil
}

// driver runs the root model the way tea.Program does: Update on one
// goroutine, every command on another.
type driver struct {
	t     *testing.T
	svc   *fakeService
	m     tea.Model
	execs int
	quit  bool
}

func newDriver(t *testing.T, svc *fakeService) *driver {
	d := &driver{t: t, svc: svc}
	m := New(svc)
	m.exec = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		d.execs++
		return func() tea.Msg { return fn(c.Run()) }
	}
	d.m = m
	d.run(m.Init())
	return d
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
	sess := &stubSession{result: &session.ExitError{Status: 3}}
	d := newDriver(t, &fakeService{hosts: testHosts, session: sess})
	connectSecond(d)
	if !sess.ran.Load() || d.execs != 1 {
		t.Fatalf("session ran = %v, execs = %d", sess.ran.Load(), d.execs)
	}
	if v := d.view(); !strings.Contains(v, "web-prod: exited (3)") {
		t.Fatalf("view lacks the exit status:\n%s", v)
	}

	sess.result = nil
	d.press(tea.KeyEnter, "")
	if v := d.view(); !strings.Contains(v, "web-prod: exited (0)") {
		t.Fatalf("view lacks exit status 0:\n%s", v)
	}
}

func TestFR_CONN_005_ConnectionLostShown(t *testing.T) {
	sess := &stubSession{result: &session.DisconnectError{Err: errors.New("connection reset by peer")}}
	d := newDriver(t, &fakeService{hosts: testHosts, session: sess})
	connectSecond(d)
	if v := d.view(); !strings.Contains(v, "web-prod: connection lost: connection reset by peer") {
		t.Fatalf("view lacks the disconnect reason:\n%s", v)
	}
}

func TestFR_CONN_005_SameHostSelected(t *testing.T) {
	sess := &stubSession{result: &session.ExitError{Status: 1}}
	d := newDriver(t, &fakeService{hosts: testHosts, session: sess})
	connectSecond(d)
	if h := selected(t, d); h.ID != "h2" {
		t.Fatalf("selected %s after the session, want h2 (web-prod)", h.ID)
	}
	if v := d.view(); !strings.Contains(v, "> web-prod") {
		t.Fatalf("web-prod not highlighted:\n%s", v)
	}
	if len(d.root().modals) != 0 {
		t.Fatal("a modal is still open after the session")
	}
}

func TestARCH_SESS_002_AuthFailureNeverExecs(t *testing.T) {
	svc := &fakeService{hosts: testHosts, connectErr: errors.New("ssh: handshake failed: ssh: unable to authenticate")}
	d := newDriver(t, svc)
	connectSecond(d)
	if d.execs != 0 {
		t.Fatalf("tea.Exec called %d times after a failed connect", d.execs)
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
	sess := &stubSession{}
	svc := &fakeService{hosts: testHosts, session: sess}
	d := newDriver(t, svc)
	connectSecond(d)
	d.press(tea.KeyEnter, "")
	if len(svc.connected) != 2 || !sess.ran.Load() {
		t.Fatalf("connects = %d, ran = %v", len(svc.connected), sess.ran.Load())
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
	d := newDriver(t, &fakeService{hosts: testHosts, notices: []string{notice}, session: &stubSession{}})
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

func (b *blockingService) Connect(ctx context.Context, _ domain.ID) (app.Session, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.release:
		return nil, errors.New("released")
	}
}
