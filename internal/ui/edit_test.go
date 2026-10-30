package ui

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/app/apptest"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/editconflict"
	"github.com/RafaelXokito/sshelf/internal/ui/sftpview"
)

// Edit locally in the root model with the fake browser (FR-SFTP-019,
// FR-SFTP-020, design §8.10). No editor ever runs: runEditor is replaced
// by a function that records the path and answers with editorErr.

type fakeEditor struct {
	paths []string
	err   error
}

// editTab is the approved layout's SFTP tab with a fake editor.
func editTab(t *testing.T) (*driver, *apptest.FakeBrowser, *fakeEditor) {
	t.Helper()
	d, b := approvedTab(t)
	fe := &fakeEditor{}
	m := d.root()
	m.runEditor = func(path string, done func(error) tea.Msg) tea.Cmd {
		fe.paths = append(fe.paths, path)
		err := fe.err
		return func() tea.Msg { return done(err) }
	}
	d.m = m
	return d, b, fe
}

// cursorTo moves the focused pane's cursor to name.
func cursorTo(t *testing.T, d *driver, name string) {
	t.Helper()
	d.keys("g")
	for range 20 {
		if _, _, cur := where(t, d); cur == name {
			return
		}
		d.keys("j")
	}
	t.Fatalf("%s not found in the pane", name)
}

func wantCalls(t *testing.T, b *apptest.FakeBrowser, want ...string) {
	t.Helper()
	if got := b.EditCalls(); !slices.Equal(got, want) {
		t.Fatalf("edit calls = %q, want %q", got, want)
	}
}

func wantProgress(t *testing.T, d *driver, want string) {
	t.Helper()
	if p := progressLine(d); p != want {
		t.Fatalf("progress line = %q, want %q", p, want)
	}
}

func TestFR_SFTP_019_EditSavesAndReportsUnchanged(t *testing.T) {
	d, b, fe := editTab(t)
	cursorTo(t, d, ".bashrc")
	b.QueueEdit(apptest.EditStep{Result: app.EditResult{Outcome: app.EditSaved, Size: 3_000}})
	lists := b.Lists()
	d.keys("e")
	wantCalls(t, b, "fetch /home/pi/.bashrc", "finish .bashrc")
	if !slices.Equal(fe.paths, []string{"/fake-tmp/sshelf-edit-1/.bashrc"}) {
		t.Fatalf("editor ran on %q", fe.paths)
	}
	wantProgress(t, d, "saved .bashrc (3 KB)")
	if b.Lists() <= lists {
		t.Fatal("the panes were not refreshed after the upload")
	}
	if d.root().editing {
		t.Fatal("still editing")
	}

	d.keys("e") // the fake reports unchanged by default
	wantProgress(t, d, ".bashrc: no changes")
}

func TestFR_SFTP_019_NonZeroExitNoUpload(t *testing.T) {
	d, b, fe := editTab(t)
	cursorTo(t, d, ".bashrc")
	fe.err = exec.Command("sh", "-c", "exit 1").Run() // a real *exec.ExitError; not an editor
	d.keys("e")
	wantCalls(t, b, "fetch /home/pi/.bashrc", "resolve discard .bashrc /Users/me/Downloads")
	wantProgress(t, d, "editor exited with status 1; nothing uploaded")
}

func TestFR_SFTP_019_EditorNotFound(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nano -w")
	d, b, fe := editTab(t)
	cursorTo(t, d, ".bashrc")
	fe.err = &exec.Error{Name: "nano", Err: exec.ErrNotFound}
	d.keys("e")
	wantCalls(t, b, "fetch /home/pi/.bashrc", "resolve discard .bashrc /Users/me/Downloads")
	wantProgress(t, d, "editor not found: nano")
}

func TestFR_SFTP_019_UploadFailedModal(t *testing.T) {
	d, b, _ := editTab(t)
	cursorTo(t, d, ".bashrc")
	b.QueueEdit(
		apptest.EditStep{Err: apptest.ErrFake},
		apptest.EditStep{Err: app.ErrNotConnected},
		apptest.EditStep{Result: app.EditResult{Outcome: app.EditSavedAs, SavedAs: ".bashrc.local-edit", Size: 4_000, Local: true}},
	)
	d.keys("e")
	if _, ok := top(d).(editconflict.Model); !ok {
		t.Fatalf("modal = %#v", top(d))
	}
	golden(t, "sftp-edit-upload-failed", d.m)
	d.keys("enter", "esc") // neither answers: the copy is kept until a choice
	if _, ok := top(d).(editconflict.Model); !ok {
		t.Fatal("enter or esc closed the question")
	}
	d.keys("r")
	if !strings.Contains(plain(d.view()), "not connected") {
		t.Fatalf("the retry's failure is not shown:\n%s", plain(d.view()))
	}
	d.keys("l")
	wantCalls(t, b, "fetch /home/pi/.bashrc", "finish .bashrc", "resolve retry .bashrc /Users/me/Downloads",
		"resolve save-local .bashrc /Users/me/Downloads")
	if top(d) != nil {
		t.Fatal("modal still open")
	}
	wantProgress(t, d, "saved .bashrc.local-edit (4 KB)")

	// x discards.
	b.QueueEdit(apptest.EditStep{Err: apptest.ErrFake})
	d.keys("e", "x")
	if calls := b.EditCalls(); calls[len(calls)-1] != "resolve discard .bashrc /Users/me/Downloads" {
		t.Fatalf("calls %q", calls)
	}
	wantProgress(t, d, ".bashrc: edit discarded")
}

func TestFR_SFTP_019_TooLargeRefused(t *testing.T) {
	d, b, fe := editTab(t)
	for _, tc := range []struct{ name, want string }{
		{"backup.tar.gz", "backup.tar.gz: too large to edit (over 64 MiB)"},
		{"scripts", "cannot edit a directory"},
		{"..", "cannot edit a directory"},
	} {
		cursorTo(t, d, tc.name)
		d.keys("e")
		wantProgress(t, d, tc.want)
	}
	d.keys("tab") // the local pane
	cursorTo(t, d, "notes.txt")
	d.keys("e")
	wantProgress(t, d, "e edits remote files; this is the local pane")
	wantCalls(t, b)
	if len(fe.paths) != 0 {
		t.Fatal("an editor ran")
	}

	// A refusal of the back end (a link to a large file) is shown too.
	d.keys("tab")
	cursorTo(t, d, ".bashrc")
	b.SetEditErr(errors.New("too large to edit (over 64 MiB)"))
	d.keys("e")
	wantProgress(t, d, sftpview.ErrorMark+" .bashrc: too large to edit (over 64 MiB)")
	if d.root().editing {
		t.Fatal("still editing after a refusal")
	}
}

func TestFR_SFTP_020_RemoteChangedChoices(t *testing.T) {
	for _, tc := range []struct {
		keys []string
		call string
		res  app.EditResult
		want string
	}{
		{[]string{"esc"}, "keep", app.EditResult{Outcome: app.EditDiscarded}, ".bashrc: edit discarded"},
		{[]string{"k"}, "keep", app.EditResult{Outcome: app.EditDiscarded}, ".bashrc: edit discarded"},
		{[]string{"o"}, "overwrite", app.EditResult{Outcome: app.EditSaved, Size: 4_000}, "saved .bashrc (4 KB)"},
		{[]string{"s"}, "save-remote", app.EditResult{Outcome: app.EditSavedAs, SavedAs: ".bashrc.local-edit (1)", Size: 4_000}, "saved .bashrc.local-edit (1) (4 KB)"},
	} {
		d, b, _ := editTab(t)
		cursorTo(t, d, ".bashrc")
		b.QueueEdit(apptest.EditStep{Result: app.EditResult{Outcome: app.EditRemoteChanged}}, apptest.EditStep{Result: tc.res})
		d.keys("e")
		if tc.call == "keep" && tc.keys[0] == "esc" {
			golden(t, "sftp-edit-remote-changed", d.m)
			d.keys("enter") // no default
			if _, ok := top(d).(editconflict.Model); !ok {
				t.Fatal("enter answered the question")
			}
		}
		if v := plain(d.view()); !strings.Contains(v, ".bashrc changed on the server while you edited it.") {
			t.Fatalf("question not shown:\n%s", v)
		}
		d.keys(tc.keys...)
		wantCalls(t, b, "fetch /home/pi/.bashrc", "finish .bashrc", "resolve "+tc.call+" .bashrc /Users/me/Downloads")
		wantProgress(t, d, tc.want)
		if top(d) != nil {
			t.Fatal("modal still open")
		}
	}
}

func TestFR_SFTP_020_RemoteDeleted(t *testing.T) {
	d, b, _ := editTab(t)
	cursorTo(t, d, ".bashrc")
	b.QueueEdit(apptest.EditStep{Result: app.EditResult{Outcome: app.EditRemoteGone}}, apptest.EditStep{Result: app.EditResult{Outcome: app.EditSaved, Size: 10}})
	d.keys("e")
	if v := plain(d.view()); !strings.Contains(v, ".bashrc was deleted on the server while you edited it.") {
		t.Fatalf("question not shown:\n%s", v)
	}
	d.keys("o")
	wantCalls(t, b, "fetch /home/pi/.bashrc", "finish .bashrc", "resolve overwrite .bashrc /Users/me/Downloads")
	wantProgress(t, d, "saved .bashrc (10 B)")
}

// ARCH-SFTP-006: e does nothing while a connection attempt is in
// progress (its prompts would wait for the suspended UI).
func TestARCH_SFTP_006_NoEditWhileConnecting(t *testing.T) {
	d, b, fe := editTab(t)
	m := d.root()
	m.cancel = func() {}
	d.m = m
	d.send(sftpview.EditRequestMsg{Tab: m.focusedSFTP().num, Dir: "/home/pi", Name: ".bashrc"})
	wantCalls(t, b)
	if len(fe.paths) != 0 || d.root().editing {
		t.Fatal("an edit started")
	}
}
