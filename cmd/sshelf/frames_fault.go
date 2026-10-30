//go:build sshelf_faultinject

package main

import (
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// frameLog is where recordFrame writes, opened on first use.
var frameLog struct {
	once sync.Once
	mu   sync.Mutex
	f    *os.File
	last string
}

// recordFrame writes the chrome of every frame, the header (prefixed
// "H ") and the bottom line (prefixed "S "), to the file named by
// SSHELF_FAULT_FRAMES, when it is set. It exists only in the
// fault-injection build that the E2E tests run: the canary harness scans
// sshelf's own tab bar and status bar apart from the session area, which
// shows whatever the remote prints, a secret it echoes included (T-042,
// FR-TAB-003, FR-TAB-007). While an SFTP tab is focused the whole frame
// is sshelf's own (pane titles, rows, progress line, modals), so every
// line of it is written too, without styles and prefixed "F "
// (SEC-SFTP-007). A frame whose record did not change is not written
// again.
func recordFrame(v tea.View, m tea.Model) {
	frameLog.once.Do(func() {
		if p := os.Getenv("SSHELF_FAULT_FRAMES"); p != "" {
			frameLog.f, _ = os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // a path chosen by the test
		}
	})
	if frameLog.f == nil {
		return
	}
	lines := strings.Split(v.Content, "\n")
	chrome := "H " + lines[0] + "\nS " + lines[len(lines)-1] + "\n"
	if f, ok := m.(interface{ SFTPFocused() bool }); ok && f.SFTPFocused() {
		for _, l := range lines {
			chrome += "F " + ansi.Strip(l) + "\n"
		}
	}
	frameLog.mu.Lock()
	defer frameLog.mu.Unlock()
	if chrome == frameLog.last {
		return
	}
	frameLog.last = chrome
	_, _ = frameLog.f.WriteString(chrome)
}
