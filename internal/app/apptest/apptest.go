// Package apptest holds test doubles of app types. FakeSession is the
// "fake session" of the M2 tasks (tasks.md): an app.Session that
// records what it is sent and serves canned snapshots.
package apptest

import (
	"image/color"
	"slices"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/RafaelXokito/sshelf/internal/app"
)

// FakeSession is a recording app.Session. Its zero value is not
// usable; call NewFakeSession. It is safe for concurrent use.
type FakeSession struct {
	mu         sync.Mutex
	snap       app.Snapshot
	scrollback []string // lines above the screen, oldest first
	dropped    int      // scrollback lines removed by DropLines
	keys       []uv.KeyPressEvent
	mouse      []uv.MouseEvent
	pastes     []string
	bytes      []byte
	sizes      []app.Size
	fg, bg     color.Color
	err        error
	closes     int
	ended      bool

	updates chan struct{}
	done    chan struct{}
}

// NewFakeSession returns a live session showing lines.
func NewFakeSession(lines ...string) *FakeSession {
	return &FakeSession{
		snap:    app.Snapshot{Lines: slices.Clone(lines), Seq: 1},
		updates: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

// Output replaces the screen with lines, as remote output would, bumps
// Seq and notifies Updates (coalesced, never blocking).
func (f *FakeSession) Output(lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended {
		return
	}
	f.snap.Lines = slices.Clone(lines)
	f.snap.Seq++
	select {
	case f.updates <- struct{}{}:
	default:
	}
}

// SetSnapshot replaces the whole snapshot without notifying.
func (f *FakeSession) SetSnapshot(s app.Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snap = s
}

// SetScrollback sets the lines kept above the screen, oldest first.
func (f *FakeSession) SetScrollback(lines []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scrollback = slices.Clone(lines)
}

// End ends the session with err: Done and Updates are closed.
func (f *FakeSession) End(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended {
		return
	}
	f.ended = true
	f.err = err
	close(f.done)
	close(f.updates)
}

// Keys returns the keys sent so far.
func (f *FakeSession) Keys() []uv.KeyPressEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.keys)
}

// Mouse returns the mouse events sent so far.
func (f *FakeSession) Mouse() []uv.MouseEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.mouse)
}

// Pastes returns the pastes sent so far.
func (f *FakeSession) Pastes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.pastes)
}

// Bytes returns the literal bytes sent so far.
func (f *FakeSession) Bytes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.bytes)
}

// Sent reports whether anything at all was sent to the remote.
func (f *FakeSession) Sent() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.keys)+len(f.mouse)+len(f.pastes)+len(f.bytes) > 0
}

// Sizes returns every size passed to Resize.
func (f *FakeSession) Sizes() []app.Size {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sizes)
}

// Closes returns how many times Close was called.
func (f *FakeSession) Closes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

// SendKey implements app.Session.
func (f *FakeSession) SendKey(k uv.KeyPressEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, k)
	return nil
}

// SendMouse implements app.Session.
func (f *FakeSession) SendMouse(m uv.MouseEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mouse = append(f.mouse, m)
	return nil
}

// Paste implements app.Session.
func (f *FakeSession) Paste(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pastes = append(f.pastes, text)
	return nil
}

// SendBytes implements app.Session.
func (f *FakeSession) SendBytes(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bytes = append(f.bytes, b...)
	return nil
}

// Resize implements app.Session.
func (f *FakeSession) Resize(sz app.Size) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, sz)
}

// SetDefaultColors implements app.Session.
func (f *FakeSession) SetDefaultColors(fg, bg color.Color) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fg, f.bg = fg, bg
}

// Colors returns the default colours last set with SetDefaultColors.
func (f *FakeSession) Colors() (fg, bg color.Color) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fg, f.bg
}

// Snapshot implements app.Session.
func (f *FakeSession) Snapshot() app.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.snap
	s.Lines = slices.Clone(s.Lines)
	if s.Cursor != nil {
		c := *s.Cursor
		s.Cursor = &c
	}
	return s
}

// Scrollback implements app.Session: the scrollback lines, then
// the screen.
func (f *FakeSession) Scrollback(top, rows int) ([]string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := append(slices.Clone(f.scrollback), f.snap.Lines...)
	top = min(max(top, 0), len(all))
	end := min(top+max(rows, 0), len(all))
	return slices.Clone(all[top:end]), len(all)
}

// DropLines removes the n oldest scrollback lines and counts them in
// Dropped, as the scrollback cap does.
func (f *FakeSession) DropLines(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n = min(max(n, 0), len(f.scrollback))
	f.scrollback = slices.Clone(f.scrollback[n:])
	f.dropped += n
}

// Dropped implements app.Session.
func (f *FakeSession) Dropped() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dropped
}

// Updates implements app.Session.
func (f *FakeSession) Updates() <-chan struct{} { return f.updates }

// Done implements app.Session.
func (f *FakeSession) Done() <-chan struct{} { return f.done }

// Err implements app.Session.
func (f *FakeSession) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// Close implements app.Session. It is idempotent; the first call
// ends a live session with a nil error.
func (f *FakeSession) Close() error {
	f.mu.Lock()
	f.closes++
	f.mu.Unlock()
	f.End(nil)
	return nil
}

var _ app.Session = (*FakeSession)(nil)
