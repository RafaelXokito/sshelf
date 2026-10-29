package main

import "golang.org/x/sys/unix"

func dumpable() (int, error) {
	return unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
}
