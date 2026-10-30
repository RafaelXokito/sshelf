package components

import "time"

// DoubleClickInterval is the longest time between the two left presses of
// a double-click (ARCH-UI-007).
const DoubleClickInterval = 400 * time.Millisecond

// ClickMsg is a left mouse press at column X, line Y of the View of the
// component that receives it. The root model translates terminal
// coordinates into the component's own before sending it (ADR-014).
// Double is set on the second press of a double-click.
type ClickMsg struct {
	X, Y   int
	Double bool
}

// WheelMsg is one notch of the mouse wheel at column X, line Y of the
// View of the component that receives it: Delta is -1 (up) or 1 (down).
type WheelMsg struct {
	X, Y  int
	Delta int
}

// ClickTracker recognises double-clicks: two left presses on the same
// row within DoubleClickInterval. A third press after a double-click
// starts over. The caller passes the time of each press, so tests can
// inject a clock.
type ClickTracker struct {
	at    time.Time
	row   int
	armed bool // the last press can be the first of a double-click
}

// Press records a left press on row at time now and reports whether it
// completes a double-click.
func (c *ClickTracker) Press(now time.Time, row int) bool {
	if c.armed && row == c.row && now.Sub(c.at) >= 0 && now.Sub(c.at) <= DoubleClickInterval {
		c.armed = false
		return true
	}
	c.at, c.row, c.armed = now, row, true
	return false
}
