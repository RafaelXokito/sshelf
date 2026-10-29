package term

import (
	"os"

	xterm "golang.org/x/term"
)

// ResetSequence undoes terminal modes a remote program may have left on
// (design §8.6): leave the alternate screen, mouse tracking off, bracketed
// paste off, normal cursor keys and keypad, cursor visible, attributes
// reset, and pop the kitty keyboard mode.
const ResetSequence = "\x1b[?1049l" +
	"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" +
	"\x1b[?2004l" +
	"\x1b[?1l\x1b>" +
	"\x1b[?25h" +
	"\x1b[0m" +
	"\x1b[<u"

// TTY is the real terminal behind a file descriptor.
type TTY struct {
	f *os.File
}

// NewTTY returns the terminal behind f, usually os.Stdin.
func NewTTY(f *os.File) *TTY { return &TTY{f: f} }

func (t *TTY) fd() int { return int(t.f.Fd()) } //nolint:gosec // a file descriptor fits in an int

// MakeRaw puts the terminal into raw mode and returns a function that
// restores the previous state.
func (t *TTY) MakeRaw() (restore func() error, err error) {
	fd := t.fd()
	st, err := xterm.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return xterm.Restore(fd, st) }, nil
}

// Size returns the terminal's columns and rows.
func (t *TTY) Size() (cols, rows int, err error) {
	return xterm.GetSize(t.fd())
}

// Resizes delivers an event whenever the terminal is resized (SIGWINCH).
// stop ends the notifications.
func (t *TTY) Resizes() (events <-chan struct{}, stop func()) {
	return resizeEvents()
}
