package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSEC_SESSION_003_Sanitize(t *testing.T) {
	for in, want := range map[string]string{
		"plain text":                            "plain text",
		"tab\tand\nnewline":                     "tab\tand\nnewline",
		"\x1b[31mred\x1b[0m":                    "red",
		"bell\x07 cr\r del\x7f":                 "bell cr del",
		"osc\x1b]52;c;ZXZpbA==\x07end":          "oscend",
		"osc st\x1b]0;title\x1b\\end":           "osc stend",
		"unterminated \x1b]52;c;ZXZpbA==":       "unterminated ",
		"c1 csi \u009b31mx":                     "c1 csi x",
		"c1 osc \u009d0;t\u009cx":               "c1 osc x",
		"dcs \x1bPq#0\x1b\\x":                   "dcs x",
		"two-byte \x1bcreset":                   "two-byte reset",
		"trailing esc \x1b":                     "trailing esc ",
		"Password: ":                            "Password: ",
		"utf8 ok: ação 日本":                      "utf8 ok: ação 日本",
		"bad utf8 \xff\xfe":                     "bad utf8 ��",
		"c1 \u0085 nel":                         "c1  nel",
		"\x1b[?1049h\x1b[2J\x1b[Hfake prompt: ": "fake prompt: ",
	} {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

// FuzzSEC_SESSION_003_Sanitize: the output never contains U+0000–U+0008,
// U+000B–U+001F, U+007F or U+0080–U+009F, is valid UTF-8, and is stable.
func FuzzSEC_SESSION_003_Sanitize(f *testing.F) {
	for _, s := range []string{
		"", "abc", "\x1b[31mx", "\x1b]52;c;ZXZpbA==\x07", "\u009b1;2H", "\xff", "a\tb\nc\r\x00",
		"\x1b\x1b[1m", "\x1bP\x1b\\", "\u009d\u009c", "日本\x1b[",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Sanitize(in)
		if !utf8.ValidString(out) {
			t.Fatalf("Sanitize(%q) = %q: invalid UTF-8", in, out)
		}
		if i := strings.IndexFunc(out, isControl); i >= 0 {
			t.Fatalf("Sanitize(%q) = %q: control at %d", in, out, i)
		}
		if again := Sanitize(out); again != out {
			t.Fatalf("Sanitize is not idempotent on %q: %q then %q", in, out, again)
		}
	})
}
