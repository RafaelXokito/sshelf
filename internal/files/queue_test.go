package files

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// The transfer queue against sshtest's sftp subsystem (ARCH-TEST-004).

const mib = 1 << 20

// setHook sets a test hook for the rest of the test. Call it before
// openBrowser, so the hook is restored only after the browser closed and
// no job reads it any more.
func setHook[T any](t *testing.T, hook *T, v T) {
	t.Helper()
	old := *hook
	*hook = v
	t.Cleanup(func() { *hook = old })
}

func openBrowser(t *testing.T, srv *sshtest.Server) *Browser {
	t.Helper()
	b, err := Open(dialLease(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// waitFor waits until cond holds for the transfers snapshot.
func waitFor(t *testing.T, b *Browser, timeout time.Duration, cond func([]Transfer) bool) []Transfer {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ts := b.Transfers()
		if cond(ts) {
			return ts
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out; transfers:\n%s", dump(ts))
		}
		select {
		case <-b.Updates():
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// settled holds when no transfer is queued or running.
func settled(ts []Transfer) bool {
	return !slices.ContainsFunc(ts, func(x Transfer) bool { return x.State == Queued || x.State == Running })
}

// waitSettled waits for the queue to settle and fails on any failed
// transfer.
func waitSettled(t *testing.T, b *Browser) []Transfer {
	t.Helper()
	ts := waitFor(t, b, 60*time.Second, settled)
	for _, x := range ts {
		if x.State == Failed {
			t.Fatalf("transfer failed:\n%s", dump(ts))
		}
	}
	return ts
}

func dump(ts []Transfer) string {
	var sb strings.Builder
	for _, x := range ts {
		sb.WriteString(strconv.Quote(x.Name) + " " + x.State.String() + " " + x.Reason + " " +
			strconv.FormatInt(x.Done, 10) + "/" + strconv.FormatInt(x.Total, 10) + "\n")
	}
	return sb.String()
}

func byName(ts []Transfer, name string) Transfer {
	for _, x := range ts {
		if x.Name == name {
			return x
		}
	}
	return Transfer{}
}

// randFile writes size pseudo-random bytes to p and returns their SHA-256.
func randFile(t *testing.T, p string, size int64, seed byte) [32]byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // checked by the write
	rng := rand.NewChaCha8([32]byte{seed})
	h := sha256.New()
	buf := make([]byte, mib)
	for left := size; left > 0; {
		n := min(left, int64(len(buf)))
		_, _ = rng.Read(buf[:n])
		h.Write(buf[:n])
		if _, err := f.Write(buf[:n]); err != nil {
			t.Fatal(err)
		}
		left -= n
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

func fileSHA(t *testing.T, p string) [32]byte {
	t.Helper()
	f, err := os.Open(p) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // read-only
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestFR_SFTP_001_RoundTripSHA256(t *testing.T) {
	size := int64(100 * mib)
	if testing.Short() {
		size = 8 * mib
	}
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up, down := t.TempDir(), t.TempDir()
	sum := randFile(t, filepath.Join(up, "big.bin"), size, 1)
	b.Copy(Local, up, []string{"big.bin"}, sshtest.SFTPHome)
	waitSettled(t, b)
	if got := fileSHA(t, srv.SFTPDir("big.bin")); got != sum {
		t.Fatal("uploaded file differs")
	}
	b.Copy(Remote, sshtest.SFTPHome, []string{"big.bin"}, down)
	ts := waitSettled(t, b)
	if got := fileSHA(t, filepath.Join(down, "big.bin")); got != sum {
		t.Fatal("downloaded file differs")
	}
	for _, x := range ts {
		if x.State != Done || x.Done != size || x.Total != size {
			t.Fatalf("transfers:\n%s", dump(ts))
		}
	}
	if x := ts[0]; x.From != Local || ts[1].From != Remote || x.DstDir != sshtest.SFTPHome || ts[1].DstDir != down {
		t.Fatalf("directions or targets wrong: %+v", ts)
	}
}

// makeTree makes three levels, an empty directory and 200 small files
// below root.
func makeTree(t *testing.T, root string) map[string]string {
	t.Helper()
	want := map[string]string{}
	dirs := []string{"", "a", "a/b", "a/b/c", "z"}
	for _, d := range append(dirs, "empty") {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 200 {
		p := path.Join(dirs[i%len(dirs)], "f"+strconv.Itoa(i)+".txt")
		want[p] = "content " + strconv.Itoa(i*7)
		writeFile(t, filepath.Join(root, p), want[p])
	}
	want["empty/"] = ""
	return want
}

// readTree returns the files below root with their contents, and empty
// directories as "dir/".
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if ents, _ := os.ReadDir(p); len(ents) == 0 {
				got[filepath.ToSlash(rel)+"/"] = ""
			}
			return nil
		}
		got[filepath.ToSlash(rel)] = readFile(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func sameTree(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d entries, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestFR_SFTP_001_TreeBothWays(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up := t.TempDir()
	want := makeTree(t, filepath.Join(up, "tree"))
	var order []string
	var mu sync.Mutex
	setHook(t, &testHookStart, func(rel string) { mu.Lock(); order = append(order, rel); mu.Unlock() })
	b.Copy(Local, up, []string{"tree"}, sshtest.SFTPHome)
	ts := waitSettled(t, b)
	sameTree(t, readTree(t, srv.SFTPDir("tree")), want)
	if len(ts) != 1+5+200 { // tree, its 5 directories below, 200 files
		t.Fatalf("%d rows:\n%s", len(ts), dump(ts))
	}
	mu.Lock()
	for i, rel := range order { // every directory starts before its contents
		if dir := path.Dir(rel); dir != "." && !slices.Contains(order[:i], dir) {
			t.Fatalf("%s started before its directory", rel)
		}
	}
	mu.Unlock()

	down := t.TempDir()
	b.Copy(Remote, sshtest.SFTPHome, []string{"tree"}, down)
	waitSettled(t, b)
	sameTree(t, readTree(t, filepath.Join(down, "tree")), want)
}

func TestFR_SFTP_001_MtimeAndMode(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up, down := t.TempDir(), t.TempDir()
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 600, time.UTC)
	p := filepath.Join(up, "run.sh")
	writeFile(t, p, "#!/bin/sh\n")
	if err := os.Chmod(p, 0o754|fs.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	writeRemote(t, srv, "data.txt", "data")
	if err := os.Chmod(srv.SFTPDir("data.txt"), 0o640); err != nil { //nolint:gosec // the mode under test
		t.Fatal(err)
	}
	if err := os.Chtimes(srv.SFTPDir("data.txt"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	b.Copy(Local, up, []string{"run.sh"}, sshtest.SFTPHome)
	b.Copy(Remote, sshtest.SFTPHome, []string{"data.txt"}, down)
	waitSettled(t, b)
	for _, c := range []struct {
		path string
		mode fs.FileMode
	}{{srv.SFTPDir("run.sh"), 0o754}, {filepath.Join(down, "data.txt"), 0o640}} {
		fi, err := os.Stat(c.path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode() != c.mode { // no setuid, setgid or sticky bit
			t.Errorf("%s mode %v, want %v", c.path, fi.Mode(), c.mode)
		}
		if !fi.ModTime().Equal(mtime.Truncate(time.Second)) {
			t.Errorf("%s mtime %v, want %v", c.path, fi.ModTime(), mtime.Truncate(time.Second))
		}
	}
}

func TestFR_SFTP_001_SkipsSymlinksInTree(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up := t.TempDir()
	root := filepath.Join(up, "t")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	symlink(t, "a.txt", filepath.Join(root, "link"))
	symlink(t, "sub", filepath.Join(root, "dirlink"))
	symlink(t, "t/a.txt", filepath.Join(up, "marked"))
	b.Copy(Local, up, []string{"t", "marked"}, sshtest.SFTPHome)
	ts := waitSettled(t, b)
	sameTree(t, readTree(t, srv.SFTPDir("t")), map[string]string{"a.txt": "a", "sub/": ""})
	for _, n := range []string{"t/link", "t/dirlink"} {
		if x := byName(ts, n); x.State != Skipped || x.Reason != "symbolic link" {
			t.Fatalf("%s: %s %q", n, x.State, x.Reason)
		}
	}
	// A marked link to a file is copied as the file.
	if fi, err := os.Lstat(srv.SFTPDir("marked")); err != nil || !fi.Mode().IsRegular() || readFile(t, srv.SFTPDir("marked")) != "a" {
		t.Fatalf("marked link: %v, %v", fi, err)
	}

	// The same on the way down, with the links on the server.
	down := t.TempDir()
	b.Clear()
	writeRemote(t, srv, "t2/a.txt", "a")
	symlink(t, "a.txt", srv.SFTPDir("t2/link"))
	if err := syscall.Mkfifo(srv.SFTPDir("t2/fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Copy(Remote, sshtest.SFTPHome, []string{"t2"}, down)
	ts = waitSettled(t, b)
	sameTree(t, readTree(t, filepath.Join(down, "t2")), map[string]string{"a.txt": "a"})
	if x := byName(ts, "t2/link"); x.State != Skipped || x.Reason != "symbolic link" {
		t.Fatalf("t2/link: %s %q", x.State, x.Reason)
	}
	if x := byName(ts, "t2/fifo"); x.State != Skipped || x.Reason != "special file" {
		t.Fatalf("t2/fifo: %s %q", x.State, x.Reason)
	}
}

func TestFR_SFTP_016_KeepBothNaming(t *testing.T) {
	for name, want := range map[string]string{
		"backup.tar.gz": "backup (2).tar.gz",
		"notes":         "notes (2)",
		".bashrc":       ".bashrc (2)",
		"a.":            "a (2).",
	} {
		if got := KeepBothName(name, 2); got != want {
			t.Errorf("KeepBothName(%q, 2) = %q, want %q", name, got, want)
		}
	}
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up := t.TempDir()
	writeFile(t, filepath.Join(up, "backup.tar.gz"), "new")
	writeRemote(t, srv, "backup.tar.gz", "old")
	writeRemote(t, srv, "backup (1).tar.gz", "old 1")
	b.Copy(Local, up, []string{"backup.tar.gz"}, sshtest.SFTPHome)
	ts := waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return ts[0].State == Waiting })
	c := ts[0].Conflict
	if c == nil || c.Local.Size != 3 || c.Remote.Size != 3 || c.Remote.ModTime.IsZero() {
		t.Fatalf("conflict = %+v", c)
	}
	b.Answer(ts[0].ID, KeepBoth, false)
	waitSettled(t, b)
	for n, want := range map[string]string{"backup.tar.gz": "old", "backup (1).tar.gz": "old 1", "backup (2).tar.gz": "new"} {
		if got := readFile(t, srv.SFTPDir(n)); got != want {
			t.Errorf("%s = %q, want %q", n, got, want)
		}
	}
}

func TestFR_SFTP_016_ApplyToAllPerBatch(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up := t.TempDir()
	names := []string{"a", "b", "c"}
	for _, n := range names {
		writeFile(t, filepath.Join(up, n), "new "+n)
		writeRemote(t, srv, n, "old")
	}
	b.Copy(Local, up, names, sshtest.SFTPHome)
	ts := waitFor(t, b, 10*time.Second, func(ts []Transfer) bool {
		return !slices.ContainsFunc(ts, func(x Transfer) bool { return x.State != Waiting })
	})
	b.Answer(ts[0].ID, Overwrite, true)
	waitSettled(t, b)
	for _, n := range names {
		if got := readFile(t, srv.SFTPDir(n)); got != "new "+n {
			t.Fatalf("%s = %q", n, got)
		}
	}
	// A later copy asks again.
	writeRemote(t, srv, "a", "old")
	b.Copy(Local, up, []string{"a"}, sshtest.SFTPHome)
	ts = waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return ts[len(ts)-1].State == Waiting })
	b.Answer(ts[len(ts)-1].ID, Skip, false)
	ts = waitSettled(t, b)
	if x := ts[len(ts)-1]; x.State != Skipped || readFile(t, srv.SFTPDir("a")) != "old" {
		t.Fatalf("second copy: %s, a = %q", x.State, readFile(t, srv.SFTPDir("a")))
	}
	// Skip for all skips the waiting ones and those still to come.
	writeRemote(t, srv, "b", "old")
	writeRemote(t, srv, "c", "old")
	b.Clear()
	b.Copy(Local, up, names, sshtest.SFTPHome)
	ts = waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return ts[0].State == Waiting })
	b.Answer(ts[0].ID, Skip, true)
	ts = waitSettled(t, b)
	for _, x := range ts {
		if x.State != Skipped {
			t.Fatalf("skip for all:\n%s", dump(ts))
		}
	}
}

func TestFR_SFTP_016_OldFileIntactUntilDone(t *testing.T) {
	srv := sftpSrv(t)
	up, down := t.TempDir(), t.TempDir()
	var checks int
	var target string
	var mu sync.Mutex
	// While data moves, the old target is intact and the data goes to the
	// part file.
	setHook(t, &testHookChunk, func() {
		mu.Lock()
		defer mu.Unlock()
		checks++
		if got, _ := os.ReadFile(target); string(got) != "old" { //nolint:gosec // test dir
			t.Errorf("%s changed during the transfer: %d bytes", target, len(got))
		}
	})
	b := openBrowser(t, srv)
	sumUp := randFile(t, filepath.Join(up, "up.bin"), 4*mib, 2)
	writeRemote(t, srv, "up.bin", "old")
	sumDown := randFile(t, srv.SFTPDir("down.bin"), 4*mib, 3)
	writeFile(t, filepath.Join(down, "down.bin"), "old")
	for _, c := range []struct {
		from     Side
		src, dst string
		name     string
		target   string
		sum      [32]byte
	}{
		{Local, up, sshtest.SFTPHome, "up.bin", srv.SFTPDir("up.bin"), sumUp},
		{Remote, sshtest.SFTPHome, down, "down.bin", filepath.Join(down, "down.bin"), sumDown},
	} {
		mu.Lock()
		target, checks = c.target, 0
		mu.Unlock()
		b.Clear()
		b.Copy(c.from, c.src, []string{c.name}, c.dst)
		ts := waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return ts[0].State == Waiting })
		b.Answer(ts[0].ID, Overwrite, false)
		waitSettled(t, b)
		if fileSHA(t, c.target) != c.sum {
			t.Fatalf("%s not replaced", c.name)
		}
		mu.Lock()
		if checks < 4 {
			t.Fatalf("%s: only %d checks", c.name, checks)
		}
		mu.Unlock()
	}
}

// cancelOnOpen cancels each transfer as soon as its part file is open.
func cancelOnOpen(t *testing.T, b **Browser) {
	setHook(t, &testHookOpened, func(rel string) {
		for _, x := range (*b).Transfers() {
			if x.Name == rel {
				(*b).Cancel(x.ID)
			}
		}
	})
}

func TestFR_SFTP_015_CancelRemovesPart(t *testing.T) {
	srv := sftpSrv(t)
	var b *Browser
	cancelOnOpen(t, &b)
	b = openBrowser(t, srv)
	up, down := t.TempDir(), t.TempDir()
	randFile(t, filepath.Join(up, "up.bin"), 4*mib, 4)
	randFile(t, srv.SFTPDir("down.bin"), 4*mib, 5)
	b.Copy(Local, up, []string{"up.bin"}, sshtest.SFTPHome)
	b.Copy(Remote, sshtest.SFTPHome, []string{"down.bin"}, down)
	ts := waitFor(t, b, 10*time.Second, settled)
	for _, x := range ts {
		if x.State != Cancelled || x.Reason != "" {
			t.Fatalf("transfers:\n%s", dump(ts))
		}
	}
	for _, p := range []string{srv.SFTPDir("up.bin"), srv.SFTPDir("up.bin" + PartSuffix), filepath.Join(down, "down.bin"), filepath.Join(down, "down.bin"+PartSuffix)} {
		if exists(p) {
			t.Errorf("%s exists after cancel", p)
		}
	}
	// Retry starts again and completes.
	setHook(t, &testHookOpened, nil)
	for _, x := range ts {
		b.Retry(x.ID)
	}
	waitSettled(t, b)
	if !exists(srv.SFTPDir("up.bin")) || !exists(filepath.Join(down, "down.bin")) {
		t.Fatal("retry did not copy")
	}
}

func TestFR_SFTP_015_CancelDirectoryCancelsQueued(t *testing.T) {
	srv := sftpSrv(t)
	gate := make(chan struct{})
	setHook(t, &testHookOpened, func(string) { <-gate })
	b := openBrowser(t, srv)
	up := t.TempDir()
	makeTree(t, filepath.Join(up, "tree"))
	b.Copy(Local, up, []string{"tree"}, sshtest.SFTPHome)
	ts := waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return len(ts) > 10 })
	b.Cancel(ts[0].ID)
	close(gate)
	ts = waitFor(t, b, 10*time.Second, settled)
	for _, x := range ts[1:] {
		if x.State != Cancelled && x.State != Done {
			t.Fatalf("transfers:\n%s", dump(ts))
		}
	}
	if n := len(readTree(t, srv.SFTPDir("tree"))); n > 10 {
		t.Fatalf("%d entries copied after the cancel", n)
	}
}

func TestFR_SFTP_015_HistoryLimit(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	for range HistoryLimit + 50 {
		b.Copy(Local, "/", []string{"bad/name"}, "/") // fails at once, with no I/O
	}
	ts := b.Transfers()
	if len(ts) != HistoryLimit || ts[0].ID != 51 {
		t.Fatalf("%d rows, first %d", len(ts), ts[0].ID)
	}
	b.Clear()
	if len(b.Transfers()) != 0 {
		t.Fatal("Clear left rows")
	}
}

func TestARCH_SFTP_003_AtMostTwo(t *testing.T) {
	srv := sftpSrv(t)
	var mu sync.Mutex
	most := 0
	var b *Browser
	setHook(t, &testHookChunk, func() {
		n := 0
		for _, x := range b.Transfers() {
			if x.State == Running {
				n++
			}
		}
		mu.Lock()
		most = max(most, n)
		mu.Unlock()
		time.Sleep(time.Millisecond)
	})
	b = openBrowser(t, srv)
	up := t.TempDir()
	var names []string
	for i := range 10 {
		n := "f" + strconv.Itoa(i)
		randFile(t, filepath.Join(up, n), 2*mib, byte(i))
		names = append(names, n)
	}
	b.Copy(Local, up, names, sshtest.SFTPHome)
	waitSettled(t, b)
	mu.Lock()
	defer mu.Unlock()
	if w := srv.SFTPMaxOpenWriters(); w != Workers || most != Workers {
		t.Fatalf("at most %d open writers and %d running, want %d", w, most, Workers)
	}
}

func TestARCH_SFTP_003_UpdatesCoalesce(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	start := time.Now()
	for range 10_000 {
		b.Copy(Local, "/", []string{"bad/name"}, "/") // a state change each, no I/O
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("10,000 copies took %v", el)
	}
	if n := len(b.Updates()); n != 1 {
		t.Fatalf("%d pending notifications, want 1", n)
	}
	<-b.Updates()
	if n := len(b.Updates()); n != 0 {
		t.Fatalf("%d pending notifications after reading one", n)
	}
}

// slowChunks makes every block of data a transfer moves take d.
func slowChunks(t *testing.T, d time.Duration) {
	setHook(t, &testHookChunk, func() { time.Sleep(d) })
}

func TestARCH_SFTP_004_CancelWithinOneSecond(t *testing.T) {
	srv := sftpSrv(t)
	slowChunks(t, 200*time.Microsecond)
	b := openBrowser(t, srv)
	up, down := t.TempDir(), t.TempDir()
	randFile(t, filepath.Join(up, "up.bin"), 64*mib, 6)
	randFile(t, srv.SFTPDir("down.bin"), 64*mib, 7)
	for _, c := range []struct {
		from      Side
		src, name string
		dst, part string
	}{
		{Local, up, "up.bin", sshtest.SFTPHome, srv.SFTPDir("up.bin" + PartSuffix)},
		{Remote, sshtest.SFTPHome, "down.bin", down, filepath.Join(down, "down.bin"+PartSuffix)},
	} {
		b.Clear()
		b.Copy(c.from, c.src, []string{c.name}, c.dst)
		ts := waitFor(t, b, 30*time.Second, func(ts []Transfer) bool { return ts[0].Done >= 32*mib })
		if !exists(c.part) {
			t.Fatalf("%s: no part file while running", c.name)
		}
		start := time.Now()
		b.Cancel(ts[0].ID)
		ts = waitFor(t, b, 2*time.Second, settled)
		if el := time.Since(start); el > time.Second || ts[0].State != Cancelled {
			t.Fatalf("%s: %s after %v", c.name, ts[0].State, el)
		}
		if exists(c.part) {
			t.Fatalf("%s: part file left", c.name)
		}
	}
}

func TestSEC_SFTP_003_NoPartialFinalName(t *testing.T) {
	srv := sftpSrv(t)
	slowChunks(t, 100*time.Microsecond)
	b := openBrowser(t, srv)
	up, down := t.TempDir(), t.TempDir()
	size := int64(16 * mib)
	randFile(t, filepath.Join(up, "up.bin"), size, 8)
	randFile(t, srv.SFTPDir("down.bin"), size, 9)
	b.Copy(Local, up, []string{"up.bin"}, sshtest.SFTPHome)
	b.Copy(Remote, sshtest.SFTPHome, []string{"down.bin"}, down)
	polls := 0
	for !settled(b.Transfers()) {
		for _, p := range []string{srv.SFTPDir("up.bin"), filepath.Join(down, "down.bin")} {
			if fi, err := os.Stat(p); err == nil && fi.Size() != size {
				t.Fatalf("%s has %d bytes under its final name", p, fi.Size())
			}
		}
		for _, p := range []string{srv.SFTPDir("up.bin" + PartSuffix), filepath.Join(down, "down.bin"+PartSuffix)} {
			// 0600 while data is written: before that it is empty (a remote
			// part is created with the server's mode, then set to 0600), after
			// it complete (its final mode is set before the rename).
			if fi, err := os.Stat(p); err == nil && fi.Mode().Perm() != PartMode && fi.Size() != 0 && fi.Size() != size {
				t.Fatalf("%s mode %v with %d bytes", p, fi.Mode(), fi.Size())
			}
		}
		polls++
		time.Sleep(time.Millisecond)
	}
	waitSettled(t, b)
	if polls < 10 {
		t.Fatalf("only %d polls", polls)
	}
}

func TestSEC_SFTP_003_SizeChangedFails(t *testing.T) {
	srv := sftpSrv(t)
	up, down := t.TempDir(), t.TempDir()
	grow := map[string]string{"up.bin": filepath.Join(up, "up.bin"), "down.bin": srv.SFTPDir("down.bin"), "short.bin": srv.SFTPDir("short.bin")}
	// The source changes once it is open.
	setHook(t, &testHookOpened, func(rel string) {
		p := grow[rel]
		if rel == "short.bin" {
			_ = os.Truncate(p, mib)
			return
		}
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test dir
		if err == nil {
			_, _ = f.Write(bytes.Repeat([]byte("x"), 100))
			_ = f.Close()
		}
	})
	b := openBrowser(t, srv)
	randFile(t, grow["up.bin"], 2*mib, 10)
	randFile(t, grow["down.bin"], 2*mib, 11)
	randFile(t, grow["short.bin"], 2*mib, 12)
	b.Copy(Local, up, []string{"up.bin"}, sshtest.SFTPHome)
	b.Copy(Remote, sshtest.SFTPHome, []string{"down.bin", "short.bin"}, down)
	ts := waitFor(t, b, 10*time.Second, settled)
	for _, x := range ts {
		if x.State != Failed || x.Reason != "size changed during transfer" {
			t.Fatalf("transfers:\n%s", dump(ts))
		}
	}
	for _, p := range []string{srv.SFTPDir("up.bin"), srv.SFTPDir("up.bin" + PartSuffix),
		filepath.Join(down, "down.bin"), filepath.Join(down, "down.bin"+PartSuffix), filepath.Join(down, "short.bin")} {
		if exists(p) {
			t.Errorf("%s exists", p)
		}
	}
}

func TestSEC_SFTP_003_PosixRenameOrFallback(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(strconv.FormatBool(fallback), func(t *testing.T) {
			setHook(t, &renameFallback, fallback)
			srv := sftpSrv(t)
			b := openBrowser(t, srv)
			if _, ok := b.remote.transfer.HasExtension("posix-rename@openssh.com"); !ok {
				t.Fatal("sshtest does not offer posix-rename")
			}
			up := t.TempDir()
			writeFile(t, filepath.Join(up, "f"), "new")
			writeRemote(t, srv, "f", "old")
			b.Copy(Local, up, []string{"f"}, sshtest.SFTPHome)
			ts := waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return ts[0].State == Waiting })
			b.Answer(ts[0].ID, Overwrite, false)
			waitSettled(t, b)
			if got := readFile(t, srv.SFTPDir("f")); got != "new" || exists(srv.SFTPDir("f"+PartSuffix)) {
				t.Fatalf("f = %q", got)
			}
		})
	}
}

func TestSEC_SFTP_004_OversizeFileCut(t *testing.T) {
	srv := sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Oversize: mib}))
	b := openBrowser(t, srv)
	down := t.TempDir()
	b.Copy(Remote, sshtest.SFTPHome, []string{path.Base(sshtest.OversizeFile)}, down)
	ts := waitFor(t, b, 10*time.Second, settled)
	if x := ts[0]; x.State != Failed || x.Reason != "size changed during transfer" || x.Done != mib {
		t.Fatalf("transfers:\n%s", dump(ts))
	}
	if ents, _ := os.ReadDir(down); len(ents) != 0 {
		t.Fatalf("left %v", ents)
	}
}

func TestFR_SFTP_016_DirectoryMergedTypeMismatchFails(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up := t.TempDir()
	for _, d := range []string{"d", "g"} {
		if err := os.Mkdir(filepath.Join(up, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(up, "d", "x"), "x")
	writeFile(t, filepath.Join(up, "f"), "f")
	writeRemote(t, srv, "d/old", "old")                       // merged
	if err := os.Mkdir(srv.SFTPDir("f"), 0o700); err != nil { // a directory where a file goes
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(up, "g", "y"), "y")
	writeRemote(t, srv, "g", "a file where a directory goes")
	b.Copy(Local, up, []string{"d", "f", "g"}, sshtest.SFTPHome)
	ts := waitFor(t, b, 10*time.Second, settled)
	if x := byName(ts, "d/x"); x.State != Done || readFile(t, srv.SFTPDir("d/old")) != "old" {
		t.Fatalf("merge:\n%s", dump(ts))
	}
	if x := byName(ts, "f"); x.State != Failed || x.Reason != ErrNotFile.Error() {
		t.Fatalf("f: %s %q", x.State, x.Reason)
	}
	if x := byName(ts, "g"); x.State != Failed || x.Reason != ErrNotDir.Error() {
		t.Fatalf("g: %s %q", x.State, x.Reason)
	}
}

func TestSEC_SFTP_004_TreeDepthCapped(t *testing.T) {
	srv := sftpSrv(t, sshtest.WithSFTPHostile(sshtest.SFTPHostile{Depth: 70}))
	b := openBrowser(t, srv)
	down := t.TempDir()
	b.Copy(Remote, sshtest.SFTPHome, []string{path.Base(sshtest.DeepDir)}, down)
	ts := waitFor(t, b, 60*time.Second, settled)
	skipped := slices.IndexFunc(ts, func(x Transfer) bool { return x.State == Skipped && x.Reason == ErrTooDeep.Error() })
	if skipped < 0 {
		t.Fatalf("no row skipped as too deep:\n%s", dump(ts))
	}
	depth := strings.Count(ts[skipped].Name, "/") + 1
	if depth != MaxDepth+1 {
		t.Fatalf("skipped at depth %d", depth)
	}
	if !exists(filepath.Join(down, "deep", filepath.FromSlash(strings.Repeat("d/", MaxDepth-1)))) {
		t.Fatal("the levels up to MaxDepth were not copied")
	}
}

func TestFR_SFTP_001_LocalNameRules(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	b.Copy(Local, "/", []string{"..", "a/b"}, sshtest.SFTPHome)
	for _, x := range b.Transfers() {
		if x.State != Failed || !errors.Is(CheckName(".."), ErrBadName) || x.Reason != ErrBadName.Error() {
			t.Fatalf("%q: %s %q", x.Name, x.State, x.Reason)
		}
	}
}
