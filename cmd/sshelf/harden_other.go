//go:build unix && !linux

package main

// hardenPlatform adds nothing outside Linux: there, RLIMIT_CORE 0 is all of
// SEC-MEM-003.
func hardenPlatform() error { return nil }
