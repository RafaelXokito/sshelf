//go:build unix

package main

import (
	"bytes"
	"crypto/sha256"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The hostile-server suite through the real binary (T-057, D-020): unsafe
// names, symlink tricks in the download folder, a huge listing and escape
// sequences in server text, each against sshtest's hostile SFTP mode
// (ARCH-TEST-004).

// startSFTPIn is startSFTP with the local pane in local.
func startSFTPIn(t *testing.T, local string, extra ...sshtest.Option) *sftpRun {
	t.Helper()
	opts := append([]sshtest.Option{sshtest.WithSFTP(t.TempDir())}, extra...)
	return &sftpRun{slice: startWith(t, sshelfBinary(t), 1, launch{opts: opts, dir: local}), local: local}
}

// waitFrameLine waits until a whole SFTP frame recorded in frames (the
// fault build's SSHELF_FAULT_FRAMES file, lines prefixed "F ") has a line
// that shows want, compared without whitespace.
func waitFrameLine(t *testing.T, frames, want string) {
	t.Helper()
	needle := whitespace.ReplaceAllString(want, "")
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, _ := os.ReadFile(frames) //nolint:gosec // a test temp file
		for l := range strings.SplitSeq(string(b), "\n") {
			if strings.HasPrefix(l, "F ") && strings.Contains(whitespace.ReplaceAllString(l, ""), needle) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no recorded frame shows %q; frames:\n%s", want, b)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sftpForbiddenIn is forbiddenIn (SEC-SESSION-005 AC1) for an SFTP tab's
// output: the session corpus' payload text is not involved.
func sftpForbiddenIn(b []byte) []string {
	var out []string
	for _, f := range forbiddenIn(b) {
		if !strings.HasPrefix(f, "hostile text:") {
			out = append(out, f)
		}
	}
	return out
}

// treeOf maps every path under root, relative to it, to its type and,
// for a regular file, its content; a symbolic link to its target.
func treeOf(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			out[rel] = "link " + target
			return err
		case d.IsDir():
			out[rel] = "dir"
		default:
			b, err := os.ReadFile(p) //nolint:gosec // a test temp dir
			out[rel] = "file " + string(b)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSEC_SFTP_001_E2EHostileDownload: the remote hostile directory lists
// every unsafe name of SEC-SFTP-001 AC1. The pane shows exactly ok.txt,
// evil and b with the warning; a recursive download of the directory
// creates only those three files, as direct children of the target, and a
// walk of the download folder's parent finds nothing else changed (AC2).
// The PTY output holds none of the hostile names' control bytes.
func TestSEC_SFTP_001_E2EHostileDownload(t *testing.T) {
	parent := t.TempDir()
	local := filepath.Join(parent, "downloads")
	if err := os.Mkdir(local, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "sibling.txt"), []byte("sibling"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := treeOf(t, parent)
	r := startSFTPIn(t, local, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Names: true}))
	start := r.waitText(0, "web-prod demo@127.0.0.1") // past sshelf's own terminal setup
	off := r.openSFTPHost("hostile")
	r.typeKeys("jc") // .., hostile: download the directory
	want := filepath.Join(local, "hostile")
	for _, name := range []string{"ok.txt", "evil", "b"} {
		r.waitFile(filepath.Join(want, name), sha256.Sum256([]byte(sshtest.HostileContent)), 10*time.Second)
	}
	r.typeKeys("\r") // into hostile (the cursor is still on it)
	r.waitText(off, "⚠ 8 entries with unsafe names not shown")
	time.Sleep(200 * time.Millisecond) // any late write would show up
	after := treeOf(t, parent)
	for _, p := range []string{"downloads/hostile", "downloads/hostile/ok.txt", "downloads/hostile/evil", "downloads/hostile/b"} {
		if _, ok := after[p]; !ok {
			t.Errorf("%s missing", p)
		}
		delete(after, p)
	}
	if !maps.Equal(before, after) {
		t.Errorf("the download folder's parent changed:\nbefore %q\nafter  %q", before, after)
	}
	window := r.out.From(start)
	for _, f := range sftpForbiddenIn(window) {
		t.Error("sshelf wrote " + f)
	}
	for _, name := range sshtest.HostileNames {
		if strings.ContainsFunc(name, unicode.IsControl) && bytes.Contains(window, []byte(name)) {
			t.Errorf("the terminal output holds the name %q", name)
		}
	}
	r.quitAll("Quit and close 1 tab?")
}

// TestSEC_SFTP_002_E2EPlantedSymlink: the download folder holds links
// planted to elsewhere: notes.txt to a file, photos to a directory and
// big.iso.sshelf-part to a file. Downloading photos/ fails with "exists
// and is not a directory", big.iso fails with "unsafe part file", and
// notes.txt, answered Overwrite, replaces the link with a regular file.
// Nothing outside the download folder changes (AC1 to AC3).
func TestSEC_SFTP_002_E2EPlantedSymlink(t *testing.T) {
	local, outside := t.TempDir(), t.TempDir()
	for name, content := range map[string]string{"secret.txt": "secret", "part-target": "unchanged"} {
		if err := os.WriteFile(filepath.Join(outside, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(outside, "album"), 0o700); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"notes.txt": "secret.txt", "photos": "album", "big.iso.sshelf-part": "part-target",
	} {
		if err := os.Symlink(filepath.Join(outside, target), filepath.Join(local, link)); err != nil {
			t.Fatal(err)
		}
	}
	before := treeOf(t, outside)
	r := startSFTPIn(t, local)
	if err := os.Mkdir(r.remote("photos"), 0o755); err != nil { //nolint:gosec // a served test dir
		t.Fatal(err)
	}
	for name, content := range map[string]string{"notes.txt": "remote notes", "photos/pic.jpg": "jpeg", "big.iso": "iso"} {
		if err := os.WriteFile(r.remote(name), []byte(content), 0o644); err != nil { //nolint:gosec // served test files
			t.Fatal(err)
		}
	}
	off := r.openSFTPHost("notes.txt") // .., photos, big.iso, notes.txt
	r.typeKeys("jcjcjc")               // photos, big.iso, notes.txt
	// The transfers view lists photos, big.iso and notes.txt, oldest
	// first, with the reasons cut to fit 80 columns. Each state is checked
	// on its own: a row redrawn in place may not repeat its name.
	r.typeKeys("t")
	r.waitText(off, "failed: exist") // photos: exists and is not a directory
	r.waitText(off, "failed: unsaf") // big.iso: unsafe part file
	off = r.waitText(off, "waiting") // notes.txt
	r.typeKeys("jj\r")               // answer notes.txt
	r.waitText(off, "notes.txt already exists")
	r.typeKeys("o")
	r.waitFile(filepath.Join(local, "notes.txt"), sha256.Sum256([]byte("remote notes")), 10*time.Second)

	if fi, err := os.Lstat(filepath.Join(local, "notes.txt")); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("notes.txt is not a regular file: %v", err)
	}
	if fi, err := os.Lstat(filepath.Join(local, "big.iso.sshelf-part")); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the planted part link was not left alone: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(local, "big.iso")); !os.IsNotExist(err) {
		t.Errorf("big.iso was written: %v", err)
	}
	if after := treeOf(t, outside); !maps.Equal(before, after) {
		t.Errorf("outside the download folder changed:\nbefore %q\nafter  %q", before, after)
	}
	r.quitAll("Quit and close 1 tab?")
}

// liveHeap has sshelf, started with SSHELF_FAULT_HEAP=file (heap_fault.go),
// collect garbage and report its live heap in bytes.
func (s *slice) liveHeap(file string) uint64 {
	s.t.Helper()
	_ = os.Remove(file)
	if err := s.cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		s.t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if b, err := os.ReadFile(file); err == nil { //nolint:gosec // a test temp file
			n, err := strconv.ParseUint(string(b), 10, 64)
			if err != nil {
				s.t.Fatal(err)
			}
			return n
		}
		if time.Now().After(deadline) {
			s.t.Fatal("sshelf did not report its heap")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSEC_SFTP_004_E2EHugeListingHeap: a remote directory of 150,000
// entries is listed up to its 100,000th entry with "⚠ listing stopped at
// 100,000 entries", and sshelf's live heap with that listing open is at
// most NFR-SIZE-004's 16 MiB above the idle SFTP tab's (AC1).
func TestSEC_SFTP_004_E2EHugeListingHeap(t *testing.T) {
	heap := filepath.Join(t.TempDir(), "heap")
	r := &sftpRun{slice: startWith(t, sshelfBinary(t), 1, launch{
		env:  []string{"SSHELF_FAULT_HEAP=" + heap},
		opts: []sshtest.Option{sshtest.WithSFTP(t.TempDir()), sshtest.WithSFTPHostile(sshtest.SFTPHostile{Entries: 150_000})},
		dir:  t.TempDir(),
	})}
	off := r.openSFTPHost("many")
	off = r.waitText(off, "10k+ items") // the item count of FR-SFTP-006 is done
	idle := r.liveHeap(heap)
	r.typeKeys("j\r")
	off = r.waitText(off, "⚠ listing stopped at 100,000 entries")
	r.typeKeys("G") // the last row is the 100,000th name
	r.waitText(off, "> f0099999")
	if bytes.Contains(r.out.From(0), []byte("f0100000")) {
		t.Error("an entry past the 100,000th is shown")
	}
	listed := r.liveHeap(heap)
	t.Logf("live heap: idle SFTP tab %d KiB, with the listing %d KiB", idle>>10, listed>>10)
	if listed > idle+16<<20 {
		t.Errorf("the live heap grew by %d KiB with the 100,000-entry listing, want at most 16 MiB", (listed-idle)>>10)
	}
	r.quitAll("Quit and close 1 tab?")
}

// TestSEC_SFTP_004_E2EDeepChainAndOversize: a recursive download of a
// directory chain 70 levels deep stops at level 64 and skips the deeper
// entry with a reason (AC2), and a file that keeps sending data past its
// reported size fails and leaves nothing behind (AC3).
func TestSEC_SFTP_004_E2EDeepChainAndOversize(t *testing.T) {
	local := t.TempDir()
	r := startSFTPIn(t, local, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Depth: 70, Oversize: 1 << 20}))
	off := r.openSFTPHost("oversize.bin")              // .., deep, oversize.bin
	r.typeKeys("jjckc")                                // oversize.bin, then deep
	off = r.waitText(off, "copied 0 files, skipped 1") // deep/d/…: too many nested directories
	r.typeKeys("t")                                    // oversize.bin's row is the first
	r.waitText(off, "failed: size")                    // size changed during transfer
	levels := 0
	for p := filepath.Join(local, "deep"); ; p = filepath.Join(p, "d") {
		if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
			break
		}
		levels++
	}
	if levels != 64 {
		t.Errorf("%d levels copied, want 64", levels)
	}
	if _, err := os.Lstat(filepath.Join(local, "oversize.bin")); !os.IsNotExist(err) {
		t.Errorf("oversize.bin: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(local, "oversize.bin.sshelf-part")); !os.IsNotExist(err) {
		t.Errorf("oversize.bin's part file: %v", err)
	}
	r.quitAll("Quit and close 1 tab?")
}

// TestSEC_SFTP_008_PTYScan: the server answers realpath(".") with an OSC
// title and a screen clear in the path, and fails the open of denied with
// HostileStatus (an OSC 52 clipboard write, an OSC 0 title, a CSI, a
// newline and 200 extra columns). The pane title shows the path sanitised
// (AC2), the failed download's reason is shown sanitised in the transfers
// view (its folding and cut are TestSEC_SFTP_008_StatusMessageSanitised's),
// and sshelf's PTY output over the whole run holds no forbidden sequence
// (AC1).
func TestSEC_SFTP_008_PTYScan(t *testing.T) {
	frames, local := filepath.Join(t.TempDir(), "frames.txt"), t.TempDir()
	r := &sftpRun{slice: startWith(t, sshelfBinary(t), 1, launch{
		env: []string{"SSHELF_FAULT_FRAMES=" + frames},
		opts: []sshtest.Option{sshtest.WithSFTP(t.TempDir()), sshtest.WithSFTPHostile(sshtest.SFTPHostile{
			Status: sshtest.HostileStatus, Realpath: sshtest.SFTPHome + "/\x1b]0;title\x07\x1b[2Jhome",
		})},
		dir: local,
	}), local: local}
	start := r.waitText(0, "web-prod demo@127.0.0.1")
	// The exact path does not exist in sshtest: the listing fails, and h
	// goes to its parent.
	off := r.openSFTPHost("cannot open /home/pi/home: no such file or directory")
	// The pane title shows the realpath, sanitised. It is read from the
	// recorded frames, not the PTY output: the title is drawn as "Remote:"
	// while the realpath is pending, and the renderer then writes only the
	// path, so "Remote: /home/pi/home" need not be in the output in one
	// piece (this flaked under load).
	waitFrameLine(t, frames, "Remote: /home/pi/home")
	r.typeKeys("h")
	off = r.waitText(off, "denied")
	r.typeKeys("jc")
	r.waitText(off, "1 failed")
	r.typeKeys("t")
	r.waitText(off, "failed: permi")   // "permission denied second line xxx…", cut to the column
	time.Sleep(100 * time.Millisecond) // the last frame
	window := r.out.From(start)
	for _, f := range sftpForbiddenIn(window) {
		t.Error("sshelf wrote " + f)
	}
	for _, bad := range []string{"pwned", "title", "cHduZWQ"} {
		if bytes.Contains(window, []byte(bad)) {
			t.Errorf("the terminal output holds %q", bad)
		}
	}
	r.quitAll("Quit and close 1 tab?")
}
