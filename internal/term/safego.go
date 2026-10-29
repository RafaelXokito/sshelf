package term

import (
	"runtime/debug"
	"sync/atomic"
)

// reporter receives every panic SafeGo recovers; see SetPanicReporter.
var reporter atomic.Pointer[func(recovered any, stack []byte)]

// SetPanicReporter makes SafeGo pass each recovered panic and the stack of
// the goroutine that panicked to report, before onPanic runs. main sets it
// to write a crash report (ARCH-SEC-004). report must not touch the
// terminal. nil removes it.
func SetPanicReporter(report func(recovered any, stack []byte)) {
	if report == nil {
		reporter.Store(nil)
		return
	}
	reporter.Store(&report)
}

// SafeGo runs fn in a new goroutine. If fn panics, the panic is recovered,
// reported (see SetPanicReporter) and passed to onPanic instead of crashing
// the program (design §8.6). Every goroutine sshelf starts goes through
// SafeGo (ARCH-SEC-004); the archtest forbids bare go statements elsewhere.
func SafeGo(fn func(), onPanic func(recovered any)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if report := reporter.Load(); report != nil {
					(*report)(r, debug.Stack())
				}
				onPanic(r)
			}
		}()
		fn()
	}()
}
