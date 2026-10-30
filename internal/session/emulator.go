package session

// This is the only file that imports github.com/charmbracelet/x/vt
// (ADR-013): a later version or another emulator touches this file only.

import (
	"image/color"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
	"github.com/charmbracelet/x/vt"
)

// DefaultScrollback is the number of scrollback lines a session keeps
// (D-013, NFR-SIZE-003).
const DefaultScrollback = 2000

// scrollbackSlack is how many lines beyond the cap the emulator may hold
// during one write. sshelf trims them itself after every write, which is
// how it counts the lines the cap drops (vt does not report them).
const scrollbackSlack = writeChunk

// screen wraps a vt.Emulator with sshelf's own mutex (ADR-013 limitation 5:
// vt.SafeEmulator can block rendering on its reply pipe). Every method
// takes the lock; none of them waits on the network. Bytes for the remote
// (encoded keys, pastes, mouse events and the emulator's answers to
// queries) come out of Read, which takes no lock: a reply pump must read
// it all the time, because the emulator writes them into an unbuffered
// pipe while the lock is held.
type screen struct {
	mu   sync.Mutex
	emu  *vt.Emulator
	pipe *io.PipeWriter // the emulator's side of the reply pipe
	pr   io.Reader      // the reading side, through the emulator
	seq  uint64

	sbCap   int // scrollback lines kept
	sbLen   int // scrollback lines after the last write
	dropped int // lines dropped from the front of the history so far

	// Modes, tracked through the emulator's callbacks.
	mouseX10, mouseNormal, mouseHighlight, mouseButton, mouseAny bool
	mouseSGR                                                     bool // ?1006
	appCursor, autowrap                                          bool

	// The wide-character workaround (D-015, ADR-013 limitation 2): a
	// second parser finds where each printed character starts, so the
	// emulator can be fed up to that point and its cursor checked before a
	// double-width character that would not fit in the line.
	shadow   *ansi.Parser
	buf      []byte // the bytes of the write being parsed (held + new)
	pos      int    // index in buf of the byte the shadow parser is at
	fed      int    // bytes of buf already written to the emulator
	held     []byte // an incomplete UTF-8 sequence kept for the next write
	prev     rune   // the last printed rune; 0 after any other event
	wideLast bool   // the last character cluster started with a wide rune
}

// newScreen returns a cols×rows screen keeping scrollback lines of
// history (0 means DefaultScrollback).
func newScreen(cols, rows, scrollback int) *screen {
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	if scrollback <= 0 {
		scrollback = DefaultScrollback
	}
	s := &screen{emu: vt.NewEmulator(cols, rows), autowrap: true, sbCap: scrollback}
	s.emu.SetScrollbackSize(scrollback + scrollbackSlack)
	if pw, ok := s.emu.InputPipe().(*io.PipeWriter); ok {
		s.pipe = pw
	}
	s.pr = readerFunc(s.emu.Read)
	// Title and IconName stay nil: remote titles are ignored (D-014).
	s.emu.SetCallbacks(vt.Callbacks{
		EnableMode:  func(m ansi.Mode) { s.setMode(m, true) },
		DisableMode: func(m ansi.Mode) { s.setMode(m, false) },
	})
	s.shadow = ansi.NewParser()
	s.shadow.SetDataSize(16) // the shadow parser never needs string data
	s.shadow.SetHandler(ansi.Handler{
		Print:     s.onPrint,
		Execute:   func(byte) { s.onOther() },
		HandleCsi: func(ansi.Cmd, ansi.Params) { s.onOther() },
		HandleEsc: func(ansi.Cmd) { s.onOther() },
		HandleDcs: func(ansi.Cmd, ansi.Params, []byte) { s.onOther() },
		HandleOsc: func(int, []byte) { s.onOther() },
		HandlePm:  func([]byte) { s.onOther() },
		HandleApc: func([]byte) { s.onOther() },
		HandleSos: func([]byte) { s.onOther() },
	})
	return s
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// setMode records the modes sshelf needs to know about. It runs inside
// the emulator, under s.mu.
func (s *screen) setMode(m ansi.Mode, on bool) {
	switch m {
	case ansi.ModeMouseX10:
		s.mouseX10 = on
	case ansi.ModeMouseNormal:
		s.mouseNormal = on
	case ansi.ModeMouseHighlight:
		s.mouseHighlight = on
	case ansi.ModeMouseButtonEvent:
		s.mouseButton = on
	case ansi.ModeMouseAnyEvent:
		s.mouseAny = on
	case ansi.ModeMouseExtSgr:
		s.mouseSGR = on
	case ansi.ModeCursorKeys:
		s.appCursor = on
	case ansi.ModeAutoWrap:
		s.autowrap = on
	}
}

func (s *screen) mouseMode() MouseMode {
	switch {
	case s.mouseAny:
		return MouseAny
	case s.mouseButton:
		return MouseDrag
	case s.mouseX10, s.mouseNormal, s.mouseHighlight:
		return MousePress
	}
	return MouseNone
}

// Read returns bytes for the remote: encoded input and the emulator's
// replies. It takes no lock and blocks until there are some, or returns
// io.EOF after closeReplies.
func (s *screen) Read(p []byte) (int, error) { return s.pr.Read(p) }

// closeReplies ends Read with io.EOF. Later replies are discarded. It does
// not touch the emulator's own state, so it is safe while Read blocks.
func (s *screen) closeReplies() {
	if s.pipe != nil {
		_ = s.pipe.CloseWithError(io.EOF)
	}
}

// write feeds remote output to the emulator.
func (s *screen) write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	if len(s.held) > 0 {
		s.buf = append(s.held, p...)
		s.held = nil
	} else {
		s.buf = p
	}
	s.fed = 0
	for i := len(s.buf) - len(p); i < len(s.buf); i++ {
		s.pos = i
		s.shadow.Advance(s.buf[i])
	}
	end := len(s.buf)
	// Keep an incomplete UTF-8 character for the next write, so that the
	// workaround can always feed the emulator up to a character's first
	// byte.
	if s.shadow.State() == parser.Utf8State {
		for j := end - 1; j >= s.fed && j >= end-utf8.UTFMax; j-- {
			if utf8.RuneStart(s.buf[j]) {
				end = j
				break
			}
		}
		s.held = append([]byte(nil), s.buf[end:]...)
	}
	s.feed(end)
	s.buf = nil
}

// feed writes s.buf[s.fed:end] to the emulator.
func (s *screen) feed(end int) {
	if end > s.fed {
		s.emuWrite(s.buf[s.fed:end])
		s.fed = end
	}
}

// emuWrite writes b to the emulator and keeps the scrollback at its cap.
func (s *screen) emuWrite(b []byte) {
	_, _ = s.emu.Write(b)
	s.trimScrollback()
}

// trimScrollback drops the lines beyond the cap and counts them, and
// counts the lines a remote cleared (ED 3) as dropped too, so that a
// position in the history stays valid (see Session.Dropped). A write
// that pushes more than scrollbackSlack lines at once loses the excess
// uncounted inside vt; feedEmulator keeps writes to writeChunk bytes.
func (s *screen) trimScrollback() {
	n := s.emu.ScrollbackLen()
	if n < s.sbLen { // cleared
		s.dropped += s.sbLen
	}
	if n > s.sbCap {
		s.dropped += n - s.sbCap
		s.emu.SetScrollbackSize(s.sbCap)
		s.emu.SetScrollbackSize(s.sbCap + scrollbackSlack)
		n = s.sbCap
	}
	s.sbLen = n
}

// droppedLines returns how many lines have left the front of the history.
func (s *screen) droppedLines() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// onOther handles every parser event other than a printed character.
func (s *screen) onOther() {
	s.prev = 0
	s.wideLast = false
}

// onPrint runs for every printed rune, on the shadow parser, before the
// emulator has seen the rune's bytes. x/vt v0.1.0 drops a double-width
// character that does not fit in the line, and after one that fills the
// last two columns it overwrites the last column instead of wrapping. Both
// are fixed here: before such a character is printed, and before the
// character after one that ended at the margin, the emulator gets "\r\n",
// which is what the pending wrap of an xterm does.
func (s *screen) onPrint(r rune) {
	prev := s.prev
	s.prev = r
	if r < 0x300 { // no wide or combining character below U+0300
		if s.wideLast {
			s.wideLast = false
			s.wrapIfPending(r, false)
		}
		return
	}
	w := runeWidth(r)
	if w == 0 || joins(prev, r) {
		return // part of the cluster before it
	}
	wide := w > 1
	if s.wideLast || wide {
		s.wrapIfPending(r, wide)
	}
	s.wideLast = wide
}

// wrapIfPending feeds the emulator up to rune r and moves the cursor to the
// next line if r would not fit (wide) or if the previous wide character
// filled the line.
func (s *screen) wrapIfPending(r rune, wide bool) {
	start := s.pos + 1 - utf8.RuneLen(r)
	if start < s.fed {
		return // cannot happen: incomplete characters are held back
	}
	s.feed(start)
	if !s.autowrap {
		return
	}
	pos := s.emu.CursorPosition()
	w := s.emu.Width()
	switch {
	case wide && pos.X >= w-1:
		// A wide character in the last column: xterm wraps it.
	case pos.X == w-1 && w >= 2 && s.cellWidth(w-2, pos.Y) == 2:
		// The previous wide character filled the last two columns.
	default:
		return
	}
	s.emuWrite([]byte("\r\n"))
}

func (s *screen) cellWidth(x, y int) int {
	if c := s.emu.CellAt(x, y); c != nil {
		return c.Width
	}
	return 0
}

// runeWidth is the display width of r on its own.
func runeWidth(r rune) int {
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], r)
	return ansi.GraphemeWidth.StringWidth(string(b[:n]))
}

// joins reports whether r continues the grapheme cluster of prev rather
// than starting a new one, for the wide runes that can: emoji after a
// zero-width joiner, skin-tone modifiers and the second regional indicator
// of a flag.
func joins(prev, r rune) bool {
	const zwj = 0x200D
	switch {
	case prev == zwj:
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // emoji modifiers
		return true
	case isRegional(r) && isRegional(prev):
		return true
	}
	return false
}

func isRegional(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// resize changes the screen size. Lines are cut or padded, not reflowed
// (ADR-013 limitation 6).
func (s *screen) resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cols == s.emu.Width() && rows == s.emu.Height() {
		return
	}
	s.emu.Resize(cols, rows)
	s.trimScrollback()
}

// snapshot copies the visible screen.
func (s *screen) snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	cols, rows := s.emu.Width(), s.emu.Height()
	snap := Snapshot{
		Lines: make([]string, rows),
		Mouse: s.mouseMode(),
		Alt:   s.emu.IsAltScreen(),
		Seq:   s.seq,
	}
	var b strings.Builder
	for y := range rows {
		b.Reset()
		renderCells(&b, cols, func(x int) *uv.Cell { return s.emu.CellAt(x, y) })
		snap.Lines[y] = b.String()
	}
	if !s.emu.CursorHidden() {
		p := s.emu.CursorPosition()
		snap.Cursor = &Cursor{X: p.X, Y: p.Y}
	}
	return snap
}

// scrollbackLen returns the number of lines in the scrollback buffer.
func (s *screen) scrollbackLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emu.ScrollbackLen()
}

// scrollback returns rows lines starting at top of the history: the
// scrollback lines, oldest first, followed by the current screen. total is
// the number of lines in the history. Lines are rendered like snapshot
// lines, at the current width.
func (s *screen) scrollback(top, rows int) (lines []string, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cols, height := s.emu.Width(), s.emu.Height()
	sb := s.emu.Scrollback()
	n := 0
	if sb != nil {
		n = sb.Len()
	}
	total = n + height
	top = max(0, min(top, total))
	rows = max(0, min(rows, total-top))
	lines = make([]string, 0, rows)
	var b strings.Builder
	for i := top; i < top+rows; i++ {
		b.Reset()
		if i < n {
			line := sb.Line(i)
			renderCells(&b, cols, func(x int) *uv.Cell {
				if x < len(line) {
					return &line[x]
				}
				return nil
			})
		} else {
			y := i - n
			renderCells(&b, cols, func(x int) *uv.Cell { return s.emu.CellAt(x, y) })
		}
		lines = append(lines, b.String())
	}
	return lines, total
}

// sendKey encodes a key press for the remote: the shim for the keys the
// emulator drops (ARCH-SESS-017), otherwise the emulator's SendKey.
func (s *screen) sendKey(k uv.KeyPressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq, ok := encodeKey(k, s.appCursor); ok {
		if seq != "" {
			_, _ = io.WriteString(s.emu.InputPipe(), seq)
		}
		return
	}
	s.emu.SendKey(normalizeKey(k))
}

// sendMouse encodes a mouse event for the remote, X10 or SGR (?1006) as
// the remote asked (ADR-013 limitation 7), and drops it unless the
// remote's mouse mode reports it (wantsMouse).
func (s *screen) sendMouse(m uv.MouseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.wantsMouse(m) {
		return
	}
	if r, ok := m.(uv.MouseReleaseEvent); ok && !s.mouseSGR {
		// X10 encoding has no button in a release (button code 3); the
		// emulator would send the pressed button, as for a press.
		r.Button = uv.MouseNone
		m = r
	}
	s.emu.SendMouse(m)
}

// wantsMouse reports whether the remote's mouse modes report m (FR-TAB-011):
// presses and wheel notches in every mode; releases in all but X10 (?9);
// motion with a button held under ?1002, and any motion under ?1003. The
// emulator itself only checks that some mode is on.
func (s *screen) wantsMouse(m uv.MouseEvent) bool {
	switch m.(type) {
	case uv.MouseMotionEvent:
		return s.mouseAny || s.mouseButton && m.Mouse().Button != uv.MouseNone
	case uv.MouseReleaseEvent:
		return s.mouseNormal || s.mouseHighlight || s.mouseButton || s.mouseAny
	}
	return s.mouseMode() != MouseNone
}

// paste sends text, bracketed when the remote enabled ?2004.
func (s *screen) paste(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emu.Paste(text)
}

// sendBytes sends b to the remote unchanged, in order with keys and
// replies.
func (s *screen) sendBytes(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.emu.InputPipe().Write(b)
}

// setDefaultColors sets the colours the emulator reports for OSC 10 and
// 11 queries (ARCH-SESS-016). nil keeps the emulator's default.
func (s *screen) setDefaultColors(fg, bg color.Color) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fg != nil {
		s.emu.SetDefaultForegroundColor(fg)
	}
	if bg != nil {
		s.emu.SetDefaultBackgroundColor(bg)
	}
}

// renderCells writes cols cells as text with SGR styles only. Hyperlinks
// are dropped (ARCH-SESS-011), and any control character a cell might hold
// is drawn as a space, so the result can never carry an escape sequence
// other than SGR (SEC-SESSION-005).
func renderCells(b *strings.Builder, cols int, at func(x int) *uv.Cell) {
	var pen uv.Style
	for x := 0; x < cols; {
		c := at(x)
		w := 1
		content := " "
		var style uv.Style
		if c != nil {
			style = c.Style
			if c.Width > 1 && x+c.Width <= cols && safeContent(c.Content) {
				w, content = c.Width, c.Content
			} else if c.Width == 1 && c.Content != "" && safeContent(c.Content) {
				content = c.Content
			}
		}
		if !style.Equal(&pen) {
			if style.IsZero() {
				b.WriteString(ansi.ResetStyle)
			} else {
				b.WriteString(uv.StyleDiff(&pen, &style))
			}
			pen = style
		}
		b.WriteString(content)
		x += w
	}
	if !pen.IsZero() {
		b.WriteString(ansi.ResetStyle)
	}
}

// safeContent reports whether s holds no C0 or C1 control character and
// no DEL, raw or UTF-8 encoded, and is valid UTF-8.
func safeContent(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return false
		}
	}
	return true
}
