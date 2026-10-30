package sshtest

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// AnyUser, used as the user in WithPassword, WithAuthorizedKey or
// WithKeyboardInteractive, matches every user name.
const AnyUser = "*"

// AuthAttempt is one authentication request the server received.
type AuthAttempt struct {
	Conn    int    // 1-based connection number
	User    string // user name sent by the client
	Method  string // "none", "password", "publickey" or "keyboard-interactive"
	Success bool
}

// Request is one request the server received: a channel open, a channel
// request or a global request. Fields that do not apply to Type are zero.
type Request struct {
	Conn    int    // 1-based connection number
	Channel int    // 1-based channel number within the connection; 0 for global requests
	Type    string // "session" (channel open), "pty-req", "window-change", "env", "shell", "exec", or a global request name
	Term    string // pty-req
	Width   int    // pty-req, window-change: columns
	Height  int    // pty-req, window-change: rows
	Name    string // env
	Value   string // env
	Command string // exec
}

// Question is one keyboard-interactive prompt and the answer that passes it.
type Question struct {
	Prompt string
	Echo   bool
	Answer string
}

// Challenge is one keyboard-interactive round.
type Challenge struct {
	Instruction string
	Questions   []Question
}

// Handler runs a shell or exec request. It returns when the session is over;
// the server closes the channel afterwards. The default handler is the
// scripted shell (see Shell).
type Handler func(*Session)

// Option configures a Server.
type Option func(*Server) error

// WithHostKey adds a host key.
func WithHostKey(s ssh.Signer) Option {
	return func(srv *Server) error {
		srv.hostKeys = append(srv.hostKeys, s)
		return nil
	}
}

// WithHostKeyTypes adds one newly generated host key per type (Ed25519,
// ECDSA, RSA). Without any host key option the server has one Ed25519 key.
func WithHostKeyTypes(kinds ...string) Option {
	return func(srv *Server) error {
		for _, k := range kinds {
			s, err := GenerateSigner(k)
			if err != nil {
				return err
			}
			srv.hostKeys = append(srv.hostKeys, s)
		}
		return nil
	}
}

// WithPassword accepts password auth for user with password.
func WithPassword(user, password string) Option {
	return func(srv *Server) error {
		srv.passwords[user] = password
		return nil
	}
}

// WithAuthorizedKey accepts public-key auth for user with key.
func WithAuthorizedKey(user string, key ssh.PublicKey) Option {
	return func(srv *Server) error {
		srv.keys[user] = append(srv.keys[user], key)
		return nil
	}
}

// WithKeyboardInteractive accepts keyboard-interactive auth for user when
// every challenge, sent in order, is answered correctly.
func WithKeyboardInteractive(user string, challenges ...Challenge) Option {
	return func(srv *Server) error {
		srv.kbd[user] = challenges
		return nil
	}
}

// WithPort makes the server listen on 127.0.0.1:port instead of a
// random port (for sshtestd demos; tests keep the random port).
func WithPort(port int) Option {
	return func(srv *Server) error {
		srv.port = port
		return nil
	}
}

// WithHandler replaces the scripted shell for shell and exec requests.
func WithHandler(h Handler) Option {
	return func(srv *Server) error {
		srv.handler = h
		return nil
	}
}

// Server is an in-process SSH server listening on 127.0.0.1.
type Server struct {
	ln        net.Listener
	config    *ssh.ServerConfig
	hostKeys  []ssh.Signer
	passwords map[string]string
	keys      map[string][]ssh.PublicKey
	kbd       map[string][]Challenge
	handler   Handler
	port      int // 0: a random free port

	wg sync.WaitGroup

	mu       sync.Mutex
	closed   bool
	nconn    int
	conns    map[int]net.Conn
	versions []string
	attempts []AuthAttempt
	requests []Request
	closes   []Request
	received map[[2]int][]byte
}

// New starts a server for a test and closes it when the test ends.
func New(t testing.TB, opts ...Option) *Server {
	t.Helper()
	s, err := Start(opts...)
	if err != nil {
		t.Fatalf("sshtest: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Start starts a server on a random loopback port. Close stops it.
func Start(opts ...Option) (*Server, error) {
	s := &Server{
		passwords: map[string]string{},
		keys:      map[string][]ssh.PublicKey{},
		kbd:       map[string][]Challenge{},
		handler:   Shell,
		conns:     map[int]net.Conn{},
		received:  map[[2]int][]byte{},
	}
	for _, o := range opts {
		if err := o(s); err != nil {
			return nil, err
		}
	}
	if len(s.hostKeys) == 0 {
		if err := WithHostKeyTypes(Ed25519)(s); err != nil {
			return nil, err
		}
	}
	s.config = s.serverConfig()

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)))
	if err != nil {
		return nil, err
	}
	s.ln = ln
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *Server) serverConfig() *ssh.ServerConfig {
	c := &ssh.ServerConfig{
		AuthLogCallback: func(md ssh.ConnMetadata, method string, err error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.attempts = append(s.attempts, AuthAttempt{
				Conn: connNumber(md), User: md.User(), Method: method, Success: err == nil,
			})
			// Recorded here rather than after NewServerConn returns: this
			// callback runs before the server sends auth success, so the
			// version is visible as soon as the client's dial returns.
			if err == nil {
				s.versions = append(s.versions, string(md.ClientVersion()))
			}
		},
	}
	if len(s.passwords) > 0 {
		c.PasswordCallback = func(md ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			for _, u := range []string{md.User(), AnyUser} {
				if want, ok := s.passwords[u]; ok && want == string(pw) {
					return nil, nil
				}
			}
			return nil, errors.New("sshtest: wrong password")
		}
	}
	if len(s.keys) > 0 {
		c.PublicKeyCallback = func(md ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, u := range []string{md.User(), AnyUser} {
				for _, k := range s.keys[u] {
					if bytes.Equal(k.Marshal(), key.Marshal()) {
						return nil, nil
					}
				}
			}
			return nil, errors.New("sshtest: unknown public key")
		}
	}
	if len(s.kbd) > 0 {
		c.KeyboardInteractiveCallback = func(md ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			challenges, ok := s.kbd[md.User()]
			if !ok {
				challenges, ok = s.kbd[AnyUser]
			}
			if !ok {
				return nil, errors.New("sshtest: keyboard-interactive not configured for user")
			}
			for _, ch := range challenges {
				prompts := make([]string, len(ch.Questions))
				echos := make([]bool, len(ch.Questions))
				for i, q := range ch.Questions {
					prompts[i], echos[i] = q.Prompt, q.Echo
				}
				answers, err := client(md.User(), ch.Instruction, prompts, echos)
				if err != nil {
					return nil, err
				}
				if len(answers) != len(ch.Questions) {
					return nil, errors.New("sshtest: wrong number of answers")
				}
				for i, q := range ch.Questions {
					if answers[i] != q.Answer {
						return nil, errors.New("sshtest: wrong answer")
					}
				}
			}
			return nil, nil
		}
	}
	for _, k := range s.hostKeys {
		c.AddHostKey(k)
	}
	return c
}

// connNumber recovers the connection number that accept stored in the
// local address wrapper.
func connNumber(md ssh.ConnMetadata) int {
	if a, ok := md.LocalAddr().(numberedAddr); ok {
		return a.n
	}
	return 0
}

// numberedAddr tags a connection's local address with its number so the
// auth callbacks, which only see ConnMetadata, can record it.
type numberedAddr struct {
	net.Addr
	n int
}

type numberedConn struct {
	net.Conn
	n int
}

func (c numberedConn) LocalAddr() net.Addr { return numberedAddr{c.Conn.LocalAddr(), c.n} }

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = c.Close()
			return
		}
		s.nconn++
		n := s.nconn
		s.conns[n] = c
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serveConn(numberedConn{c, n})
	}
}

func (s *Server) serveConn(c numberedConn) {
	defer s.wg.Done()
	defer func() {
		_ = c.Close()
		s.mu.Lock()
		delete(s.conns, c.n)
		s.closes = append(s.closes, Request{Conn: c.n, Type: "disconnect"})
		s.mu.Unlock()
	}()
	sc, chans, reqs, err := ssh.NewServerConn(c, s.config)
	if err != nil {
		return
	}
	defer func() { _ = sc.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for r := range reqs {
			s.record(Request{Conn: c.n, Type: r.Type})
			_ = r.Reply(r.Type == "keepalive@openssh.com", nil)
		}
	}()
	nch := 0
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "sshtest: only session channels")
			continue
		}
		nch++
		s.record(Request{Conn: c.n, Channel: nch, Type: "session"})
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		sess := &Session{ch: ch, srv: s, conn: c.n, channel: nch, env: map[string]string{}}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess.serve(chReqs)
		}()
	}
	// The connection is gone; make sure the channel goroutines see it.
	_ = sc.Close()
	wg.Wait()
}

func (s *Server) record(r Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r)
}

// Addr returns the listening address, "127.0.0.1:<port>".
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Host returns the listening IP address.
func (s *Server) Host() string { return s.ln.Addr().(*net.TCPAddr).IP.String() }

// Port returns the listening port.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// HostKeys returns the server's public host keys.
func (s *Server) HostKeys() []ssh.PublicKey {
	out := make([]ssh.PublicKey, len(s.hostKeys))
	for i, k := range s.hostKeys {
		out[i] = k.PublicKey()
	}
	return out
}

// KnownHostsLines returns one OpenSSH known_hosts line per host key, for the
// server's address ("[127.0.0.1]:<port> <type> <base64>").
func (s *Server) KnownHostsLines() []string {
	var out []string
	for _, k := range s.HostKeys() {
		out = append(out, knownhosts.Line([]string{knownhosts.Normalize(s.Addr())}, k))
	}
	return out
}

// KnownHostsLine returns the known_hosts line of the first host key.
func (s *Server) KnownHostsLine() string { return s.KnownHostsLines()[0] }

// ClientVersions returns the version string of every client that completed
// the handshake, in order.
func (s *Server) ClientVersions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.versions)
}

// AuthAttempts returns every authentication attempt, in order.
func (s *Server) AuthAttempts() []AuthAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.attempts)
}

// Requests returns every request received, in order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// RequestsOfType returns the requests of one type, in order.
func (s *Server) RequestsOfType(typ string) []Request {
	var out []Request
	for _, r := range s.Requests() {
		if r.Type == typ {
			out = append(out, r)
		}
	}
	return out
}

// Closes returns, in order, every channel the server closed (Type
// "close", with Conn and Channel) and every connection that ended (Type
// "disconnect", with Conn).
func (s *Server) Closes() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.closes)
}

// Received returns every byte a handler has read from a channel so far.
func (s *Server) Received(conn, channel int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.received[[2]int{conn, channel}])
}

// WaitFor polls cond every few milliseconds until it returns true or the
// timeout expires, and reports whether it returned true.
func (s *Server) WaitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// DropConnections closes every open connection abruptly, without an exit
// status, as a network failure would.
func (s *Server) DropConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.Close()
	}
}

// Close stops the server, drops its connections and waits for every
// goroutine it started.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	err := s.ln.Close()
	s.DropConnections()
	s.wg.Wait()
	return err
}

// Session is one session channel, handed to a Handler.
type Session struct {
	ch      ssh.Channel
	srv     *Server
	conn    int
	channel int

	mu      sync.Mutex
	pty     bool
	term    string
	width   int
	height  int
	command string
	env     map[string]string
	started bool
	exited  bool
}

func (ss *Session) serve(reqs <-chan *ssh.Request) {
	var wg sync.WaitGroup
	defer func() {
		ss.srv.mu.Lock()
		ss.srv.closes = append(ss.srv.closes, Request{Conn: ss.conn, Channel: ss.channel, Type: "close"})
		ss.srv.mu.Unlock()
	}()
	defer wg.Wait()
	defer func() { _ = ss.ch.Close() }()
	for r := range reqs {
		rec := Request{Conn: ss.conn, Channel: ss.channel, Type: r.Type}
		ok := true
		switch r.Type {
		case "pty-req":
			var p struct {
				Term          string
				Cols, Rows    uint32
				Width, Height uint32
				Modes         string
			}
			if ssh.Unmarshal(r.Payload, &p) != nil {
				ok = false
				break
			}
			rec.Term, rec.Width, rec.Height = p.Term, int(p.Cols), int(p.Rows)
			ss.mu.Lock()
			ss.pty, ss.term, ss.width, ss.height = true, p.Term, int(p.Cols), int(p.Rows)
			ss.mu.Unlock()
		case "window-change":
			var p struct{ Cols, Rows, Width, Height uint32 }
			if ssh.Unmarshal(r.Payload, &p) != nil {
				ok = false
				break
			}
			rec.Width, rec.Height = int(p.Cols), int(p.Rows)
			ss.mu.Lock()
			ss.width, ss.height = int(p.Cols), int(p.Rows)
			ss.mu.Unlock()
		case "env":
			var p struct{ Name, Value string }
			if ssh.Unmarshal(r.Payload, &p) != nil {
				ok = false
				break
			}
			rec.Name, rec.Value = p.Name, p.Value
			ss.mu.Lock()
			ss.env[p.Name] = p.Value
			ss.mu.Unlock()
		case "shell", "exec":
			if r.Type == "exec" {
				var p struct{ Command string }
				if ssh.Unmarshal(r.Payload, &p) != nil {
					ok = false
					break
				}
				rec.Command = p.Command
			}
			ss.mu.Lock()
			if ss.started {
				ok = false
			}
			ss.started = true
			ss.command = rec.Command
			ss.mu.Unlock()
		default:
			ok = false
		}
		ss.srv.record(rec)
		if r.WantReply {
			_ = r.Reply(ok, nil)
		}
		if ok && (r.Type == "shell" || r.Type == "exec") {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ss.srv.handler(ss)
				_ = ss.ch.Close()
			}()
		}
	}
}

// Read reads the client's input. The server records every byte read
// (see Received).
func (ss *Session) Read(p []byte) (int, error) {
	n, err := ss.ch.Read(p)
	if n > 0 {
		k := [2]int{ss.conn, ss.channel}
		ss.srv.mu.Lock()
		ss.srv.received[k] = append(ss.srv.received[k], p[:n]...)
		ss.srv.mu.Unlock()
	}
	return n, err
}

// Write writes to the client's stdout.
func (ss *Session) Write(p []byte) (int, error) { return ss.ch.Write(p) }

// Stderr writes to the client's stderr.
func (ss *Session) Stderr(p []byte) (int, error) { return ss.ch.Stderr().Write(p) }

// PTY reports whether the client requested a PTY.
func (ss *Session) PTY() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.pty
}

// Size returns the PTY's current columns and rows.
func (ss *Session) Size() (cols, rows int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.width, ss.height
}

// Term returns the TERM value of the pty-req.
func (ss *Session) Term() string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.term
}

// Command returns the exec command, or "" for a shell.
func (ss *Session) Command() string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.command
}

// Env returns the value of an env request.
func (ss *Session) Env(name string) string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.env[name]
}

// Exit sends exit-status code and closes the channel. Later calls do nothing.
func (ss *Session) Exit(code int) {
	ss.mu.Lock()
	if ss.exited {
		ss.mu.Unlock()
		return
	}
	ss.exited = true
	ss.mu.Unlock()
	status := struct{ Status uint32 }{uint32(code)} //nolint:gosec // exit codes are small and non-negative
	_, _ = ss.ch.SendRequest("exit-status", false, ssh.Marshal(&status))
	_ = ss.ch.CloseWrite()
	_ = ss.ch.Close()
}

// ExitSignal reports that the remote command was killed by signal (e.g.
// "KILL") and closes the channel. Later calls, and Exit after it, do
// nothing.
func (ss *Session) ExitSignal(signal string) {
	ss.mu.Lock()
	if ss.exited {
		ss.mu.Unlock()
		return
	}
	ss.exited = true
	ss.mu.Unlock()
	msg := struct {
		Signal     string
		CoreDumped bool
		Error      string
		Lang       string
	}{Signal: signal}
	_, _ = ss.ch.SendRequest("exit-signal", false, ssh.Marshal(&msg))
	_ = ss.ch.CloseWrite()
	_ = ss.ch.Close()
}

// Shell is the default Handler: a scripted shell. With a PTY it echoes input
// and ends lines with "\r\n". It understands:
//
//	echo <words>   prints the words
//	size           prints the PTY size as "<cols>x<rows>"
//	exit [N]       exits with status N (default 0)
//
// and the M2 commands of scripts.go (flood, stream, replay, modes, query,
// hostile, echo-hex). For an exec request it runs the command once and
// exits with its status. End of input exits with status 0.
func Shell(ss *Session) {
	nl := "\n"
	if ss.PTY() {
		nl = "\r\n"
	}
	in := bufio.NewReader(ss)
	if cmd := ss.Command(); cmd != "" {
		if runScript(ss, in, cmd) {
			ss.Exit(0)
			return
		}
		out, code, _ := runCommand(ss, cmd)
		_, _ = ss.Write([]byte(strings.ReplaceAll(out, "\n", nl)))
		ss.Exit(code)
		return
	}
	echo := func(s string) {
		if ss.PTY() {
			_, _ = ss.Write([]byte(s))
		}
	}
	prompt := func() { _, _ = ss.Write([]byte("$ ")) }

	prompt()
	var line []byte
	for {
		b, err := in.ReadByte()
		if err != nil {
			ss.Exit(0)
			return
		}
		switch b {
		case '\r', '\n':
			echo(nl)
			cmd := string(line)
			line = line[:0]
			if runScript(ss, in, cmd) {
				prompt()
				continue
			}
			out, code, exit := runCommand(ss, cmd)
			_, _ = ss.Write([]byte(strings.ReplaceAll(out, "\n", nl)))
			if exit {
				ss.Exit(code)
				return
			}
			prompt()
		case 0x7f, 0x08:
			if len(line) > 0 {
				line = line[:len(line)-1]
				echo("\b \b")
			}
		case 0x03:
			echo("^C" + nl)
			line = line[:0]
			prompt()
		case 0x04:
			if len(line) == 0 {
				ss.Exit(0)
				return
			}
		default:
			line = append(line, b)
			echo(string(b))
		}
	}
}

// runCommand runs one scripted command line and returns its output (lines
// ended by "\n"), its status, and whether the shell should exit.
func runCommand(ss *Session, line string) (out string, code int, exit bool) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", 0, false
	}
	switch f[0] {
	case "echo":
		return strings.Join(f[1:], " ") + "\n", 0, false
	case "size":
		c, r := ss.Size()
		return fmt.Sprintf("%dx%d\n", c, r), 0, false
	case "exit":
		if len(f) > 1 {
			n, err := strconv.Atoi(f[1])
			if err != nil || n < 0 || n > 255 {
				return "sshtest: exit: bad status\n", 2, false
			}
			return "", n, true
		}
		return "", 0, true
	default:
		return "sshtest: unknown command: " + f[0] + "\n", 127, false
	}
}
