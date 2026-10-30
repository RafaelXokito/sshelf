package session

import (
	"strconv"

	uv "github.com/charmbracelet/ultraviolet"
)

// The key-encoding shim (ARCH-SESS-017, design §9.4). x/vt v0.1.0's
// SendKey sends nothing for special keys with Shift, Alt+Shift or Ctrl,
// nothing for Shift or Ctrl with Enter, Backspace, Escape and Ctrl+Shift
// with a letter, and ESC[H/ESC[F for Home and End in application cursor
// mode. encodeKey produces xterm's encoding for those keys; every other key
// goes to SendKey. The shim is deleted when a vt release fixes them
// (ADR-013).

// csiTilde is the CSI <n> ~ number of each key xterm encodes that way.
var csiTilde = map[rune]int{
	uv.KeyInsert: 2, uv.KeyDelete: 3, uv.KeyPgUp: 5, uv.KeyPgDown: 6,
	uv.KeyF5: 15, uv.KeyF6: 17, uv.KeyF7: 18, uv.KeyF8: 19,
	uv.KeyF9: 20, uv.KeyF10: 21, uv.KeyF11: 23, uv.KeyF12: 24,
}

// csiFinal is the final byte of each key xterm encodes as CSI 1;<m> X
// when modified (SS3 X or CSI X when not).
var csiFinal = map[rune]byte{
	uv.KeyUp: 'A', uv.KeyDown: 'B', uv.KeyRight: 'C', uv.KeyLeft: 'D',
	uv.KeyHome: 'H', uv.KeyEnd: 'F',
	uv.KeyF1: 'P', uv.KeyF2: 'Q', uv.KeyF3: 'R', uv.KeyF4: 'S',
}

// ctrlSymbols are the xterm control bytes of Ctrl with a digit or symbol.
var ctrlSymbols = map[rune]byte{
	'@': 0x00, '2': 0x00, ' ': 0x00,
	'[': 0x1b, '3': 0x1b,
	'\\': 0x1c, '4': 0x1c,
	']': 0x1d, '5': 0x1d,
	'^': 0x1e, '6': 0x1e, '~': 0x1e,
	'_': 0x1f, '7': 0x1f, '/': 0x1f, '-': 0x1f,
	'8': 0x7f, '?': 0x7f,
}

const keyMods = uv.ModShift | uv.ModAlt | uv.ModCtrl

// xtermMod is xterm's modifier parameter: 1 + Shift 1 + Alt 2 + Ctrl 4.
func xtermMod(m uv.KeyMod) int {
	n := 1
	if m&uv.ModShift != 0 {
		n++
	}
	if m&uv.ModAlt != 0 {
		n += 2
	}
	if m&uv.ModCtrl != 0 {
		n += 4
	}
	return n
}

// encodeKey returns the bytes for k when the shim handles it, and false
// when the emulator's SendKey encodes it correctly. A handled key with an
// empty sequence has no terminal encoding (media keys, F13 and above) and
// is dropped on purpose.
func encodeKey(k uv.KeyPressEvent, appCursor bool) (string, bool) {
	mods := k.Mod & keyMods
	if n, ok := csiTilde[k.Code]; ok {
		if mods == 0 {
			return "", false
		}
		return "\x1b[" + strconv.Itoa(n) + ";" + strconv.Itoa(xtermMod(mods)) + "~", true
	}
	if f, ok := csiFinal[k.Code]; ok {
		switch {
		case mods != 0:
			return "\x1b[1;" + strconv.Itoa(xtermMod(mods)) + string(f), true
		case appCursor && (k.Code == uv.KeyHome || k.Code == uv.KeyEnd):
			return "\x1bO" + string(f), true
		}
		return "", false
	}
	alt := ""
	if mods&uv.ModAlt != 0 {
		alt = "\x1b"
	}
	ctrl := mods&uv.ModCtrl != 0
	shift := mods&uv.ModShift != 0
	switch k.Code {
	case uv.KeyEnter:
		if ctrl || shift {
			return alt + "\r", true
		}
	case uv.KeyBackspace:
		if ctrl {
			return alt + "\x08", true
		}
		if shift {
			return alt + "\x7f", true
		}
	case uv.KeyEscape:
		if ctrl || shift {
			return alt + "\x1b", true
		}
	case uv.KeyTab:
		if ctrl {
			return alt + "\t", true
		}
	case uv.KeySpace:
		if ctrl {
			return alt + "\x00", true
		}
		if shift {
			return alt + " ", true
		}
	}
	if k.Code >= uv.KeyExtended {
		if isVTKey(k.Code) {
			return "", false
		}
		return "", true // no terminal encoding
	}
	if ctrl {
		c := k.Code
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' {
			return alt + string(c-'a'+1), true
		}
		if b, ok := ctrlSymbols[c]; ok {
			return alt + string(rune(b)), true
		}
		// xterm sends the character itself for Ctrl with any other key.
		if k.Text != "" {
			return alt + k.Text, true
		}
		return alt + string(k.Code), true
	}
	if shift && k.Text == "" && k.Code >= 0x20 {
		// A shifted key without its text: send the shifted character.
		c := k.Code
		if k.ShiftedCode != 0 {
			c = k.ShiftedCode
		}
		return alt + string(c), true
	}
	return "", false
}

// isVTKey reports whether code is a special key the emulator encodes.
func isVTKey(code rune) bool {
	switch code {
	case uv.KeyUp, uv.KeyDown, uv.KeyRight, uv.KeyLeft, uv.KeyHome, uv.KeyEnd,
		uv.KeyInsert, uv.KeyDelete, uv.KeyPgUp, uv.KeyPgDown,
		uv.KeyKpEnter, uv.KeyKpEqual, uv.KeyKpMultiply, uv.KeyKpPlus, uv.KeyKpComma,
		uv.KeyKpMinus, uv.KeyKpDecimal, uv.KeyKpDivide,
		uv.KeyKp0, uv.KeyKp1, uv.KeyKp2, uv.KeyKp3, uv.KeyKp4,
		uv.KeyKp5, uv.KeyKp6, uv.KeyKp7, uv.KeyKp8, uv.KeyKp9:
		return true
	}
	return code >= uv.KeyF1 && code <= uv.KeyF12
}

// normalizeKey clears the fields the emulator's table lookup compares but
// sshelf does not use, so that a key from Bubble Tea matches the table.
func normalizeKey(k uv.KeyPressEvent) uv.KeyPressEvent {
	k.IsRepeat = false
	k.BaseCode, k.ShiftedCode = 0, 0
	k.Mod &= keyMods
	if k.Mod&uv.ModCtrl != 0 || k.Code >= uv.KeyExtended || k.Code < 0x20 || k.Code == uv.KeyBackspace {
		k.Text = ""
	}
	return k
}
