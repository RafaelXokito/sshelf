//go:build sshelf_faultinject

package main

import (
	"os"
	"os/signal"
	"runtime"
	"runtime/metrics"
	"strconv"
	"syscall"

	"github.com/RafaelXokito/sshelf/internal/term"
)

// When SSHELF_FAULT_HEAP names a file, the fault-injection build answers
// each SIGUSR1 with a garbage collection and then writes its live heap in
// bytes to that file (written aside and renamed, so a reader never sees
// half of it). Only the E2E tests run this build: they measure sshelf's
// heap from outside the process (SEC-SFTP-004 AC1).
func init() {
	p := os.Getenv("SSHELF_FAULT_HEAP")
	if p == "" {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	term.SafeGo(func() {
		for range ch {
			runtime.GC()
			s := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
			metrics.Read(s)
			tmp := p + ".tmp"
			if os.WriteFile(tmp, []byte(strconv.FormatUint(s[0].Value.Uint64(), 10)), 0o600) == nil {
				_ = os.Rename(tmp, p)
			}
		}
	}, func(any) {})
}
