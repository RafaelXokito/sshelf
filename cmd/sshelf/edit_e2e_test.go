//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Edit locally end to end (T-055: FR-SFTP-019, FR-SFTP-020, SEC-SFTP-005,
// ARCH-SFTP-006). sshelf runs in a PTY with EDITOR pointing at a copy of
// testdata/fake-editor.sh, which never opens a real editor, and TMPDIR at
// a temp dir, so that the test sees every sshelf-edit-* directory.

// editRun is an SFTP slice with the fake editor.
type editRun struct {
	*sftpRun
	ctl    string // FAKE_EDITOR_DIR: the editor, its mode, log and args
	tmp    string // sshelf's TMPDIR
	editor string // the editor's path (EDITOR)
}

// startEdit starts sshelf with the fake editor and opens an SFTP tab from
// the host list once the remote file notes.txt holds "v1".
func startEdit(t *testing.T, env []string, opts ...sshtest.Option) *editRun {
	t.Helper()
	ctl, tmp, local := t.TempDir(), t.TempDir(), t.TempDir()
	script, err := os.ReadFile(filepath.Join("testdata", "fake-editor.sh"))
	if err != nil {
		t.Fatal(err)
	}
	ed := filepath.Join(ctl, "editor")
	if err := os.WriteFile(ed, script, 0o700); err != nil { //nolint:gosec // the fake editor must be executable
		t.Fatal(err)
	}
	env = append([]string{"EDITOR=" + ed, "VISUAL=", "FAKE_EDITOR_DIR=" + ctl, "TMPDIR=" + tmp}, env...)
	opts = append([]sshtest.Option{sshtest.WithSFTP(t.TempDir())}, opts...)
	r := &sftpRun{slice: startWith(t, sshelfBinary(t), 1, launch{env: env, opts: opts, dir: local}), local: local}
	if err := os.WriteFile(r.remote("notes.txt"), []byte("v1\n"), 0o640); err != nil { //nolint:gosec // a mode the upload must keep
		t.Fatal(err)
	}
	return &editRun{sftpRun: r, ctl: ctl, tmp: tmp, editor: ed}
}

// mode sets what the fake editor does next.
func (e *editRun) mode(m string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.ctl, "mode"), []byte(m), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// ctlFile reads a file of the fake editor's dir ("" when missing).
func (e *editRun) ctlFile(name string) string {
	b, _ := os.ReadFile(filepath.Join(e.ctl, name)) //nolint:gosec // the test's temp dir
	return string(b)
}

// edit presses e and waits for want on the screen.
func (e *editRun) edit(want string) {
	e.t.Helper()
	off := e.out.Len()
	e.typeKeys("e")
	e.waitText(off, want)
}

// noEditDirs fails if an sshelf-edit-* directory is left in TMPDIR.
func (e *editRun) noEditDirs(when string) {
	e.t.Helper()
	m, err := filepath.Glob(filepath.Join(e.tmp, "sshelf-edit-*"))
	if err != nil {
		e.t.Fatal(err)
	}
	if len(m) > 0 {
		e.t.Fatalf("%s: %q left behind", when, m)
	}
}

// remoteText is the remote file's content.
func (e *editRun) remoteText(name string) string {
	b, err := os.ReadFile(e.remote(name))
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(b)
}

// TestFR_SFTP_019_E2EFakeEditorAppends: e on a remote file runs the fake
// editor on a private copy, which appends a line; the remote file holds
// it, keeps its mode, and no temp file remains.
func TestFR_SFTP_019_E2EFakeEditorAppends(t *testing.T) {
	e := startEdit(t, nil)
	e.openSFTPHost("notes.txt")
	e.typeKeys("j") // notes.txt
	e.edit("saved notes.txt (")
	if got := e.remoteText("notes.txt"); got != "v1\nedited by the fake editor\n" {
		t.Fatalf("remote file = %q", got)
	}
	if fi, err := os.Stat(e.remote("notes.txt")); err != nil || fi.Mode().Perm() != 0o640 {
		t.Fatalf("remote mode %v, %v", fi.Mode(), err)
	}
	args := strings.Split(strings.TrimSpace(e.ctlFile("args")), "\n")
	if len(args) != 1 || filepath.Base(args[0]) != "notes.txt" || !strings.HasPrefix(filepath.Base(filepath.Dir(args[0])), "sshelf-edit-") {
		t.Fatalf("the editor got %q", args)
	}
	e.noEditDirs("after the edit")
	e.quitAll("Quit and close 1 tab?")
	e.checkStty()
}

// TestSEC_SFTP_005_E2ERemovedOnEveryPath is SEC-SFTP-005 AC2 through the
// binary: after each ending the copy's directory is gone. The first
// upload fails (sshtest), the last one loses the connection.
func TestSEC_SFTP_005_E2ERemovedOnEveryPath(t *testing.T) {
	part := "notes.txt.sshelf-part"
	e := startEdit(t, nil, sshtest.WithSFTPFault(part, 0, sshtest.FaultFail), sshtest.WithSFTPFault("other.txt.sshelf-part", 0, sshtest.FaultDrop))
	if err := os.WriteFile(filepath.Join(e.ctl, "remote"), []byte(e.remote("notes.txt")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.remote("other.txt"), []byte("o1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.openSFTPHost("notes.txt")
	e.typeKeys("j")

	e.mode("append") // the upload fails: the question, then x
	e.edit("Upload of notes.txt failed")
	e.typeKeys("x")
	e.waitText(0, "notes.txt: edit discarded")
	e.noEditDirs("upload failed, discarded")

	e.mode("same")
	e.edit("notes.txt: no changes")
	e.noEditDirs("unchanged")

	e.mode("fail")
	e.edit("editor exited with status 1; nothing uploaded")
	e.noEditDirs("editor exit status 1")

	// The progress line said "saving notes.txt…" before "saved …": the
	// renderer may skip the unchanged "sav", so the checks start after it.
	for _, tc := range []struct{ key, want, remote string }{
		{"k", "notes.txt: edit discarded", "v1\nchanged on the server\n"},
		{"s", "ed notes.txt.local-edit (", "v1\nchanged on the server\nchanged on the server\n"},
		{"o", "ed notes.txt (", "v1\nchanged on the server\nchanged on the server\nedited by the fake editor\n"},
	} {
		e.mode("remote")
		e.edit("notes.txt changed on the server while you edited it.")
		off := e.out.Len()
		e.typeKeys(tc.key)
		e.waitText(off, tc.want)
		e.noEditDirs("remote changed, " + tc.key)
		if got := e.remoteText("notes.txt"); got != tc.remote {
			t.Fatalf("%s: remote file %q, want %q", tc.key, got, tc.remote)
		}
	}
	if got := e.remoteText("notes.txt.local-edit"); !strings.HasSuffix(got, "edited by the fake editor\n") {
		t.Fatalf("notes.txt.local-edit = %q", got)
	}

	e.mode("append")
	if err := os.Rename(e.editor, e.editor+".away"); err != nil {
		t.Fatal(err)
	}
	e.edit("editor not found:")
	e.noEditDirs("editor not found")
	if err := os.Rename(e.editor+".away", e.editor); err != nil {
		t.Fatal(err)
	}

	// The connection drops during the upload of other.txt (the last row):
	// the question, then x.
	e.typeKeys("G")
	off := e.out.Len()
	e.typeKeys("e")
	e.waitText(off, "Upload of other.txt failed")
	e.typeKeys("x")
	e.waitText(off, "other.txt: edit discarded")
	e.noEditDirs("connection lost during upload")
	if got := e.remoteText("other.txt"); got != "o1\n" {
		t.Fatalf("other.txt = %q", got)
	}
	e.prefix("h")
	e.quit() // the tab is disconnected with nothing paused: q quits at once
}

// waitLog waits until the fake editor's log has n lines starting with
// word.
func waitLog(t *testing.T, e *editRun, word string, n int) {
	t.Helper()
	count := func() int { return strings.Count("\n"+e.ctlFile("log"), "\n"+word) }
	deadline := time.Now().Add(10 * time.Second)
	for count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("the fake editor logged %q %d times, want %d: %q", word, count(), n, e.ctlFile("log"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSEC_SFTP_005_E2ECrashRemovesCopy: a panic forced in Update after the
// download (sshelf_faultinject) leaves no copy behind, and no editor ran.
func TestSEC_SFTP_005_E2ECrashRemovesCopy(t *testing.T) {
	e := startEdit(t, []string{"SSHELF_FAULT=edit-fetched"})
	e.openSFTPHost("notes.txt")
	e.typeKeys("j")
	e.typeKeys("e")
	select {
	case <-e.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("sshelf did not crash; output:\n%q", e.out.From(0))
	}
	var ee *exec.ExitError
	if !errors.As(e.exitErr, &ee) || ee.ExitCode() != exitCrash {
		t.Fatalf("sshelf exited with %v, want status %d", e.exitErr, exitCrash)
	}
	e.noEditDirs("crash")
	if log := e.ctlFile("log"); log != "" {
		t.Fatalf("the editor ran: %q", log)
	}
	e.checkStty()
}

// TestARCH_SFTP_006_SessionsKeepReading: while the fake editor holds the
// terminal for 3 s, a session in another tab keeps reading a 2 MB/s
// stream (more than its 2 MiB SSH window, which a session that stopped
// reading would leave full), and a 64 MiB download keeps running.
func TestARCH_SFTP_006_SessionsKeepReading(t *testing.T) {
	e := startEdit(t, nil)
	sum := writeRandom(t, e.remote("big.bin"), 64<<20)
	e.waitText(0, "web-prod demo@127.0.0.1")
	e.openSession(e.srv, 1) // tab 1, channel 1 of connection 1
	e.typeKeys("stream 2M\r")
	if !e.srv.WaitFor(5*time.Second, func() bool { return e.srv.Sent(1, 1) > 1<<20 }) {
		t.Fatal("the stream did not start")
	}
	e.openSFTPOn("notes.txt") // tab 2 on the same connection
	e.typeKeys("jc")          // download big.bin
	e.typeKeys("j")           // notes.txt
	e.mode("wait")
	if err := os.WriteFile(filepath.Join(e.ctl, "wait"), []byte("3"), 0o600); err != nil {
		t.Fatal(err)
	}
	off := e.out.Len()
	e.typeKeys("e")
	waitLog(t, e, "start", 1)
	sent, read := e.srv.Sent(1, 1), e.srv.SFTPBytesRead("big.bin")
	waitLog(t, e, "end", 1)
	sentDuring, readDuring := e.srv.Sent(1, 1)-sent, e.srv.SFTPBytesRead("big.bin")-read
	if sentDuring < 3<<20 {
		t.Fatalf("the session took %d bytes while the editor ran, want more than its 2 MiB window", sentDuring)
	}
	if readDuring == 0 && read < 64<<20 {
		t.Fatalf("the download made no progress while the editor ran (%d bytes before)", read)
	}
	e.waitText(off, "saved notes.txt (")
	e.waitFile(filepath.Join(e.local, "big.bin"), sum, 60*time.Second)
	e.prefix("1")
	e.typeKeys("x") // stops the stream
	e.typeKeys("echo after-edit\r")
	e.waitText(off, "after-edit")
	e.noEditDirs("after the edit")
	e.quitAll("Quit and close 2 tabs?")
}

// TestARCH_SFTP_006_ResizeAfterEditor: the terminal is resized while the
// editor holds it; when the editor exits, the session in the other tab
// gets the new size as one window-change, and the frame is redrawn.
func TestARCH_SFTP_006_ResizeAfterEditor(t *testing.T) {
	e := startEdit(t, nil)
	e.waitText(0, "web-prod demo@127.0.0.1")
	e.openSession(e.srv, 1)
	e.openSFTPOn("notes.txt")
	e.typeKeys("j")
	e.mode("wait")
	if err := os.WriteFile(filepath.Join(e.ctl, "wait"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(e.srv.RequestsOfType("window-change"))
	off := e.out.Len()
	e.typeKeys("e")
	waitLog(t, e, "start", 1)
	if err := pty.Setsize(e.master, &pty.Winsize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	// sshelf has no controlling terminal (Setsid): send what the terminal
	// would.
	if err := e.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	e.waitText(off, "saved notes.txt (")
	resized := func() bool {
		for _, r := range e.srv.RequestsOfType("window-change")[before:] {
			if r.Width == 100 && r.Height == 28 {
				return true
			}
		}
		return false
	}
	if !e.srv.WaitFor(5*time.Second, resized) {
		t.Fatalf("window-changes %+v, want 100×28", e.srv.RequestsOfType("window-change")[before:])
	}
	time.Sleep(300 * time.Millisecond) // a second one would come within the debounce
	if got := e.srv.RequestsOfType("window-change")[before:]; len(got) != 1 {
		t.Fatalf("window-changes %+v, want exactly one", got)
	}
	e.prefix("1")
	off = e.out.Len()
	e.typeKeys("size\r")
	e.waitText(off, "100x28")
	e.noEditDirs("after the edit")
	e.quitAll("Quit and close 2 tabs?")
}
