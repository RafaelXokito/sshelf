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

// SaveState saves the state of the terminal behind f and returns a
// function that restores it. main saves the startup state so that its
// panic handler can put the terminal back whatever mode it was left in
// (ARCH-SEC-004).
func SaveState(f *os.File) (restore func() error, err error) {
	fd := int(f.Fd()) //nolint:gosec // a file descriptor fits in an int
	st, err := xterm.GetState(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return xterm.Restore(fd, st) }, nil
}
