package session

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/term"
)

// ResizeDebounce is how long a burst of resizes is collected before one
// window-change is sent (ARCH-SESS-004).
const ResizeDebounce = 30 * time.Millisecond

// InputLimit is the most input a session queues for a remote that is not
// reading it (ARCH-SESS-014).
const InputLimit = 1 << 20

const (
	readChunk  = 32 << 10 // bytes read from the channel at a time
	writeChunk = 4 << 10  // most output fed to the emulator per lock
	sendChunk  = 32 << 10 // most input written to the channel at a time
	// drainTimeout bounds how long the end of a session waits for output
	// that already arrived to reach the emulator.
	drainTimeout = time.Second
	// maxKeyBytes is more than any encoded key or mouse event.
	maxKeyBytes = 64
)

// ErrInternal means a session goroutine panicked; the session was ended.
var ErrInternal = errors.New("internal error")

// ErrInputFull means the session's input queue holds InputLimit bytes the
// remote has not read; the input was not sent (ARCH-SESS-014).
var ErrInputFull = errors.New("remote is not reading input")

// ErrClosed means the session has ended; the input was not sent.
var ErrClosed = errors.New("session ended")

// ExitError reports a remote exit with a non-zero status or by signal.
type ExitError struct {
	Status int
	Signal string // e.g. "KILL"; "" when the remote exited normally
}

func (e *ExitError) Error() string {
	if e.Signal != "" {
		return "killed by signal " + e.Signal
	}
	return fmt.Sprintf("exited (%d)", e.Status)
}

// DisconnectError reports a session that ended because the connection was
// lost.
type DisconnectError struct {
	Err error
}

func (e *DisconnectError) Error() string {
	reason := "connection closed"
	if e.Err != nil && !errors.Is(e.Err, io.EOF) {
		reason = e.Err.Error()
	}
	return "connection lost: " + reason
}

func (e *DisconnectError) Unwrap() error { return e.Err }

// Options configure a new session.
type Options struct {
	// Term is the remote TERM. "" means xterm-256color for New (the
	// terminal the emulator implements, ADR-012) and $TERM, then
	// xterm-256color, for NewAttached.
	Term    string
	Env     map[string]string // sent as env requests; rejections are ignored
	Command string            // "" = login shell
	// Size is the PTY and emulator size, the session area (New only).
	// Zero means 80×24.
	Size Size
	// Scrollback is the number of lines kept (New only); 0 means
	// DefaultScrollback (NFR-SIZE-003).
	Scrollback int
}

// Session is an SSH session with a PTY whose output runs into an
// in-process terminal emulator (ADR-012, design §8.4). Its goroutines read
// the channel, pump the emulator's replies, write input and send
// window-changes, independently of the UI; no method blocks on the
// network.
type Session struct {
	client *ssh.Client
	sess   *ssh.Session
	scr    emulator
	in     *inputQueue
	stdin  io.WriteCloser

	updates chan struct{} // capacity 1, closed after done
	done    chan struct{}
	err     error // written before done is closed

	resizes  chan Size     // capacity 1: the latest size wins
	stop     chan struct{} // closed when the session ends
	failed   chan struct{} // closed when a goroutine panicked
	failOnce sync.Once

	closing    atomic.Bool // Close was called
	closeReq   chan struct{}
	closeOnce  sync.Once
	closeErr   error
	finishOnce sync.Once
}

// emulator is what a Session needs from its terminal emulator: *screen
// (emulator.go), or a recording double in tests.
type emulator interface {
	io.Reader // bytes for the remote: encoded input and query replies
	write(p []byte)
	closeReplies()
	resize(cols, rows int)
	snapshot() Snapshot
	scrollback(top, rows int) ([]string, int)
	scrollbackLen() int
	sendKey(k uv.KeyPressEvent)
	sendMouse(m uv.MouseEvent)
	paste(text string)
	sendBytes(b []byte)
	setDefaultColors(fg, bg color.Color)
}

// New opens a session channel on client, requests a PTY of opts.Size with
// TERM xterm-256color (or opts.Term), sends the environment, starts the
// shell or command and the session's goroutines. The session owns client
// from now on and closes it when it ends. New is only called with a client
// sshclient.Connect returned, so the host is verified and authenticated
// (ARCH-SESS-002).
func New(client *ssh.Client, opts Options) (*Session, error) {
	return newSession(client, opts, func(sz Size, scrollback int) emulator {
		return newScreen(sz.Cols, sz.Rows, scrollback)
	})
}

func newSession(client *ssh.Client, opts Options, newEmulator func(Size, int) emulator) (*Session, error) {
	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	size := opts.Size
	if size.Cols <= 0 || size.Rows <= 0 {
		size = Size{Cols: 80, Rows: 24}
	}
	s := &Session{
		client:   client,
		sess:     sess,
		scr:      newEmulator(size, opts.Scrollback),
		in:       &inputQueue{limit: InputLimit, ready: make(chan struct{}, 1)},
		updates:  make(chan struct{}, 1),
		done:     make(chan struct{}),
		resizes:  make(chan Size, 1),
		stop:     make(chan struct{}),
		failed:   make(chan struct{}),
		closeReq: make(chan struct{}),
	}
	stdout, stderr, err := s.start(opts, size)
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, err
	}
	s.run(stdout, stderr, size)
	return s, nil
}

func (s *Session) start(opts Options, size Size) (stdout, stderr io.Reader, err error) {
	termName := opts.Term
	if termName == "" {
		termName = "xterm-256color"
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := s.sess.RequestPty(termName, size.Rows, size.Cols, modes); err != nil {
		return nil, nil, fmt.Errorf("request pty: %w", err)
	}
	for _, k := range slices.Sorted(maps.Keys(opts.Env)) {
		_ = s.sess.Setenv(k, opts.Env[k]) // servers may refuse env; that is fine
	}
	if s.stdin, err = s.sess.StdinPipe(); err != nil {
		return nil, nil, err
	}
	if stdout, err = s.sess.StdoutPipe(); err != nil {
		return nil, nil, err
	}
	if stderr, err = s.sess.StderrPipe(); err != nil {
		return nil, nil, err
	}
	if opts.Command != "" {
		err = s.sess.Start(opts.Command)
	} else {
		err = s.sess.Shell()
	}
	return stdout, stderr, err
}

// run starts the goroutines (design §8.4), all through term.SafeGo.
func (s *Session) run(stdout, stderr io.Reader, size Size) {
	var all, out sync.WaitGroup
	goWG := func(fn func(), wgs ...*sync.WaitGroup) {
		for _, wg := range wgs {
			wg.Add(1)
		}
		term.SafeGo(func() {
			defer func() {
				for _, wg := range wgs {
					wg.Done()
				}
			}()
			fn()
		}, s.panicked)
	}
	goWG(func() { s.readOutput(stdout) }, &all, &out)
	goWG(func() { s.readOutput(stderr) }, &all, &out)
	goWG(s.pumpReplies, &all)
	goWG(s.writeInput, &all)
	goWG(func() { s.sendResizes(size) }, &all)

	waitDone := make(chan error, 1)
	connDone := make(chan error, 1)
	goWG(func() { waitDone <- s.sess.Wait() }, &all)
	goWG(func() { connDone <- s.client.Wait() }, &all)
	term.SafeGo(func() { s.waitEnd(waitDone, connDone, &all, &out) }, func(any) {
		s.panicked(nil)
		s.finish(ErrInternal, &all)
	})
}

// panicked ends the session after a goroutine panicked (ARCH-SESS-005):
// it unblocks every goroutine; the waiter reports ErrInternal.
func (s *Session) panicked(any) {
	s.failOnce.Do(func() { close(s.failed) })
	s.scr.closeReplies()
	_ = s.sess.Close()
}

// readOutput feeds what the channel delivers to the emulator, reading the
// next chunk only once the emulator has processed this one, so a fast
// remote is slowed by the SSH window rather than buffered (ARCH-SESS-014).
func (s *Session) readOutput(r io.Reader) {
	buf := make([]byte, readChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			term.Fault("session-output")
			s.feed(buf[:n])
			s.notify()
		}
		if err != nil {
			return
		}
	}
}

// feed writes p to the emulator in pieces of at most writeChunk bytes, so
// Snapshot never waits long for the lock. A piece ends after a control
// character where there is one, which never splits a grapheme cluster.
func (s *Session) feed(p []byte) {
	for len(p) > 0 {
		n := len(p)
		if n > writeChunk {
			n = writeChunk
			for i := writeChunk - 1; i >= writeChunk/2; i-- {
				if p[i] < 0x20 {
					n = i + 1
					break
				}
			}
		}
		s.scr.write(p[:n])
		p = p[n:]
	}
}

// notify tells the UI that output was processed, without blocking: one
// pending notification stands for any number of chunks (ARCH-SESS-015).
func (s *Session) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

// pumpReplies moves what the emulator sends to the remote (encoded input
// and query answers) into the input queue. It never waits on the network;
// replies that do not fit are dropped.
func (s *Session) pumpReplies() {
	buf := make([]byte, readChunk)
	for {
		n, err := s.scr.Read(buf)
		if n > 0 {
			s.in.push(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// writeInput drains the input queue to the channel. It is the only
// goroutine that waits for the remote to read.
func (s *Session) writeInput() {
	for {
		chunk := s.in.next(s.stop)
		if chunk == nil {
			return
		}
		term.Fault("session-input")
		if _, err := s.stdin.Write(chunk); err != nil {
			return
		}
		s.in.consume(len(chunk))
	}
}

// sendResizes sends one window-change per burst of resizes, ResizeDebounce
// after the last one, and only when the size differs from what the remote
// has.
func (s *Session) sendResizes(sent Size) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var want Size
	for {
		select {
		case want = <-s.resizes:
			timer.Reset(ResizeDebounce)
		case <-timer.C:
			if want != sent {
				sent = want
				_ = s.sess.WindowChange(want.Rows, want.Cols)
			}
		case <-s.stop:
			return
		}
	}
}

// waitEnd waits for the session to end, classifies the end and stops
// everything.
func (s *Session) waitEnd(waitDone, connDone <-chan error, all, out *sync.WaitGroup) {
	var result error
	select {
	case werr := <-waitDone:
		result = classify(werr, connDone)
	case cerr := <-connDone:
		// The exit status may have arrived just before the connection
		// closed; prefer it.
		select {
		case werr := <-waitDone:
			result = classify(werr, closedConn(cerr))
		case <-time.After(100 * time.Millisecond):
			result = &DisconnectError{Err: cerr}
		}
	case <-s.failed:
		result = ErrInternal
	case <-s.closeReq:
	}
	if s.closing.Load() {
		result = nil // ended by Close
	}
	// Let output that already arrived reach the emulator.
	waitTimeout(out, drainTimeout)
	s.finish(result, all)
}

// finish closes the connection, stops every goroutine, records the result
// and closes Done and Updates, in that order (ARCH-SESS-008).
func (s *Session) finish(result error, all *sync.WaitGroup) {
	s.finishOnce.Do(func() { s.finishNow(result, all) })
}

func (s *Session) finishNow(result error, all *sync.WaitGroup) {
	_ = s.closeConn()
	close(s.stop)
	s.scr.closeReplies()
	all.Wait()
	select {
	case <-s.failed:
		result = ErrInternal
	default:
	}
	s.err = result
	slog.Debug("session: ended", "result", result)
	close(s.done)
	close(s.updates)
}

// closeConn closes the channel and the connection once.
func (s *Session) closeConn() error {
	s.closeOnce.Do(func() {
		_ = s.sess.Close()
		s.closeErr = s.client.Close()
	})
	return s.closeErr
}

// send runs fn, which writes at most need bytes for the remote, unless the
// session ended or the input queue has no room for them.
func (s *Session) send(need int, fn func()) error {
	select {
	case <-s.done:
		return ErrClosed
	default:
	}
	if s.in.room() < need {
		return ErrInputFull
	}
	fn()
	return nil
}

// SendKey encodes a key press for the remote according to the emulator's
// modes (ARCH-SESS-017). It never blocks on the network.
func (s *Session) SendKey(k uv.KeyPressEvent) error {
	return s.send(maxKeyBytes, func() { s.scr.sendKey(k) })
}

// SendMouse encodes a mouse event for the remote. The emulator drops it
// unless the remote enabled a mouse mode.
func (s *Session) SendMouse(m uv.MouseEvent) error {
	return s.send(maxKeyBytes, func() { s.scr.sendMouse(m) })
}

// Paste sends text, bracketed when the remote enabled ?2004.
func (s *Session) Paste(text string) error {
	return s.send(len(text)+maxKeyBytes, func() { s.scr.paste(text) })
}

// SendBytes sends b unchanged, e.g. 0x1d for Ctrl+] Ctrl+].
func (s *Session) SendBytes(b []byte) error {
	return s.send(len(b), func() { s.scr.sendBytes(b) })
}

// Resize resizes the emulator at once and sends the remote one
// window-change for a burst of resizes (ARCH-SESS-004).
func (s *Session) Resize(sz Size) {
	if sz.Cols <= 0 || sz.Rows <= 0 {
		return
	}
	s.scr.resize(sz.Cols, sz.Rows)
	select {
	case <-s.resizes:
	default:
	}
	select {
	case s.resizes <- sz:
	default:
	}
}

// SetDefaultColors sets the colours the emulator reports to the remote's
// OSC 10 and 11 queries, from the user's terminal (ARCH-SESS-016).
func (s *Session) SetDefaultColors(fg, bg color.Color) { s.scr.setDefaultColors(fg, bg) }

// Snapshot copies the screen under the emulator lock.
func (s *Session) Snapshot() Snapshot { return s.scr.snapshot() }

// Scrollback returns rows rendered lines of the history from line top:
// the scrollback, oldest first, then the current screen. total is the
// number of lines in the history.
func (s *Session) Scrollback(top, rows int) (lines []string, total int) {
	return s.scr.scrollback(top, rows)
}

// ScrollbackLen returns the number of lines in the scrollback buffer, at
// most Options.Scrollback.
func (s *Session) ScrollbackLen() int { return s.scr.scrollbackLen() }

// Updates delivers a notification after output was processed; several
// chunks coalesce into one. It is closed after the session ended
// (ARCH-SESS-015).
func (s *Session) Updates() <-chan struct{} { return s.updates }

// Done is closed when the session has ended and its goroutines stopped.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err returns how the session ended, once Done is closed: nil for exit
// status 0 or Close, *ExitError, *DisconnectError or ErrInternal. It
// returns nil while the session runs.
func (s *Session) Err() error {
	select {
	case <-s.done:
		return s.err
	default:
		return nil
	}
}

// Close ends the session and closes the connection. It is idempotent and
// does not wait for Done.
func (s *Session) Close() error {
	if s.closing.CompareAndSwap(false, true) {
		close(s.closeReq)
	}
	return s.closeConn()
}

// inputPeak returns the most bytes the input queue ever held (tests).
func (s *Session) inputPeak() int { return s.in.maxLen() }

// inputQueue is a session's input buffer of at most limit bytes. The
// writer goroutine removes bytes only once they were written, so the
// limit covers input in flight too.
type inputQueue struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
	peak  int
	ready chan struct{} // capacity 1: something was pushed
}

// push appends p if it fits and reports whether it did.
func (q *inputQueue) push(p []byte) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.buf.Len()+len(p) > q.limit {
		return false
	}
	q.buf.Write(p)
	q.peak = max(q.peak, q.buf.Len())
	select {
	case q.ready <- struct{}{}:
	default:
	}
	return true
}

func (q *inputQueue) room() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.limit - q.buf.Len()
}

func (q *inputQueue) maxLen() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.peak
}

// next waits for input and returns a copy of up to sendChunk bytes, or
// nil once stop is closed.
func (q *inputQueue) next(stop <-chan struct{}) []byte {
	for {
		q.mu.Lock()
		if n := min(q.buf.Len(), sendChunk); n > 0 {
			b := bytes.Clone(q.buf.Bytes()[:n])
			q.mu.Unlock()
			return b
		}
		q.mu.Unlock()
		select {
		case <-q.ready:
		case <-stop:
			return nil
		}
	}
}

// consume drops n written bytes from the front of the queue.
func (q *inputQueue) consume(n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.buf.Next(n)
}

// classify turns the result of ssh.Session.Wait into a session's end. A
// session that ended without an exit status is reported as a lost
// connection, with the connection's error when it arrives in time.
func classify(werr error, connDone <-chan error) error {
	if werr == nil {
		return nil
	}
	var ee *ssh.ExitError
	if errors.As(werr, &ee) {
		return &ExitError{Status: ee.ExitStatus(), Signal: ee.Signal()}
	}
	select {
	case cerr := <-connDone:
		return &DisconnectError{Err: cerr}
	case <-time.After(250 * time.Millisecond):
		return &DisconnectError{Err: werr}
	}
}

// closedConn returns a channel that already holds err.
func closedConn(err error) <-chan error {
	c := make(chan error, 1)
	c <- err
	return c
}

// waitTimeout waits for wg or until d has passed.
func waitTimeout(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	term.SafeGo(func() { wg.Wait(); close(done) }, func(any) {})
	select {
	case <-done:
	case <-time.After(d):
	}
}
