//go:build unix

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
	"github.com/RafaelXokito/sshelf/internal/ui"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// stillTerm is a session.Terminal that never resizes and needs no TTY.
type stillTerm struct{}

func (stillTerm) MakeRaw() (func() error, error)     { return func() error { return nil }, nil }
func (stillTerm) Size() (int, int, error)            { return 80, 24, nil }
func (stillTerm) Resizes() (<-chan struct{}, func()) { return nil, func() {} }

// startModel runs the root model's Init as Bubble Tea would. Since T-023,
// Init returns a batch of the config load and the asynchronous
// credential-store probe (D-101), so each command runs on its own goroutine
// and its result is fed back in arrival order. startModel returns once
// LoadedMsg has been handled, with a channel that yields the remaining
// startup messages and is closed when every command has finished.
func startModel(t *testing.T, svc app.Service) (tea.Model, <-chan tea.Msg) {
	t.Helper()
	var m tea.Model = ui.New(svc)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	var cmds []tea.Cmd
	switch msg := m.Init()().(type) {
	case tea.BatchMsg:
		cmds = msg
	default:
		cmds = []tea.Cmd{func() tea.Msg { return msg }}
	}
	msgs := make(chan tea.Msg, len(cmds))
	var wg sync.WaitGroup
	for _, c := range cmds {
		if c == nil {
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); msgs <- c() }()
	}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case msg := <-msgs:
			m, _ = m.Update(msg)
			if _, ok := msg.(ui.LoadedMsg); ok {
				rest := make(chan tea.Msg, len(cmds))
				go func() {
					wg.Wait()
					close(msgs)
					for msg := range msgs {
						rest <- msg
					}
					close(rest)
				}()
				return m, rest
			}
		case <-timeout:
			t.Fatal("startup load did not finish within 10 s")
		}
	}
}

// loadModel starts the root model and feeds back every startup message,
// the credential-store probe result included.
func loadModel(t *testing.T, svc app.Service) tea.Model {
	t.Helper()
	m, rest := startModel(t, svc)
	for msg := range rest {
		m, _ = m.Update(msg)
	}
	return m
}

// heldProbe is an app.Service whose credential-store probe does not return
// until release is closed, standing in for a slow keychain.
type heldProbe struct {
	app.Service
	release chan struct{}
}

func (h heldProbe) SecretsAvailable(ctx context.Context) error {
	select {
	case <-h.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return h.Service.SecretsAvailable(ctx)
}

// TestNFR_PERF_001_NoSecretReadAtStartup starts the root model against the
// 1,000-host fixture with a MemStore and checks that the host list shows
// without any read from the credential store, and while the asynchronous
// credential-store probe (D-101) is still outstanding.
func TestNFR_PERF_001_NoSecretReadAtStartup(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	first := writeFixture1k(t, xdg)
	store := secrets.NewMemStore()
	svc := heldProbe{
		Service: app.New(app.Config{Dir: filepath.Join(xdg, "sshelf"), Secrets: store, Home: home, Terminal: stillTerm{}}),
		release: make(chan struct{}),
	}
	m, rest := startModel(t, svc)
	if v := m.View().Content; !strings.Contains(v, first) {
		t.Fatalf("host list lacks %s while the store probe is pending:\n%s", first, v)
	}
	close(svc.release)
	for msg := range rest {
		m, _ = m.Update(msg)
	}
	if v := m.View().Content; !strings.Contains(v, first) {
		t.Fatalf("host list lacks %s after the store probe:\n%s", first, v)
	}
	if n := store.Calls(secrets.OpAvailable); n != 1 {
		t.Errorf("%d store probes at startup, want 1", n)
	}
	if n := store.Calls(secrets.OpGet); n != 0 {
		t.Fatalf("%d secret reads at startup, want 0", n)
	}
}

// bannerProxy forwards one TCP port to target and records when each
// connection's first bytes, the client's version string, arrive.
type bannerProxy struct {
	ln     net.Listener
	target string

	mu      sync.Mutex
	banners []time.Time
	wg      sync.WaitGroup
}

func newBannerProxy(t *testing.T, target string) *bannerProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &bannerProxy{ln: ln, target: target}
	p.wg.Add(1)
	go p.accept()
	t.Cleanup(func() { _ = ln.Close(); p.wg.Wait() })
	return p
}

func (p *bannerProxy) accept() {
	defer p.wg.Done()
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.wg.Add(1)
		go func() { defer p.wg.Done(); p.serve(c) }()
	}
}

func (p *bannerProxy) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	buf := make([]byte, 256)
	n, err := c.Read(buf)
	at := time.Now()
	if err != nil || !bytes.HasPrefix(buf[:n], []byte("SSH-2.0-")) {
		return
	}
	p.mu.Lock()
	p.banners = append(p.banners, at)
	p.mu.Unlock()
	up, err := net.Dial("tcp", p.target)
	if err != nil {
		return
	}
	defer func() { _ = up.Close() }()
	if _, err := up.Write(buf[:n]); err != nil {
		return
	}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(c, up); _ = c.Close(); close(done) }()
	_, _ = io.Copy(up, c)
	_ = up.Close()
	<-done
}

func (p *bannerProxy) banner(i int) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i < len(p.banners) {
		return p.banners[i], true
	}
	return time.Time{}, false
}

func (p *bannerProxy) port() int { return p.ln.Addr().(*net.TCPAddr).Port }

// TestNFR_PERF_003_ConnectOverhead drives the root model with a trusted
// key-file host at 127.0.0.1 and a MemStore, and measures the time from
// ConnectRequestMsg to the client's version string reaching the server: the
// median of 10 runs must be at most 100 ms. A proxy in front of the test
// server timestamps the banner as it arrives.
func TestNFR_PERF_003_ConnectOverhead(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	if raceEnabled {
		t.Skip("timing limit not meaningful under -race; CI runs it in the perf step")
	}
	home, xdg := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(filepath.Join(home, ".ssh", "id_ed25519"), sshtest.Ed25519, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("demo", pub))
	proxy := newBannerProxy(t, srv.Addr())
	addr := "127.0.0.1:" + strconv.Itoa(proxy.port())

	dir := filepath.Join(xdg, "sshelf")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var kh strings.Builder
	for _, k := range srv.HostKeys() {
		kh.WriteString(knownhosts.Line([]string{knownhosts.Normalize(addr)}, k) + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "known_hosts"), []byte(kh.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"version": 1,
  "identities": [{"id": "0192a000-0000-7000-8000-000000000001", "name": "demo", "key_path": "~/.ssh/id_ed25519"}],
  "hosts": [{"id": "0192a000-0000-7000-8000-000000000002", "label": "web-prod", "hostname": "127.0.0.1",
             "port": ` + strconv.Itoa(proxy.port()) + `, "user": "demo", "identity_id": "0192a000-0000-7000-8000-000000000001"}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	store := secrets.NewMemStore()
	svc := app.New(app.Config{Dir: dir, KnownHostsFiles: []string{filepath.Join(dir, "known_hosts")}, Secrets: store, Home: home, Terminal: stillTerm{}})
	// Progress messages are dropped; a prompt would block Connect and time out.
	svc.SetSend(func(any) {})

	const runs = 10
	times := make([]time.Duration, runs)
	for i := range times {
		m := loadModel(t, svc)
		start := time.Now()
		_, cmd := m.Update(hostlist.ConnectRequestMsg{HostID: "0192a000-0000-7000-8000-000000000002", Label: "web-prod"})
		if cmd == nil {
			t.Fatal("ConnectRequestMsg returned no command")
		}
		result := make(chan tea.Msg, 1)
		go func() { result <- cmd() }()
		var msg tea.Msg
		select {
		case msg = <-result:
		case <-time.After(10 * time.Second):
			t.Fatal("connect did not finish within 10 s")
		}
		at, ok := proxy.banner(i)
		if !ok {
			t.Fatalf("run %d: no client version reached the server; result %#v", i, msg)
		}
		times[i] = at.Sub(start)
		connected, ok := msg.(ui.ConnectedMsg)
		if !ok {
			t.Fatalf("run %d: connect result %#v", i, msg)
		}
		_ = connected.Session.Close()
	}
	if n := store.Calls(secrets.OpGet); n != 0 {
		t.Errorf("%d secret reads for a key-file host", n)
	}
	slices.Sort(times)
	median := (times[runs/2-1] + times[runs/2]) / 2
	const limit = 100 * time.Millisecond
	if median > limit {
		t.Fatalf("connect overhead: median %v of %v, want at most %v", median, times, limit)
	}
	t.Logf("connect overhead: median %v of %v", median, times)
	if got := len(srv.ClientVersions()); got != runs {
		t.Errorf("server saw %d authenticated connections, want %d", got, runs)
	}
}
