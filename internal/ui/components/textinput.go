package components

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
)

// DefaultCharLimit is the most runes a TextInput accepts when CharLimit is 0.
const DefaultCharLimit = 1024

// Mask is what an EchoMasked input shows while it holds a value. Its length
// is fixed, so it never reveals the length of the secret (SEC-LOG-005).
const Mask = "••••••••"

// EchoMode says how a TextInput shows its value.
type EchoMode int

const (
	// EchoNormal shows the value as typed.
	EchoNormal EchoMode = iota
	// EchoMasked shows Mask while the input holds a value and the
	// placeholder while it is empty, never the value or its length.
	EchoMasked
	// EchoNone shows nothing at all, not even a mask, only the cursor
	// (FR-IDENT-004: a keyboard-interactive answer the server does not
	// echo).
	EchoNone
)

var (
	placeholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cursorStyle      = lipgloss.NewStyle().Reverse(true)
)

// TextInput is a single-line text field. It replaces bubbles' textinput,
// which imports a clipboard package (SEC-CLIP-005); pasting arrives as
// tea.PasteMsg from the terminal and needs no clipboard access.
//
// Use it by pointer: NewTextInput returns one, its methods change it in
// place, and a form or modal owns its inputs. The zero value is not
// usable; call NewTextInput.
//
// Typed and pasted text is passed through domain.Sanitize, and newlines and
// tabs are dropped, so the value never holds control characters. Reset
// zeroes the runes, and editing never leaves an unzeroed copy of the value
// behind in a discarded buffer, so the input can hold a secret.
//
// Keys (from ui/keys): ← → or ctrl+b/ctrl+f move, home/end or
// ctrl+a/ctrl+e jump, backspace and delete erase, ctrl+u and ctrl+k erase to
// the start and end. Every key with text inserts it. Keys are handled only
// while the input is focused.
type TextInput struct {
	// Placeholder is shown, dimmed, while the value is empty.
	Placeholder string
	// Echo says how the value is shown; see EchoMode.
	Echo EchoMode
	// CharLimit caps the value in runes; 0 means DefaultCharLimit.
	CharLimit int
	// Width is the most terminal columns View uses, including the cursor
	// cell; 0 means unlimited. A longer value scrolls horizontally to keep
	// the cursor visible, and a wide rune is never cut in half.
	Width int

	value   []rune
	pos     int // cursor, as an index into value (0..len(value))
	focused bool
}

// NewTextInput returns an empty, blurred input with EchoNormal.
func NewTextInput() *TextInput { return &TextInput{} }

// Value returns the current text.
func (t *TextInput) Value() string { return string(t.value) }

// Len returns the length of the value in runes.
func (t *TextInput) Len() int { return len(t.value) }

// SetValue replaces the value with s (sanitised like typed text, and
// truncated to the limit) and puts the cursor at the end.
func (t *TextInput) SetValue(s string) {
	t.Reset()
	t.Insert(s)
}

// Reset zeroes and empties the value.
func (t *TextInput) Reset() {
	clear(t.value)
	t.value = t.value[:0]
	t.pos = 0
}

// Focus makes the input take keys and show its cursor.
func (t *TextInput) Focus() { t.focused = true }

// Blur makes the input ignore keys and hide its cursor.
func (t *TextInput) Blur() { t.focused = false }

// Focused reports whether the input has focus.
func (t *TextInput) Focused() bool { return t.focused }

// Cursor returns the cursor position in runes from the start.
func (t *TextInput) Cursor() int { return t.pos }

// SetCursor moves the cursor to rune position i, clamped to the value.
func (t *TextInput) SetCursor(i int) { t.pos = min(max(i, 0), len(t.value)) }

// Left moves the cursor one rune left.
func (t *TextInput) Left() { t.SetCursor(t.pos - 1) }

// Right moves the cursor one rune right.
func (t *TextInput) Right() { t.SetCursor(t.pos + 1) }

// Home moves the cursor to the start.
func (t *TextInput) Home() { t.pos = 0 }

// End moves the cursor past the last rune.
func (t *TextInput) End() { t.pos = len(t.value) }

func (t *TextInput) limit() int {
	if t.CharLimit > 0 {
		return t.CharLimit
	}
	return DefaultCharLimit
}

// Insert inserts s at the cursor and moves the cursor past it. s is
// sanitised; newlines and tabs are dropped; runes beyond the limit are
// dropped.
func (t *TextInput) Insert(s string) {
	var add []rune
	for _, r := range domain.Sanitize(s) {
		if r == '\n' || r == '\t' {
			continue
		}
		if len(t.value)+len(add) >= t.limit() {
			break
		}
		add = append(add, r)
	}
	if len(add) == 0 {
		return
	}
	n := len(t.value) + len(add)
	if n > cap(t.value) {
		grown := make([]rune, len(t.value), max(n, 2*cap(t.value), 16))
		copy(grown, t.value)
		clear(t.value[:cap(t.value)]) // leave no copy behind
		t.value = grown
	}
	t.value = t.value[:n]
	copy(t.value[t.pos+len(add):], t.value[t.pos:n-len(add)])
	copy(t.value[t.pos:], add)
	clear(add)
	t.pos += len(add)
}

// erase removes value[from:to] and zeroes the freed tail.
func (t *TextInput) erase(from, to int) {
	if from >= to {
		return
	}
	n := copy(t.value[from:], t.value[to:])
	clear(t.value[from+n:])
	t.value = t.value[:from+n]
	t.pos = from
}

// Backspace deletes the rune before the cursor.
func (t *TextInput) Backspace() {
	if t.pos > 0 {
		t.erase(t.pos-1, t.pos)
	}
}

// Delete deletes the rune under the cursor.
func (t *TextInput) Delete() {
	if t.pos < len(t.value) {
		t.erase(t.pos, t.pos+1)
	}
}

// DeleteToStart deletes everything before the cursor.
func (t *TextInput) DeleteToStart() { t.erase(0, t.pos) }

// DeleteToEnd deletes everything from the cursor on.
func (t *TextInput) DeleteToEnd() {
	p := t.pos
	t.erase(p, len(t.value))
	t.pos = p
}

// Update handles a key press or a paste while the input is focused. It
// returns no command; the signature matches FormField.
func (t *TextInput) Update(msg tea.Msg) tea.Cmd {
	if !t.focused {
		return nil
	}
	switch msg := msg.(type) {
	case tea.PasteMsg:
		t.Insert(msg.Content)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.CursorLeft):
			t.Left()
		case key.Matches(msg, keys.CursorRight):
			t.Right()
		case key.Matches(msg, keys.CursorHome):
			t.Home()
		case key.Matches(msg, keys.CursorEnd):
			t.End()
		case key.Matches(msg, keys.DeleteBack):
			t.Backspace()
		case key.Matches(msg, keys.DeleteForward):
			t.Delete()
		case key.Matches(msg, keys.DeleteToStart):
			t.DeleteToStart()
		case key.Matches(msg, keys.DeleteToEnd):
			t.DeleteToEnd()
		default:
			if msg.Text != "" {
				t.Insert(msg.Text)
			}
		}
	}
	return nil
}

// View renders the input. A focused input shows its cursor: "_" after the
// last rune, or the rune under it in reverse video. An empty input shows
// the placeholder, dimmed. The result is at most Width columns wide and is
// not padded.
func (t *TextInput) View() string {
	empty := len(t.value) == 0
	switch {
	case t.Echo == EchoNone:
		return t.cursorAtEnd("")
	case empty && t.Placeholder != "":
		ph := t.Placeholder
		if t.Width > 0 {
			w := t.Width
			if t.focused {
				w-- // the cursor cell
			}
			ph = clip([]rune(ph), w)
		}
		return t.cursorAtEnd("") + placeholderStyle.Render(ph)
	case t.Echo == EchoMasked:
		if empty {
			return t.cursorAtEnd("")
		}
		return t.cursorAtEnd(Mask)
	}

	if !t.focused {
		if t.Width > 0 {
			return clip(t.value, t.Width)
		}
		return string(t.value)
	}
	start, end := t.window()
	if t.pos == len(t.value) {
		return string(t.value[start:]) + "_"
	}
	return string(t.value[start:t.pos]) + cursorStyle.Render(string(t.value[t.pos])) + string(t.value[t.pos+1:end])
}

func (t *TextInput) cursorAtEnd(s string) string {
	if t.focused {
		return s + "_"
	}
	return s
}

// window returns the runes [start, end) a focused input shows, so that the
// cursor and its cell fit in Width columns. The window starts at the first
// rune when that keeps the cursor visible, and otherwise ends at the cursor.
func (t *TextInput) window() (start, end int) {
	if t.Width <= 0 {
		return 0, len(t.value)
	}
	end = t.pos
	used := 1 // the "_" after the last rune
	if t.pos < len(t.value) {
		used = runeWidth(t.value[t.pos])
		end++
	}
	before := 0
	for _, r := range t.value[:t.pos] {
		before += runeWidth(r)
	}
	if before+used > t.Width {
		start = t.pos
		for start > 0 && used+runeWidth(t.value[start-1]) <= t.Width {
			start--
			used += runeWidth(t.value[start])
		}
		return start, end
	}
	used += before
	for end < len(t.value) && used+runeWidth(t.value[end]) <= t.Width {
		used += runeWidth(t.value[end])
		end++
	}
	return 0, end
}

// clip returns the leading runes of rs that fit in w columns.
func clip(rs []rune, w int) string {
	used, end := 0, 0
	for end < len(rs) && used+runeWidth(rs[end]) <= w {
		used += runeWidth(rs[end])
		end++
	}
	return string(rs[:end])
}

func runeWidth(r rune) int { return lipgloss.Width(string(r)) }
