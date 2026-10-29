package ui

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// ansiSeq matches the escape sequences lipgloss emits, so goldens hold
// what the ASCII colour profile would render (design §10).
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// golden compares a rendered view, without colour and trailing spaces, with
// testdata/<name>.golden.
func golden(t *testing.T, name, view string) {
	t.Helper()
	lines := strings.Split(ansiSeq.ReplaceAllString(view, ""), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	got := strings.Join(lines, "\n")
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
