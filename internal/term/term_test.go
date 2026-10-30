package term

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/testutil"
)

func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }

func TestSafeGoRecoversPanic(t *testing.T) {
	got := make(chan any, 1)
	SafeGo(func() { panic("boom") }, func(r any) { got <- r })
	select {
	case r := <-got:
		if r != "boom" {
			t.Fatalf("recovered %v, want boom", r)
		}
	case <-time.After(time.Second):
		t.Fatal("onPanic not called")
	}
}

// panicsHere is a named frame for the stack check below.
//
//go:noinline
func panicsHere() { panic("boom") }

func TestARCH_SEC_004_SafeGoReportsBeforeOnPanic(t *testing.T) {
	var order []string
	var stack []byte
	done := make(chan struct{})
	SetPanicReporter(func(r any, s []byte) { order = append(order, fmt.Sprint("report ", r)); stack = s })
	t.Cleanup(func() { SetPanicReporter(nil) })
	SafeGo(panicsHere, func(r any) { order = append(order, fmt.Sprint("onPanic ", r)); close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onPanic not called")
	}
	if strings.Join(order, ", ") != "report boom, onPanic boom" {
		t.Fatalf("calls: %v", order)
	}
	if !strings.Contains(string(stack), "panicsHere") {
		t.Fatalf("reported stack lacks the panicking frame:\n%s", stack)
	}

	SetPanicReporter(nil)
	got := make(chan any, 1)
	SafeGo(panicsHere, func(r any) { got <- r })
	if r := <-got; r != "boom" {
		t.Fatalf("without a reporter: recovered %v", r)
	}
}

func TestResetSequenceCoversDesign(t *testing.T) {
	// Design §8.6: alt screen off, mouse off, bracketed paste off, cursor
	// keys and keypad normal, cursor shown, attributes reset, kitty pop.
	for _, want := range []string{"\x1b[?1049l", "\x1b[?1000l", "\x1b[?1002l", "\x1b[?1003l", "\x1b[?1006l",
		"\x1b[?2004l", "\x1b[?1l", "\x1b>", "\x1b[?25h", "\x1b[0m", "\x1b[<u"} {
		if !strings.Contains(ResetSequence, want) {
			t.Errorf("ResetSequence lacks %q", want)
		}
	}
}
