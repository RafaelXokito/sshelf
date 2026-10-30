package files

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Resume, pause and reconnect (FR-SFTP-017, FR-SFTP-018, ARCH-SFTP-005).

func TestARCH_SFTP_005_ResumeDecisionTable(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	rec := &record{size: 64 * mib, mtime: t0}
	src := FileInfo{Size: 64 * mib, ModTime: t0}
	part := partInfo{exists: true, regular: true, size: 20 * mib, mtime: t1}

	down := []struct {
		name   string
		rec    *record
		remote FileInfo
		part   partInfo
		off    int64
		reason string
	}{
		{"all true", rec, src, part, 20 * mib, ""},
		{"first start", nil, src, part, 0, ""},
		{"no part", rec, src, partInfo{}, 0, "part file missing"},
		{"part not regular", rec, src, partInfo{exists: true, size: 1}, 0, "unsafe part file"},
		{"remote size changed", rec, FileInfo{Size: 65 * mib, ModTime: t0}, part, 0, "remote file changed"},
		{"remote mtime changed", rec, FileInfo{Size: 64 * mib, ModTime: t1}, part, 0, "remote file changed"},
		{"part larger", rec, src, partInfo{exists: true, regular: true, size: 65 * mib}, 0, "part file larger than the remote file"},
	}
	for _, c := range down {
		off, reason := resumeDownload(c.rec, c.remote, c.part)
		if off != c.off || reason != c.reason {
			t.Errorf("download, %s: resume at %d (%q), want %d (%q)", c.name, off, reason, c.off, c.reason)
		}
	}

	cp := &checkpoint{off: 16 * mib, size: 16 * mib, mtime: t1}
	cpPart := partInfo{exists: true, regular: true, size: 16 * mib, mtime: t1}
	up := []struct {
		name   string
		rec    *record
		cp     *checkpoint
		local  FileInfo
		part   partInfo
		off    int64
		reason string
	}{
		{"all true", rec, cp, src, cpPart, 16 * mib, ""},
		{"writes in flight: larger and newer", rec, cp, src, partInfo{exists: true, regular: true, size: 20 * mib, mtime: t1.Add(time.Second)}, 16 * mib, ""},
		{"writes in flight: larger, same time", rec, cp, src, partInfo{exists: true, regular: true, size: 20 * mib, mtime: t1}, 16 * mib, ""},
		{"first start", nil, nil, src, cpPart, 0, ""},
		{"local size changed", rec, cp, FileInfo{Size: 63 * mib, ModTime: t0}, cpPart, 0, "local file changed"},
		{"local mtime changed", rec, cp, FileInfo{Size: 64 * mib, ModTime: t1}, cpPart, 0, "local file changed"},
		{"no checkpoint", rec, nil, src, cpPart, 0, "no checkpoint"},
		{"no part", rec, cp, src, partInfo{}, 0, "part file missing"},
		{"part not regular", rec, cp, src, partInfo{exists: true, size: 16 * mib, mtime: t1}, 0, "part file is not a regular file"},
		{"part larger than the local file", rec, cp, src, partInfo{exists: true, regular: true, size: 65 * mib, mtime: t1}, 0, "part file larger than the local file"},
		{"part shorter than the checkpoint", rec, cp, src, partInfo{exists: true, regular: true, size: 8 * mib, mtime: t1}, 0, "part file shorter than the checkpoint"},
		{"same size, other time", rec, cp, src, partInfo{exists: true, regular: true, size: 16 * mib, mtime: t1.Add(time.Second)}, 0, "part file changed"},
		{"larger but older", rec, cp, src, partInfo{exists: true, regular: true, size: 20 * mib, mtime: t0}, 0, "part file changed"},
	}
	for _, c := range up {
		off, reason := resumeUpload(c.rec, c.cp, c.local, c.part)
		if off != c.off || reason != c.reason {
			t.Errorf("upload, %s: resume at %d (%q), want %d (%q)", c.name, off, reason, c.off, c.reason)
		}
	}
}

// waitPaused waits until the browser lost its connection and nothing runs.
func waitPaused(t *testing.T, b *Browser) []Transfer {
	t.Helper()
	select {
	case <-b.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("Done not closed after the drop")
	}
	if b.State() != Disconnected {
		t.Fatal("State() is not Disconnected")
	}
	return waitFor(t, b, 10*time.Second, func(ts []Transfer) bool {
		return !slices.ContainsFunc(ts, func(x Transfer) bool { return x.State == Running })
	})
}

// attach reconnects b to srv.
func attach(t *testing.T, b *Browser, srv *sshtest.Server) {
	t.Helper()
	if err := b.Attach(dialLease(t, srv)); err != nil {
		t.Fatal(err)
	}
	if b.State() != Connected {
		t.Fatal("not connected after Attach")
	}
}

func TestFR_SFTP_017_DownloadResumes(t *testing.T) {
	srv := sftpSrv(t, sshtest.WithSFTPFault("big.bin", 20*mib, sshtest.FaultDrop))
	b := openBrowser(t, srv)
	down := t.TempDir()
	sum := randFile(t, srv.SFTPDir("big.bin"), 64*mib, 20)
	b.Copy(Remote, sshtest.SFTPHome, []string{"big.bin"}, down)
	ts := waitPaused(t, b)
	if ts[0].State != Paused {
		t.Fatalf("transfers:\n%s", dump(ts))
	}
	fi, err := os.Stat(filepath.Join(down, "big.bin"+PartSuffix))
	if err != nil || fi.Size() < 16*mib {
		t.Fatalf("part file after the drop: %v, %v", fi, err)
	}
	before := srv.SFTPBytesRead("big.bin")
	attach(t, b, srv)
	ts = waitSettled(t, b)
	if fileSHA(t, filepath.Join(down, "big.bin")) != sum {
		t.Fatal("resumed download differs")
	}
	if again := srv.SFTPBytesRead("big.bin") - before; again >= 48*mib {
		t.Fatalf("read %d MiB the second time", again/mib)
	}
	if ts[0].State != Done || ts[0].Reason != "" {
		t.Fatalf("transfers:\n%s", dump(ts))
	}
}

func TestFR_SFTP_017_UploadResumesFromCheckpoint(t *testing.T) {
	part := "big.bin" + PartSuffix
	srv := sftpSrv(t, sshtest.WithSFTPFault(part, 20*mib, sshtest.FaultDrop))
	b := openBrowser(t, srv)
	up := t.TempDir()
	sum := randFile(t, filepath.Join(up, "big.bin"), 64*mib, 21)
	b.Copy(Local, up, []string{"big.bin"}, sshtest.SFTPHome)
	ts := waitPaused(t, b)
	if ts[0].State != Paused {
		t.Fatalf("transfers:\n%s", dump(ts))
	}
	b.mu.Lock()
	cp := *b.jobs[0].cp
	b.mu.Unlock()
	if cp.off < 16*mib || cp.off%checkpointEvery != 0 {
		t.Fatalf("checkpoint at %d", cp.off)
	}
	before := srv.SFTPBytesWritten(part)
	attach(t, b, srv)
	ts = waitSettled(t, b)
	if fileSHA(t, srv.SFTPDir("big.bin")) != sum {
		t.Fatal("resumed upload differs")
	}
	// Truncated to the checkpoint, and only the rest sent again.
	if again := srv.SFTPBytesWritten(part) - before; again != 64*mib-cp.off || again > 48*mib {
		t.Fatalf("wrote %d bytes the second time, checkpoint at %d", again, cp.off)
	}
	if ts[0].State != Done || ts[0].Reason != "" {
		t.Fatalf("transfers:\n%s", dump(ts))
	}
}

func TestFR_SFTP_017_RemoteChangedRestarts(t *testing.T) {
	srv := sftpSrv(t, sshtest.WithSFTPFault("big.bin", 4*mib, sshtest.FaultDrop))
	b := openBrowser(t, srv)
	down := t.TempDir()
	randFile(t, srv.SFTPDir("big.bin"), 16*mib, 22)
	b.Copy(Remote, sshtest.SFTPHome, []string{"big.bin"}, down)
	waitPaused(t, b)
	sum := randFile(t, srv.SFTPDir("big.bin"), 16*mib, 23) // changed while disconnected
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(srv.SFTPDir("big.bin"), later, later); err != nil {
		t.Fatal(err)
	}
	before := srv.SFTPBytesRead("big.bin")
	attach(t, b, srv)
	ts := waitSettled(t, b)
	if ts[0].Reason != "restarted: remote file changed" {
		t.Fatalf("transfers:\n%s", dump(ts))
	}
	if fileSHA(t, filepath.Join(down, "big.bin")) != sum {
		t.Fatal("restarted download differs")
	}
	if again := srv.SFTPBytesRead("big.bin") - before; again < 16*mib {
		t.Fatalf("read only %d bytes after the restart", again)
	}
}

func TestFR_SFTP_017_LeftoverPartNotResumed(t *testing.T) {
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	up, down := t.TempDir(), t.TempDir()
	sumDown := randFile(t, srv.SFTPDir("down.bin"), 2*mib, 24)
	sumUp := randFile(t, filepath.Join(up, "up.bin"), 2*mib, 25)
	// Parts left by an earlier run, with the right sizes but other data.
	randFile(t, filepath.Join(down, "down.bin"+PartSuffix), mib, 26)
	randFile(t, srv.SFTPDir("up.bin"+PartSuffix), mib, 27)
	b.Copy(Remote, sshtest.SFTPHome, []string{"down.bin"}, down)
	b.Copy(Local, up, []string{"up.bin"}, sshtest.SFTPHome)
	ts := waitSettled(t, b)
	if fileSHA(t, filepath.Join(down, "down.bin")) != sumDown || fileSHA(t, srv.SFTPDir("up.bin")) != sumUp {
		t.Fatal("a leftover part was resumed")
	}
	for _, x := range ts {
		if x.Reason != "" {
			t.Fatalf("transfers:\n%s", dump(ts))
		}
	}
	if exists(filepath.Join(down, "down.bin"+PartSuffix)) || exists(srv.SFTPDir("up.bin"+PartSuffix)) {
		t.Fatal("part file left")
	}
}

func TestFR_SFTP_018_DropPausesAll(t *testing.T) {
	srv := sftpSrv(t)
	slowChunks(t, 200*time.Microsecond)
	b := openBrowser(t, srv)
	up, down := t.TempDir(), t.TempDir()
	for i, n := range []string{"a", "b", "c"} {
		randFile(t, srv.SFTPDir(n), 16*mib, byte(30+i))
	}
	randFile(t, filepath.Join(up, "u"), 16*mib, 33)
	b.Copy(Remote, sshtest.SFTPHome, []string{"a", "b", "c"}, down)
	b.Copy(Local, up, []string{"u"}, sshtest.SFTPHome)
	waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return ts[0].Done > mib })
	srv.DropConnections()
	ts := waitPaused(t, b)
	for _, x := range ts {
		if x.State != Paused {
			t.Fatalf("transfers:\n%s", dump(ts))
		}
	}
	if !exists(filepath.Join(down, "a"+PartSuffix)) {
		t.Fatal("the running download's part file was not kept")
	}
	if _, err := b.List(bg, Remote, sshtest.SFTPHome); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("remote List while disconnected = %v", err)
	}
	if _, err := b.List(bg, Local, down); err != nil {
		t.Fatalf("local List while disconnected = %v", err)
	}
	// Cancelling a paused download removes its part file.
	b.Cancel(ts[0].ID)
	if exists(filepath.Join(down, "a"+PartSuffix)) {
		t.Fatal("part file left after cancelling a paused transfer")
	}
}

func TestFR_SFTP_018_AttachResumesInOrder(t *testing.T) {
	srv := sftpSrv(t)
	var mu sync.Mutex
	var starts []string
	setHook(t, &testHookStart, func(rel string) { mu.Lock(); starts = append(starts, rel); mu.Unlock() })
	slowChunks(t, 200*time.Microsecond)
	b := openBrowser(t, srv)
	down := t.TempDir()
	names := []string{"a", "b", "c", "d", "e"}
	sums := map[string][32]byte{}
	for i, n := range names {
		sums[n] = randFile(t, srv.SFTPDir(n), 16*mib, byte(40+i))
	}
	b.Copy(Remote, sshtest.SFTPHome, names, down)
	waitFor(t, b, 10*time.Second, func(ts []Transfer) bool { return ts[0].Done > mib })
	srv.DropConnections()
	paused := waitPaused(t, b)
	var want []string
	for _, x := range paused {
		if x.State == Paused {
			want = append(want, x.Name)
		}
	}
	if len(want) < 4 || !slices.Equal(want, names[len(names)-len(want):]) {
		t.Fatalf("paused: %v", want)
	}
	if (<-chan struct{})(b.Done()) == nil {
		t.Fatal("no Done channel")
	}
	old := b.Done()
	mu.Lock()
	starts = nil
	mu.Unlock()
	attach(t, b, srv)
	if b.Done() == old {
		t.Fatal("Done not renewed by Attach")
	}
	waitSettled(t, b)
	mu.Lock()
	got := slices.Clone(starts)
	mu.Unlock()
	if !slices.Equal(got, want) {
		t.Fatalf("started after Attach: %v, want %v", got, want)
	}
	for _, n := range names {
		if fileSHA(t, filepath.Join(down, n)) != sums[n] {
			t.Fatalf("%s differs", n)
		}
	}
	if err := b.Attach(dialLease(t, srv)); !errors.Is(err, ErrAttached) {
		t.Fatalf("Attach while connected = %v", err)
	}
	if !srv.WaitFor(time.Second, func() bool { return strings.Count(closesOf(srv), "disconnect") == 2 }) {
		t.Fatalf("closes = %s, want the refused lease's connection closed", closesOf(srv))
	}
}

func closesOf(srv *sshtest.Server) string {
	var sb strings.Builder
	for _, c := range srv.Closes() {
		sb.WriteString(c.Type + " ")
	}
	return sb.String()
}
