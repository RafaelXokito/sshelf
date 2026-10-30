package session

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the replay goldens in testdata/replay")

// replayFixtures are the recordings in testdata/replay, all made at 80×24
// with TERM=xterm-256color (testdata/replay/README).
var replayFixtures = []string{"vim-edit", "htop", "less-search", "tmux-split", "utf8-wide"}

// screenText is the golden form of a screen: each line without styles
// and trailing spaces, then the cursor, the screen and the mouse mode.
func screenText(snap Snapshot) string {
	var b strings.Builder
	for _, l := range snap.Lines {
		b.WriteString(plainLine(l))
		b.WriteByte('\n')
	}
	if snap.Cursor != nil {
		fmt.Fprintf(&b, "cursor: %d,%d\n", snap.Cursor.X, snap.Cursor.Y)
	} else {
		b.WriteString("cursor: hidden\n")
	}
	fmt.Fprintf(&b, "alt: %v\nmouse: %d\n", snap.Alt, snap.Mouse)
	return b.String()
}

// sgrText is the golden form of the styled lines, with ESC written as \e.
func sgrText(snap Snapshot) string {
	var b strings.Builder
	for _, l := range snap.Lines {
		b.WriteString(strings.ReplaceAll(l, "\x1b", `\e`))
		b.WriteByte('\n')
	}
	return b.String()
}

func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // a fixture of this package
	if err != nil {
		t.Fatalf("%v (run go test ./internal/session -run Replay -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

// TestARCH_TEST_002_Replay feeds each recording to an emulator in 1-byte,
// 7-byte and 4 KiB chunks; every chunking must produce the golden screen.
// utf8-wide passes because of sshelf's wide-character workaround (D-015).
func TestARCH_TEST_002_Replay(t *testing.T) {
	for _, name := range replayFixtures {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "replay", name+".bin")) //nolint:gosec // a fixture of this package
			if err != nil {
				t.Fatal(err)
			}
			for _, chunk := range []int{4096, 7, 1} {
				s, _ := drainScreen(t, 80, 24)
				for i := 0; i < len(data); i += chunk {
					s.write(data[i:min(i+chunk, len(data))])
				}
				snap := s.snapshot()
				checkRendered(t, snap.Lines)
				base := filepath.Join("testdata", "replay", name)
				if chunk == 4096 || !*update {
					compareGolden(t, base+".golden", screenText(snap))
					compareGolden(t, base+".sgr.golden", sgrText(snap))
				}
				if t.Failed() {
					t.Fatalf("%d-byte chunks", chunk)
				}
			}
		})
	}
}
