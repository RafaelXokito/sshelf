//go:build unix && !linux

package main

// dumpable reports -1: only Linux has a dumpable flag.
func dumpable() (int, error) { return -1, nil }
