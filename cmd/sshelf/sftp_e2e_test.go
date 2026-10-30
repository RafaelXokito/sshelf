//go:build unix

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The SFTP tab end to end (T-054): opened from the host list and from a
// session tab, uploads and downloads with checksums, resume after a
// dropped connection, closing and quitting. sshelf runs in a PTY in a
// temp dir (the local pane's start, FR-SFTP-005); web-prod's sshtest
// serves another temp dir with the home /home/pi (ARCH-TEST-004).

// sftpRun is a slice whose web-prod serves SFTP.
type sftpRun struct {
	*slice
	local string // sshelf's working directory: the local pane
}

// startSFTP starts sshelf in a new local dir, with web-prod serving SFTP
// from a new dir and the extra server options.
func startSFTP(t *testing.T, extra ...sshtest.Option) *sftpRun {
	t.Helper()
	local := t.TempDir()
	opts := append([]sshtest.Option{sshtest.WithSFTP(t.TempDir())}, extra...)
	return &sftpRun{slice: startWith(t, sshelfBinary(t), 1, launch{opts: opts, dir: local}), local: local}
}

// remote is the local path of the remote file name in /home/pi.
func (r *sftpRun) remote(name string) string { return r.srv.SFTPDir(name) }

// openSFTPHost presses s on the host list (web-prod selected), waits for
// the new SFTP tab to be focused and for the remote pane to list want,
// and returns the output offset after it.
func (r *sftpRun) openSFTPHost(want string) int {
	r.t.Helper()
	r.waitText(0, "web-prod demo@127.0.0.1")
	off := r.out.Len()
	r.typeKeys("s")
	r.tabs++
	r.waitText(off, fmt.Sprintf("[%d sftp: web-prod]", r.tabs))
	return r.waitText(off, want)
}

// openSFTPOn presses Ctrl+] s on the focused session tab and waits as
// openSFTPHost does.
func (r *sftpRun) openSFTPOn(want string) int {
	r.t.Helper()
	off := r.out.Len()
	r.prefix("s")
	r.tabs++
	r.waitText(off, fmt.Sprintf("[%d sftp: web-prod]", r.tabs))
	return r.waitText(off, want)
}

// disconnects counts the connections srv has seen end.
func disconnects(srv *sshtest.Server) int {
	n := 0
	for _, c := range srv.Closes() {
		if c.Type == "disconnect" {
			n++
		}
	}
	return n
}

// waitDisconnects waits until srv has seen exactly n connections end, and
// checks that no more end within a short while (each closes once).
func (r *sftpRun) waitDisconnects(n int) {
	r.t.Helper()
	if !r.srv.WaitFor(5*time.Second, func() bool { return disconnects(r.srv) >= n }) {
		r.t.Fatalf("%d connections closed, want %d; closes %+v", disconnects(r.srv), n, r.srv.Closes())
	}
	time.Sleep(100 * time.Millisecond)
	if got := disconnects(r.srv); got != n {
		r.t.Fatalf("%d connections closed, want exactly %d", got, n)
	}
}

// writeRandom writes size random bytes to path and returns their SHA-256.
func writeRandom(t *testing.T, path string, size int64) [32]byte {
	t.Helper()
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// fileSum returns the SHA-256 and size of path.
func fileSum(path string) ([32]byte, int64, error) {
	f, err := os.Open(path) //nolint:gosec // a test temp dir
	if err != nil {
		return [32]byte{}, 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, n, err
}

// zeroSum is the SHA-256 of size zero bytes.
func zeroSum(size int64) [32]byte {
	h := sha256.New()
	_, _ = io.Copy(h, io.LimitReader(zeroReader{}, size))
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// waitFile waits until path exists with the SHA-256 want: a transfer
// renames its part file into place only once it is complete.
func (r *sftpRun) waitFile(path string, want [32]byte, within time.Duration) {
	r.t.Helper()
	deadline := time.Now().Add(within)
	for {
		sum, _, err := fileSum(path)
		if err == nil && sum == want {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("%s: %v, sum matches %v; output since start:\n%q", path, err, sum == want, r.out.From(max(r.out.Len()-4000, 0)))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// noParts fails if dir holds anything but the names in keep.
func noParts(t *testing.T, dir string, keep ...string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		ok := false
		for _, k := range keep {
			ok = ok || e.Name() == k
		}
		if !ok {
			t.Errorf("%s holds %s", dir, e.Name())
		}
	}
}

// quitAll goes to the Hosts tab, presses q, waits for question and
// answers y, and waits for sshelf to exit 0.
func (r *sftpRun) quitAll(question string) {
	r.t.Helper()
	r.prefix("h")
	off := r.out.Len()
	r.typeKeys("q")
	r.waitText(off, question)
	r.typeKeys("y")
	r.waitQuit()
}

// TestFR_SFTP_002_E2EOpenFromHostList: s on the host list connects
// through the normal connect flow and opens a focused SFTP tab listing
// /home/pi, with no PTY and no shell; closing it closes the connection
// once (FR-SFTP-025).
func TestFR_SFTP_002_E2EOpenFromHostList(t *testing.T) {
	r := startSFTP(t)
	writeRandom(t, r.remote("hello.txt"), 10)
	off := r.openSFTPHost("hello.txt")
	if n := len(r.srv.RequestsOfType("pty-req")) + len(r.srv.RequestsOfType("shell")); n != 0 {
		t.Fatalf("%d pty-req or shell requests", n)
	}
	subs := r.srv.RequestsOfType("subsystem")
	if len(subs) == 0 {
		t.Fatal("no subsystem request")
	}
	for _, s := range subs {
		if s.Name != "sftp" || s.Conn != 1 {
			t.Fatalf("subsystem request %+v", s)
		}
	}
	r.prefix("q")
	off = r.waitText(off, "Close SFTP tab 1?")
	r.typeKeys("y")
	r.waitText(off, "enter connect")
	r.waitDisconnects(1)
	r.quit() // nothing live: q quits at once
	r.checkStty()
}

// TestFR_SFTP_002_E2ENoSftpNoTab: a server that refuses the subsystem
// opens no tab, closes the connection and says why.
func TestFR_SFTP_002_E2ENoSftpNoTab(t *testing.T) {
	r := startSFTP(t, sshtest.WithoutSFTP())
	r.waitText(0, "web-prod demo@127.0.0.1")
	off := r.out.Len()
	r.typeKeys("s")
	r.waitText(off, "web-prod: the server does not offer SFTP")
	r.waitDisconnects(1)
	if bytes.Contains(r.out.From(off), []byte("sftp: web-prod")) {
		t.Fatal("an SFTP tab opened")
	}
	r.quit()
}

// TestFR_SFTP_003_E2ESameConnectionNoAuth: Ctrl+] s on a live session
// opens the SFTP tab on the session's connection: no new connection, no
// new authentication, a subsystem channel on connection 1.
func TestFR_SFTP_003_E2ESameConnectionNoAuth(t *testing.T) {
	r := startSFTP(t)
	writeRandom(t, r.remote("hello.txt"), 10)
	r.waitText(0, "web-prod demo@127.0.0.1")
	r.openSession(r.srv, 1)
	auths := len(r.srv.AuthAttempts())
	r.openSFTPOn("hello.txt")
	if n := len(r.srv.ClientVersions()); n != 1 {
		t.Fatalf("%d connections, want 1", n)
	}
	if n := len(r.srv.AuthAttempts()); n != auths {
		t.Fatalf("auth attempts went from %d to %d", auths, n)
	}
	subs := r.srv.RequestsOfType("subsystem")
	if len(subs) == 0 || subs[0].Conn != 1 {
		t.Fatalf("subsystem requests %+v, want them on connection 1", subs)
	}
	r.quitAll("Quit and close 2 tabs?")
	r.waitDisconnects(1)
	r.checkStty()
}

// TestFR_SFTP_004_E2ECloseEitherFirst: a session tab and an SFTP tab on
// one connection; whichever ends first, the other keeps working, and the
// connection closes once, after both.
func TestFR_SFTP_004_E2ECloseEitherFirst(t *testing.T) {
	t.Run("shell exits first", func(t *testing.T) {
		r := startSFTP(t)
		sum := writeRandom(t, r.remote("keep.bin"), 32<<20)
		r.waitText(0, "web-prod demo@127.0.0.1")
		off := r.openSession(r.srv, 1)
		r.openSFTPOn("keep.bin")
		r.typeKeys("jc") // download keep.bin
		r.prefix("1")
		r.typeKeys("exit 0\r")
		off = r.waitText(off, "exited (0)")
		r.typeKeys("x")
		r.waitFile(filepath.Join(r.local, "keep.bin"), sum, 30*time.Second)
		if n := disconnects(r.srv); n != 0 {
			t.Fatalf("%d connection closes after the shell exited", n)
		}
		writeRandom(t, r.remote("late.txt"), 10)
		r.prefix("2")
		r.typeKeys("R")
		off = r.waitText(off, "late.txt")
		r.prefix("q")
		r.waitText(off, "Close SFTP tab 2?")
		r.typeKeys("y")
		r.waitDisconnects(1)
		r.quit()
	})
	t.Run("sftp closed first", func(t *testing.T) {
		r := startSFTP(t)
		writeRandom(t, r.remote("hello.txt"), 10)
		r.waitText(0, "web-prod demo@127.0.0.1")
		r.openSession(r.srv, 1)
		off := r.openSFTPOn("hello.txt")
		r.prefix("q")
		off = r.waitText(off, "Close SFTP tab 2?")
		r.typeKeys("y")
		off = r.waitText(off, "[1 web-prod]")
		r.typeKeys("echo still-here\r")
		off = r.waitText(off, "still-here")
		if n := disconnects(r.srv); n != 0 {
			t.Fatalf("%d connection closes after the SFTP tab closed", n)
		}
		r.endSession(off, "exit 0", "exited (0)")
		r.waitDisconnects(1)
		r.quit()
	})
}

// TestFR_SFTP_001_E2EUploadDownload: c in the local pane uploads a file
// and c in the remote pane downloads another; both arrive with the
// original's SHA-256 and no part file is left.
func TestFR_SFTP_001_E2EUploadDownload(t *testing.T) {
	r := startSFTP(t)
	up := writeRandom(t, filepath.Join(r.local, "up.bin"), 16<<20)
	down := writeRandom(t, r.remote("down.bin"), 16<<20)
	r.openSFTPHost("down.bin")

	r.typeKeys("\t") // the local pane: .., up.bin
	r.typeKeys("jc") // upload up.bin into /home/pi
	r.waitFile(r.remote("up.bin"), up, 30*time.Second)

	r.typeKeys("\t") // the remote pane: .., down.bin, up.bin
	r.typeKeys("jc") // download down.bin into the local dir
	r.waitFile(filepath.Join(r.local, "down.bin"), down, 30*time.Second)

	noParts(t, r.local, "up.bin", "down.bin")
	noParts(t, r.remote(""), "up.bin", "down.bin")
	r.quitAll("Quit and close 1 tab?")
	r.waitDisconnects(1)
}

// TestFR_SFTP_018_E2EResumeAfterDrop: the connection drops after 20 MiB
// of a 64 MiB transfer; the tab shows ✕ and the paused transfer, r
// reconnects through the normal connect flow, and the transfer resumes
// (FR-SFTP-017): it finishes with the right SHA-256, and at most 48 MiB
// are sent the second time.
func TestFR_SFTP_018_E2EResumeAfterDrop(t *testing.T) {
	const size = 64 << 20
	t.Run("download", func(t *testing.T) {
		r := startSFTP(t, sshtest.WithSFTPZeroFile("zero.bin", size), sshtest.WithSFTPFault("zero.bin", 20<<20, sshtest.FaultDrop))
		off := r.openSFTPHost("zero.bin")
		r.typeKeys("jc")
		r.waitText(off, "1 transfer paused: connection lost · r reconnect")
		r.waitText(off, "✕]") // the tab bar marks the lost connection
		r.waitHandlesClosed()
		first := r.srv.SFTPBytesRead("zero.bin")
		r.typeKeys("r")
		r.waitFile(filepath.Join(r.local, "zero.bin"), zeroSum(size), 60*time.Second)
		if again := r.srv.SFTPBytesRead("zero.bin") - first; again > 48<<20 {
			t.Fatalf("read %d bytes after the reconnect, want at most 48 MiB", again)
		}
		if n := len(r.srv.ClientVersions()); n != 2 {
			t.Fatalf("%d connections, want 2", n)
		}
		noParts(t, r.local, "zero.bin")
		r.quitAll("Quit and close 1 tab?")
		r.waitDisconnects(2)
	})
	t.Run("upload", func(t *testing.T) {
		part := "up.bin.sshelf-part"
		r := startSFTP(t, sshtest.WithSFTPFault(part, 20<<20, sshtest.FaultDrop))
		sum := writeRandom(t, filepath.Join(r.local, "up.bin"), size)
		writeRandom(t, r.remote("hello.txt"), 10)
		off := r.openSFTPHost("hello.txt")
		r.typeKeys("\tjc")
		// sshtest closes the TCP connection in the middle of the upload's
		// stream. On macOS the client then sometimes sees nothing until
		// TCP keepalive (Go's default, 15 s idle) draws a reset: a
		// goroutine dump during the stall shows sshelf's connection
		// reader waiting in read and every write waiting for a reply, and
		// the debug log has "connection reset by peer" 15 s after the
		// drop (seen on CI and under load locally). A silent drop is
		// detected that way on a real network too (ssh keepalives are
		// v1, FR-CONN-009), so allow for it here.
		paused := "1 transfer paused: connection lost · r reconnect"
		r.srv.WaitFor(40*time.Second, func() bool {
			return strings.Contains(screenText(r.out.From(off)), whitespace.ReplaceAllString(paused, ""))
		})
		r.waitText(off, paused)
		r.waitHandlesClosed()
		first := r.srv.SFTPBytesWritten(part)
		r.typeKeys("r")
		r.waitFile(r.remote("up.bin"), sum, 60*time.Second)
		if again := r.srv.SFTPBytesWritten(part) - first; again > 48<<20 {
			t.Fatalf("wrote %d bytes after the reconnect, want at most 48 MiB", again)
		}
		noParts(t, r.remote(""), "up.bin", "hello.txt")
		r.quitAll("Quit and close 1 tab?")
		r.waitDisconnects(2)
	})
}

// waitHandlesClosed waits until sshtest's request server for the dropped
// connection has applied the requests that were in flight and closed its
// files, so that byte counts read afterwards count only what the
// reconnect sends (a late write counted as resent failed the 48 MiB
// bound under load).
func (r *sftpRun) waitHandlesClosed() {
	r.t.Helper()
	if !r.srv.WaitFor(5*time.Second, func() bool { return r.srv.SFTPOpenHandles() == 0 }) {
		r.t.Fatalf("%d SFTP handles still open after the drop", r.srv.SFTPOpenHandles())
	}
}

// bigFile is a virtual remote file large enough to still be downloading
// when a test closes the tab or quits.
const bigFile = 4 << 30

// startDownload opens an SFTP tab from the host list, starts the
// download of big.bin and waits for its progress line.
func (r *sftpRun) startDownload() int {
	r.t.Helper()
	off := r.openSFTPHost("big.bin")
	r.typeKeys("jc")
	return r.waitText(off, "↓ big.bin")
}

// TestFR_SFTP_025_E2ECloseReleases: Ctrl+] q on an SFTP tab with a
// running download asks with the count, then cancels the transfer
// (its part file is removed), closes the channels and the connection.
func TestFR_SFTP_025_E2ECloseReleases(t *testing.T) {
	r := startSFTP(t, sshtest.WithSFTPZeroFile("big.bin", bigFile))
	off := r.startDownload()
	r.prefix("q")
	off = r.waitText(off, "Close SFTP tab 1? 1 transfer will stop.")
	r.typeKeys("y")
	r.waitText(off, "enter connect")
	r.waitDisconnects(1)
	if !r.srv.WaitFor(5*time.Second, func() bool { ents, _ := os.ReadDir(r.local); return len(ents) == 0 }) {
		noParts(t, r.local)
	}
	r.quit()
}

// TestFR_TAB_009_M3_SigtermCancelsTransfers: SIGTERM and SIGHUP while a
// download runs quit without asking: exit status 0, the part file
// removed, the connection closed, the terminal restored.
func TestFR_TAB_009_M3_SigtermCancelsTransfers(t *testing.T) {
	for name, sig := range map[string]syscall.Signal{"SIGTERM": syscall.SIGTERM, "SIGHUP": syscall.SIGHUP} {
		t.Run(name, func(t *testing.T) {
			r := startSFTP(t, sshtest.WithSFTPZeroFile("big.bin", bigFile))
			r.startDownload()
			if err := r.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			r.waitQuit()
			r.waitDisconnects(1)
			noParts(t, r.local)
			r.checkStty()
		})
	}
}

// TestARCH_SFTP_007_QuitClosesAll: with a session, an SFTP tab on its
// connection and an SFTP tab of its own downloading, q asks "Quit and
// close 3 tabs? 1 transfer will stop."; y cancels the download (its part
// file is removed) and closes both connections, once each.
func TestARCH_SFTP_007_QuitClosesAll(t *testing.T) {
	r := startSFTP(t, sshtest.WithSFTPZeroFile("big.bin", bigFile))
	r.waitText(0, "web-prod demo@127.0.0.1")
	r.openSession(r.srv, 1)
	r.openSFTPOn("big.bin")
	off := r.out.Len()
	r.prefix("h")
	r.waitText(off, "[Hosts]")
	r.startDownload()
	r.quitAll("Quit and close 3 tabs? 1 transfer will stop.")
	r.waitDisconnects(2)
	noParts(t, r.local)
	r.checkStty()
}
