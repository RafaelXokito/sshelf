package session

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/muesli/cancelreader"
	"golang.org/x/crypto/ssh"

	"github.com/RafaelXokito/sshelf/internal/term"
)

// ResizeDebounce is how long a burst of resize events is collected before
// one window-change is sent (ARCH-SESS-004).
const ResizeDebounce = 30 * time.Millisecond

// drainTimeout bounds how long Run waits for buffered remote output after
// the session ended.
const drainTimeout = time.Second

// ErrInternal means a session goroutine panicked; the session was ended.
var ErrInternal = errors.New("internal error")

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

// Terminal is the local terminal a session is attached to.
type Terminal interface {
	// MakeRaw puts the terminal into raw mode and returns a function that
	// restores the previous state.
	MakeRaw() (restore func() error, err error)
	// Size returns the terminal's columns and rows.
	Size() (cols, rows int, err error)
	// Resizes delivers an event whenever the terminal is resized. stop ends
	// the notifications.
	Resizes() (events <-chan struct{}, stop func())
}

// Options configure a new session.
type Options struct {
	Term    string            // "" = $TERM, then xterm-256color
	Env     map[string]string // sent as env requests; rejections are ignored
	Command string            // "" = login shell
}

// Session is an interactive SSH session with a PTY. It satisfies
// tea.ExecCommand structurally: the UI hands it the terminal with tea.Exec
// and gets it back when Run returns.
type Session struct {
	client *ssh.Client
	sess   *ssh.Session
	term   Terminal

	remoteIn  io.WriteCloser
	remoteOut io.Reader
	remoteErr io.Reader

	stdin          io.Reader
	stdout, stderr io.Writer

	// cols and rows are the size last sent to the remote (pty-req or
	// window-change). Only the resize goroutine touches them after New.
	cols, rows int

	closeOnce sync.Once
}

// New opens a session channel on client, requests a PTY with the terminal's
// size, sends the environment and starts the shell or command. The session
// owns client from now on and closes it when it ends.
func New(client *ssh.Client, t Terminal, opts Options) (*Session, error) {
	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	s := &Session{client: client, sess: sess, term: t}
	if err := s.start(opts); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) start(opts Options) error {
	cols, rows, err := s.term.Size()
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	s.cols, s.rows = cols, rows
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := s.sess.RequestPty(termName(opts.Term), rows, cols, modes); err != nil {
		return fmt.Errorf("request pty: %w", err)
	}
	for _, k := range slices.Sorted(maps.Keys(opts.Env)) {
		_ = s.sess.Setenv(k, opts.Env[k]) // servers may refuse env; that is fine
	}
	if s.remoteIn, err = s.sess.StdinPipe(); err != nil {
		return err
	}
	if s.remoteOut, err = s.sess.StdoutPipe(); err != nil {
		return err
	}
	if s.remoteErr, err = s.sess.StderrPipe(); err != nil {
		return err
	}
	if opts.Command != "" {
		return s.sess.Start(opts.Command)
	}
	return s.sess.Shell()
}

// termName picks the remote TERM: the option, then $TERM, then
// xterm-256color.
func termName(opt string) string {
	if opt != "" {
		return opt
	}
	if t := os.Getenv("TERM"); t != "" {
		return t
	}
	return "xterm-256color"
}

// SetStdin implements tea.ExecCommand.
func (s *Session) SetStdin(r io.Reader) { s.stdin = r }

// SetStdout implements tea.ExecCommand.
func (s *Session) SetStdout(w io.Writer) { s.stdout = w }

// SetStderr implements tea.ExecCommand.
func (s *Session) SetStderr(w io.Writer) { s.stderr = w }

// Close ends the session and closes the connection. Run calls it; call it
// directly only for a session that will never run.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		_ = s.sess.Close()
		err = s.client.Close()
	})
	return err
}

// Run attaches the session to the terminal until it ends. It returns nil
// when the remote exited with status 0, an *ExitError for any other exit, a
// *DisconnectError when the connection was lost, or ErrInternal when a
// session goroutine panicked. On every path, including a panic in Run, it
// restores the terminal state and writes term.ResetSequence (ARCH-SESS-005).
func (s *Session) Run() (err error) {
	defer func() { _ = s.Close() }()
	stdin, stdout, stderr := s.stdin, s.stdout, s.stderr
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	restore, err := s.term.MakeRaw()
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	// Deferred, so it also runs while a panic in Run unwinds; main's
	// handler then deals with the panic (ARCH-SEC-004).
	defer func() {
		_ = restore()
		_, _ = io.WriteString(stdout, term.ResetSequence)
	}()
	term.Fault("session-run")

	// A cancelable reader, so the stdin pump stops before Run returns and
	// never steals the TUI's next keystroke.
	in, err := cancelreader.NewReader(stdin)
	if err != nil {
		return fmt.Errorf("stdin: %w", err)
	}
	defer func() { _ = in.Close() }()
	// If Run itself panics, stop the pumps before the panic moves on to
	// main's handler. No recover, so the handler gets the original stack.
	unwinding := true
	defer func() {
		if unwinding {
			in.Cancel()
			_ = s.Close()
		}
	}()

	var (
		all, out, inWG sync.WaitGroup
		failed         = make(chan struct{})
		failOnce       sync.Once
	)
	onPanic := func(any) {
		failOnce.Do(func() { close(failed) })
		_ = s.sess.Close()
	}
	goWG := func(wg *sync.WaitGroup, fn func()) {
		wg.Add(1)
		term.SafeGo(func() { defer wg.Done(); fn() }, onPanic)
	}

	out.Add(2)
	goWG(&all, func() { defer out.Done(); _, _ = io.Copy(stdout, s.remoteOut) })
	goWG(&all, func() { defer out.Done(); _, _ = io.Copy(stderr, s.remoteErr) })
	goWG(&inWG, func() { s.pumpStdin(in) })

	events, stopResizes := s.term.Resizes()
	stopWinch := make(chan struct{})
	goWG(&all, func() { s.forwardResizes(events, stopWinch) })

	waitDone := make(chan error, 1)
	connDone := make(chan error, 1)
	goWG(&all, func() { waitDone <- s.sess.Wait() })
	goWG(&all, func() { connDone <- s.client.Wait() })

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
	case <-failed:
		result = ErrInternal
	}

	// Let buffered output reach the terminal, then stop everything and wait
	// for every goroutine, the stdin reader included.
	waitTimeout(&out, drainTimeout)
	canceled := in.Cancel()
	stopResizes()
	close(stopWinch)
	_ = s.Close()
	all.Wait()
	// Only a file (the real stdin, a pipe) can be canceled mid-Read. Any
	// other reader is left to stop at its next Read.
	if canceled {
		inWG.Wait()
	}
	unwinding = false

	select {
	case <-failed:
		result = ErrInternal
	default:
	}
	slog.Debug("session: ended", "result", result)
	return result
}

// pumpStdin copies local input to the remote until the reader is canceled
// or the remote stops accepting input.
func (s *Session) pumpStdin(in io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			term.Fault("session-goroutine")
			if _, werr := s.remoteIn.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				_ = s.remoteIn.Close()
			}
			return
		}
	}
}

// forwardResizes sends one window-change per burst of resize events,
// ResizeDebounce after the last one, and only when the size differs from
// the one the remote has.
func (s *Session) forwardResizes(events <-chan struct{}, stop <-chan struct{}) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-events:
			timer.Reset(ResizeDebounce)
		case <-timer.C:
			cols, rows, err := s.term.Size()
			if err != nil || (cols == s.cols && rows == s.rows) {
				continue
			}
			s.cols, s.rows = cols, rows
			_ = s.sess.WindowChange(rows, cols)
		case <-stop:
			return
		}
	}
}

// classify turns the result of ssh.Session.Wait into Run's result. A session
// that ended without an exit status is reported as a lost connection, with
// the connection's error when it arrives in time.
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
