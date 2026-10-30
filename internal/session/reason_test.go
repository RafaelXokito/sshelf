package session

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// hostileReason is server text with everything SEC-SESSION-006 must
// remove: an OSC 52 clipboard write, a title, a CSI, a C1 control, a
// newline, a tab and far more than MaxReasonWidth columns.
var hostileReason = "KILL\x1b]52;c;ZXZpbA==\x07\x1b]0;evil-title\x07\x1b[2J\x9b31m\nline2\tcol " + strings.Repeat("x", 200)

// checkReason fails unless reason is one clean line of at most
// MaxReasonWidth columns, cut with "…".
func checkReason(t *testing.T, reason string) {
	t.Helper()
	if w := ansi.StringWidth(reason); w > MaxReasonWidth {
		t.Errorf("reason is %d columns, want at most %d: %q", w, MaxReasonWidth, reason)
	}
	if !strings.HasSuffix(reason, "…") {
		t.Errorf("a cut reason ends with …: %q", reason)
	}
	for _, r := range reason {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			t.Errorf("reason holds control %U: %q", r, reason)
		}
	}
	for _, bad := range []string{"ZXZpbA", "evil-title", "52;c", "[2J"} {
		if strings.Contains(reason, bad) {
			t.Errorf("reason holds %q: %q", bad, reason)
		}
	}
}

// TestSEC_SESSION_006_ReasonSanitisedAndCut: both end errors that carry
// server text render it sanitised, on one line and cut to
// MaxReasonWidth columns; the ones that carry none are unchanged.
func TestSEC_SESSION_006_ReasonSanitisedAndCut(t *testing.T) {
	checkReason(t, (&ExitError{Signal: hostileReason}).Error())
	checkReason(t, (&DisconnectError{Err: errors.New("ssh: disconnect, reason 11: " + hostileReason)}).Error())

	for err, want := range map[error]string{
		&ExitError{Status: 3}:                        "exited (3)",
		&ExitError{Signal: "KILL"}:                   "killed by signal KILL",
		&DisconnectError{}:                           "connection lost: connection closed",
		&DisconnectError{Err: errors.New("a\nb\tc")}: "connection lost: a b c",
	} {
		if got := err.Error(); got != want {
			t.Errorf("%#v: got %q, want %q", err, got, want)
		}
	}
}

// TestSEC_SESSION_006_SignalFromServer: a server that ends the session
// with a hostile exit-signal name gets an ExitError whose reason is safe,
// and the debug log's end record holds that reason, not the raw name.
func TestSEC_SESSION_006_SignalFromServer(t *testing.T) {
	var log syncBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv := newServer(t, sshtest.WithHandler(func(ss *sshtest.Session) {
		ss.ExitSignal(hostileReason)
	}))
	s := open(t, srv, Options{})
	waitDone(t, s)
	var ee *ExitError
	if !errors.As(s.Err(), &ee) {
		t.Fatalf("Err() = %v, want an *ExitError", s.Err())
	}
	checkReason(t, s.Err().Error())
	// The raw 0x9b byte is invalid UTF-8 and becomes U+FFFD (Sanitize).
	if !strings.HasPrefix(s.Err().Error(), "killed by signal KILL�31m line2 col xxx") {
		t.Errorf("reason %q", s.Err().Error())
	}
	got := log.String()
	if !strings.Contains(got, "session: ended") || strings.Contains(got, "ZXZpbA") || strings.Contains(got, "evil-title") || strings.Contains(got, strings.Repeat("x", MaxReasonWidth)) {
		t.Errorf("debug log %q", got)
	}
}

// syncBuffer is a bytes.Buffer safe for the session's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
