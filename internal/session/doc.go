// Package session runs SSH sessions (a channel with a PTY). From M2,
// Session feeds remote output into an in-process terminal emulator
// (github.com/charmbracelet/x/vt, wrapped in emulator.go) and encodes the
// user's input for the remote; the UI renders its snapshots (ADR-012,
// ADR-013). It never touches the local terminal: no raw mode, no reads
// from stdin, no writes to stdout. The package does not import Bubble Tea.
package session
