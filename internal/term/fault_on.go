//go:build sshelf_faultinject

package term

import (
	"fmt"
	"os"
)

// Fault panics when the SSHELF_FAULT environment variable names point.
// It exists only in builds with the sshelf_faultinject tag, for the panic
// tests of ARCH-SEC-004. The panic message carries SSHELF_FAULT_CANARY, so
// the tests can check that crash reports never include it.
func Fault(point string) {
	if os.Getenv("SSHELF_FAULT") == point {
		panic(fmt.Errorf("fault injected at %s: %s", point, os.Getenv("SSHELF_FAULT_CANARY")))
	}
}
