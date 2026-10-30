//go:build unix

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"go.uber.org/goleak"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

var (
	binDir    string
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

func TestMain(m *testing.M) {
	if os.Getenv(hardenChildEnv) == "1" {
		os.Exit(hardenChild())
	}
	dir, err := os.MkdirTemp("", "sshelf-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	if code == 0 {
		if err := goleak.Find(); err != nil {
			fmt.Fprintln(os.Stderr, "goleak:", err)
			code = 1
		}
	}
	os.Exit(code)
}

// Stamped into the test binary the way the release build does it (T-026).
const (
	e2eVersion = "v0.0.0-e2e"
	e2eCommit  = "e2ecommit0123"
	e2eDate    = "2026-01-02T03:04:05Z"
)

// sshelfBinary builds cmd/sshelf once per test run.
func sshelfBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		binPath = filepath.Join(binDir, "sshelf")
		ldflags := "-X main.version=" + e2eVersion + " -X main.commit=" + e2eCommit + " -X main.date=" + e2eDate
		// sshelf_faultinject swaps in an in-memory credential store
		// (store_fault.go): E2E runs never touch the OS keychain and behave
		// the same on hosts with and without one.
		out, err := exec.Command("go", "build", "-tags", "sshelf_faultinject", "-ldflags", ldflags, "-o", binPath, ".").CombinedOutput() //nolint:gosec // test builds into its own temp dir
		if err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return binPath
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Len()
}

func (b *syncBuffer) From(off int) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.b.Bytes()[off:])
}

var (
	cursorForward = regexp.MustCompile(`\x1b\[(\d*)C`)
	escapes       = regexp.MustCompile(`\x1b\[[0-9;:?<>=]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[P^_][^\x1b]*\x1b\\|\x1b[()][0-9A-Za-z]|\x1b[ -~]`)
	whitespace    = regexp.MustCompile(`\s+`)
)

// screenText reduces terminal output to its printed characters without
// whitespace, so text checks do not depend on how the renderer moved the
// cursor between cells.
func screenText(b []byte) string {
	s := cursorForward.ReplaceAllString(string(b), " ")
	s = escapes.ReplaceAllString(s, "")
	return whitespace.ReplaceAllString(s, "")
}

// slice is the T-004 demo: sshelf in a PTY, with a config dir holding one
// key-file host, web-prod, that points at an in-process sshtest server,
// and optionally more hosts on servers of their own (see startServers).
type slice struct {
	t       *testing.T
	srv     *sshtest.Server   // web-prod's
	srvs    []*sshtest.Server // every host's, in list order; srvs[0] is srv
	master  *os.File
	tty     *os.File
	cmd     *exec.Cmd
	out     *syncBuffer
	exited  chan struct{} // closed once the process has exited
	exitErr error
	tabs    int    // session tabs opened so far; numbers are never reused
	before  string // stty -a before launch
	state   string // sshelf state dir (log and crash reports) under home
	cfgDir  string // sshelf config dir (XDG_CONFIG_HOME/sshelf)
	home    string // HOME
}

func stty(t *testing.T, tty *os.File) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "stty", "-a")
	cmd.Stdin = tty
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("stty -a: %v (stderr %q)", err, stderr.String())
	}
	return string(out)
}

// stateDir is sshelf's state dir (log and crash reports) for HOME home,
// with XDG_STATE_HOME unset (ADR-005).
func stateDir(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Logs", "sshelf")
	}
	return filepath.Join(home, ".local", "state", "sshelf")
}

// startSlice starts sshelf with env added to its minimal environment.
func startSlice(t *testing.T, env ...string) *slice {
	t.Helper()
	return startSliceBin(t, sshelfBinary(t), env...)
}

func startSliceBin(t *testing.T, bin string, env ...string) *slice {
	t.Helper()
	return startServers(t, bin, 1, env...)
}

// hostLabels are the labels of the hosts startServers configures, in
// list order.
var hostLabels = []string{"web-prod", "web-test", "web-uat"}

// startServers starts bin with n (at most 3) key-file hosts, each on its
// own sshtest server: web-prod, web-test and web-uat.
func startServers(t *testing.T, bin string, n int, env ...string) *slice {
	t.Helper()
	return startWith(t, bin, n, launch{env: env})
}

// launch adds to what startWith sets up.
type launch struct {
	env  []string         // added to sshelf's minimal environment
	opts []sshtest.Option // for every server
	dir  string           // sshelf's working directory; "" is the test's
}

// startWith is startServers with server options and a working directory.
func startWith(t *testing.T, bin string, n int, l launch) *slice {
	t.Helper()
	if _, err := exec.LookPath("stty"); err != nil {
		t.Skip("stty not available")
	}

	home, xdg := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(filepath.Join(home, ".ssh", "id_ed25519"), sshtest.Ed25519, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	srvs := make([]*sshtest.Server, n)
	var known, hosts []string
	for i := range srvs {
		srvs[i] = sshtest.New(t, append([]sshtest.Option{sshtest.WithAuthorizedKey("demo", pub)}, l.opts...)...)
		known = append(known, srvs[i].KnownHostsLine())
		hosts = append(hosts, fmt.Sprintf(`{"id": "0192a000-0000-7000-8000-00000000000%d", "label": %q, "hostname": "127.0.0.1",
             "port": %d, "user": "demo", "identity_id": "0192a000-0000-7000-8000-000000000001"}`, i+2, hostLabels[i], srvs[i].Port()))
	}
	srv := srvs[0]
	cfgDir := filepath.Join(xdg, "sshelf")
	if err := os.Mkdir(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "known_hosts"), []byte(strings.Join(known, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"version": 1,
  "identities": [{"id": "0192a000-0000-7000-8000-000000000001", "name": "demo", "key_path": "~/.ssh/id_ed25519"}],
  "hosts": [` + strings.Join(hosts, ",\n") + `]}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	master, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no PTY available: %v", err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	s := &slice{t: t, srv: srv, srvs: srvs, master: master, tty: tty, out: &syncBuffer{}, exited: make(chan struct{}), cfgDir: cfgDir, home: home}
	s.state = stateDir(home)
	s.before = stty(t, tty)

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(s.out, master)
	}()

	s.cmd = exec.Command(bin) //nolint:gosec // the binary this test just built
	s.cmd.Env = []string{
		"HOME=" + home, "XDG_CONFIG_HOME=" + xdg, "TERM=xterm-256color", "PATH=" + os.Getenv("PATH"),
	}
	s.cmd.Env = append(s.cmd.Env, l.env...)
	s.cmd.Dir = l.dir
	s.cmd.Stdin, s.cmd.Stdout, s.cmd.Stderr = tty, tty, tty
	// A new session without a controlling terminal: sshelf only needs its
	// stdin to be a TTY. With Setctty, macOS revokes and resets the slave
	// when sshelf exits, which would make the stty comparison meaningless.
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.exitErr = s.cmd.Wait(); close(s.exited) }()
	t.Cleanup(func() {
		select {
		case <-s.exited:
		default:
			_ = s.cmd.Process.Kill()
			<-s.exited
		}
		_ = tty.Close()
		_ = master.Close()
		<-drained
	})
	return s
}

// waitText waits until the output written since off shows want (compared
// without whitespace) and returns the offset just past the check.
func (s *slice) waitText(off int, want string) int {
	s.t.Helper()
	needle := whitespace.ReplaceAllString(want, "")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if strings.Contains(screenText(s.out.From(off)), needle) {
			return s.out.Len()
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %q; output since offset:\n%q", want, s.out.From(off))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *slice) typeKeys(keys string) {
	s.t.Helper()
	if _, err := s.master.WriteString(keys); err != nil {
		s.t.Fatal(err)
	}
}

// shells counts the shells srv has started.
func shells(srv *sshtest.Server) int { return len(srv.RequestsOfType("shell")) }

// openSession presses Enter on the selected host, whose server is srv,
// waits for srv's n-th shell and for the new tab to be focused in the
// header ("[<number> <label>]"), and returns the output offset after it.
// The prompt itself may not be redrawn: the renderer skips cells that
// equal the previous tab's.
func (s *slice) openSession(srv *sshtest.Server, n int) int {
	s.t.Helper()
	off := s.out.Len()
	s.typeKeys("\r")
	if !srv.WaitFor(10*time.Second, func() bool { return shells(srv) >= n }) {
		s.t.Fatalf("no shell was started; output:\n%q", s.out.From(0))
	}
	s.tabs++
	return s.waitText(off, fmt.Sprintf("[%d ", s.tabs))
}

// endSession types cmd (an exit) into the focused session, waits for the
// ended tab's status bar to show reason, closes the tab with x and waits
// for the host list. It returns the output offset after the list.
func (s *slice) endSession(off int, cmd, reason string) int {
	s.t.Helper()
	s.typeKeys(cmd + "\r")
	off = s.waitText(off, reason+" · r reconnect · x close")
	s.typeKeys("x")
	return s.waitText(off, "enter connect")
}

// prefix types Ctrl+] and then key.
func (s *slice) prefix(key string) {
	s.t.Helper()
	s.typeKeys("\x1d" + key)
}

// run performs the demo: Enter on web-prod, `exit 3` in the remote shell
// inside the frame, then x on the ended tab. It returns the output offset
// after the host list came back.
func (s *slice) run() int {
	s.t.Helper()
	s.waitText(0, "web-prod demo@127.0.0.1")
	off := s.openSession(s.srv, 1)
	return s.endSession(off, "exit 3", "exited (3)")
}

// quit presses q on the host list and waits for sshelf to exit.
func (s *slice) quit() {
	s.t.Helper()
	s.typeKeys("q")
	s.waitQuit()
}

// waitQuit waits for sshelf to exit with status 0.
func (s *slice) waitQuit() {
	s.t.Helper()
	select {
	case <-s.exited:
		if s.exitErr != nil {
			s.t.Fatalf("sshelf exited with %v; output:\n%q", s.exitErr, s.out.From(0))
		}
	case <-time.After(10 * time.Second):
		s.t.Fatalf("sshelf did not quit; output:\n%q", s.out.From(0))
	}
}

// checkStty fails unless the terminal is back in its startup mode.
func (s *slice) checkStty() {
	s.t.Helper()
	if after := stty(s.t, s.tty); after != s.before {
		s.t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", s.before, after)
	}
}

func TestFR_CONN_001_E2EKeyFileSlice(t *testing.T) {
	s := startSlice(t)
	s.run()
	s.quit()
	s.checkStty()
	if _, err := os.Stat(s.state); !os.IsNotExist(err) {
		t.Fatalf("state dir created without SSHELF_DEBUG (SEC-LOG-003): %v", err)
	}
}

// TestFR_CONN_001_M2_TermIsXterm256color: whatever the local TERM, the
// remote PTY is xterm-256color, the terminal the emulator implements, and
// its size is the session area: the 80×24 terminal minus the header and
// the status bar.
func TestFR_CONN_001_M2_TermIsXterm256color(t *testing.T) {
	s := startSlice(t, "TERM=screen-256color")
	s.run()
	s.quit()
	pty := s.srv.RequestsOfType("pty-req")
	if len(pty) != 1 || pty[0].Term != "xterm-256color" {
		t.Fatalf("pty-req = %+v, want TERM xterm-256color", pty)
	}
}

// TestFR_CONN_001_M2_PtySizeIsSessionArea: the PTY is the session area,
// and the remote's output is drawn inside the frame.
func TestFR_CONN_001_M2_PtySizeIsSessionArea(t *testing.T) {
	s := startSlice(t)
	start := s.waitText(0, "web-prod demo@127.0.0.1")
	off := s.openSession(s.srv, 1)
	if pty := s.srv.RequestsOfType("pty-req"); len(pty) != 1 || pty[0].Width != 80 || pty[0].Height != 22 {
		t.Fatalf("pty-req = %+v, want 80×22", pty)
	}
	s.waitText(start, "connected · demo@127.0.0.1") // the frame's status bar
	s.typeKeys("size\r")
	off = s.waitText(off, "80x22")
	s.endSession(off, "exit 0", "exited (0)")
	s.quit()
}

// TestFR_CONN_005_TerminalRestored: modes the remote turns on stay in its
// emulator and never reach the real terminal; after the session the TUI
// owns the keyboard, and after quitting stty -a is unchanged.
func TestFR_CONN_005_TerminalRestored(t *testing.T) {
	s := startSlice(t)
	s.waitText(0, "web-prod demo@127.0.0.1")
	off := s.openSession(s.srv, 1)
	s.typeKeys("modes 1,1004\r") // application cursor keys, focus events
	s.typeKeys("echo set\r")
	off = s.waitText(off, "set")
	off = s.endSession(off, "exit 3", "exited (3)")
	// The TUI is back and owns the keyboard: the first key after the
	// session reaches it (j on a one-host list changes nothing, q quits).
	s.typeKeys("j")
	time.Sleep(50 * time.Millisecond)
	if out := s.out.From(off); bytes.Contains(out, []byte("exit 3")) {
		t.Fatalf("remote output after return: %q", out)
	}
	s.quit()
	s.checkStty()
	for _, seq := range []string{"\x1b[?1h", "\x1b[?1004h"} {
		if bytes.Contains(s.out.From(0), []byte(seq)) {
			t.Errorf("the remote's %q reached the terminal", seq)
		}
	}
}

// TestSEC_LOG_003_E2EConnectStagesLogged runs the slice with SSHELF_DEBUG=1:
// the log, mode 0600, records the connect stages and auth method attempts.
func TestSEC_LOG_003_E2EConnectStagesLogged(t *testing.T) {
	s := startSlice(t, "SSHELF_DEBUG=1")
	s.run()
	s.quit()
	logFile := filepath.Join(s.state, "sshelf.log")
	st, err := os.Stat(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %04o", st.Mode().Perm())
	}
	b, err := os.ReadFile(logFile) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`msg="sshelf starting"`, `msg="connect: start" host=web-prod`, `msg="connect: dialling"`,
		`msg="connect: verifying host key"`, `msg="connect: auth attempt" user=demo host=127.0.0.1 method=publickey`,
		`msg="connect: authenticated"`, `msg="connect: session open"`, `msg="session: ended" result="exited (3)"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("log lacks %s; log:\n%s", want, b)
		}
	}
}
