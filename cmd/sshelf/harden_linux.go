package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func hardenPlatform() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_DUMPABLE, 0): %w", err)
	}
	return nil
}
