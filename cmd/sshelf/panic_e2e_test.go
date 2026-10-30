//go:build unix

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var (
	faultOnce sync.Once
	faultPath string
	faultErr  error
)

// faultBinary builds cmd/sshelf with the sshelf_faultinject tag once per
// test run. SSHELF_FAULT then names the point that panics (ARCH-SEC-004).
func faultBinary(t *testing.T) string {
	t.Helper()
	faultOnce.Do(func() {
		faultPath = filepath.Join(binDir, "sshelf-faultinject")
		out, err := exec.Command("go", "build", "-tags", "sshelf_faultinject", "-o", faultPath, ".").CombinedOutput() //nolint:gosec // test builds into its own temp dir
		if err != nil {
			faultErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if faultErr != nil {
		t.Fatal(faultErr)
	}
	return faultPath
}

const faultCanary = "CANARY-fault-5d1e0b"

func startFault(t *testing.T, point string) *slice {
	t.Helper()
	return startSliceBin(t, faultBinary(t), "SSHELF_FAULT="+point, "SSHELF_FAULT_CANARY="+faultCanary)
}

// waitExit waits for sshelf to exit and returns its exit status.
func (s *slice) waitExit() int {
	s.t.Helper()
	select {
	case <-s.exited:
	case <-time.After(10 * time.Second):
		s.t.Fatalf("sshelf did not exit; output:\n%q", s.out.From(0))
	}
	if ee, ok := errors.AsType[*exec.ExitError](s.exitErr); ok {
		return ee.ExitCode()
	}
	if s.exitErr != nil {
		s.t.Fatal(s.exitErr)
	}
	return 0
}

// checkCrash checks the terminal is back in its startup (cooked) mode, and
// that there is exactly one crash report, mode 0600, and that neither it nor
// the terminal output contains the canary from the panic message.
func (s *slice) checkCrash() {
	s.t.Helper()
	if after := stty(s.t, s.tty); after != s.before {
		s.t.Fatalf("stty -a changed:\nbefore:\n%s\nafter:\n%s", s.before, after)
	}
	reports, _ := filepath.Glob(filepath.Join(s.state, "crash-*.txt"))
	if len(reports) != 1 {
		s.t.Fatalf("crash reports = %v, want one", reports)
	}
	st, err := os.Stat(reports[0])
	if err != nil || st.Mode().Perm() != 0o600 {
		s.t.Fatalf("crash report: %v, mode %v", err, st.Mode())
	}
	b, err := os.ReadFile(reports[0])
	if err != nil {
		s.t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("sshelf crash report")) || bytes.Contains(b, []byte(faultCanary)) {
		s.t.Fatalf("crash report:\n%s", b)
	}
	// Give the drain a moment to collect the last output.
	time.Sleep(50 * time.Millisecond)
	if bytes.Contains(s.out.From(0), []byte(faultCanary)) {
		s.t.Fatalf("terminal output contains the canary:\n%q", s.out.From(0))
	}
}

func TestARCH_SEC_004_PanicInUpdateExit70(t *testing.T) {
	s := startFault(t, "update")
	off := s.waitText(0, "web-prod demo@127.0.0.1")
	s.typeKeys("j")
	if code := s.waitExit(); code != 70 {
		t.Fatalf("exit status %d, want 70; output:\n%q", code, s.out.From(0))
	}
	s.checkCrash()
	out := s.out.From(off)
	if !bytes.Contains(out, []byte("\x1b[?1049l\x1b[?1000l")) {
		t.Fatalf("no reset sequence after the panic; output:\n%q", out)
	}
	s.waitText(off, "crash report:")
}

// TestARCH_SEC_004_PanicWithSessionOpenExit70: a panic in Update while a
// session is open in its tab (on the session's first output). sshelf
// exits 70 with the terminal restored.
func TestARCH_SEC_004_PanicWithSessionOpenExit70(t *testing.T) {
	s := startFault(t, "session-update")
	off := s.waitText(0, "web-prod demo@127.0.0.1")
	s.typeKeys("\r")
	if code := s.waitExit(); code != 70 {
		t.Fatalf("exit status %d, want 70; output:\n%q", code, s.out.From(0))
	}
	s.checkCrash()
	s.waitText(off, "crash report:")
}

// TestARCH_SEC_004_PanicInSessionGoroutine: a panic in a session goroutine
// (the input writer, on the first key typed into the session) ends the
// session with "internal error" in its tab; the TUI keeps running and
// quits normally.
func TestARCH_SEC_004_PanicInSessionGoroutine(t *testing.T) {
	s := startFault(t, "session-input")
	s.waitText(0, "web-prod demo@127.0.0.1")
	off := s.openSession(s.srv, 1)
	s.typeKeys("x")
	off = s.waitText(off, "internal error · r reconnect · x close")
	s.typeKeys("x")
	off = s.waitText(off, "enter connect")
	s.typeKeys("j")
	s.quit()
	s.checkCrash()
	if bytes.Contains(s.out.From(off), []byte("crash report:")) {
		t.Fatal("a recovered goroutine panic was announced as a crash")
	}
}

// TestARCH_SESS_005_M2_OtherSessionSurvivesPanic: with two sessions open,
// a panic in one session's goroutine ends that session with "internal
// error" while the other still echoes input; quitting then restores the
// terminal (stty -a unchanged, checkCrash).
func TestARCH_SESS_005_M2_OtherSessionSurvivesPanic(t *testing.T) {
	s := startServers(t, faultBinary(t), 2, "SSHELF_FAULT=session-input", "SSHELF_FAULT_ONCE=1", "SSHELF_FAULT_CANARY="+faultCanary)
	s.waitText(0, "web-test demo@127.0.0.1")
	s.openSession(s.srvs[0], 1) // tab 1: web-prod
	s.prefix("h")
	s.typeKeys("j")
	off := s.openSession(s.srvs[1], 1) // tab 2: web-test
	s.typeKeys("z")                    // the first input written panics
	off = s.waitText(off, "internal error · r reconnect · x close")
	s.prefix("1")
	s.typeKeys("echo alive\r")
	s.waitText(off, "alive")
	if !s.srvs[0].WaitFor(5*time.Second, func() bool { return bytes.Contains(s.srvs[0].Received(1, 1), []byte("echo alive\r")) }) {
		t.Fatalf("web-prod did not get the input; output:\n%q", s.out.From(0))
	}
	s.prefix("h")
	s.typeKeys("q")
	s.waitText(off, "Quit and close 1 tab?")
	s.typeKeys("y")
	s.waitQuit()
	s.checkCrash()
}
