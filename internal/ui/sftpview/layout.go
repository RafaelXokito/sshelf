// Package sftpview is the SFTP tab's view (ADR-020, design §9.6): the two
// panes, the progress line, the footer, the transfers view and the filter
// field. It is a plain model; every call to app.Browser runs in a command
// (ARCH-UI-002). Layout gives the regions View draws, for hit-testing
// (ARCH-UI-007).
package sftpview

// Rect is a rectangle of the terminal: columns X to X+W−1, rows Y to
// Y+H−1, counted from 0 at the top left, the header row included.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the cell at column x, row y is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// Regions are the parts of an SFTP tab's frame.
type Regions struct {
	Boxes    [2]Rect // the local and remote pane boxes, borders included
	Rows     [2]Rect // the entry rows inside each box
	Progress Rect    // the progress line
	Footer   Rect    // the key hints
}

// MtimeFrom is the inner pane width from which the modification time is
// shown (FR-SFTP-006).
const MtimeFrom = 50

// Layout returns the regions of an SFTP tab at width×height (design §9.6):
// row 0 is the header; rows 1 to height−3 hold the two boxes side by side,
// the left one width/2 columns wide and the right one the rest (the odd
// column goes to the remote pane, FR-SFTP-023); row height−2 is the
// progress line and row height−1 the footer.
func Layout(width, height int) Regions {
	boxH := max(height-3, 2)
	lw := width / 2
	var r Regions
	r.Boxes[0] = Rect{X: 0, Y: 1, W: lw, H: boxH}
	r.Boxes[1] = Rect{X: lw, Y: 1, W: width - lw, H: boxH}
	for i, b := range r.Boxes {
		r.Rows[i] = Rect{X: b.X + 1, Y: b.Y + 1, W: max(b.W-2, 0), H: max(b.H-2, 0)}
	}
	r.Progress = Rect{X: 0, Y: 1 + boxH, W: width, H: 1}
	r.Footer = Rect{X: 0, Y: 2 + boxH, W: width, H: 1}
	return r
}
