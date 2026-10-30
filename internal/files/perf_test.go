package files

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"slices"
	"testing"
	"time"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Transfer throughput and transfer memory (NFR-PERF-007, NFR-SIZE-004)
// against sshtest's sftp subsystem over loopback. CI runs them without
// -race in the perf-sftp job; the lines starting "sftp:" go to the job
// summary and the PR template's perf section.

// perfSkip skips a perf check under -race and -short.
func perfSkip(tb testing.TB) {
	tb.Helper()
	if raceEnabled {
		tb.Skip("timing and memory limits not meaningful under -race; CI runs it in the perf-sftp job")
	}
	if testing.Short() {
		tb.Skip("perf check")
	}
}

// waitDone waits until transfer id is done and fails on any other end.
func waitDone(tb testing.TB, b *Browser, id TransferID, timeout time.Duration) {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ts := b.Transfers()
		i := slices.IndexFunc(ts, func(x Transfer) bool { return x.ID == id })
		if i >= 0 && ts[i].State == Done {
			return
		}
		if i >= 0 && ts[i].State.Finished() || time.Now().After(deadline) {
			tb.Fatalf("transfer %d did not finish; transfers:\n%s", id, dump(ts))
		}
		select {
		case <-b.Updates():
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// copyOne queues one copy of name and returns its transfer.
func copyOne(b *Browser, from Side, srcDir, name, dstDir string) TransferID {
	bt := b.Copy(from, srcDir, []string{name}, dstDir)
	for _, x := range b.Transfers() {
		if x.Batch == bt {
			return x.ID
		}
	}
	return 0
}

// timedCopy copies name and returns the time from enqueue to done; the
// target is removed first so the copy does not ask (FR-SFTP-016).
func timedCopy(tb testing.TB, b *Browser, from Side, srcDir, name, dstDir, target string) time.Duration {
	tb.Helper()
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		tb.Fatal(err)
	}
	start := time.Now()
	id := copyOne(b, from, srcDir, name, dstDir)
	waitDone(tb, b, id, 2*time.Minute)
	return time.Since(start)
}

// mbps is n bytes in d as MB/s (10^6 bytes).
func mbps(n int64, d time.Duration) float64 { return float64(n) / d.Seconds() / 1e6 }

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

// TestNFR_PERF_007_Throughput: a 256 MiB file of random bytes is
// downloaded and uploaded over loopback at a median of at least 50 MB/s
// (3 runs each way), and both copies have the source's SHA-256.
func TestNFR_PERF_007_Throughput(t *testing.T) {
	perfSkip(t)
	const size = 256 * mib
	const floor = 50.0 // MB/s
	srv := sftpSrv(t)
	b := openBrowser(t, srv)
	local := t.TempDir()
	sum := randFile(t, srv.SFTPDir("down.bin"), size, 7)
	upSum := randFile(t, filepath.Join(local, "up.bin"), size, 8)

	var down, up []time.Duration
	for range 3 {
		down = append(down, timedCopy(t, b, Remote, sshtest.SFTPHome, "down.bin", local, filepath.Join(local, "down.bin")))
		up = append(up, timedCopy(t, b, Local, local, "up.bin", sshtest.SFTPHome, srv.SFTPDir("up.bin")))
	}
	if got := fileSHA(t, filepath.Join(local, "down.bin")); got != sum {
		t.Fatal("downloaded file differs from its source")
	}
	if got := fileSHA(t, srv.SFTPDir("up.bin")); got != upSum {
		t.Fatal("uploaded file differs from its source")
	}
	dm, um := mbps(size, median(down)), mbps(size, median(up))
	t.Logf("sftp: throughput download %.0f MB/s, upload %.0f MB/s (256 MiB, median of 3; runs %v / %v)", dm, um, down, up)
	if dm < floor || um < floor {
		t.Fatalf("throughput download %.1f MB/s, upload %.1f MB/s; want at least %.0f MB/s each way", dm, um, floor)
	}
}

// benchTransfer copies a 64 MiB file b.N times and reports MB/s.
func benchTransfer(b *testing.B, from Side) {
	perfSkip(b)
	const size = 64 * mib
	srv := sftpSrv(b)
	br := openBrowser(b, srv)
	local := b.TempDir()
	srcDir, dstDir, target := sshtest.SFTPHome, local, filepath.Join(local, "f.bin")
	src := srv.SFTPDir("f.bin")
	if from == Local {
		srcDir, dstDir, target, src = local, sshtest.SFTPHome, srv.SFTPDir("f.bin"), filepath.Join(local, "f.bin")
	}
	randFile(b, src, size, 9)
	b.SetBytes(size)
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			b.Fatal(err)
		}
		b.StartTimer()
		waitDone(b, br, copyOne(br, from, srcDir, "f.bin", dstDir), 2*time.Minute)
	}
}

// BenchmarkNFR_PERF_007_Download reports download MB/s over loopback.
func BenchmarkNFR_PERF_007_Download(b *testing.B) { benchTransfer(b, Remote) }

// BenchmarkNFR_PERF_007_Upload reports upload MB/s over loopback.
func BenchmarkNFR_PERF_007_Upload(b *testing.B) { benchTransfer(b, Local) }

// heap is the heap held by objects, live or not yet swept (objects), and
// the live heap the last collection marked (live).
type heap struct{ objects, live uint64 }

func readHeap() heap {
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}, {Name: "/gc/heap/live:bytes"}}
	metrics.Read(s)
	return heap{s[0].Value.Uint64(), s[1].Value.Uint64()}
}

// gcHeap is readHeap after a full collection: both values are the live
// heap.
func gcHeap() heap {
	runtime.GC()
	return readHeap()
}

// peakDuring samples the heap every 50 ms while run runs and returns the
// highest samples.
func peakDuring(run func()) heap {
	peak := readHeap()
	sample := func() {
		h := readHeap()
		peak.objects, peak.live = max(peak.objects, h.objects), max(peak.live, h.live)
	}
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				sample()
			}
		}
	}()
	run()
	close(stop)
	<-stopped
	sample()
	return peak
}

// growth is how far above base v went, 0 if it did not.
func growth(v, base uint64) uint64 {
	if v > base {
		return v - base
	}
	return 0
}

// sparseFile makes a file of size zero bytes that takes no disk.
func sparseFile(t *testing.T, p string, size int64) {
	t.Helper()
	f, err := os.Create(p) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestNFR_SIZE_004_TransferHeap: while a download and an upload run at
// once, the heap grows by at most 16 MiB over the open tab's, for 256 MiB
// and for 1 GiB files alike, and closing the tab gives the memory back.
// The 16 MiB bound applies to every object the heap holds, garbage not
// yet collected included; whether the use depends on the file size is
// read from the live heap, since the garbage a 50 ms sample catches
// depends on where the collector is in its cycle, and a longer transfer
// takes more samples (specs/nfr.md, NFR-SIZE-004).
func TestNFR_SIZE_004_TransferHeap(t *testing.T) {
	perfSkip(t)
	const (
		limit  = 16 * mib
		spread = 4 * mib
	)
	sizes := []int64{256 * mib, 1 << 30}
	srv := sftpSrv(t, sshtest.WithSFTPZeroFile("zero256.bin", sizes[0]), sshtest.WithSFTPZeroFile("zero1g.bin", sizes[1]))
	b, err := Open(dialLease(t, srv), Options{})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = b.Close()
		}
	})
	local := t.TempDir()
	sparseFile(t, filepath.Join(local, "up256.bin"), sizes[0])
	sparseFile(t, filepath.Join(local, "up1g.bin"), sizes[1])
	// One small transfer each way first, so that what every transfer
	// allocates once (buffers, pools) is in the baseline: an open tab
	// that has transferred before.
	writeFile(t, filepath.Join(local, "warm.txt"), "warm")
	if err := os.Mkdir(filepath.Join(local, "w"), 0o700); err != nil {
		t.Fatal(err)
	}
	waitDone(t, b, copyOne(b, Local, local, "warm.txt", sshtest.SFTPHome), time.Minute)
	waitDone(t, b, copyOne(b, Remote, sshtest.SFTPHome, "warm.txt", filepath.Join(local, "w")), time.Minute)
	b.Clear()

	baseline := gcHeap()
	var all, live []uint64
	for i, name := range []string{"256", "1g"} {
		base := gcHeap()
		peak := peakDuring(func() {
			down := copyOne(b, Remote, sshtest.SFTPHome, "zero"+name+".bin", local)
			up := copyOne(b, Local, local, "up"+name+".bin", sshtest.SFTPHome)
			waitDone(t, b, down, 5*time.Minute)
			waitDone(t, b, up, 5*time.Minute)
		})
		all = append(all, growth(peak.objects, base.objects))
		live = append(live, growth(peak.live, base.live))
		t.Logf("sftp: heap peak growth %.1f MiB (live %.1f MiB) with two %d MiB transfers",
			float64(all[i])/mib, float64(live[i])/mib, sizes[i]/mib)
		// Free the disk before the next size.
		for _, p := range []string{filepath.Join(local, "zero"+name+".bin"), srv.SFTPDir("up" + name + ".bin")} {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		}
		b.Clear()
	}
	for i, g := range all {
		if g > limit {
			t.Errorf("heap grew by %.1f MiB with %d MiB files; want at most %d MiB", float64(g)/mib, sizes[i]/mib, limit/mib)
		}
	}
	if d := max(live[0], live[1]) - min(live[0], live[1]); d >= spread {
		t.Errorf("live heap peaks differ by %.1f MiB between 256 MiB and 1 GiB files; want less than %d MiB", float64(d)/mib, spread/mib)
	}

	closed = true
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if after := gcHeap(); after.objects > baseline.objects+2*mib {
		t.Errorf("heap after closing the tab %.1f MiB above the open tab's; want within 2 MiB", float64(after.objects-baseline.objects)/mib)
	}
}
