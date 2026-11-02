// Package update checks GitHub for a newer sshelf release and replaces the
// running binary with it (D-026, D-110, D-111, ADR-021).
//
// It is the only sshelf package that imports net/http (ARCH-UPD-001). It
// talks to one host over HTTPS, sends nothing but a "sshelf/<version>"
// User-Agent, caps every response, and turns everything it shows into a
// version it parsed itself or sanitised text (SEC-UPD-001). An update is
// written only after its SHA-256 matched the release's checksums.txt, and
// is swapped in with a rename in the binary's own directory (SEC-UPD-002).
package update
