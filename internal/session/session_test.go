package session

import (
	"bytes"
	"crypto/rand"
	"errors"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"go.uber.org/goleak"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The M2 session (design §8.4) against the in-process server.

func newServer(t *testing.T, opts ...sshtest.Option) *sshtest.Server {
	t.Helper()
	return sshtest.New(t, append([]sshtest.Option{sshtest.WithPassword("u", "pw")}, opts...)...)
}

// open starts a session and makes sure it has ended when the test does.
func open(t *testing.T, srv *sshtest.Server, opts Options) *Session {
	t.Helper()
	s, err := New(dialServer(t, srv), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		<-s.Done()
	})
	return s
}

func waitDone(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not end")
	}
}

// screenText returns the screen without styles.
func plainScreen(s *Session) string {
	var b strings.Builder
	for _, l := range s.Snapshot().Lines {
		b.WriteString(plainLine(l))
		b.WriteByte('\n')
	}
	return b.String()
}

// waitScreen waits until the screen shows want.
func waitScreen(t *testing.T, s *Session, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(plainScreen(s), want) {
		if time.Now().After(deadline) {
			t.Fatalf("screen never showed %q:\n%s", want, plainScreen(s))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitReceived waits until the server has read want on channel 1 of
// connection conn.
func waitReceived(t *testing.T, srv *sshtest.Server, conn int, want string) {
	t.Helper()
	if !srv.WaitFor(5*time.Second, func() bool { return bytes.Contains(srv.Received(conn, 1), []byte(want)) }) {
		t.Fatalf("server received %q, want it to contain %q", srv.Received(conn, 1), want)
	}
}

func TestARCH_SESS_003_M2_RandomBytesUnchanged(t *testing.T) {
	in := make([]byte, 256)
	_, _ = rand.Read(in)
	got := make(chan []byte, 1)
	srv := newServer(t, sshtest.WithHandler(func(ss *sshtest.Session) {
		b := make([]byte, len(in))
		_, _ = io.ReadFull(ss, b)
		got <- b
		ss.Exit(0)
	}))
	s := open(t, srv, Options{})
	if err := s.SendBytes(in); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if b := <-got; !bytes.Equal(b, in) {
		t.Fatalf("server received\n%x\nwant\n%x", b, in)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

// recorder is an emulator double that records every byte of output on
// its way to the real screen.
type recorder struct {
	*screen
	mu  sync.Mutex
	out bytes.Buffer
}

func (r *recorder) write(p []byte) {
	r.mu.Lock()
	r.out.Write(p)
	r.mu.Unlock()
	r.screen.write(p)
}

func TestARCH_SESS_003_M2_OutputOnlyToEmulator(t *testing.T) {
	out := make([]byte, 64<<10)
	_, _ = rand.Read(out)
	srv := newServer(t, sshtest.WithHandler(func(ss *sshtest.Session) {
		_, _ = ss.Write(out)
		ss.Exit(0)
	}))
	var rec *recorder
	s, err := newSession(dialServer(t, srv), Options{}, func(sz Size, sb int) emulator {
		rec = &recorder{screen: newScreen(sz.Cols, sz.Rows, sb)}
		return rec
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if !bytes.Equal(rec.out.Bytes(), out) {
		t.Fatalf("emulator received %d bytes, want the %d bytes the server sent, unchanged", rec.out.Len(), len(out))
	}
	// Nothing in the session's code can reach the local terminal.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f) //nolint:gosec // a source file of this package
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"os.Stdout", "os.Stderr", "os.Stdin", "golang.org/x/term", "MakeRaw", "cancelreader"} {
			if bytes.Contains(src, []byte(bad)) {
				t.Errorf("%s refers to %s", f, bad)
			}
		}
	}
}

func TestARCH_SESS_004_M2_DebouncedWindowChange(t *testing.T) {
	srv := newServer(t)
	var sessions []*Session
	for range 3 {
		sessions = append(sessions, open(t, srv, Options{Size: Size{Cols: 80, Rows: 22}}))
	}
	changes := func(conn int) []sshtest.Request {
		var out []sshtest.Request
		for _, r := range srv.RequestsOfType("window-change") {
			if r.Conn == conn {
				out = append(out, r)
			}
		}
		return out
	}

	// An unchanged size sends nothing.
	sessions[0].Resize(Size{Cols: 80, Rows: 22})
	time.Sleep(3 * ResizeDebounce)
	if wc := changes(1); len(wc) != 0 {
		t.Fatalf("unchanged size sent %+v", wc)
	}

	start := time.Now()
	for _, s := range sessions {
		s.Resize(Size{Cols: 100, Rows: 28})
	}
	for i := range sessions {
		if !srv.WaitFor(time.Second, func() bool { return len(changes(i+1)) == 1 }) {
			t.Fatalf("session %d: no window-change", i+1)
		}
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("window-changes arrived after %v, want ≤ 100ms", d)
	}
	for i, s := range sessions {
		if wc := changes(i + 1); wc[0].Width != 100 || wc[0].Height != 28 {
			t.Errorf("session %d: window-change %dx%d, want 100x28", i+1, wc[0].Width, wc[0].Height)
		}
		if snap := s.Snapshot(); len(snap.Lines) != 28 || len(plainLine(snap.Lines[0])) > 100 {
			t.Errorf("session %d: emulator not resized at once: %d lines", i+1, len(snap.Lines))
		}
	}

	// A burst of 10 sizes within 50 ms: at most 2 requests per session.
	for i := range 10 {
		for _, s := range sessions {
			s.Resize(Size{Cols: 101 + i, Rows: 30})
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(4 * ResizeDebounce)
	for i := range sessions {
		wc := changes(i + 1)[1:]
		if len(wc) == 0 || len(wc) > 2 {
			t.Fatalf("session %d: burst sent %d window-changes, want 1 or 2", i+1, len(wc))
		}
		if last := wc[len(wc)-1]; last.Width != 110 || last.Height != 30 {
			t.Errorf("session %d: last window-change %dx%d, want 110x30", i+1, last.Width, last.Height)
		}
	}
}

func TestFR_CONN_001_M2_PtyIsXterm256ColorAtSessionSize(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	srv := newServer(t)
	s := open(t, srv, Options{Size: Size{Cols: 100, Rows: 30}, Env: map[string]string{"LANG": "C.UTF-8"}})
	_ = s.SendBytes([]byte("exit 0\r"))
	waitDone(t, s)
	pty := srv.RequestsOfType("pty-req")
	if len(pty) != 1 || pty[0].Term != "xterm-256color" || pty[0].Width != 100 || pty[0].Height != 30 {
		t.Fatalf("pty-req = %+v, want xterm-256color 100x30", pty)
	}
	if env := srv.RequestsOfType("env"); len(env) != 1 || env[0].Name != "LANG" {
		t.Fatalf("env requests = %+v", env)
	}
}

// signalHandler exits by signal once it reads a byte.
func signalHandler(ss *sshtest.Session) {
	b := make([]byte, 1)
	_, _ = ss.Read(b)
	ss.ExitSignal("KILL")
}

// endCases ends a session in each way a remote or the user can.
var endCases = []struct {
	name    string
	handler sshtest.Handler // nil = the scripted shell
	end     func(*sshtest.Server, *Session)
	check   func(error) bool
}{
	{"exit 0", nil, func(_ *sshtest.Server, s *Session) { _ = s.SendBytes([]byte("exit 0\r")) },
		func(err error) bool { return err == nil }},
	{"exit 3", nil, func(_ *sshtest.Server, s *Session) { _ = s.SendBytes([]byte("exit 3\r")) },
		func(err error) bool {
			var ee *ExitError
			return errors.As(err, &ee) && ee.Status == 3 && err.Error() == "exited (3)"
		}},
	{"signal", signalHandler, func(_ *sshtest.Server, s *Session) { _ = s.SendBytes([]byte("x")) },
		func(err error) bool {
			var ee *ExitError
			return errors.As(err, &ee) && ee.Signal == "KILL"
		}},
	{"dropped connection", nil, func(srv *sshtest.Server, _ *Session) { srv.DropConnections() },
		func(err error) bool {
			var de *DisconnectError
			return errors.As(err, &de) && strings.HasPrefix(err.Error(), "connection lost: ")
		}},
	{"closed", nil, func(_ *sshtest.Server, s *Session) { _ = s.Close() },
		func(err error) bool { return err == nil }},
}

func TestARCH_SESS_005_M2_EachEndStopsGoroutines(t *testing.T) {
	for _, tc := range endCases {
		t.Run(tc.name, func(t *testing.T) {
			before := goleak.IgnoreCurrent()
			var opts []sshtest.Option
			if tc.handler != nil {
				opts = append(opts, sshtest.WithHandler(tc.handler))
			}
			srv, err := sshtest.Start(append(opts, sshtest.WithPassword("u", "pw"))...)
			if err != nil {
				t.Fatal(err)
			}
			s, err := New(dialServer(t, srv), Options{})
			if err != nil {
				t.Fatal(err)
			}
			waitScreen(t, s, "")
			tc.end(srv, s)
			waitDone(t, s)
			if !tc.check(s.Err()) {
				t.Errorf("Err() = %v", s.Err())
			}
			_ = srv.Close()
			goleak.VerifyNone(t, before)
		})
	}
}

func TestARCH_SESS_008_CauseReportedOnce(t *testing.T) {
	for _, tc := range endCases {
		t.Run(tc.name, func(t *testing.T) {
			var opts []sshtest.Option
			if tc.handler != nil {
				opts = append(opts, sshtest.WithHandler(tc.handler))
			}
			srv := newServer(t, opts...)
			s := open(t, srv, Options{})
			if tc.handler == nil {
				waitScreen(t, s, "$")
			}
			tc.end(srv, s)
			// Updates is closed exactly once, and only after Done.
			for range s.Updates() {
			}
			select {
			case <-s.Done():
			default:
				t.Fatal("Updates closed before Done")
			}
			if !tc.check(s.Err()) {
				t.Errorf("Err() = %v", s.Err())
			}
			if err := s.Err(); err != s.Err() {
				t.Error("Err() changed")
			}
			if !srv.WaitFor(time.Second, func() bool {
				for _, c := range srv.Closes() {
					if c.Type == "disconnect" && c.Conn == 1 {
						return true
					}
				}
				return false
			}) {
				t.Errorf("server did not see the connection close: %+v", srv.Closes())
			}
		})
	}
}

func TestARCH_SESS_010_ThreeSessionsReadIndependently(t *testing.T) {
	before := goleak.IgnoreCurrent()
	srv, err := sshtest.Start(sshtest.WithPassword("u", "pw"))
	if err != nil {
		t.Fatal(err)
	}
	var sessions []*Session
	for range 3 {
		s, err := New(dialServer(t, srv), Options{})
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, s)
		waitScreen(t, s, "$")
	}
	// Only the first is "focused"; output reaches every emulator anyway.
	for i, s := range sessions {
		if err := s.SendBytes([]byte("echo out-" + string(rune('a'+i)) + "\r")); err != nil {
			t.Fatal(err)
		}
	}
	for i, s := range sessions {
		want := "out-" + string(rune('a'+i))
		waitScreen(t, s, "\n"+want)
		waitReceived(t, srv, i+1, "echo "+want)
		for j := range sessions {
			if other := "out-" + string(rune('a'+j)); j != i && strings.Contains(plainScreen(s), other) {
				t.Errorf("session %d shows %s", i+1, other)
			}
		}
	}
	for _, s := range sessions {
		_ = s.Close()
	}
	for _, s := range sessions {
		waitDone(t, s)
	}
	if !srv.WaitFor(time.Second, func() bool {
		n := 0
		for _, c := range srv.Closes() {
			if c.Type == "disconnect" {
				n++
			}
		}
		return n == 3
	}) {
		t.Fatalf("closes = %+v, want 3 disconnects", srv.Closes())
	}
	_ = srv.Close()
	goleak.VerifyNone(t, before)
}

func TestARCH_SESS_011_HostileCorpusStaysInEmulator(t *testing.T) {
	srv := newServer(t)
	s := open(t, srv, Options{Command: "hostile"})
	waitDone(t, s)
	snap := s.Snapshot()
	checkRendered(t, snap.Lines)
	lines, _ := s.Scrollback(0, 1000)
	checkRendered(t, lines)
	// The emulator answered nothing it should not: no title report.
	if got := srv.Received(1, 1); bytes.Contains(got, []byte("evil")) {
		t.Fatalf("a title went back to the server: %q", got)
	}
}

// stalledServer's handler sends queries, then never reads its input until
// the test ends.
func stalledServer(t *testing.T, queries int) *sshtest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := newServer(t, sshtest.WithHandler(func(ss *sshtest.Session) {
		for range queries {
			if _, err := ss.Write([]byte("\x1b[c")); err != nil {
				return
			}
		}
		<-release
	}))
	t.Cleanup(func() { close(release) }) // runs before the server's Close
	return srv
}

func TestARCH_SESS_014_StalledRemoteNeverBlocks(t *testing.T) {
	s := open(t, stalledServer(t, 200), Options{})
	// Fill the channel window and the queue.
	chunk := bytes.Repeat([]byte("x"), 1<<10)
	for range 20000 {
		if err := s.SendBytes(chunk); errors.Is(err, ErrInputFull) {
			break
		}
	}
	start := time.Now()
	full := 0
	for range 10000 {
		if err := s.SendKey(uv.KeyPressEvent{Code: 'a', Text: "a"}); errors.Is(err, ErrInputFull) {
			full++
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("10,000 SendKey calls took %v, want ≤ 1s", d)
	}
	if full == 0 {
		t.Fatal("no ErrInputFull with a stalled remote")
	}
	// A query from the remote while the writer is stalled does not block
	// rendering.
	start = time.Now()
	_ = s.Snapshot()
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Fatalf("Snapshot took %v with a stalled writer", d)
	}
}

func TestARCH_SESS_014_InputFull(t *testing.T) {
	s := open(t, stalledServer(t, 0), Options{})
	chunk := bytes.Repeat([]byte("y"), 32<<10)
	var full bool
	for range 1000 {
		if err := s.SendBytes(chunk); err != nil {
			if !errors.Is(err, ErrInputFull) || err.Error() != "remote is not reading input" {
				t.Fatalf("SendBytes = %v", err)
			}
			full = true
			break
		}
	}
	if !full {
		t.Fatal("the queue never filled")
	}
	if p := s.inputPeak(); p > InputLimit || p < InputLimit-len(chunk) {
		t.Fatalf("queue peaked at %d bytes, want just under %d", p, InputLimit)
	}
	// Top it up in small pieces, so less room is left than a key needs.
	// Until the channel window is used up the writer may still take a
	// little from the queue between the top-up and the key; top up again
	// then.
	var err error
	for range 100 {
		for range InputLimit {
			if s.SendBytes([]byte("z")) != nil {
				break
			}
		}
		if err = s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter}); errors.Is(err, ErrInputFull) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("SendKey on a full queue = %v", err)
}

func TestARCH_SESS_015_UpdatesCoalesce(t *testing.T) {
	srv := newServer(t, sshtest.WithHandler(func(ss *sshtest.Session) {
		for i := range 10000 {
			if _, err := ss.Write([]byte{'a' + byte(i%26)}); err != nil {
				return
			}
		}
		ss.Exit(0)
	}))
	s := open(t, srv, Options{})
	n := 0
	for range s.Updates() {
		if l := len(s.Updates()); l > 1 {
			t.Fatalf("%d pending notifications", l)
		}
		n++
		time.Sleep(5 * time.Millisecond) // a slow UI
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("Updates closed before Done")
	}
	if n == 0 || n >= 10000 {
		t.Fatalf("%d notifications for 10,000 chunks", n)
	}
	if _, ok := <-s.Updates(); ok {
		t.Fatal("Updates delivered after it was closed")
	}
}

func TestARCH_SESS_016_QueryRepliesReachRemote(t *testing.T) {
	srv := newServer(t)
	s := open(t, srv, Options{Size: Size{Cols: 90, Rows: 20}, Command: "query"})
	waitDone(t, s)
	got := string(srv.Received(1, 1))
	for _, want := range []string{"\x1b[?62;1;6;22c", "\x1b[1;1R", "\x1b]11;rgb:0000/0000/0000"} {
		if !strings.Contains(got, want) {
			t.Errorf("server received %q, want it to contain %q", got, want)
		}
	}
}

func TestARCH_SESS_016_DefaultColorsAnswerOSC11(t *testing.T) {
	srv := newServer(t)
	s := open(t, srv, Options{})
	s.SetDefaultColors(nil, color.RGBA{R: 0xfd, G: 0xf6, B: 0xe3, A: 0xff})
	waitScreen(t, s, "$")
	if err := s.SendBytes([]byte("query\r")); err != nil {
		t.Fatal(err)
	}
	waitReceived(t, srv, 1, "rgb:fdfd/f6f6/e3e3")
	// The cursor position is reported within the session area.
	if got := string(srv.Received(1, 1)); !strings.Contains(got, "\x1b[2;1R") {
		t.Errorf("server received %q, want the CPR ESC[2;1R", got)
	}
}

func TestARCH_SESS_017_KeysReachRemoteThroughEchoHex(t *testing.T) {
	srv := newServer(t)
	s := open(t, srv, Options{Command: "echo-hex"})
	for _, k := range []uv.KeyPressEvent{
		{Code: uv.KeyUp, Mod: uv.ModCtrl},
		{Code: uv.KeyF5, Mod: uv.ModCtrl},
		{Code: 'q', Text: "q"},
	} {
		if err := s.SendKey(k); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Paste("hi"); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, s, "1b 5b 31 3b 35 41 1b 5b 31 35 3b 35 7e 71 68 69")
}

// panicky is an emulator double that panics on output containing "boom",
// as a bug in the emulator would, on the session's output goroutine.
type panicky struct{ *screen }

func (p panicky) write(b []byte) {
	if bytes.Contains(b, []byte("boom")) {
		panic("injected: emulator write")
	}
	p.screen.write(b)
}

func TestARCH_SESS_005_M2_PanicEndsOnlyThisSession(t *testing.T) {
	srv := newServer(t)
	a, err := newSession(dialServer(t, srv), Options{}, func(sz Size, sb int) emulator {
		return panicky{newScreen(sz.Cols, sz.Rows, sb)}
	})
	if err != nil {
		t.Fatal(err)
	}
	b := open(t, srv, Options{})
	waitScreen(t, a, "$")
	waitScreen(t, b, "$")
	_ = a.SendBytes([]byte("echo boom\r"))
	waitDone(t, a)
	if !errors.Is(a.Err(), ErrInternal) {
		t.Fatalf("a.Err() = %v, want ErrInternal", a.Err())
	}
	if err := a.SendBytes([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("input to an ended session = %v, want ErrClosed", err)
	}
	_ = b.SendBytes([]byte("echo still here\r"))
	waitScreen(t, b, "\nstill here")
	select {
	case <-b.Done():
		t.Fatal("the other session ended")
	default:
	}
}
