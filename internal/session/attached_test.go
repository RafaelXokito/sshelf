package session

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/term"
	"github.com/RafaelXokito/sshelf/internal/testutil"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }

// fakeTerm is a Terminal driven by the test.
type fakeTerm struct {
	mu         sync.Mutex
	cols, rows int
	raw        int
	restored   int
	events     chan struct{}
}

func newFakeTerm(cols, rows int) *fakeTerm {
	return &fakeTerm{cols: cols, rows: rows, events: make(chan struct{}, 16)}
}

func (f *fakeTerm) MakeRaw() (func() error, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raw++
	return func() error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.restored++
		return nil
	}, nil
}

func (f *fakeTerm) Size() (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows, nil
}

func (f *fakeTerm) Resizes() (<-chan struct{}, func()) { return f.events, func() {} }

func (f *fakeTerm) resize(cols, rows int) {
	f.mu.Lock()
	f.cols, f.rows = cols, rows
	f.mu.Unlock()
	f.events <- struct{}{}
}

// syncBuffer is a goroutine-safe bytes.Buffer.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.b.Bytes())
}

// harness runs a Session against an sshtest server with a fake terminal,
// an os.Pipe for stdin and a buffer for stdout.
type harness struct {
	srv    *sshtest.Server
	term   *fakeTerm
	sess   *Attached
	stdin  *os.File // write end
	stdout *syncBuffer
	done   chan error
}

func dialServer(t *testing.T, srv *sshtest.Server) *ssh.Client {
	t.Helper()
	c, err := ssh.Dial("tcp", srv.Addr(), &ssh.ClientConfig{
		User:            "u",
		Auth:            []ssh.AuthMethod{ssh.Password("pw")},
		HostKeyCallback: ssh.FixedHostKey(srv.HostKeys()[0]),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func start(t *testing.T, ft *fakeTerm, opts Options, srvOpts ...sshtest.Option) *harness {
	t.Helper()
	srv := sshtest.New(t, append([]sshtest.Option{sshtest.WithPassword("u", "pw")}, srvOpts...)...)
	s, err := NewAttached(dialServer(t, srv), ft, opts)
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	h := &harness{srv: srv, term: ft, sess: s, stdin: w, stdout: &syncBuffer{}, done: make(chan error, 1)}
	s.SetStdin(r)
	s.SetStdout(h.stdout)
	s.SetStderr(io.Discard)
	go func() { h.done <- s.Run() }()
	return h
}

func (h *harness) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-h.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func (h *harness) send(t *testing.T, s string) {
	t.Helper()
	if _, err := h.stdin.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// untilQ is a handler that waits for a 'q' from the client, then exits 0.
func untilQ(ss *sshtest.Session) {
	buf := make([]byte, 1)
	for {
		if _, err := ss.Read(buf); err != nil || buf[0] == 'q' {
			ss.Exit(0)
			return
		}
	}
}

func TestARCH_SESS_003_BytesPassThroughUnchanged(t *testing.T) {
	in, out := make([]byte, 256), make([]byte, 256)
	_, _ = rand.Read(in)
	_, _ = rand.Read(out)
	got := make(chan []byte, 1)
	h := start(t, newFakeTerm(80, 24), Options{}, sshtest.WithHandler(func(ss *sshtest.Session) {
		b := make([]byte, len(in))
		_, err := io.ReadFull(ss, b)
		got <- b
		if err == nil {
			_, _ = ss.Write(out)
		}
		ss.Exit(0)
	}))
	if _, err := h.stdin.Write(in); err != nil {
		t.Fatal(err)
	}
	if err := h.wait(t); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if b := <-got; !bytes.Equal(b, in) {
		t.Fatalf("server received\n%x\nwant\n%x", b, in)
	}
	if want := append(bytes.Clone(out), term.ResetSequence...); !bytes.Equal(h.stdout.Bytes(), want) {
		t.Fatalf("stdout =\n%q\nwant the server bytes followed by the reset sequence\n%q", h.stdout.Bytes(), want)
	}
	if h.term.raw != 1 || h.term.restored != 1 {
		t.Fatalf("raw mode entered %d and restored %d times, want 1 and 1", h.term.raw, h.term.restored)
	}
}

func windowChanges(srv *sshtest.Server) []sshtest.Request { return srv.RequestsOfType("window-change") }

func TestARCH_SESS_004_WindowChangeWithin100ms(t *testing.T) {
	h := start(t, newFakeTerm(80, 24), Options{}, sshtest.WithHandler(untilQ))
	start := time.Now()
	h.term.resize(132, 50)
	ok := h.srv.WaitFor(time.Second, func() bool {
		wc := windowChanges(h.srv)
		return len(wc) == 1 && wc[0].Width == 132 && wc[0].Height == 50
	})
	elapsed := time.Since(start)
	h.send(t, "q")
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("window-change not received: %+v", windowChanges(h.srv))
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("window-change arrived after %v, want ≤ 100ms", elapsed)
	}
}

func TestARCH_SESS_004_BurstMaxTwoRequests(t *testing.T) {
	h := start(t, newFakeTerm(80, 24), Options{}, sshtest.WithHandler(untilQ))
	for i := range 5 {
		h.term.resize(100+i, 30+i)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	h.send(t, "q")
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	wc := windowChanges(h.srv)
	if len(wc) == 0 || len(wc) > 2 {
		t.Fatalf("burst produced %d window-change requests, want 1 or 2: %+v", len(wc), wc)
	}
	if last := wc[len(wc)-1]; last.Width != 104 || last.Height != 34 {
		t.Fatalf("last window-change = %dx%d, want 104x34", last.Width, last.Height)
	}
}

func TestFR_CONN_004_ResizeReachesRemote(t *testing.T) {
	h := start(t, newFakeTerm(80, 24), Options{})
	h.term.resize(120, 40)
	if !h.srv.WaitFor(time.Second, func() bool { return len(windowChanges(h.srv)) == 1 }) {
		t.Fatal("no window-change")
	}
	h.send(t, "size\rexit 0\r")
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if out := string(h.stdout.Bytes()); !strings.Contains(out, "120x40") {
		t.Fatalf("remote size output %q does not contain 120x40", out)
	}
}

func TestFR_CONN_001_PtyUsesTermAndSize(t *testing.T) {
	for _, tc := range []struct {
		name, env, opt, want string
	}{
		{"from $TERM", "xterm-kitty", "", "xterm-kitty"},
		{"option wins", "xterm-kitty", "screen-256color", "screen-256color"},
		{"default", "", "", "xterm-256color"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERM", tc.env)
			h := start(t, newFakeTerm(100, 30), Options{Term: tc.opt, Env: map[string]string{"LANG": "C.UTF-8"}})
			h.send(t, "exit 0\r")
			if err := h.wait(t); err != nil {
				t.Fatal(err)
			}
			pty := h.srv.RequestsOfType("pty-req")
			if len(pty) != 1 || pty[0].Term != tc.want || pty[0].Width != 100 || pty[0].Height != 30 {
				t.Fatalf("pty-req = %+v, want %s 100x30", pty, tc.want)
			}
			if env := h.srv.RequestsOfType("env"); len(env) != 1 || env[0].Name != "LANG" {
				t.Fatalf("env requests = %+v", env)
			}
		})
	}
}

func TestARCH_SESS_005_ExitStatusReturned(t *testing.T) {
	h := start(t, newFakeTerm(80, 24), Options{})
	h.send(t, "exit 3\r")
	err := h.wait(t)
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Status != 3 || err.Error() != "exited (3)" {
		t.Fatalf("Run() = %v, want exited (3)", err)
	}
}

func TestARCH_SESS_005_DisconnectReturned(t *testing.T) {
	h := start(t, newFakeTerm(80, 24), Options{}, sshtest.WithHandler(untilQ))
	time.Sleep(20 * time.Millisecond)
	h.srv.DropConnections()
	err := h.wait(t)
	var de *DisconnectError
	if !errors.As(err, &de) || !strings.HasPrefix(err.Error(), "connection lost: ") {
		t.Fatalf("Run() = %v, want a DisconnectError", err)
	}
	if h.term.restored != 1 || !bytes.HasSuffix(h.stdout.Bytes(), []byte(term.ResetSequence)) {
		t.Fatal("terminal not restored after a disconnect")
	}
}

func TestARCH_SESS_005_CommandOption(t *testing.T) {
	h := start(t, newFakeTerm(80, 24), Options{Command: "echo hi"})
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if ex := h.srv.RequestsOfType("exec"); len(ex) != 1 || ex[0].Command != "echo hi" {
		t.Fatalf("exec requests = %+v", ex)
	}
	if !strings.HasPrefix(string(h.stdout.Bytes()), "hi\r\n") {
		t.Fatalf("stdout = %q", h.stdout.Bytes())
	}
}
