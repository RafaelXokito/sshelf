//go:build race

package main

// raceEnabled reports whether the race detector is on; timing limits are
// meaningless under its 10–100× slowdown.
const raceEnabled = true
