package sftpview

import (
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
)

// Message prefixes on the progress line (FR-SFTP-024, NFR-UX-003).
const (
	ErrorMark   = "✕"
	WarningMark = "⚠"
)

// FormatSize formats n bytes in 1000-based units with at most one decimal
// below 10 (FR-SFTP-006): "999 B", "3 KB", "1.2 MB", "840 MB", "12 GB".
func FormatSize(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(max(n, 0), 10) + " B"
	}
	units := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	v := float64(n)
	for i, u := range units {
		v /= 1000
		if v < 9.95 {
			s := strconv.FormatFloat(v, 'f', 1, 64)
			return strings.TrimSuffix(s, ".0") + " " + u
		}
		if v < 999.5 || i == len(units)-1 {
			return strconv.FormatFloat(v, 'f', 0, 64) + " " + u
		}
	}
	return ""
}

// countUnknown is a directory whose item count is not known yet.
const countUnknown = -1

// countFailed is a directory that could not be counted.
const countFailed = -2

// FormatCount is a directory's item count (FR-SFTP-006): "…" until known,
// "1 item", "12 items", "10k+ items" above 10,000.
func FormatCount(n int) string {
	switch {
	case n == countUnknown:
		return "…"
	case n == countFailed:
		return "?"
	case n == 1:
		return "1 item"
	case n > app.ViewCountLimit:
		return "10k+ items"
	}
	return strconv.Itoa(n) + " items"
}

// FormatTime is a modification time in now's location (local time):
// "Oct 30 14:02" within the last 12 months, "2025-03-01" otherwise
// (FR-SFTP-006).
func FormatTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(now.Location())
	if t.After(now.AddDate(-1, 0, 0)) && !t.After(now.Add(24*time.Hour)) {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("2006-01-02")
}

// ErrText is an error as the progress line shows it (errText).
func ErrText(err error) string { return errText(err) }

// errText is an error as the progress line shows it: sanitised, with the
// common file-system errors in words (FR-SFTP-024, SEC-SFTP-008).
func errText(err error) string {
	switch {
	case errors.Is(err, app.ErrNotConnected):
		return "not connected"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, fs.ErrNotExist):
		return "no such file or directory"
	case errors.Is(err, fs.ErrExist):
		return "already exists"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return domain.Sanitize(err.Error())
}

// cutLeft cuts s to w columns from the left with "…", so its end stays
// visible (FR-SFTP-005).
func cutLeft(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	return "…" + ansi.TruncateLeft(s, sw-w+1, "")
}

// cut cuts s to w columns with "…".
func cut(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// padRight pads s with spaces to w columns.
func padRight(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// padLeft right-aligns s in w columns.
func padLeft(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}
