package sftpview

import (
	"cmp"
	"context"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
)

// row is one row of a pane: the ".." row or an entry.
type row struct {
	parent bool
	e      app.Entry
}

// pane is one side of the tab: its directory, its sorted listing, and
// the cursor, scroll position, marks, filter and item counts, which
// belong to that pane and directory (FR-SFTP-007, FR-SFTP-009).
type pane struct {
	side    app.Side
	start   string // the starting directory (FR-SFTP-005); "" until known
	dir     string // the directory shown, as used for operations
	title   string // sanitised, as the browser reported it
	loaded  bool   // a listing has been shown
	entries []app.Entry
	rows    []row // what is shown: "..", then the entries the hidden toggle and the filter let through
	cursor  int   // index in rows
	top     int   // first row on screen

	marks  map[string]bool // by Name
	counts map[string]int  // item counts of directories, by Name

	filter    *components.TextInput
	filtering bool // the filter field has the keyboard

	seq  int    // the latest List request; older replies are dropped
	want string // the name to put the cursor on when that reply comes

	countCtx  context.Context    // this directory's counts run in it
	countStop context.CancelFunc // cancels them; nil if none
	counting  string             // the directory being counted; "" if none
}

func newPane(side app.Side) *pane {
	f := components.NewTextInput()
	return &pane{side: side, marks: map[string]bool{}, counts: map[string]int{}, filter: f}
}

// join is dir/name on the pane's side: POSIX paths for the remote,
// the OS's for the local side.
func (p *pane) join(dir, name string) string {
	if p.side == app.Remote {
		return path.Join(dir, name)
	}
	return filepath.Join(dir, name)
}

// parentDir is dir's parent and the name of dir in it; at the root, ok
// is false.
func (p *pane) parentDir() (parent, name string, ok bool) {
	dir := p.dir
	if p.side == app.Remote {
		parent, name = path.Dir(dir), path.Base(dir)
	} else {
		parent, name = filepath.Dir(dir), filepath.Base(dir)
	}
	return parent, name, parent != dir
}

// atRoot reports whether the pane shows "/", which has no ".." row.
func (p *pane) atRoot() bool {
	_, _, ok := p.parentDir()
	return !ok
}

// sortEntries sorts a listing (FR-SFTP-006): directories first, then
// everything else (links among the files), each by name case-insensitively,
// ties by byte order.
func sortEntries(es []app.Entry) {
	slices.SortFunc(es, func(a, b app.Entry) int {
		ad, bd := a.Kind == app.KindDir, b.Kind == app.KindDir
		if ad != bd {
			if ad {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
}

// displayName is an entry's name as shown: sanitised, with "/" after a
// directory and "@" after a link (FR-SFTP-006).
func displayName(e app.Entry) string {
	d := e.Display
	if d == "" {
		d = e.Name
	}
	d = domain.Sanitize(d)
	switch e.Kind {
	case app.KindDir:
		d += "/"
	case app.KindSymlink:
		d += "@"
	}
	return d
}

// hiddenName reports whether name is hidden by "." (FR-SFTP-008).
func hiddenName(name string) bool { return strings.HasPrefix(name, ".") }

// rebuild recomputes the rows from the entries, the hidden toggle and the
// filter, keeping the cursor on the same row's name when it is still
// shown.
func (p *pane) rebuild(hidden bool) {
	keep, hadParent := p.cursorName()
	p.rows = nil
	if p.loaded && !p.atRoot() {
		p.rows = append(p.rows, row{parent: true})
	}
	q := strings.ToLower(p.filter.Value())
	for _, e := range p.entries {
		if hidden && hiddenName(e.Name) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(displayName(e)), q) {
			continue
		}
		p.rows = append(p.rows, row{e: e})
	}
	p.cursor = 0
	if !hadParent {
		p.selectName(keep)
	}
}

// cursorName is the name under the cursor, and whether it is the ".."
// row.
func (p *pane) cursorName() (string, bool) {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return "", false
	}
	r := p.rows[p.cursor]
	return r.e.Name, r.parent
}

// current is the row under the cursor.
func (p *pane) current() (row, bool) {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return row{}, false
	}
	return p.rows[p.cursor], true
}

// selectName puts the cursor on name if it is shown, and reports
// whether it is.
func (p *pane) selectName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range p.rows {
		if !r.parent && r.e.Name == name {
			p.cursor = i
			return true
		}
	}
	return false
}

// move moves the cursor by delta rows, within the rows.
func (p *pane) move(delta int) {
	p.cursor = min(max(p.cursor+delta, 0), max(len(p.rows)-1, 0))
}

// scroll keeps the cursor within the visible rows.
func (p *pane) scroll(visible int) {
	visible = max(visible, 1)
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+visible {
		p.top = p.cursor - visible + 1
	}
	p.top = min(max(p.top, 0), max(len(p.rows)-visible, 0))
}

// marked returns the marked entries' names, in listing order, including
// marks the filter hides (FR-SFTP-008).
func (p *pane) marked() []string {
	var out []string
	for _, e := range p.entries {
		if p.marks[e.Name] {
			out = append(out, e.Name)
		}
	}
	return out
}

// leave forgets everything that belongs to the directory shown: marks,
// filter, counts, cursor and scroll (FR-SFTP-008, FR-SFTP-009), and
// cancels its pending counts (FR-SFTP-006).
func (p *pane) leave() {
	clear(p.marks)
	clear(p.counts)
	p.filter.Reset()
	p.filter.Blur()
	p.filtering = false
	p.cursor, p.top = 0, 0
	p.stopCounts()
}

// stopCounts cancels the directory's pending counts.
func (p *pane) stopCounts() {
	if p.countStop != nil {
		p.countStop()
		p.countStop = nil
	}
	p.counting = ""
}
