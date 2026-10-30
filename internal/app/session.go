package app

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/RafaelXokito/sshelf/internal/session"
)

// The session shape (design §8.4). The types are the session package's.

// Size is a session area in cells.
type Size = session.Size

// Cursor is the remote cursor, zero-based within the session area.
type Cursor = session.Cursor

// MouseMode is the mouse reporting a remote app asked for.
type MouseMode = session.MouseMode

// Mouse modes (design §8.4).
const (
	MouseNone  = session.MouseNone
	MousePress = session.MousePress // ?9 / ?1000
	MouseDrag  = session.MouseDrag  // ?1002
	MouseAny   = session.MouseAny   // ?1003
)

// Snapshot is a copy of a session's screen, taken under the emulator lock.
type Snapshot = session.Snapshot

// Session is a session rendered inside sshelf's frame (design §8.4,
// ADR-012). Input methods never block. Updates is closed after the end;
// then Err reports the cause once: nil (exit 0), *session.ExitError,
// *session.DisconnectError or session.ErrInternal.
type Session interface {
	SendKey(k uv.KeyPressEvent) error
	SendMouse(m uv.MouseEvent) error
	Paste(text string) error
	SendBytes(b []byte) error
	Resize(sz Size)
	SetDefaultColors(fg, bg color.Color)

	Snapshot() Snapshot
	// Scrollback returns up to rows lines of the session's scrollback
	// followed by its screen, as one view, starting at line top (0 is the
	// oldest line kept), and the number of lines in that view.
	Scrollback(top, rows int) (lines []string, total int)
	// Dropped returns how many lines have left the front of that view
	// since the session started (the scrollback cap). A view position
	// stored as top+Dropped() stays on the same line.
	Dropped() int

	Updates() <-chan struct{}
	Done() <-chan struct{}
	Err() error
	Close() error
}

// The emulator-backed session is a Session.
var _ Session = (*session.Session)(nil)
