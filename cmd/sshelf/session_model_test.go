//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
	"github.com/RafaelXokito/sshelf/internal/ui"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// The root model driven directly, as tea.Program would, with real
// sessions connected to the in-process server (T-037).

// liveModel is the root model with an app.App whose one key-file host,
// web-prod, points at srv.
type liveModel struct {
	t   *testing.T
	m   tea.Model
	srv *sshtest.Server
}

const liveHostID = "0192a000-0000-7000-8000-000000000002"

func newLiveModel(t *testing.T) *liveModel {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(filepath.Join(home, ".ssh", "id_ed25519"), sshtest.Ed25519, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("demo", pub))
	kh := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(kh, []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"version": 1,
  "identities": [{"id": "0192a000-0000-7000-8000-000000000001", "name": "demo", "key_path": "~/.ssh/id_ed25519"}],
  "hosts": [{"id": %q, "label": "web-prod", "hostname": "127.0.0.1", "port": %d, "user": "demo",
             "identity_id": "0192a000-0000-7000-8000-000000000001"}]}`, liveHostID, srv.Port())
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := app.New(app.Config{Dir: dir, KnownHostsFiles: []string{kh}, Secrets: secrets.NewMemStore(), Home: home})
	svc.SetSend(func(any) {}) // progress only; a prompt would time out
	lm := &liveModel{t: t, m: loadModel(t, svc), srv: srv}
	t.Cleanup(func() {
		if m, ok := lm.m.(ui.Model); ok {
			m.CloseTabs()
		}
	})
	return lm
}

// connect opens a session tab to web-prod, the way the program would
// deliver the connect command's result. Session listeners are not run:
// the tests read the server's side.
func (l *liveModel) connect() {
	l.t.Helper()
	var cmd tea.Cmd
	l.m, cmd = l.m.Update(hostlist.ConnectRequestMsg{HostID: domain.ID(liveHostID), Label: "web-prod"})
	msg := cmd()
	for {
		b, ok := msg.(tea.BatchMsg)
		if !ok {
			break
		}
		msg = nil
		for _, c := range b {
			if c != nil {
				msg = c()
				break
			}
		}
	}
	if _, ok := msg.(ui.ConnectedMsg); !ok {
		l.t.Fatalf("connect result %#v", msg)
	}
	l.m, _ = l.m.Update(msg)
}

// received returns what connection conn's session channel received.
func (l *liveModel) received(conn int) []byte { return l.srv.Received(conn, 1) }

// TestARCH_SESS_004_M2_BurstPerSession: a burst of 10 size messages
// within 50 ms sends each of three sessions at most 2 window-changes, the
// last one for the final session area.
func TestARCH_SESS_004_M2_BurstPerSession(t *testing.T) {
	l := newLiveModel(t)
	for range 3 {
		l.connect()
	}
	if !l.srv.WaitFor(5*time.Second, func() bool { return len(l.srv.RequestsOfType("shell")) == 3 }) {
		t.Fatal("three shells did not start")
	}
	for i := range 10 {
		l.m, _ = l.m.Update(tea.WindowSizeMsg{Width: 90 + i, Height: 30})
		time.Sleep(4 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	for conn := 1; conn <= 3; conn++ {
		var got []sshtest.Request
		for _, r := range l.srv.RequestsOfType("window-change") {
			if r.Conn == conn {
				got = append(got, r)
			}
		}
		if len(got) == 0 || len(got) > 2 {
			t.Fatalf("connection %d: %d window-changes %+v, want 1 or 2", conn, len(got), got)
		}
		if last := got[len(got)-1]; last.Width != 99 || last.Height != 28 {
			t.Fatalf("connection %d: last window-change %d×%d, want 99×28", conn, last.Width, last.Height)
		}
	}
}

// keystrokeP99 sends n key presses to the focused session through Update
// and returns the 99th percentile of the time until the server has read
// each key's bytes.
func (l *liveModel) keystrokeP99(conn, n int) time.Duration {
	l.t.Helper()
	keys := []struct {
		msg   tea.KeyPressMsg
		bytes int
	}{
		{tea.KeyPressMsg{Code: 'a', Text: "a"}, 1},
		{tea.KeyPressMsg{Code: tea.KeyUp}, 3},
		{tea.KeyPressMsg{Code: 'z', Text: "z"}, 1},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, 3},
	}
	times := make([]time.Duration, n)
	have := len(l.received(conn))
	for i := range times {
		k := keys[i%len(keys)]
		start := time.Now()
		l.m, _ = l.m.Update(k.msg)
		want := have + k.bytes
		deadline := start.Add(5 * time.Second)
		for len(l.received(conn)) < want {
			if time.Now().After(deadline) {
				l.t.Fatalf("key %d never reached the server", i)
			}
			time.Sleep(20 * time.Microsecond)
		}
		times[i] = time.Since(start)
		have = want
	}
	slices.Sort(times)
	return times[len(times)*99/100]
}

// TestNFR_PERF_004_KeystrokeOverhead: 1,000 key presses (printable keys
// and arrows) reach the server within 5 ms at the 99th percentile, also
// while another session streams output at 1 MB/s.
func TestNFR_PERF_004_KeystrokeOverhead(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	if raceEnabled {
		t.Skip("timing limit not meaningful under -race; CI runs it in the perf step")
	}
	l := newLiveModel(t)
	l.m, _ = l.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	l.connect() // connection 1: streams
	l.connect() // connection 2: focused, typed into
	if !l.srv.WaitFor(5*time.Second, func() bool { return len(l.srv.RequestsOfType("shell")) == 2 }) {
		t.Fatal("two shells did not start")
	}
	const limit = 5 * time.Millisecond
	if p99 := l.keystrokeP99(2, 1000); p99 > limit {
		t.Fatalf("keystroke overhead p99 %v, want at most %v", p99, limit)
	} else {
		t.Logf("keystroke overhead p99 %v", p99)
	}

	// Tab 1 streams at 1 MB/s while tab 2 is typed into.
	l.m, _ = l.m.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	l.m, _ = l.m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	for _, r := range "stream 1M\r" {
		l.m, _ = l.m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !l.srv.WaitFor(5*time.Second, func() bool { return strings.Contains(string(l.received(1)), "stream 1M\r") }) {
		t.Fatal("stream did not start")
	}
	l.m, _ = l.m.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	l.m, _ = l.m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if p99 := l.keystrokeP99(2, 1000); p99 > limit {
		t.Fatalf("keystroke overhead while streaming p99 %v, want at most %v", p99, limit)
	} else {
		t.Logf("keystroke overhead while streaming p99 %v", p99)
	}
}
