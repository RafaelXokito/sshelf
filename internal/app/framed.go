package app

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

// The M2 session shape (design §8.4). Until T-037 wires the emulator-backed
// session.Session, these types are defined here with the shapes design §8.4
// gives the session package; T-037 turns them into aliases of the session
// types and renames FramedSession to Session.

// Size is a session area in cells.
type Size struct{ Cols, Rows int }

// Cursor is the remote cursor, zero-based within the session area.
type Cursor struct{ X, Y int }

// MouseMode is the mouse reporting a remote app asked for.
type MouseMode int

// Mouse modes (design §8.4).
const (
	MouseNone  MouseMode = iota
	MousePress           // ?9 / ?1000
	MouseDrag            // ?1002
	MouseAny             // ?1003
)

// Snapshot is a copy of a session's screen, taken under the emulator lock.
type Snapshot struct {
	Lines  []string  // Size.Rows lines of Size.Cols cells, SGR styles only
	Cursor *Cursor   // nil when the remote hides it
	Mouse  MouseMode // what the remote app asked for
	Alt    bool      // alternate screen active
	Seq    uint64    // increases with every processed output chunk
}

// FramedSession is a session rendered inside sshelf's frame (design §8.4,
// ADR-012). Input methods never block. Updates is closed after the end;
// then Err reports the cause once: nil (exit 0), *session.ExitError,
// *session.DisconnectError or session.ErrInternal.
type FramedSession interface {
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

	Updates() <-chan struct{}
	Done() <-chan struct{}
	Err() error
	Close() error
}
