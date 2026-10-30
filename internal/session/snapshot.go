package session

// Size is a terminal size in cells.
type Size struct{ Cols, Rows int }

// MouseMode is the mouse reporting the remote application asked for
// (design §8.4, ADR-014).
type MouseMode int

// The mouse modes, from none to every motion.
const (
	MouseNone  MouseMode = iota // no mouse reporting
	MousePress                  // ?9, ?1000 or ?1001: button presses (and releases)
	MouseDrag                   // ?1002: presses, releases and motion with a button held
	MouseAny                    // ?1003: every motion
)

// Cursor is the emulator's cursor position, 0-based, in the session area.
type Cursor struct{ X, Y int }

// Snapshot is a copy of a session's screen, taken under the emulator lock
// so the UI never sees a half-written screen.
type Snapshot struct {
	// Lines holds Size.Rows lines of Size.Cols cells each, styled with SGR
	// sequences only: no OSC, no hyperlinks, no control characters
	// (ARCH-SESS-011, SEC-SESSION-005).
	Lines  []string
	Cursor *Cursor   // nil when the remote hides the cursor
	Mouse  MouseMode // the mouse reporting the remote asked for
	Alt    bool      // the alternate screen is active
	Seq    uint64    // increases with every processed output chunk
}
