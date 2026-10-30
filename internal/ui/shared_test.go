package ui

import (
	"strings"
	"testing"
)

// TestFR_CONN_014_M3_SharedQuestion: the Ctrl+] q question names the SFTP
// tab that keeps the connection open, and stays "Disconnect …?" when no
// SFTP tab uses it. Server-free labels are sanitised.
func TestFR_CONN_014_M3_SharedQuestion(t *testing.T) {
	if got, want := disconnectQuestion("web-prod", 0), "Disconnect web-prod?"; got != want {
		t.Errorf("no SFTP tab: %q, want %q", got, want)
	}
	if got, want := disconnectQuestion("web-prod", 3), "Close this web-prod session? SFTP tab 3 stays connected."; got != want {
		t.Errorf("shared: %q, want %q", got, want)
	}
	if got := disconnectQuestion("a\x1b]0;x\x07b", 2); strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("label not sanitised: %q", got)
	}

	// Root model: with no SFTP tab on the session's connection, the M2
	// question is unchanged.
	d, _, _ := twoSessions(t)
	d.prefix("q")
	if v := d.view(); !strings.Contains(v, "Disconnect web-prod?") {
		t.Fatalf("no confirmation:\n%s", v)
	}
}
