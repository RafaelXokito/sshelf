//go:build unix

package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The canary harness extended to the SFTP tab (T-057, SEC-SFTP-007): the
// M3 flows run in the fault-injection build, which records every frame of
// a focused SFTP tab whole (frames_fault.go), and every sink is scanned as
// for T-028 and T-042. The local pane is the run's working directory and
// the remote one sshtest's served directory; both hold the content
// canaries on purpose and are not scanned.

// sftpCanaryHost serves SFTP from a new directory for alice with
// password pw, and the extra options.
func sftpCanaryHost(t *testing.T, pw string, extra ...sshtest.Option) *sshtest.Server {
	t.Helper()
	return sshtest.New(t, append([]sshtest.Option{sshtest.WithPassword("alice", pw), sshtest.WithSFTP(t.TempDir())}, extra...)...)
}

// fakeEditor copies T-055's fake editor (testdata/fake-editor.sh) into a
// new directory, its FAKE_EDITOR_DIR, as "editor", and returns that
// directory.
func fakeEditor(t *testing.T) string {
	t.Helper()
	ctl := t.TempDir()
	script, err := os.ReadFile(filepath.Join("testdata", "fake-editor.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctl, "editor"), script, 0o700); err != nil { //nolint:gosec // the fake editor must be executable
		t.Fatal(err)
	}
	return ctl
}

// openSFTP presses s on the selected host and waits for its SFTP tab to
// be focused and its remote pane to list want; it returns the output
// offset after it.
func (c *canaryRun) openSFTP(label, want string) int {
	c.t.Helper()
	off := c.out.Len()
	c.typeKeys("s")
	c.tabs++
	c.waitText(off, fmt.Sprintf("[%d sftp: %s]", c.tabs, label))
	return c.waitText(off, want)
}

// waitFile waits until path holds want.
func (c *canaryRun) waitFile(path string, want []byte) {
	c.t.Helper()
	sum := sha256.Sum256(want)
	deadline := time.Now().Add(30 * time.Second)
	for {
		if b, err := os.ReadFile(path); err == nil && sha256.Sum256(b) == sum { //nolint:gosec // a test temp dir
			return
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("%s never held the expected content; output:\n%q", path, c.out.From(max(c.out.Len()-4000, 0)))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// sftpFrames returns the recorded lines of the frames drawn while an SFTP
// tab was focused.
func (c *canaryRun) sftpFrames() []string {
	c.t.Helper()
	b, err := os.ReadFile(c.frames)
	if err != nil {
		c.t.Fatal(err)
	}
	var out []string
	for l := range strings.SplitSeq(string(b), "\n") {
		if rest, ok := strings.CutPrefix(l, "F "); ok {
			out = append(out, rest)
		}
	}
	return out
}

// wantFrames fails unless some recorded SFTP frame line holds each of
// want, so that a clean scan covered the SFTP tab.
func (c *canaryRun) wantFrames(want ...string) {
	c.t.Helper()
	frames := strings.Join(c.sftpFrames(), "\n")
	for _, w := range want {
		if !strings.Contains(frames, w) {
			c.t.Errorf("no recorded SFTP frame shows %q", w)
		}
	}
}

// quitSFTP goes to the Hosts tab and quits, answering the question about
// the n open tabs.
func (c *canaryRun) quitSFTP(n int) {
	c.t.Helper()
	c.prefix("h")
	off := c.out.Len()
	c.typeKeys("q")
	c.waitText(off, fmt.Sprintf("Quit and close %d tab", n))
	c.typeKeys("y")
	c.waitQuit()
}

// TestSEC_SFTP_007_CanaryHarnessM3 runs the M3 flows of SEC-SFTP-007 AC1
// with SSHELF_DEBUG=1 on a host whose password is a canary: an SFTP tab
// opened from the host list, a download cancelled, the download and the
// upload of files whose content is a canary, and a download that the
// connection drops halfway and that resumes after a reconnect. The
// content and password canaries appear in no log record, crash report,
// frame, stdout or stderr, and each transfer record carries the path,
// direction, size and outcome only (AC2). Edit-locally runs the fake
// editor of T-055 on a file whose content is a canary, once unchanged on
// the server and once changed there meanwhile (answered Overwrite); the
// private copy lives in the scanned TMPDIR and must be gone.
func TestSEC_SFTP_007_CanaryHarnessM3(t *testing.T) {
	const (
		pw   = "CANARY-sftp-pw-7d0e93"          //nolint:gosec // made-up test secrets
		up   = "CANARY-upload-content-41c8a2"   //nolint:gosec
		down = "CANARY-download-content-9b6f07" //nolint:gosec
		edit = "CANARY-edit-content-5e21bd"     //nolint:gosec
		size = 8 << 20
	)
	cs := canaries{{"password", pw}, {"uploaded content", up}, {"downloaded content", down}, {"edited content", edit}}
	srv := sftpCanaryHost(t, pw, sshtest.WithSFTPZeroFile("big.bin", 4<<30),
		sshtest.WithSFTPZeroFile("zero.bin", size), sshtest.WithSFTPFault("zero.bin", 2<<20, sshtest.FaultDrop))
	for name, content := range map[string]string{"down.txt": down, "edit.txt": edit} {
		if err := os.WriteFile(srv.SFTPDir(name), []byte(content+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctl := fakeEditor(t)
	c := startCanary(t, cs, func(_, cfgDir string) {
		writeKnownHosts(t, cfgDir, srv.KnownHostsLine())
	}, "SSHELF_DEBUG=1", "EDITOR="+filepath.Join(ctl, "editor"), "VISUAL=", "FAKE_EDITOR_DIR="+ctl)
	if err := os.WriteFile(filepath.Join(c.work, "up.txt"), []byte(up+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.waitText(0, "Add host")
	c.addHost(hostForm{label: "files", srv: srv, user: "alice", password: pw})
	off := c.openSFTP("files", "zero.bin") // .., big.bin, down.txt, edit.txt, zero.bin

	// Edit locally: unchanged on the server, then changed there.
	c.typeKeys("jjje")
	off = c.waitText(off, "saved edit.txt (")
	if err := os.WriteFile(filepath.Join(ctl, "remote"), []byte(srv.SFTPDir("edit.txt")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctl, "mode"), []byte("remote"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.typeKeys("e")
	off = c.waitText(off, "edit.txt changed on the server while you edited it.")
	c.typeKeys("o")
	off = c.waitText(off, "ed edit.txt (")                                                                                                             // "saved", after "saving"
	if b, _ := os.ReadFile(srv.SFTPDir("edit.txt")); !bytes.HasPrefix(b, []byte(edit)) || !bytes.HasSuffix(b, []byte("edited by the fake editor\n")) { //nolint:gosec // a test temp dir
		t.Errorf("edit.txt = %q", b)
	}
	if m, _ := filepath.Glob(filepath.Join(c.tmp, "sshelf-edit-*")); len(m) > 0 {
		t.Errorf("edit copies left behind: %q", m)
	}

	// Cancel a download in the transfers view: its row is the first.
	c.typeKeys("gjc")
	off = c.waitText(off, "↓ big.bin")
	c.typeKeys("t")
	off = c.waitText(off, "Transfers (1)")
	c.typeKeys("x")
	off = c.waitText(off, "cancelled")
	c.typeKeys("t")

	c.typeKeys("jc") // down.txt
	c.waitFile(filepath.Join(c.work, "down.txt"), []byte(down+"\n"))
	c.typeKeys("\tGc") // the local pane's last row, up.txt
	c.waitFile(srv.SFTPDir("up.txt"), []byte(up+"\n"))

	// The connection drops after 2 MiB of zero.bin; r reconnects with the
	// stored password and the download resumes.
	c.typeKeys("\tGc") // the remote pane's last row, zero.bin
	// The progress line may still show the upload's result: the footer
	// says the tab is disconnected.
	c.waitText(off, "r reconnect · x close · t transfers")
	c.typeKeys("r")
	c.waitFile(filepath.Join(c.work, "zero.bin"), make([]byte, size))
	if n := len(srv.ClientVersions()); n != 2 {
		t.Errorf("%d connections, want 2", n)
	}

	c.quitSFTP(1)
	c.check()
	c.wantFrames("Remote: /home/pi", "down.txt", "up.txt")
	c.wantLog(`msg="sftp: open" host=files`, "direction=download", "direction=upload",
		"outcome=cancelled", "outcome=paused", "outcome=done", "path=/home/pi/zero.bin")
	for l := range strings.SplitSeq(string(c.log()), "\n") {
		if !strings.Contains(l, `msg="sftp: transfer"`) {
			continue
		}
		var keys []string
		for _, f := range strings.Fields(l) {
			if k, _, ok := strings.Cut(f, "="); ok && !strings.ContainsAny(k, `"`) {
				keys = append(keys, k)
			}
		}
		if want := []string{"time", "level", "msg", "path", "direction", "size", "outcome"}; !slices.Equal(keys, want) {
			t.Errorf("transfer record with keys %v, want %v: %s", keys, want, l)
		}
	}
	if ents, _ := os.ReadDir(c.work); len(ents) != 3 { // up.txt, down.txt, zero.bin: no part file
		t.Errorf("the local pane's directory holds %d entries", len(ents))
	}
}

// TestSEC_SFTP_007_NoTransferRecordsWhenLoggingOff: with logging off
// (the default), an upload and a download produce no record at all: no
// state directory is created (AC2).
func TestSEC_SFTP_007_NoTransferRecordsWhenLoggingOff(t *testing.T) {
	const pw = "CANARY-nolog-pw-2a5c19" //nolint:gosec // a made-up test secret
	srv := sftpCanaryHost(t, pw)
	if err := os.WriteFile(srv.SFTPDir("down.txt"), []byte("down\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startCanary(t, canaries{{"password", pw}}, func(_, cfgDir string) {
		writeKnownHosts(t, cfgDir, srv.KnownHostsLine())
	})
	if err := os.WriteFile(filepath.Join(c.work, "up.txt"), []byte("up\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.waitText(0, "Add host")
	c.addHost(hostForm{label: "files", srv: srv, user: "alice", password: pw})
	c.openSFTP("files", "down.txt")
	c.typeKeys("jc")
	c.waitFile(filepath.Join(c.work, "down.txt"), []byte("down\n"))
	c.typeKeys("\tGc")
	c.waitFile(srv.SFTPDir("up.txt"), []byte("up\n"))
	c.quitSFTP(1)
	c.check()
	if _, err := os.Stat(c.state); !os.IsNotExist(err) {
		t.Errorf("state dir created without SSHELF_DEBUG: %v", err)
	}
}

// TestSEC_SFTP_007_PasswordCanaryNotInFrames: the password typed at the
// connect prompt to open an SFTP tab never appears in the tab's frames
// (pane titles, rows, progress line, the transfers view, the conflict and
// mkdir modals, the help), as for the session tab (AC3, FR-TAB-007).
func TestSEC_SFTP_007_PasswordCanaryNotInFrames(t *testing.T) {
	const pw = "CANARY-frames-pw-e8a374" //nolint:gosec // a made-up test secret
	srv := sftpCanaryHost(t, pw)
	if err := os.WriteFile(srv.SFTPDir("same.txt"), []byte("remote\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startCanary(t, canaries{{"password", pw}}, func(_, cfgDir string) {
		writeKnownHosts(t, cfgDir, srv.KnownHostsLine())
	})
	if err := os.WriteFile(filepath.Join(c.work, "same.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.waitText(0, "Add host")
	c.addHost(hostForm{label: "files", srv: srv, user: "alice"}) // no stored password
	off := c.out.Len()
	c.typeKeys("s")
	off = c.waitText(off, "Password for alice@127.0.0.1")
	c.typeKeys(pw + keyEnter)
	c.tabs++
	c.waitText(off, "[1 sftp: files]")
	off = c.waitText(off, "same.txt")

	c.typeKeys("jc") // same.txt exists locally: a question
	off = c.waitText(off, "same.txt exists · t to answer")
	c.typeKeys("t")
	off = c.waitText(off, "waiting")
	c.typeKeys(keyEnter)
	off = c.waitText(off, "same.txt already exists")
	c.typeKeys("n") // keep both
	c.waitFile(filepath.Join(c.work, "same (1).txt"), []byte("remote\n"))
	c.typeKeys("t") // back to the panes
	c.typeKeys("m")
	off = c.waitText(off, "New directory")
	c.typeKeys(keyEsc) // a key sent right after Esc would read as alt+key: wait for the panes' footer
	off = c.waitText(off, "tab switch pane")
	c.typeKeys("?")
	off = c.waitText(off, "SFTP tab keys")
	c.typeKeys(keyEsc)
	c.waitText(off, "tab switch pane")

	c.quitSFTP(1)
	c.check()
	c.wantFrames("Remote: /home/pi", "already exists", "New directory", "SFTP tab keys", "Transfers (1)")
	frames := []byte(strings.Join(c.sftpFrames(), "\n"))
	if bytes.Contains(frames, []byte(pw)) {
		t.Error("an SFTP frame holds the password")
	}
}
