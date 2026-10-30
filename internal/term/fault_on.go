//go:build sshelf_faultinject

package term

import (
	"fmt"
	"os"
	"sync/atomic"
)

// faulted records that a fault fired, for SSHELF_FAULT_ONCE.
var faulted atomic.Bool

// Fault panics when the SSHELF_FAULT environment variable names point.
// With SSHELF_FAULT_ONCE=1 it panics only the first time, so that a test
// can check that the rest of sshelf keeps running (two sessions, one of
// them failing). It exists only in builds with the sshelf_faultinject
// tag, for the panic tests of ARCH-SEC-004 and ARCH-SESS-005. The panic
// message carries SSHELF_FAULT_CANARY, so the tests can check that crash
// reports never include it.
func Fault(point string) {
	if os.Getenv("SSHELF_FAULT") != point {
		return
	}
	if os.Getenv("SSHELF_FAULT_ONCE") == "1" && faulted.Swap(true) {
		return
	}
	panic(fmt.Errorf("fault injected at %s: %s", point, os.Getenv("SSHELF_FAULT_CANARY")))
}
