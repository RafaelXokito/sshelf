//go:build unix

package main

import (
	"bytes"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The M2 framed sessions end to end: several tabs at once, resize,
// disconnect, quit and signals (T-037).

// openSame opens n sessions to web-prod, going back to the host list
// between them, and leaves the last one focused.
func (s *slice) openSame(n int) {
	s.t.Helper()
	s.waitText(0, "web-prod demo@127.0.0.1")
	for i := 1; i <= n; i++ {
		if i > 1 {
			s.prefix("h")
		}
		s.openSession(s.srv, i)
	}
}

// waitCloses waits until srv has seen n channels close.
func waitCloses(t *testing.T, s *slice, srv *sshtest.Server, n int, within time.Duration) {
	t.Helper()
	if !srv.WaitFor(within, func() bool { return len(srv.Closes()) >= n }) {
		t.Fatalf("%d channel closes within %v, want %d; output:\n%q", len(srv.Closes()), within, n, s.out.From(0))
	}
}

// TestFR_TAB_002_E2ETwoServers connects two hosts on two servers at
// once: each tab gets its own connection, input reaches the right server
// and its output shows inside the frame; a tab in the background keeps
// reading (FR-TAB-008) and its output is there when it is focused again.
func TestFR_TAB_002_E2ETwoServers(t *testing.T) {
	s := startServers(t, sshelfBinary(t), 2)
	prod, test := s.srvs[0], s.srvs[1]
	s.waitText(0, "web-test demo@127.0.0.1")
	s.openSession(prod, 1) // tab 1: web-prod
	s.prefix("h")
	s.typeKeys("j")
	off := s.openSession(test, 1) // tab 2: web-test
	s.typeKeys("echo on-web-test\r")
	off = s.waitText(off, "on-web-test")

	s.prefix("1")
	off = s.waitText(off, "[1 web-prod]")
	s.typeKeys("flood 1M\r") // keeps writing while its tab is in the background
	s.prefix("2")
	off = s.waitText(off, "[2 web-test]")
	time.Sleep(time.Second)
	s.prefix("1")
	s.waitText(off, "flood done")

	if !bytes.Contains(prod.Received(1, 1), []byte("flood 1M\r")) || bytes.Contains(prod.Received(1, 1), []byte("on-web-test")) {
		t.Fatalf("web-prod received %q", prod.Received(1, 1))
	}
	if !bytes.Contains(test.Received(1, 1), []byte("echo on-web-test\r")) || bytes.Contains(test.Received(1, 1), []byte("flood")) {
		t.Fatalf("web-test received %q", test.Received(1, 1))
	}
	if len(prod.ClientVersions()) != 1 || len(test.ClientVersions()) != 1 {
		t.Fatal("each tab should have its own connection")
	}
	s.prefix("h")
	s.typeKeys("q")
	s.waitText(off, "Quit and close 2 tabs?")
	s.typeKeys("y")
	s.waitQuit()
	waitCloses(t, s, prod, 1, 2*time.Second)
	waitCloses(t, s, test, 1, 2*time.Second)
	s.checkStty()
}

// TestFR_TAB_002_E2ETwoSessionsSameHost: connecting to web-prod twice
// opens tabs 1 and 2, each with its own SSH connection.
func TestFR_TAB_002_E2ETwoSessionsSameHost(t *testing.T) {
	s := startSlice(t)
	s.openSame(2)
	if n := len(s.srv.ClientVersions()); n != 2 {
		t.Fatalf("%d connections, want 2", n)
	}
	s.typeKeys("echo two\r")
	s.waitText(0, "two")
	if !bytes.Contains(s.srv.Received(2, 1), []byte("echo two\r")) || bytes.Contains(s.srv.Received(1, 1), []byte("echo two")) {
		t.Fatalf("received: conn 1 %q, conn 2 %q", s.srv.Received(1, 1), s.srv.Received(2, 1))
	}
}

// TestFR_CONN_014_E2EChannelClosed: Ctrl+] q and y close the SSH channel
// within 1 s and close the tab.
func TestFR_CONN_014_E2EChannelClosed(t *testing.T) {
	s := startSlice(t)
	s.openSame(1)
	off := s.out.Len()
	s.prefix("q")
	off = s.waitText(off, "Disconnect web-prod?")
	s.typeKeys("y")
	waitCloses(t, s, s.srv, 1, time.Second)
	s.waitText(off, "enter connect") // back on the host list
	s.quit()
}

// TestFR_TAB_006_KeyBytesOnTheWire: keys typed into a focused session
// reach the server encoded as xterm does, sshelf's own bindings included.
func TestFR_TAB_006_KeyBytesOnTheWire(t *testing.T) {
	s := startSlice(t)
	s.openSame(1)
	s.typeKeys("echo-hex\r")
	if !s.srv.WaitFor(5*time.Second, func() bool { return bytes.Contains(s.srv.Received(1, 1), []byte("echo-hex\r")) }) {
		t.Fatal("echo-hex did not reach the server")
	}
	// Up, Ctrl+C, q, /, Ctrl+Left and a paste.
	s.typeKeys("\x1b[A")
	s.typeKeys("\x03")
	s.typeKeys("q/")
	s.typeKeys("\x1b[1;5D")
	s.typeKeys(paste("pasted"))
	want := "echo-hex\r\x1b[A\x03q/\x1b[1;5Dpasted"
	if !s.srv.WaitFor(5*time.Second, func() bool { return bytes.Contains(s.srv.Received(1, 1), []byte(want)) }) {
		t.Fatalf("server received %q, want it to contain %q", s.srv.Received(1, 1), want)
	}
	s.prefix("q")
	s.typeKeys("y")
	waitCloses(t, s, s.srv, 1, 2*time.Second)
	s.quit()
}

// TestFR_CONN_004_M2_ResizeReachesAllSessions: with three sessions open
// (one focused, two in the background), resizing the terminal to 100×30
// sends each server a window-change for the 100×28 session area.
func TestFR_CONN_004_M2_ResizeReachesAllSessions(t *testing.T) {
	s := startSlice(t)
	s.openSame(3)
	if err := pty.Setsize(s.master, &pty.Winsize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	// sshelf has no controlling terminal (Setsid), so the kernel does not
	// signal it; send what the terminal would.
	if err := s.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	resized := func() bool {
		got := map[int]bool{}
		for _, r := range s.srv.RequestsOfType("window-change") {
			if r.Width == 100 && r.Height == 28 {
				got[r.Conn] = true
			}
		}
		return len(got) == 3
	}
	if !s.srv.WaitFor(5*time.Second, resized) {
		t.Fatalf("window-changes = %+v, want 100×28 on 3 connections", s.srv.RequestsOfType("window-change"))
	}
	off := s.out.Len()
	s.typeKeys("size\r")
	s.waitText(off, "100x28")
	for conn := 1; conn <= 3; conn++ {
		n := 0
		for _, r := range s.srv.RequestsOfType("window-change") {
			if r.Conn == conn {
				n++
			}
		}
		if n > 2 {
			t.Errorf("connection %d got %d window-changes for one resize", conn, n)
		}
	}
}

// TestARCH_SESS_010_QuitClosesAll: quitting with three sessions open asks
// first, then closes every connection; the terminal is restored
// (ARCH-SESS-005 revision: stty -a unchanged after exit).
func TestARCH_SESS_010_QuitClosesAll(t *testing.T) {
	s := startSlice(t)
	s.openSame(3)
	s.prefix("h")
	off := s.out.Len()
	s.typeKeys("q")
	s.waitText(off, "Quit and close 3 tabs?")
	s.typeKeys("y")
	s.waitQuit()
	waitCloses(t, s, s.srv, 3, 2*time.Second)
	s.checkStty()
}

// TestARCH_SESS_005_M2_SttyRestoredOnExit: SIGTERM and SIGHUP quit
// sshelf cleanly while a session is open: exit status 0, the connection
// closed and stty -a unchanged.
func TestARCH_SESS_005_M2_SttyRestoredOnExit(t *testing.T) {
	for name, sig := range map[string]syscall.Signal{"SIGTERM": syscall.SIGTERM, "SIGHUP": syscall.SIGHUP} {
		t.Run(name, func(t *testing.T) {
			s := startSlice(t)
			s.openSame(1)
			if err := s.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			s.waitQuit()
			waitCloses(t, s, s.srv, 1, 2*time.Second)
			s.checkStty()
		})
	}
}

// TestFR_TAB_008_BackgroundTenThousandLines: a remote command that prints
// over 10,000 lines while its tab is in the background completes (the
// server records the exit), and focusing the tab shows the end of the
// output.
func TestFR_TAB_008_BackgroundTenThousandLines(t *testing.T) {
	s := startSlice(t)
	s.openSame(1)
	s.typeKeys("flood 1M\rexit 7\r") // 1 MiB of 100-column lines, then exit
	off := s.out.Len()
	s.prefix("h")
	s.waitText(off, "[Hosts]")
	waitCloses(t, s, s.srv, 1, 10*time.Second)
	off = s.out.Len()
	s.prefix("1")
	s.waitText(off, "flood done")
	s.waitText(off, "exited (7)")
}
