//go:build unix

package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// waitFast is waitText polling every millisecond, for timings. It returns
// how long after start want appeared.
func (s *slice) waitFast(off int, want string, start time.Time) time.Duration {
	s.t.Helper()
	needle := whitespace.ReplaceAllString(want, "")
	deadline := start.Add(5 * time.Second)
	for !strings.Contains(screenText(s.out.From(off)), needle) {
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %q", want)
		}
		time.Sleep(time.Millisecond)
	}
	return time.Since(start)
}

// TestNFR_PERF_006_TenSessions opens 10 sessions to shells streaming
// 100 KB/s each, then checks that Ctrl+] n draws the next tab within
// 50 ms (median of 10 switches), that 200 keystrokes reach the focused
// session's server within 20 ms at the 99th percentile, and that RSS
// stays within 10 sessions' heap bound (NFR-SIZE-003) plus the idle base
// (NFR-SIZE-001).
func TestNFR_PERF_006_TenSessions(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("NFR-PERF-006 is checked on Linux")
	}
	if testing.Short() {
		t.Skip("timing")
	}
	if raceEnabled {
		t.Skip("timing limit not meaningful under -race; CI runs it in the perf step")
	}
	s := startSlice(t)
	s.waitText(0, "web-prod demo@127.0.0.1")
	const n = 10
	for i := 1; i <= n; i++ {
		if i > 1 {
			s.prefix("h")
		}
		s.openSession(s.srv, i)
		s.typeKeys("stream 100K\r")
	}
	if !s.srv.WaitFor(5*time.Second, func() bool {
		return strings.Contains(string(s.srv.Received(n, 1)), "stream 100K\r")
	}) {
		t.Fatal("the streams did not start")
	}

	// Ten switches with Ctrl+] n, through tabs 2 to 10 and again to 2.
	var switches []time.Duration
	focused := n
	for len(switches) < 10 {
		target := focused%n + 1
		if target == 1 { // Ctrl+] n from tab 10 goes to Hosts; jump instead
			off := s.out.Len()
			s.prefix("1")
			s.waitText(off, "[1 web-prod]")
			focused = 1
			continue
		}
		off := s.out.Len()
		start := time.Now()
		s.prefix("n")
		switches = append(switches, s.waitFast(off, fmt.Sprintf("[%d web-prod]", target), start))
		focused = target
	}
	slices.Sort(switches)
	if median := switches[len(switches)/2]; median > 50*time.Millisecond {
		t.Errorf("tab switch: median %v of %v, want at most 50 ms", median, switches)
	} else {
		t.Logf("tab switch: median %v of %v", median, switches)
	}

	// 200 keystrokes to the focused tab (its stream stops at the first
	// key; the nine others keep streaming).
	times := make([]time.Duration, 200)
	have := len(s.srv.Received(focused, 1))
	for i := range times {
		start := time.Now()
		s.typeKeys("a")
		for len(s.srv.Received(focused, 1)) <= have {
			if time.Since(start) > 5*time.Second {
				t.Fatalf("keystroke %d never arrived", i)
			}
			time.Sleep(100 * time.Microsecond)
		}
		times[i] = time.Since(start)
		have++
	}
	slices.Sort(times)
	if p99 := times[len(times)*99/100]; p99 > 20*time.Millisecond {
		t.Errorf("keystroke overhead p99 %v, want at most 20 ms", p99)
	} else {
		t.Logf("keystroke overhead p99 %v", p99)
	}

	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(s.cmd.Process.Pid)).Output() //nolint:gosec // ps with a pid
	if err != nil {
		t.Fatal(err)
	}
	kib, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	const limit = (10*48 + 60) << 10 // KiB
	if kib > limit {
		t.Errorf("RSS %d KiB with 10 sessions, want at most %d KiB", kib, limit)
	} else {
		t.Logf("RSS %d KiB with 10 sessions", kib)
	}
}
