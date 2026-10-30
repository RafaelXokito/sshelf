package ui

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/ui/sftpview"
)

// Large listings (NFR-PERF-009): a 10,000-entry listing reaches the
// screen within 200 ms of arriving, and each filter keystroke within
// 50 ms. CI runs it without -race in the perf-sftp job.

const bigEntries = 10_000

// bigListing is 10,000 entries in no order: directories, files and
// links, one in 20 hidden, names of 12 to 60 characters.
func bigListing() []app.Entry {
	rng := rand.New(rand.NewPCG(9, 9)) //nolint:gosec // a fixture, not a secret
	words := []string{"backup", "photo", "Report", "data", "log", "archive", "notes", "build", "cache", "export"}
	es := make([]app.Entry, 0, bigEntries)
	for i := range bigEntries {
		var name strings.Builder
		if i%20 == 0 {
			name.WriteString(".")
		}
		kind := []string{"dir", "file", "file", "file", "link"}[i%5]
		fmt.Fprintf(&name, "%s%05d", kind, i)
		for name.Len() < 12+rng.IntN(49) {
			name.WriteString("-" + words[rng.IntN(len(words))])
		}
		n := name.String()[:min(name.Len(), 60)]
		switch kind {
		case "dir":
			es = append(es, dir(n))
		case "link":
			es = append(es, link(n))
		default:
			es = append(es, file(n, int64(rng.IntN(1<<30))))
		}
	}
	rng.Shuffle(len(es), func(i, j int) { es[i], es[j] = es[j], es[i] })
	return es
}

// firstShown is the first entry a pane shows with hidden files off: the
// first visible directory by name, case-insensitively.
func firstShown(es []app.Entry) string {
	var dirs []string
	for _, e := range es {
		if e.Kind == app.KindDir && !strings.HasPrefix(e.Name, ".") {
			dirs = append(dirs, e.Name)
		}
	}
	slices.SortFunc(dirs, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return dirs[0]
}

// bigTab opens an SFTP tab at 120×40 whose remote /r holds big/, a
// directory of 10,000 entries, with hidden files off; the cursor is on
// big/.
func bigTab(tb testing.TB) (*driver, []app.Entry) {
	tb.Helper()
	es := bigListing()
	b := apptest.NewFakeBrowser(1, "/l", "/r")
	b.SetDir(app.Local, "/l", "/l", file("a.txt", 1))
	b.SetDir(app.Remote, "/r", "/r", dir("big"))
	b.SetDir(app.Remote, "/r/big", "/r/big", es...)
	d, _ := sftpOpen(tb, b)
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	d.keys(".", "j") // hidden files off (FR-SFTP-008); the cursor on big/
	return d, es
}

// collect runs cmd off the Update goroutine, as tea.Program does, and
// returns the messages of it and of every command in its batches.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	switch msg := (<-ch).(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, collect(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// openBig presses enter on big/ and returns the listing's message, and
// the other messages the key produced.
func openBig(tb testing.TB, d *driver) (sftpview.ListedMsg, []tea.Msg) {
	tb.Helper()
	m, cmd := d.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.m = m
	var listed *sftpview.ListedMsg
	var rest []tea.Msg
	for _, msg := range collect(cmd) {
		if l, ok := msg.(sftpview.ListedMsg); ok && l.Dir == "/r/big" {
			listed = &l
			continue
		}
		rest = append(rest, msg)
	}
	if listed == nil || len(listed.Listing.Entries) != bigEntries {
		tb.Fatalf("enter on big/ did not list it: %v", listed)
	}
	return *listed, rest
}

// timed delivers msg to Update, draws the frame, and returns how long
// both took, the frame and the command.
func timed(d *driver, msg tea.Msg) (time.Duration, string, tea.Cmd) {
	start := time.Now()
	m, cmd := d.m.Update(msg)
	v := m.View()
	elapsed := time.Since(start)
	d.m = m
	return elapsed, v.Content, cmd
}

// TestNFR_PERF_009_TenThousandEntries: from Update receiving a 10,000-
// entry listing to the View showing its first rows takes at most 200 ms
// (median of 5), and each of 5 filter keystrokes at most 50 ms.
func TestNFR_PERF_009_TenThousandEntries(t *testing.T) {
	if raceEnabled {
		t.Skip("timing limit not meaningful under -race; CI runs it in the perf-sftp job")
	}
	const firstLimit, filterLimit = 200 * time.Millisecond, 50 * time.Millisecond
	d, es := bigTab(t)
	first := firstShown(es)
	var times []time.Duration
	for i := range 5 {
		if i > 0 {
			d.keys("h") // back to /r, the cursor on big/
		}
		listed, rest := openBig(t, d)
		took, view, cmd := timed(d, listed)
		if !strings.Contains(plain(view), "│>  ..") || !strings.Contains(plain(view), "│ ▸ "+first[:8]) {
			t.Fatalf("the frame does not show .. and then %q:\n%s", first, plain(view))
		}
		times = append(times, took)
		d.run(cmd)
		for _, msg := range rest {
			d.send(msg)
		}
	}
	slices.Sort(times)
	t.Logf("sftp: 10,000-entry listing to first frame median %v (runs %v)", times[2], times)
	if times[2] > firstLimit {
		t.Fatalf("first frame median %v; want at most %v", times[2], firstLimit)
	}

	d.keys("/")
	var worst time.Duration
	for _, r := range "e0012" {
		took, _, cmd := timed(d, tea.KeyPressMsg{Code: r, Text: string(r)})
		worst = max(worst, took)
		d.run(cmd)
	}
	if got := rowNames(t, d, app.Remote); len(got) < 2 || !strings.Contains(got[1], "e0012") {
		t.Fatalf("filtered rows %q", got)
	}
	t.Logf("sftp: filter keystroke over 10,000 entries worst %v", worst)
	if worst > filterLimit {
		t.Fatalf("filter keystroke took %v; want at most %v", worst, filterLimit)
	}
}

// BenchmarkNFR_PERF_009_SortAndRender applies a 10,000-entry listing and
// draws the frame.
func BenchmarkNFR_PERF_009_SortAndRender(b *testing.B) {
	d, _ := bigTab(b)
	listed, _ := openBig(b, d)
	b.ResetTimer()
	for range b.N {
		_, _, _ = timed(d, listed)
	}
}
