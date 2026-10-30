package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// ansiSeq matches the escape sequences lipgloss emits, so goldens hold
// what the ASCII colour profile would render (design §10).
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// goldenSizes are the terminal sizes every screen is rendered at.
var goldenSizes = []struct{ w, h int }{{80, 24}, {120, 40}}

// plain removes colour and trailing spaces from a view.
func plain(view string) string {
	lines := strings.Split(ansiSeq.ReplaceAllString(view, ""), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// golden renders the root model at 80×24 and 120×40 and compares each
// view, without colour and trailing spaces, with
// testdata/<name>.<w>x<h>.golden. It also checks the layout (NFR-UX-002).
func golden(t *testing.T, name string, m tea.Model) {
	t.Helper()
	for _, sz := range goldenSizes {
		sm, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		view := sm.View().Content
		checkLayout(t, sm.(Model), view, sz.w, sz.h)
		compareGolden(t, fmt.Sprintf("%s.%dx%d", name, sz.w, sz.h), plain(view))
	}
}

// checkLayout fails unless view has exactly h lines, none wider than w,
// and ends with the footer, or on a session tab with its status bar
// (NFR-UX-002, ARCH-UI-005).
func checkLayout(t *testing.T, m Model, view string, w, h int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Errorf("%dx%d: %d lines, want %d:\n%s", w, h, len(lines), h, plain(view))
	}
	for i, l := range lines {
		if lw := lipgloss.Width(l); lw > w {
			t.Errorf("%dx%d: line %d is %d columns wide: %q", w, h, i+1, lw, plain(l))
		}
	}
	if !strings.HasPrefix(plain(lines[0]), "sshelf │") {
		t.Errorf("%dx%d: first line %q is not the header", w, h, plain(lines[0]))
	}
	foot := strings.TrimSpace(plain(m.footer()))
	if st := m.focused(); st != nil {
		foot = strings.TrimSpace(plain(m.sessionStatus(st)))
	}
	last := strings.TrimSpace(plain(lines[len(lines)-1]))
	if foot == "" || (last != foot && !strings.HasPrefix(foot, strings.TrimSuffix(last, "…"))) {
		t.Errorf("%dx%d: last line %q is not the footer %q", w, h, last, foot)
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // testdata
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if got != string(want) {
		t.Fatalf("view differs from %s:\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
