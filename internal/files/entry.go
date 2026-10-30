// Package files is the back end of the SFTP tab (design §8.9): listings and
// operations on the local and the remote side, and the transfer queue.
package files

import (
	"io/fs"
	"time"
)

// Side is one pane of the SFTP tab.
type Side int

// The two sides.
const (
	Local Side = iota
	Remote
)

// Other returns the other side.
func (s Side) Other() Side { return 1 - s }

// String is "Local" or "Remote", as the pane titles say.
func (s Side) String() string {
	if s == Remote {
		return "Remote"
	}
	return "Local"
}

// Kind is what an entry is, as Lstat reports it (links are not followed).
type Kind int

// Entry kinds.
const (
	File Kind = iota
	Dir
	Symlink
	Other
)

// Entry is one row of a listing.
type Entry struct {
	Name    string      // the exact bytes used for operations
	Display string      // sanitised for the screen; invalid UTF-8 as U+FFFD
	Kind    Kind        // Dir, File, Symlink, Other
	Size    int64       // of the entry itself (a link's own size)
	Mode    fs.FileMode // permission bits
	ModTime time.Time
}

// Listing is one directory as a pane shows it.
type Listing struct {
	Side      Side
	Dir       string  // as used for operations
	Title     string  // sanitised, ~ for the local home
	Entries   []Entry // unsorted; the view sorts
	Skipped   int     // unsafe names left out (FR-SFTP-024)
	Truncated bool    // stopped at MaxEntries (or the remote byte budget)
}

const (
	// MaxEntries is the most entries a listing holds (SEC-SFTP-004).
	MaxEntries = 100_000
	// MaxDepth is the deepest a recursive walk descends (SEC-SFTP-004).
	MaxDepth = 64
	// PartSuffix ends the name of a file being transferred (SEC-SFTP-003).
	PartSuffix = ".sshelf-part"
)

// kindOf maps a file mode to an entry kind.
func kindOf(m fs.FileMode) Kind {
	switch {
	case m&fs.ModeSymlink != 0:
		return Symlink
	case m.IsDir():
		return Dir
	case m.IsRegular():
		return File
	default:
		return Other
	}
}
