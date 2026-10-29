package domain

import (
	"strings"
	"unicode/utf8"
)

// Sanitize makes an untrusted string (server-supplied or imported) safe to
// render in sshelf's UI (SEC-SESSION-003). It removes ANSI escape
// sequences (CSI, OSC, DCS, SOS, PM, APC and two-byte ESC sequences, in
// their 7-bit and C1 forms), then every remaining C0 control except tab
// and newline, DEL and every C1 control. Invalid UTF-8 becomes U+FFFD.
// Sanitize(Sanitize(s)) == Sanitize(s).
func Sanitize(s string) string {
	if isClean(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == 0x1b: // ESC
			if i >= len(s) {
				continue
			}
			next, n := utf8.DecodeRuneInString(s[i:])
			switch next {
			case '[':
				i = skipCSI(s, i+n)
			case ']', 'P', 'X', '^', '_':
				i = skipString(s, i+n)
			default:
				if next >= 0x20 && next < 0x7f {
					i += n // a two-byte sequence such as ESC 7 or ESC c
				}
			}
		case r == 0x9b: // C1 CSI
			i = skipCSI(s, i)
		case r == 0x9d, r == 0x90, r == 0x98, r == 0x9e, r == 0x9f: // C1 OSC, DCS, SOS, PM, APC
			i = skipString(s, i)
		case r == utf8.RuneError && size == 1:
			b.WriteRune(utf8.RuneError)
		case isControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isControl reports whether r is a C0 control other than tab and newline,
// DEL or a C1 control.
func isControl(r rune) bool {
	return (r < 0x20 && r != '\t' && r != '\n') || (r >= 0x7f && r <= 0x9f)
}

func isClean(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if isControl(r) {
			return false
		}
	}
	return true
}

// skipCSI returns the index after the CSI sequence whose parameters start
// at i: parameter and intermediate bytes, then one final byte. A malformed
// sequence ends at the first byte outside those ranges, which is dropped.
func skipCSI(s string, i int) int {
	for i < len(s) {
		c := s[i]
		i++
		if c >= 0x40 && c <= 0x7e { // final byte
			return i
		}
		if c < 0x20 || c > 0x7e {
			return i
		}
	}
	return i
}

// skipString returns the index after a control string (OSC, DCS, ...)
// whose body starts at i. It ends at BEL, ESC \ or the C1 ST; an
// unterminated string runs to the end of s.
func skipString(s string, i int) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch r {
		case 0x07, 0x9c:
			return i
		case 0x1b:
			if i < len(s) && s[i] == '\\' {
				i++
			}
			return i
		}
	}
	return i
}
