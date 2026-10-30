package files

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Name rules shared by the local and the remote side (D-020): one place, so
// a name a server sends and a name the user types obey the same check.

// CheckName refuses a name that is not one safe path element: empty, ".",
// "..", or containing "/", NUL or a control character (C0, DEL or C1).
// These are the rules of FR-SFTP-010 for typed names and of SEC-SFTP-001
// for remote entries, and every name is checked again before it is joined
// to a path. A name it accepts joined to a directory is a direct child of
// that directory.
func CheckName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, '/') {
		return ErrBadName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return ErrBadName
		}
	}
	return nil
}

// Display makes a name or path safe to show on one line: escape sequences
// and controls removed (SEC-SESSION-003), invalid UTF-8, tabs and
// newlines as U+FFFD. Operations keep using the exact bytes.
func Display(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' {
			return utf8.RuneError
		}
		return r
	}, domain.Sanitize(name))
}

// MaxReasonWidth is the most columns a server-supplied message takes
// (SEC-SFTP-008).
const MaxReasonWidth = 80

// Reason makes server-supplied text (a status message, a realpath result,
// a link target) safe to show or log: sanitised, each run of whitespace
// folded to one space, and cut to MaxReasonWidth columns with "…"
// (SEC-SFTP-008).
func Reason(s string) string {
	s = strings.Join(strings.Fields(domain.Sanitize(s)), " ")
	return ansi.Truncate(s, MaxReasonWidth, "…")
}
