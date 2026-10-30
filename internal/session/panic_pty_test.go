//go:build unix

package session

import (
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/term"
)

// faultyTerm is the PTY's terminal with operations that panic on demand.
type faultyTerm struct {
	*term.TTY
	events       chan struct{}
	panicSize    atomic.Bool // Size panics (it runs on the resize goroutine)
	panicResizes bool        // Resizes panics (it runs on Run's goroutine)
}

func (f *faultyTerm) Size() (int, int, error) {
	if f.panicSize.Load() {
		panic("injected: Size")
	}
	return f.TTY.Size()
}

func (f *faultyTerm) Resizes() (<-chan struct{}, func()) {
	if f.panicResizes {
		panic("injected: Resizes")
	}
	return f.events, func() {}
}

func setupFaulty(t *testing.T, resizesPanics bool) (master, tty *os.File, s *Attached, h *ptyHarness, ft *faultyTerm) {
	t.Helper()
	master, tty, s, h = setupPTYTerm(t, readyThenBlock, func(tty *os.File) Terminal {
		ft = &faultyTerm{TTY: term.NewTTY(tty), events: make(chan struct{}, 1), panicResizes: resizesPanics}
		return ft
	})
	return master, tty, s, h, ft
}

// TestARCH_SESS_005_RestoreOnPanic: a panic in a session goroutine (here
// the resize forwarder) is recovered and reported, the session ends with
// ErrInternal, the terminal is restored and the next keystroke reaches the
// TUI (ARCH-SEC-004, design §8.6).
func TestARCH_SESS_005_RestoreOnPanic(t *testing.T) {
	reported := make(chan any, 1)
	term.SetPanicReporter(func(r any, stack []byte) {
		if !strings.Contains(string(stack), "forwardResizes") {
			t.Errorf("reported stack lacks the panicking goroutine:\n%s", stack)
		}
		reported <- r
	})
	t.Cleanup(func() { term.SetPanicReporter(nil) })

	master, tty, s, h, ft := setupFaulty(t, false)
	before := stty(t, tty)
	go func() { h.done <- s.Run() }()
	h.waitOutput(t, "ready")
	if during := stty(t, tty); !strings.Contains(during, "-icanon") {
		t.Fatalf("terminal not in raw mode during the session:\n%s", during)
	}

	ft.panicSize.Store(true)
	ft.events <- struct{}{}
	var err error
	select {
	case err = <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a session goroutine panicked")
	}
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("Run() = %v, want ErrInternal", err)
	}
	select {
	case r := <-reported:
		if r != "injected: Size" {
			t.Fatalf("reported %v", r)
		}
	default:
		t.Fatal("the panic was not reported")
	}
	if after := stty(t, tty); after != before {
		t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	h.waitOutput(t, term.ResetSequence)
	expectNextRead(t, master, tty)
}

// TestARCH_SESS_005_RestoreOnPanicInRun: a panic on Run's own goroutine,
// in raw mode, still restores the terminal and writes the reset sequence
// before it propagates to main's handler.
func TestARCH_SESS_005_RestoreOnPanicInRun(t *testing.T) {
	master, tty, s, h, _ := setupFaulty(t, true)
	before := stty(t, tty)
	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		_ = s.Run()
	}()
	select {
	case r := <-panicked:
		if r != "injected: Resizes" {
			t.Fatalf("Run panicked with %v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not panic")
	}
	if after := stty(t, tty); after != before {
		t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	h.waitOutput(t, term.ResetSequence)
	expectNextRead(t, master, tty)
}

// expectNextRead checks that the stdin pump stopped: the next line typed
// reaches the terminal's next reader.
func expectNextRead(t *testing.T, master, tty *os.File) {
	t.Helper()
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
}
