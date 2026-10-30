//go:build unix

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

var (
	releaseOnce sync.Once
	releasePath string
	releaseErr  error
)

// releaseBinary builds cmd/sshelf the way the release does: CGO_ENABLED=0,
// -trimpath and -ldflags "-s -w" plus the version stamps (NFR-BUILD-001,
// NFR-SIZE-002).
func releaseBinary(t *testing.T) string {
	t.Helper()
	releaseOnce.Do(func() {
		releasePath = filepath.Join(binDir, "sshelf-release")
		ldflags := "-s -w -X main.version=" + e2eVersion + " -X main.commit=" + e2eCommit + " -X main.date=" + e2eDate
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", releasePath, ".") //nolint:gosec // test builds into its own temp dir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			releaseErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if releaseErr != nil {
		t.Fatal(releaseErr)
	}
	return releasePath
}

// writeFixture1k writes a config directory under xdg holding 1,000 hosts
// labelled host-0000 … host-0999 and returns the first label in list order.
func writeFixture1k(t *testing.T, xdg string) string {
	t.Helper()
	dir := filepath.Join(xdg, "sshelf")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), fixture1k(), 0o600); err != nil {
		t.Fatal(err)
	}
	return "host-0000"
}

// fixture1k returns a config.json with one key-file identity and 1,000
// hosts that use it, written in reverse order so the list has to sort them.
func fixture1k() []byte {
	var b strings.Builder
	b.WriteString(`{"version": 1, "identities": [{"id": "0192a000-0000-7000-8000-000000000001", "name": "demo", "key_path": "~/.ssh/id_ed25519"}], "hosts": [`)
	for i := 999; i >= 0; i-- {
		fmt.Fprintf(&b, `{"id": "0192a000-0000-7000-9000-%012d", "label": "host-%04d", "hostname": "10.0.%d.%d", "port": 22, "user": "deploy", "identity_id": "0192a000-0000-7000-8000-000000000001"}`,
			i, i, i/256, i%256)
		if i > 0 {
			b.WriteString(",")
		}
	}
	b.WriteString("]}")
	return []byte(b.String())
}

// timeToFirstFrame starts bin in an 80×24 PTY against the config under xdg
// and returns the time from exec to the first output that shows want.
func timeToFirstFrame(t *testing.T, bin, home, xdg, want string) time.Duration {
	t.Helper()
	master, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no PTY available: %v", err)
	}
	defer func() { _ = master.Close() }()
	defer func() { _ = tty.Close() }()
	if err := pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin) //nolint:gosec // the binary this test just built
	cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + xdg, "TERM=xterm-256color", "PATH=" + os.Getenv("PATH")}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	seen := make(chan time.Time, 1)
	drained := make(chan struct{})
	needle := whitespace.ReplaceAllString(want, "")
	var out bytes.Buffer
	go func() {
		defer close(drained)
		buf := make([]byte, 32<<10)
		found := false
		for {
			n, err := master.Read(buf)
			if n > 0 && !found {
				out.Write(buf[:n])
				if strings.Contains(screenText(out.Bytes()), needle) {
					found = true
					seen <- time.Now()
				}
			}
			if err != nil {
				return
			}
		}
	}()

	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() {
		_ = cmd.Process.Kill()
		<-exited
		_ = tty.Close()
		_ = master.Close()
		<-drained
	}()

	select {
	case at := <-seen:
		_, _ = master.WriteString("q") // quit cleanly; the deferred Kill is a fallback
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
		}
		return at.Sub(start)
	case <-time.After(10 * time.Second):
		t.Fatalf("no frame with %q within 10 s", want)
		return 0
	}
}

// TestNFR_PERF_001_Startup1k launches the release binary against a
// 1,000-host config and checks that the median time from exec to the first
// frame showing the first label is at most 150 ms. One unmeasured warm-up
// run absorbs the OS's first-launch cost of a freshly built binary (on
// macOS, the code-signature scan).
func TestNFR_PERF_001_Startup1k(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E timing")
	}
	if raceEnabled {
		t.Skip("timing limit not meaningful under -race; CI runs it in the perf step")
	}
	bin := releaseBinary(t)
	home, xdg := t.TempDir(), t.TempDir()
	first := writeFixture1k(t, xdg)

	timeToFirstFrame(t, bin, home, xdg, first)
	const runs = 5
	times := make([]time.Duration, runs)
	for i := range times {
		times[i] = timeToFirstFrame(t, bin, home, xdg, first)
	}
	slices.Sort(times)
	median := times[runs/2]
	const limit = 150 * time.Millisecond
	if median > limit {
		t.Fatalf("startup with 1,000 hosts: median %v of %v, want at most %v", median, times, limit)
	}
	t.Logf("startup with 1,000 hosts: median %v of %v", median, times)
}

// Mouse tracking: the sequences that turn terminal mouse reporting on
// (cell motion is ?1002h) and off.
var (
	mouseTrackingOn  = []string{"\x1b[?1000h", "\x1b[?1002h", "\x1b[?1003h"}
	mouseTrackingOff = []string{"\x1b[?1000l", "\x1b[?1002l", "\x1b[?1003l"}
)

// lastIndex is the last position of any of seqs in out, or -1.
func lastIndex(out []byte, seqs []string) int {
	at := -1
	for _, s := range seqs {
		at = max(at, bytes.LastIndex(out, []byte(s)))
	}
	return at
}

// trackingOn reports whether mouse tracking is on after out was written:
// a sequence that turns it on comes after the last one that turns it off.
func trackingOn(out []byte) bool {
	return lastIndex(out, mouseTrackingOn) > lastIndex(out, mouseTrackingOff)
}

// runChecked runs the slice (Enter on web-prod, `exit 3`, back to the
// list) and checks mouse tracking at each step (NFR-UX-001 revision,
// D-012): on while the host list is shown, off from before the remote
// shell's first output until the list is back, and on again after.
func (s *slice) runChecked() {
	s.t.Helper()
	off := s.waitText(0, "web-prod demo@127.0.0.1")
	if !trackingOn(s.out.From(0)) {
		s.t.Errorf("mouse tracking is off on the host list; output:\n%q", s.out.From(0))
	}
	s.typeKeys("\r")
	if !s.srv.WaitFor(10*time.Second, func() bool { return len(s.srv.RequestsOfType("shell")) == 1 }) {
		s.t.Fatalf("no shell was started; output:\n%q", s.out.From(0))
	}
	off = s.waitText(off, "$")
	if trackingOn(s.out.From(0)) {
		s.t.Errorf("mouse tracking is on while the session has the terminal; output since Enter:\n%q", s.out.From(off))
	}
	s.typeKeys("exit 3\r")
	s.waitText(off, "web-prod: exited (3)")
	if !trackingOn(s.out.From(0)) {
		s.t.Error("mouse tracking is off on the host list after the session")
	}
}

// TestNFR_UX_001_NoMouseTrackingInPlainSession runs the whole slice
// (start, list, connect, session, back, quit) and follows the mouse
// tracking sequences in every byte sshelf wrote (NFR-UX-001 M2 revision):
// cell motion on the host list, none while the remote shell has the
// terminal, so its native selection works (D-012), and none after
// quitting.
func TestNFR_UX_001_NoMouseTrackingInPlainSession(t *testing.T) {
	s := startSlice(t)
	s.runChecked()
	s.quit()
	out := s.out.From(0)
	if !bytes.Contains(out, []byte("\x1b[?1002h\x1b[?1006h")) {
		t.Errorf("cell-motion tracking with SGR encoding was never enabled; output:\n%q", out)
	}
	if trackingOn(out) {
		t.Error("mouse tracking is still on after quitting")
	}
}

// TestNFR_PORT_002_TermMatrix starts sshelf under each supported TERM,
// connects to the test server, exits the remote shell and checks that the
// host list is redrawn, the session got the same TERM, mouse tracking was
// off during the session and after quitting, and stty -a is unchanged
// after quitting.
func TestNFR_PORT_002_TermMatrix(t *testing.T) {
	for _, term := range []string{"xterm", "xterm-256color", "screen-256color", "tmux-256color", "xterm-kitty", "linux"} {
		t.Run(term, func(t *testing.T) {
			s := startSlice(t, "TERM="+term)
			s.runChecked()
			// The host list is drawn again after the session.
			s.waitText(0, "web-prod demo@127.0.0.1")
			s.quit()
			if after := stty(t, s.tty); after != s.before {
				t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", s.before, after)
			}
			if reqs := s.srv.RequestsOfType("pty-req"); len(reqs) != 1 || reqs[0].Term != term {
				t.Fatalf("pty-req = %+v, want TERM %q", reqs, term)
			}
			if trackingOn(s.out.From(0)) {
				t.Error("mouse tracking is still on after quitting")
			}
		})
	}
}
