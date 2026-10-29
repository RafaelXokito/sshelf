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
	"strconv"
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
		out, err := exec.Command("go", "build", "-ldflags", ldflags, "-o", binPath, ".").CombinedOutput() //nolint:gosec // test builds into its own temp dir
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
// key-file host that points at an in-process sshtest server.
type slice struct {
	t       *testing.T
	srv     *sshtest.Server
	master  *os.File
	tty     *os.File
	cmd     *exec.Cmd
	out     *syncBuffer
	exited  chan struct{} // closed once the process has exited
	exitErr error
	before  string // stty -a before launch
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

func startSlice(t *testing.T) *slice {
	t.Helper()
	if _, err := exec.LookPath("stty"); err != nil {
		t.Skip("stty not available")
	}
	bin := sshelfBinary(t)

	home, xdg := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, err := sshtest.WriteKeyFile(filepath.Join(home, ".ssh", "id_ed25519"), sshtest.Ed25519, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.New(t, sshtest.WithAuthorizedKey("demo", pub))
	cfgDir := filepath.Join(xdg, "sshelf")
	if err := os.Mkdir(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "known_hosts"), []byte(srv.KnownHostsLine()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"version": 1,
  "identities": [{"id": "0192a000-0000-7000-8000-000000000001", "name": "demo", "key_path": "~/.ssh/id_ed25519"}],
  "hosts": [{"id": "0192a000-0000-7000-8000-000000000002", "label": "web-prod", "hostname": "127.0.0.1",
             "port": ` + strconv.Itoa(srv.Port()) + `, "user": "demo", "identity_id": "0192a000-0000-7000-8000-000000000001"}]}`
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
	s := &slice{t: t, srv: srv, master: master, tty: tty, out: &syncBuffer{}, exited: make(chan struct{}), cfgDir: cfgDir, home: home}
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

// run performs the demo: Enter on web-prod, `exit 3` in the remote shell,
// then waits for the host list to show the result. It returns the output
// offset after the result appeared.
func (s *slice) run() int {
	s.t.Helper()
	off := s.waitText(0, "web-prod demo@127.0.0.1")
	s.typeKeys("\r")
	if !s.srv.WaitFor(10*time.Second, func() bool { return len(s.srv.RequestsOfType("shell")) == 1 }) {
		s.t.Fatalf("no shell was started; output:\n%q", s.out.From(0))
	}
	off = s.waitText(off, "$")
	s.typeKeys("exit 3\r")
	return s.waitText(off, "web-prod: exited (3)")
}

// quit presses q on the host list and waits for sshelf to exit.
func (s *slice) quit() {
	s.t.Helper()
	s.typeKeys("q")
	select {
	case <-s.exited:
		if s.exitErr != nil {
			s.t.Fatalf("sshelf exited with %v; output:\n%q", s.exitErr, s.out.From(0))
		}
	case <-time.After(10 * time.Second):
		s.t.Fatalf("sshelf did not quit after q; output:\n%q", s.out.From(0))
	}
}

func TestFR_CONN_001_E2EKeyFileSlice(t *testing.T) {
	s := startSlice(t)
	s.run()
	s.quit()
	if after := stty(t, s.tty); after != s.before {
		t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", s.before, after)
	}
	pty := s.srv.RequestsOfType("pty-req")
	if len(pty) != 1 || pty[0].Term != "xterm-256color" || pty[0].Width != 80 || pty[0].Height != 24 {
		t.Fatalf("pty-req = %+v", pty)
	}
}

func TestFR_CONN_005_TerminalRestored(t *testing.T) {
	s := startSlice(t)
	off := s.run()
	// The TUI is back and owns the keyboard: the first key after the
	// session reaches it (j on a one-host list changes nothing, q quits).
	s.typeKeys("j")
	time.Sleep(50 * time.Millisecond)
	if out := s.out.From(off); bytes.Contains(out, []byte("exit 3")) {
		t.Fatalf("remote output after return: %q", out)
	}
	s.quit()
	if after := stty(t, s.tty); after != s.before {
		t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", s.before, after)
	}
	all := s.out.From(0)
	if !bytes.Contains(all, []byte("\x1b[?1049l\x1b[?1000l")) {
		t.Fatal("the session reset sequence was not written")
	}
}
