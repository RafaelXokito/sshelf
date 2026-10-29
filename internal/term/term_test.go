package term

import (
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

func TestResizesStop(t *testing.T) {
	_, stop := NewTTY(nil).Resizes()
	stop()
	stop() // idempotent
}
