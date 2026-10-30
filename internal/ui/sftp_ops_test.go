package ui

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/input"
	"github.com/RafaelXokito/sshelf/internal/ui/sftpview"
)

// localNames are the names in the fake's local ~/Downloads.
func localNames(b *apptest.FakeBrowser) []string {
	var out []string
	for _, e := range b.Entries(app.Local, "/Users/me/Downloads") {
		out = append(out, e.Name)
	}
	slices.Sort(out)
	return out
}

// top is the driver's top modal, or nil.
func top(d *driver) any { return topModal(d.root()) }

// progressLine is the plain progress line, the last but one.
func progressLine(d *driver) string {
	lines := strings.Split(plain(d.view()), "\n")
	return strings.TrimSpace(lines[len(lines)-2])
}

// clearField deletes a text field's value (ctrl+u).
func (d *driver) clearField() { d.send(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}) }

func TestFR_SFTP_010_RenameModalGolden(t *testing.T) {
	d, b := approvedTab(t)
	d.keys("tab", "G", "r") // report.pdf
	in, ok := top(d).(input.Model)
	if !ok || in.Value() != "report.pdf" {
		t.Fatalf("modal = %#v", top(d))
	}
	golden(t, "sftp-rename", d.m)
	d.clearField()
	d.typeText("final.pdf")
	d.keys("enter")
	if top(d) != nil {
		t.Fatal("modal still open")
	}
	if got := localNames(b); !slices.Equal(got, []string{"final.pdf", "notes.txt", "photos"}) {
		t.Fatalf("names = %q", got)
	}
	if _, _, cur := where(t, d); cur != "final.pdf" {
		t.Fatalf("cursor on %q, want the renamed entry", cur)
	}
	if p := progressLine(d); p != "renamed report.pdf to final.pdf" {
		t.Fatalf("progress line = %q", p)
	}
	// Esc cancels.
	d.keys("r", "x", "esc")
	if top(d) != nil || !slices.Contains(localNames(b), "final.pdf") {
		t.Fatal("esc did not cancel")
	}
	d.keys("g", "r") // .. cannot be renamed
	if top(d) != nil {
		t.Fatal("r on .. opened a modal")
	}
}

func TestFR_SFTP_010_RejectsUnsafeName(t *testing.T) {
	for _, tc := range []struct{ name, reason string }{
		{"", "the name is empty"},
		{".", "not a valid name"},
		{"..", "not a valid name"},
		{"a/b", "the name contains /"},
	} {
		d, b := approvedTab(t)
		d.keys("tab", "G", "r")
		d.clearField()
		d.send(tea.PasteMsg{Content: tc.name})
		d.keys("enter")
		in, ok := top(d).(input.Model)
		if !ok || in.Reason() != tc.reason {
			t.Fatalf("%q: modal %#v, want reason %q", tc.name, top(d), tc.reason)
		}
		if !strings.Contains(plain(d.view()), tc.reason) {
			t.Fatalf("%q: the reason is not shown", tc.name)
		}
		if got := localNames(b); !slices.Equal(got, []string{"notes.txt", "photos", "report.pdf"}) {
			t.Fatalf("%q: renamed anyway: %q", tc.name, got)
		}
	}
	// The text field drops control characters as they are typed or
	// pasted; the check refuses them anyway.
	for _, name := range []string{"a\x00b", "a\x1b[31m", "a\tb", "a\u0085b"} {
		if got := sftpview.CheckName(name); got != "the name contains a control character" {
			t.Errorf("CheckName(%q) = %q", name, got)
		}
	}
	d, b := approvedTab(t)
	d.keys("tab", "G", "r")
	d.clearField()
	d.send(tea.PasteMsg{Content: "x\x1b[31my"})
	d.keys("enter")
	for _, n := range localNames(b) {
		if strings.ContainsAny(n, "\x1b\x00") {
			t.Fatalf("renamed to %q", n)
		}
	}
}

func TestFR_SFTP_010_ExistingRefused(t *testing.T) {
	d, b := approvedTab(t)
	d.keys("tab", "G", "r")
	d.clearField()
	d.typeText("notes.txt")
	d.keys("enter")
	if in, ok := top(d).(input.Model); !ok || in.Reason() != "already exists" {
		t.Fatalf("modal = %#v", top(d))
	}
	if got := localNames(b); !slices.Equal(got, []string{"notes.txt", "photos", "report.pdf"}) {
		t.Fatalf("renamed over an existing name: %q", got)
	}
	// A name created by someone else meanwhile: the server refuses.
	d.keys("esc", "r")
	d.clearField()
	d.typeText("late.txt")
	b.SetDir(app.Local, "/Users/me/Downloads", "~/Downloads", dir("photos"), file("report.pdf", 1), file("notes.txt", 1), file("late.txt", 1))
	d.keys("enter")
	if p := progressLine(d); p != "✕ cannot rename report.pdf: already exists" {
		t.Fatalf("progress line = %q", p)
	}
	if _, _, cur := where(t, d); cur != "report.pdf" {
		t.Fatalf("cursor = %q", cur)
	}
}

func TestFR_SFTP_011_ConfirmCountsEntries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		keys  []string
		count int
		want  string
	}{
		{"one file", []string{"j", "j"}, 0, "Delete notes.txt?"},
		{"one dir", []string{"j"}, 1234, "Delete photos/ and the 1,234 entries in it? This cannot be undone."},
		{"several", []string{"j", "space", "space"}, 1234, "Delete 2 items and the 1,234 entries in them?"},
		{"too many", []string{"j"}, 100_001, "Delete photos/ and more than 100,000 entries in it? This cannot be undone."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b := approvedTab(t)
			b.SetCount(app.Local, "/Users/me/Downloads/photos", tc.count)
			d.keys("tab")
			d.keys(tc.keys...)
			d.keys("d")
			if !strings.Contains(plain(d.view()), tc.want) {
				t.Fatalf("question %q not shown:\n%s", tc.want, plain(d.view()))
			}
			if tc.name == "one dir" {
				golden(t, "sftp-delete", d.m)
			}
		})
	}
	d, _ := approvedTab(t)
	d.keys("d") // on ..
	if top(d) != nil {
		t.Fatal("d on .. asked")
	}
}

func TestFR_SFTP_011_DefaultsNo(t *testing.T) {
	for _, k := range []string{"n", "enter", "esc"} {
		d, b := approvedTab(t)
		d.keys("tab", "j", "j", "d", k)
		if top(d) != nil || len(localNames(b)) != 3 {
			t.Fatalf("%s: modal %v, names %q", k, top(d), localNames(b))
		}
	}
	d, b := approvedTab(t)
	d.keys("tab", "j", "j", "d")
	if _, ok := top(d).(confirm.Model); !ok {
		t.Fatal("no confirmation")
	}
	d.keys("y")
	if got := localNames(b); !slices.Equal(got, []string{"photos", "report.pdf"}) {
		t.Fatalf("names = %q", got)
	}
	if _, _, cur := where(t, d); cur != "report.pdf" {
		t.Fatalf("cursor = %q, want the next entry", cur)
	}
	if p := progressLine(d); p != "deleted notes.txt" {
		t.Fatalf("progress line = %q", p)
	}
}

func TestFR_SFTP_011_CountCancel(t *testing.T) {
	d, b := approvedTab(t)
	release := b.GateCounts()
	defer release()
	d.keys("tab", "j")
	// Press d without running the count: it waits behind the gate.
	d.svc.updateGID.Store(goroutineID())
	m, count := d.m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	d.m = m
	if p := progressLine(d); p != "Counting photos/… esc cancel" {
		t.Fatalf("progress line = %q", p)
	}
	golden(t, "sftp-counting", d.m)
	d.keys("esc")
	d.run(count) // the cancelled count reports context.Canceled
	if top(d) != nil || len(localNames(b)) != 3 {
		t.Fatalf("modal %v, names %q after esc", top(d), localNames(b))
	}
	if p := progressLine(d); strings.Contains(p, "Counting") {
		t.Fatalf("progress line = %q", p)
	}
}

func TestFR_SFTP_011_StopsAtFirstError(t *testing.T) {
	b := approvedBrowser(1)
	b.SetDir(app.Remote, "/home/pi", "/home/pi", file("a", 1), file("b", 1), file("c", 1))
	b.SetErr(app.Remote, "/home/pi/b", fs.ErrPermission)
	d, _ := sftpOpen(t, b)
	d.keys("j", "space", "space", "space", "d")
	if !strings.Contains(plain(d.view()), "Delete 3 items?") {
		t.Fatalf("question:\n%s", plain(d.view()))
	}
	d.keys("y")
	if p := progressLine(d); p != "✕ cannot delete b (1 deleted): permission denied" {
		t.Fatalf("progress line = %q", p)
	}
	if got := rowNames(t, d, app.Remote); !slices.Equal(got, []string{"..", "b", "c"}) {
		t.Fatalf("pane not refreshed: %q", got)
	}
}

func TestFR_SFTP_012_CursorOnNewDir(t *testing.T) {
	d, b := approvedTab(t)
	d.keys("m")
	in, ok := top(d).(input.Model)
	if !ok || in.Value() != "" {
		t.Fatalf("modal = %#v", top(d))
	}
	golden(t, "sftp-mkdir", d.m)
	d.typeText("scripts")
	d.keys("enter")
	if in, ok := top(d).(input.Model); !ok || in.Reason() != "already exists" {
		t.Fatalf("existing name: %#v", top(d))
	}
	d.clearField()
	d.typeText("new")
	d.keys("enter")
	if _, dir, cur := where(t, d); dir != "/home/pi" || cur != "new" {
		t.Fatalf("dir %q cursor %q", dir, cur)
	}
	if !slices.ContainsFunc(b.Entries(app.Remote, "/home/pi"), func(e app.Entry) bool { return e.Name == "new" && e.Kind == app.KindDir }) {
		t.Fatal("not made")
	}
}

func TestFR_SFTP_024_ErrorLine(t *testing.T) {
	d, b := approvedTab(t)
	b.SetErr(app.Remote, "/home/pi/scripts", fs.ErrPermission)
	d.keys("j", "enter")
	want := "✕ cannot open /home/pi/scripts: permission denied"
	if p := progressLine(d); p != want {
		t.Fatalf("progress line = %q", p)
	}
	if !strings.HasPrefix(sftpTabAt(t, d, 2).view.Message(), "✕ ") {
		t.Fatal("no ✕ prefix")
	}
	// The next operation within 10 s keeps it; after 10 s the next one
	// clears it.
	d.clock = d.clock.Add(9 * time.Second)
	d.keys("j")
	if p := progressLine(d); p != want {
		t.Fatalf("cleared before 10 s: %q", p)
	}
	d.clock = d.clock.Add(time.Second)
	if p := progressLine(d); p != want {
		t.Fatalf("cleared without an operation: %q", p)
	}
	d.keys("k")
	if p := progressLine(d); p != "" {
		t.Fatalf("not cleared after 10 s and an operation: %q", p)
	}
	// Server text is sanitised.
	b.SetErr(app.Remote, "/home/pi/scripts", &fs.PathError{Op: "open", Path: "x", Err: errString("bad\x1b[31m thing")})
	d.keys("enter")
	if p := progressLine(d); strings.Contains(p, "\x1b") || !strings.Contains(p, "bad") {
		t.Fatalf("progress line = %q", p)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestFR_SFTP_024_SkippedWarning(t *testing.T) {
	b := approvedBrowser(1)
	b.SetListingNotes(app.Remote, "/home/pi", 2, false)
	d, _ := sftpOpen(t, b)
	if p := progressLine(d); p != "⚠ 2 entries with unsafe names not shown" {
		t.Fatalf("progress line = %q", p)
	}
	b.SetListingNotes(app.Remote, "/home/pi", 1, true)
	d.clock = d.clock.Add(time.Minute)
	d.keys("R")
	if p := progressLine(d); p != "⚠ 1 entry with unsafe names not shown; listing stopped at 100,000 entries" {
		t.Fatalf("progress line = %q", p)
	}
}
