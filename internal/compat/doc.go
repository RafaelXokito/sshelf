// Package compat holds the backwards-compatibility test (D-027, ARCH-DATA-006):
// every directory under testdata/compat/ at the module root holds the files
// one past release wrote, and this build must read all of them and write
// them back without losing anything. It has no code of its own.
//
// Each release adds its directory, written by the release's own code:
//
//	SSHELF_COMPAT_FIXTURE=vX.Y.Z go test -run TestD027_WriteFixture ./internal/compat
//
// Old directories are never edited.
package compat
