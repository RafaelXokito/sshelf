//go:build !sshelf_faultinject

package term

// Fault is a fault-injection point. It does nothing unless sshelf is built
// with the sshelf_faultinject tag (ARCH-SEC-004 tests).
func Fault(string) {}
