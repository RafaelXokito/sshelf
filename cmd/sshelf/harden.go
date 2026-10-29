//go:build unix

package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// harden applies the process protections of SEC-MEM-003: no core dumps
// (RLIMIT_CORE 0, soft and hard) and, on Linux, a non-dumpable process,
// which also stops other processes of the same user from attaching with
// ptrace. main calls it before anything else.
func harden() error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		return fmt.Errorf("set RLIMIT_CORE to 0: %w", err)
	}
	return hardenPlatform()
}
