//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
	"github.com/RafaelXokito/sshelf/internal/ui/screens/hostlist"
)

// The SFTP tab's responsiveness checks (NFR-PERF-008, NFR-PERF-009's PTY
// variant). CI runs them without -race in the perf-sftp job.

// browserTap is the app with the browsers OpenSFTPOn opens sent to got,
// so a test can keep transfers running behind the UI.
type browserTap struct {
	*app.App
	got chan app.Browser
}

func (b browserTap) OpenSFTPOn(s app.Session, hostID domain.ID) (app.Browser, error) {
	br, err := b.App.OpenSFTPOn(s, hostID)
	if err == nil {
		b.got <- br
	}
	return br, err
}

// enqueued is a message the test put on the loop's queue at at.
type enqueued struct {
	msg tea.Msg
	at  time.Time
}

// program runs the live model the way tea.Program does: one goroutine
// (the test's) calls Update and View in turn, commands run on their own
// goroutines, and their messages and the test's keys share one queue.
// It records the longest Update or View call.
type program struct {
	l       *liveModel
	queue   chan enqueued
	done    chan struct{}
	slowest time.Duration
	slowMsg string
	ticks   int // progress ticks handled
	view    string
}

func newProgram(l *liveModel) *program {
	p := &program{l: l, queue: make(chan enqueued, 4096), done: make(chan struct{})}
	l.t.Cleanup(func() { close(p.done) })
	return p
}

func (p *program) dispatch(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				p.dispatch(c)
			}
		default:
			select {
			case p.queue <- enqueued{msg: msg, at: time.Now()}:
			case <-p.done:
			}
		}
	}()
}

// send puts msg on the queue, as a key read from the terminal would be.
func (p *program) send(msg tea.Msg) { p.queue <- enqueued{msg: msg, at: time.Now()} }

// handle runs Update on one message and draws the frame, and returns when
// the frame was drawn.
func (p *program) handle(e enqueued) time.Time {
	if fmt.Sprintf("%T", e.msg) == "ui.progressTickMsg" {
		p.ticks++
	}
	start := time.Now()
	m, cmd := p.l.m.Update(e.msg)
	p.note(time.Since(start), fmt.Sprintf("Update(%T)", e.msg))
	p.l.m = m
	p.dispatch(cmd)
	start = time.Now()
	p.view = ansi.Strip(m.View().Content)
	end := time.Now()
	p.note(end.Sub(start), "View")
	return end
}

func (p *program) note(d time.Duration, what string) {
	if d > p.slowest {
		p.slowest, p.slowMsg = d, what
	}
}

// step handles the queued messages until the queue is empty, or until
// deadline when wait is set.
func (p *program) step(deadline time.Time, wait bool) {
	for {
		if !wait {
			select {
			case e := <-p.queue:
				p.handle(e)
				continue
			default:
				return
			}
		}
		left := time.Until(deadline)
		if left <= 0 {
			return
		}
		select {
		case e := <-p.queue:
			p.handle(e)
		case <-time.After(left):
			return
		}
	}
}

// until handles messages until the frame satisfies cond.
func (p *program) until(what string, cond func(view string) bool) {
	p.l.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond(p.view) {
		if time.Now().After(deadline) {
			p.l.t.Fatalf("timed out waiting for %s; frame:\n%s", what, p.view)
		}
		p.step(time.Now().Add(10*time.Millisecond), true)
	}
}

// key sends a key and handles messages until it has been handled and its
// frame drawn; it returns how long that took from the send.
func (p *program) key(k tea.KeyPressMsg) time.Duration {
	sent := enqueued{msg: k, at: time.Now()}
	p.queue <- sent
	for {
		e := <-p.queue
		end := p.handle(e)
		if e == sent {
			return end.Sub(sent.at)
		}
	}
}

// keeper keeps a download and an upload of 256 MiB running on b until
// stop is closed: when one is done, its target is removed and it is
// queued again. It reports a failed transfer on fail.
func keeper(b app.Browser, local, remoteDir string, srv *sshtest.Server, stop <-chan struct{}) (rounds func() int, fail func() string, wait func()) {
	var mu sync.Mutex
	n, failed := 0, ""
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			ts := b.Transfers()
			busy := [2]bool{}
			for _, x := range ts {
				switch {
				case x.State == app.Failed || x.State == app.Waiting:
					mu.Lock()
					failed = x.Name + ": " + x.State.String() + " " + x.Reason
					mu.Unlock()
					return
				case !x.State.Finished():
					busy[x.From] = true
				}
			}
			if !busy[app.Remote] || !busy[app.Local] {
				b.Clear() // the transfers list keeps only the running pair
			}
			if !busy[app.Remote] {
				_ = os.Remove(filepath.Join(local, "down.bin"))
				b.Copy(app.Remote, remoteDir, []string{"down.bin"}, local)
				mu.Lock()
				n++
				mu.Unlock()
			}
			if !busy[app.Local] {
				_ = os.Remove(srv.SFTPDir("up.bin"))
				b.Copy(app.Local, local, []string{"up.bin"}, remoteDir)
				mu.Lock()
				n++
				mu.Unlock()
			}
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	rounds = func() int { mu.Lock(); defer mu.Unlock(); return n }
	fail = func() string { mu.Lock(); defer mu.Unlock(); return failed }
	return rounds, fail, func() { <-finished }
}

// transfersRunning is a live model with a session tab and an SFTP tab on
// one connection to sshtest, the SFTP tab focused, while a 256 MiB
// download and a 256 MiB upload run, each queued again as soon as it
// finishes (over loopback one takes well under a second).
type transfersRunning struct {
	l      *liveModel
	p      *program
	rounds func() int    // transfers queued so far
	failed func() string // a transfer that stopped, or ""
	stop   func()        // stops queueing and waits
}

func startTransfers(t *testing.T) *transfersRunning {
	t.Helper()
	if testing.Short() {
		t.Skip("timing")
	}
	if raceEnabled {
		t.Skip("timing limit not meaningful under -race; CI runs it in the perf-sftp job")
	}
	const size = 256 << 20
	local, remote := t.TempDir(), t.TempDir()
	t.Chdir(local) // the local pane starts in the working directory (FR-SFTP-005)
	writeRandom(t, filepath.Join(local, "up.bin"), size)
	tap := browserTap{got: make(chan app.Browser, 1)}
	l := newLiveModelWith(t, func(a *app.App) app.Service { tap.App = a; return tap }, sshtest.WithSFTP(remote))
	writeRandom(t, l.srv.SFTPDir("down.bin"), size)
	for i := range 20 {
		if err := os.WriteFile(l.srv.SFTPDir(fmt.Sprintf("a%02d.txt", i)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	p := newProgram(l)
	p.handle(enqueued{msg: tea.WindowSizeMsg{Width: 120, Height: 40}})
	p.handle(enqueued{msg: hostlist.ConnectRequestMsg{HostID: domain.ID(liveHostID), Label: "web-prod"}})
	p.until("the session tab", func(v string) bool { return strings.Contains(v, "1 web-prod") && !strings.Contains(v, "Connecting") })
	p.send(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	p.send(tea.KeyPressMsg{Code: 's', Text: "s"})
	p.until("the SFTP tab listing both panes", func(v string) bool {
		return strings.Contains(v, "2 sftp: web-prod") && strings.Contains(v, "a00.txt") && strings.Contains(v, "up.bin")
	})
	b := <-tap.got

	stop := make(chan struct{})
	rounds, failed, wait := keeper(b, local, sshtest.SFTPHome, l.srv, stop)
	stopKeeper := sync.OnceFunc(func() { close(stop); wait() })
	t.Cleanup(stopKeeper)
	p.until("both transfers running", func(string) bool {
		running := 0
		for _, x := range b.Transfers() {
			if x.State == app.Running {
				running++
			}
		}
		return running == 2
	})
	return &transfersRunning{l: l, p: p, rounds: rounds, failed: failed, stop: stopKeeper}
}

// TestNFR_PERF_008_FrameGaps drives the root model as tea.Program would
// while two transfers run (startTransfers): 200 cursor keys to the SFTP
// tab, 50 ms apart, each show the moved cursor within 100 ms; no Update
// or View call takes more than 20 ms; the progress line is redrawn at
// most 4 times a second.
func TestNFR_PERF_008_FrameGaps(t *testing.T) {
	const (
		keyLimit  = 100 * time.Millisecond
		callLimit = 20 * time.Millisecond
	)
	tr := startTransfers(t)
	p := tr.p
	// j and k alternate between ".." and a00.txt.
	p.slowest, p.ticks = 0, 0
	start := time.Now()
	var worst time.Duration
	for i := range 200 {
		k, want := tea.KeyPressMsg{Code: 'j', Text: "j"}, "│>  a00.txt"
		if i%2 == 1 {
			k, want = tea.KeyPressMsg{Code: 'k', Text: "k"}, "│>  .."
		}
		took := p.key(k)
		if !strings.Contains(p.view, want) {
			t.Fatalf("key %d: the frame does not show the cursor at %q:\n%s", i, want, p.view)
		}
		worst = max(worst, took)
		p.step(time.Now().Add(50*time.Millisecond), true)
		if f := tr.failed(); f != "" {
			t.Fatalf("a transfer stopped: %s", f)
		}
	}
	elapsed := time.Since(start)
	tr.stop()
	slowest, slowMsg, ticks := p.slowest, p.slowMsg, p.ticks
	t.Logf("sftp: during transfers, key to frame worst %v, slowest call %v (%s), %d progress ticks in %v, %d transfers queued",
		worst, slowest, slowMsg, ticks, elapsed.Round(time.Millisecond), tr.rounds())
	if worst > keyLimit {
		t.Errorf("a key took %v to reach the frame; want at most %v", worst, keyLimit)
	}
	if slowest > callLimit {
		t.Errorf("%s took %v; want at most %v", slowMsg, slowest, callLimit)
	}
	if maxTicks := int(elapsed/(250*time.Millisecond)) + 1; ticks > maxTicks {
		t.Errorf("%d progress ticks in %v; want at most %d (4 a second)", ticks, elapsed, maxTicks)
	}
	if ticks == 0 {
		t.Error("no progress tick while the transfers ran")
	}
}

// TestNFR_PERF_008_SessionKeysDuringTransfers: while two transfers run
// on the connection (startTransfers), 1,000 keys to the session tab on
// it reach the server within 50 ms at the 99th percentile (D-021: the
// upload's 64 requests in flight share the connection, so NFR-PERF-004's
// 5 ms bound applies only with no transfer on it).
func TestNFR_PERF_008_SessionKeysDuringTransfers(t *testing.T) {
	const strokeLimit = 50 * time.Millisecond
	tr := startTransfers(t)
	p, l := tr.p, tr.l
	p.send(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	p.send(tea.KeyPressMsg{Code: '1', Text: "1"})
	p.until("the session tab focused", func(v string) bool { return !strings.Contains(v, "a00.txt") })
	keys := []struct {
		msg   tea.KeyPressMsg
		bytes int
	}{
		{tea.KeyPressMsg{Code: 'a', Text: "a"}, 1},
		{tea.KeyPressMsg{Code: tea.KeyUp}, 3},
		{tea.KeyPressMsg{Code: 'z', Text: "z"}, 1},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, 3},
	}
	times := make([]time.Duration, 1000)
	have := len(l.received(1))
	for i := range times {
		k := keys[i%len(keys)]
		sent := time.Now()
		p.send(k.msg)
		want := have + k.bytes
		for len(l.received(1)) < want {
			if time.Since(sent) > 5*time.Second {
				t.Fatalf("key %d never reached the server", i)
			}
			p.step(time.Time{}, false)
			time.Sleep(20 * time.Microsecond)
		}
		times[i] = time.Since(sent)
		have = want
	}
	if f := tr.failed(); f != "" {
		t.Fatalf("a transfer stopped: %s", f)
	}
	tr.stop()
	slices.Sort(times)
	p99 := times[len(times)*99/100]
	t.Logf("sftp: session keystroke overhead during transfers p99 %v, %d transfers queued", p99, tr.rounds())
	if p99 > strokeLimit {
		t.Errorf("session keystroke overhead p99 %v during transfers; want at most %v", p99, strokeLimit)
	}
}

// TestNFR_PERF_009_E2ETenThousandEntries: in the real binary, enter on a
// remote directory of 10,000 entries shows the listing within 1 s
// (network included).
func TestNFR_PERF_009_E2ETenThousandEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	if raceEnabled {
		t.Skip("timing limit not meaningful under -race; CI runs it in the perf-sftp job")
	}
	r := startSFTP(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Entries: 10_000}))
	r.openSFTPHost("many/")
	r.typeKeys("j") // from .. to many/
	off := r.out.Len()
	start := time.Now()
	r.typeKeys("\r")
	took := r.waitFast(off, "f0000000", start)
	r.waitFast(off, "f0000001", start)
	t.Logf("sftp: 10,000-entry remote directory to first frame %v", took)
	if took > time.Second {
		t.Fatalf("the listing took %v to appear; want at most 1s", took)
	}
	r.prefix("q")
	r.waitText(off, "Close SFTP tab 1?")
	r.typeKeys("y")
	r.waitText(off, "enter connect")
	r.quit()
}
