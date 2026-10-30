//go:build unix

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFR_KH_001_E2ETrustThenNoPrompt is the T-007 demo: the server's key
// is unknown, the user trusts it in the TOFU modal and gets a shell, and
// the second connection shows no prompt.
func TestFR_KH_001_E2ETrustThenNoPrompt(t *testing.T) {
	s := startSlice(t)
	kh := filepath.Join(s.cfgDir, "known_hosts")
	if err := os.Remove(kh); err != nil { // untrusted from here on
		t.Fatal(err)
	}
	openssh := filepath.Join(s.home, ".ssh", "known_hosts")

	off := s.waitText(0, "web-prod demo@127.0.0.1")
	s.typeKeys("\r")
	off = s.waitText(off, "Unknown host key")
	if n := len(s.srv.AuthAttempts()); n != 0 {
		t.Fatalf("%d auth attempts while the prompt is open", n)
	}
	s.typeKeys("y")
	if !s.srv.WaitFor(10*time.Second, func() bool { return shells(s.srv) == 1 }) {
		t.Fatalf("no shell after trusting; output:\n%q", s.out.From(0))
	}
	s.tabs = 1
	off = s.waitText(off, "[1 web-prod]") // the session's tab
	off = s.endSession(off, "exit 3", "exited (3)")

	b, err := os.ReadFile(kh) //nolint:gosec // test temp dir
	if err != nil || strings.Count(string(b), "\n") != 1 || !strings.Contains(string(b), s.srv.KnownHostsLine()) {
		t.Fatalf("known_hosts = %q, %v", b, err)
	}

	next := s.openSession(s.srv, 2)
	if bytes.Contains(s.out.From(off), []byte("Unknown host key")) {
		t.Fatal("prompted again for a trusted key")
	}
	s.endSession(next, "exit 0", "exited (0)")
	s.quit()
	if _, err := os.Stat(openssh); err == nil {
		t.Fatal("~/.ssh/known_hosts was created")
	}
}
