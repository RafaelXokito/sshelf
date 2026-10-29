//go:build unix

package session

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/RafaelXokito/sshelf/internal/term"
	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// ptyHarness attaches a Session to the slave side of a real PTY, with the
// test playing the terminal emulator on the master side.
type ptyHarness struct {
	srv    *sshtest.Server
	output *syncBuffer // everything written to the terminal
	done   chan error
	drain  chan struct{}
}

func stty(t *testing.T, tty *os.File) string {
	t.Helper()
	cmd := exec.Command("stty", "-a")
	cmd.Stdin = tty
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("stty -a: %v", err)
	}
	return string(out)
}

// setupPTY opens a PTY and a session on its slave side, ready to Run.
func setupPTY(t *testing.T, handler sshtest.Handler) (master, tty *os.File, s *Session, h *ptyHarness) {
	t.Helper()
	return setupPTYTerm(t, handler, func(tty *os.File) Terminal { return term.NewTTY(tty) })
}

// setupPTYTerm is setupPTY with the session's Terminal built by mk.
func setupPTYTerm(t *testing.T, handler sshtest.Handler, mk func(tty *os.File) Terminal) (master, tty *os.File, s *Session, h *ptyHarness) {
	t.Helper()
	if _, err := exec.LookPath("stty"); err != nil {
		t.Skip("stty not available")
	}
	master, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no PTY available: %v", err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	h = &ptyHarness{output: &syncBuffer{}, done: make(chan error, 1), drain: make(chan struct{})}
	go func() {
		defer close(h.drain)
		_, _ = io.Copy(h.output, master)
	}()
	t.Cleanup(func() {
		_ = tty.Close()
		_ = master.Close()
		<-h.drain
	})

	h.srv = sshtest.New(t, sshtest.WithPassword("u", "pw"), sshtest.WithHandler(handler))
	s, err = New(dialServer(t, h.srv), mk(tty), Options{Term: "xterm-256color"})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStdin(tty)
	s.SetStdout(tty)
	s.SetStderr(tty)
	return master, tty, s, h
}

func (h *ptyHarness) waitOutput(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(string(h.output.Bytes()), want) {
		if time.Now().After(deadline) {
			t.Fatalf("terminal output %q does not contain %q", h.output.Bytes(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readyThenByte writes "ready", waits for one input byte, then exits 3.
func readyThenByte(ss *sshtest.Session) {
	_, _ = ss.Write([]byte("ready\r\n"))
	buf := make([]byte, 1)
	if _, err := ss.Read(buf); err != nil {
		return
	}
	ss.Exit(3)
}

// readyThenBlock writes "ready" and waits until the connection goes away.
func readyThenBlock(ss *sshtest.Session) {
	_, _ = ss.Write([]byte("ready\r\n"))
	_, _ = io.Copy(io.Discard, ss)
}

func TestARCH_SESS_005_RestoreOnRemoteExit(t *testing.T) {
	master, tty, s, h := setupPTY(t, readyThenByte)
	before := stty(t, tty)

	go func() { h.done <- s.Run() }()
	h.waitOutput(t, "ready")
	if during := stty(t, tty); !strings.Contains(during, "-icanon") || !strings.Contains(during, "-echo ") {
		t.Errorf("terminal not in raw mode during the session:\n%s", during)
	}
	if _, err := master.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	var err error
	select {
	case err = <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the remote exited")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Status != 3 {
		t.Fatalf("Run() = %v, want exited (3)", err)
	}
	if after := stty(t, tty); after != before {
		t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	h.waitOutput(t, term.ResetSequence)

	// The stdin reader stopped before Run returned: the next line typed
	// reaches whoever reads the terminal next (the TUI), not the session.
	if _, err := master.Write([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := tty.Read(buf)
		got <- string(buf[:n])
	}()
	select {
	case line := <-got:
		if line != "next\n" {
			t.Fatalf("next read = %q, want %q", line, "next\n")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the next keystrokes never reached the terminal's next reader")
	}
	if n := len(h.srv.RequestsOfType("shell")); n != 1 {
		t.Fatalf("shell requests = %d", n)
	}
}

func TestARCH_SESS_005_RestoreOnNetworkError(t *testing.T) {
	_, tty, s, h := setupPTY(t, readyThenBlock)
	before := stty(t, tty)

	go func() { h.done <- s.Run() }()
	h.waitOutput(t, "ready")
	h.srv.DropConnections()
	var err error
	select {
	case err = <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the connection dropped")
	}
	var de *DisconnectError
	if !errors.As(err, &de) {
		t.Fatalf("Run() = %v, want a DisconnectError", err)
	}
	if after := stty(t, tty); after != before {
		t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	h.waitOutput(t, term.ResetSequence)
	if !bytes.Contains(h.output.Bytes(), []byte("ready")) {
		t.Fatal("remote output lost")
	}
}
