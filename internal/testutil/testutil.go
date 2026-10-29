// Package testutil holds helpers shared by sshelf tests.
package testutil

import (
	"testing"

	"go.uber.org/goleak"
)

// VerifyTestMain runs a package's tests and fails the package if any
// goroutine is still running afterwards (NFR-REL-001). Every package that
// starts goroutines calls it from TestMain:
//
//	func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }
func VerifyTestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
