package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/testutil/sshtest"
)

// Edit locally against sshtest (FR-SFTP-019, FR-SFTP-020, SEC-SFTP-005).

// editTemp points the OS temp directory at a new dir, so that the test
// can see every edit copy, and returns it.
func editTemp(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	return tmp
}

// editDirs lists the edit directories in tmp.
func editDirsIn(t *testing.T, tmp string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(tmp, EditDirPattern+"*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func noEditDirs(t *testing.T, tmp, when string) {
	t.Helper()
	if d := editDirsIn(t, tmp); len(d) > 0 {
		t.Fatalf("%s: edit dirs remain: %q", when, d)
	}
	editDirs.Lock()
	n := len(editDirs.dirs)
	editDirs.Unlock()
	if n != 0 {
		t.Fatalf("%s: %d dirs still registered", when, n)
	}
}

func appendTo(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // a test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func fetch(t *testing.T, b *Browser, name string) *EditCopy {
	t.Helper()
	c, err := b.EditFetch(bg, "/home/pi", name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// SEC-SFTP-005 AC1: under umask 000 the directory is 0700 and the copy
// 0600, in the OS temp directory, holding the remote file's bytes.
func TestSEC_SFTP_005_ModesUnderUmask0(t *testing.T) {
	tmp := editTemp(t)
	withUmask(t, 0)
	srv := sftpSrv(t)
	writeRemote(t, srv, "notes.txt", "hello\n")
	b := openBrowser(t, srv)
	c := fetch(t, b, "notes.txt")
	di, err := os.Stat(c.Dir())
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("dir %v, file %v", di.Mode(), fi.Mode())
	}
	if filepath.Dir(c.Dir()) != tmp || filepath.Base(c.Path) != "notes.txt" {
		t.Fatalf("copy at %s", c.Path)
	}
	if got := readFile(t, c.Path); got != "hello\n" {
		t.Fatalf("copy holds %q", got)
	}
	if _, err := b.EditFinish(bg, c); err != nil {
		t.Fatal(err)
	}
	noEditDirs(t, tmp, "unchanged")
}

// SEC-SFTP-005 AC2 (back-end endings): the directory is gone after each
// way an edit ends. The editor endings and the crash are in cmd/sshelf's
// E2E tests.
func TestSEC_SFTP_005_RemovedOnEveryPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []sshtest.Option
		edit func(t *testing.T, srv *sshtest.Server, b *Browser, c *EditCopy) string // returns the remote file's content
	}{
		{"unchanged", nil, func(t *testing.T, _ *sshtest.Server, b *Browser, c *EditCopy) string {
			res, err := b.EditFinish(bg, c)
			if err != nil || res.Outcome != EditUnchanged {
				t.Fatalf("%+v, %v", res, err)
			}
			return "v1\n"
		}},
		{"changed and uploaded", nil, func(t *testing.T, _ *sshtest.Server, b *Browser, c *EditCopy) string {
			appendTo(t, c.Path, "mine\n")
			res, err := b.EditFinish(bg, c)
			if err != nil || res.Outcome != EditSaved || res.Size != 8 {
				t.Fatalf("%+v, %v", res, err)
			}
			return "v1\nmine\n"
		}},
		{"editor failed", nil, func(t *testing.T, _ *sshtest.Server, b *Browser, c *EditCopy) string {
			appendTo(t, c.Path, "mine\n")
			if res, err := b.EditResolve(bg, c, EditDiscard, ""); err != nil || res.Outcome != EditDiscarded {
				t.Fatalf("%+v, %v", res, err)
			}
			return "v1\n"
		}},
		{"remote changed, overwrite", nil, func(t *testing.T, srv *sshtest.Server, b *Browser, c *EditCopy) string {
			changeRemote(t, srv, b, c)
			if res, err := b.EditResolve(bg, c, EditOverwrite, ""); err != nil || res.Outcome != EditSaved {
				t.Fatalf("%+v, %v", res, err)
			}
			return "v1\nmine\n"
		}},
		{"remote changed, keep remote", nil, func(t *testing.T, srv *sshtest.Server, b *Browser, c *EditCopy) string {
			changeRemote(t, srv, b, c)
			if res, err := b.EditResolve(bg, c, EditKeepRemote, ""); err != nil || res.Outcome != EditDiscarded {
				t.Fatalf("%+v, %v", res, err)
			}
			return "v1\ntheirs\n"
		}},
		{"remote changed, save mine", nil, func(t *testing.T, srv *sshtest.Server, b *Browser, c *EditCopy) string {
			changeRemote(t, srv, b, c)
			res, err := b.EditResolve(bg, c, EditSaveRemote, "")
			if err != nil || res.Outcome != EditSavedAs || res.SavedAs != "notes.txt.local-edit" {
				t.Fatalf("%+v, %v", res, err)
			}
			if got := readFile(t, srv.SFTPDir("notes.txt.local-edit")); got != "v1\nmine\n" {
				t.Fatalf("saved %q", got)
			}
			return "v1\ntheirs\n"
		}},
		{"upload failed, discarded", []sshtest.Option{sshtest.WithSFTPFault("notes.txt"+PartSuffix, 0, sshtest.FaultFail)},
			func(t *testing.T, _ *sshtest.Server, b *Browser, c *EditCopy) string {
				appendTo(t, c.Path, "mine\n")
				if _, err := b.EditFinish(bg, c); err == nil {
					t.Fatal("the upload did not fail")
				}
				if _, err := os.Stat(c.Path); err != nil {
					t.Fatalf("the copy was not kept after the failure: %v", err)
				}
				if _, err := b.EditResolve(bg, c, EditDiscard, ""); err != nil {
					t.Fatal(err)
				}
				return "v1\n"
			}},
		{"connection lost during upload", []sshtest.Option{sshtest.WithSFTPFault("notes.txt"+PartSuffix, 0, sshtest.FaultDrop)},
			func(t *testing.T, _ *sshtest.Server, b *Browser, c *EditCopy) string {
				appendTo(t, c.Path, "mine\n")
				if _, err := b.EditFinish(bg, c); err == nil {
					t.Fatal("the upload did not fail")
				}
				if _, err := b.EditResolve(bg, c, EditRetry, ""); err == nil {
					t.Fatal("the retry worked without a connection")
				}
				if _, err := b.EditResolve(bg, c, EditDiscard, ""); err != nil {
					t.Fatal(err)
				}
				return "v1\n"
			}},
		{"tab closed", nil, func(t *testing.T, _ *sshtest.Server, b *Browser, _ *EditCopy) string {
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			return "v1\n"
		}},
		{"crash handler", nil, func(_ *testing.T, _ *sshtest.Server, _ *Browser, _ *EditCopy) string {
			RemoveEditCopies()
			return "v1\n"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := editTemp(t)
			srv := sftpSrv(t, tc.opts...)
			writeRemote(t, srv, "notes.txt", "v1\n")
			b := openBrowser(t, srv)
			c := fetch(t, b, "notes.txt")
			if len(editDirsIn(t, tmp)) != 1 {
				t.Fatal("no edit dir")
			}
			want := tc.edit(t, srv, b, c)
			noEditDirs(t, tmp, tc.name)
			if got := readFile(t, srv.SFTPDir("notes.txt")); got != want {
				t.Fatalf("remote holds %q, want %q", got, want)
			}
		})
	}
}

// changeRemote edits the copy and changes the remote file, and checks
// that EditFinish asks.
func changeRemote(t *testing.T, srv *sshtest.Server, b *Browser, c *EditCopy) {
	t.Helper()
	appendTo(t, c.Path, "mine\n")
	appendTo(t, srv.SFTPDir("notes.txt"), "theirs\n")
	res, err := b.EditFinish(bg, c)
	if err != nil || res.Outcome != EditRemoteChanged {
		t.Fatalf("%+v, %v", res, err)
	}
	if _, err := os.Stat(c.Path); err != nil {
		t.Fatalf("the copy was not kept for the question: %v", err)
	}
}

// FR-SFTP-019: the upload keeps the remote file's permission bits, and a
// link is resolved with realpath, so the target is replaced and the link
// stays a link.
func TestFR_SFTP_019_UploadKeepsModeAndLink(t *testing.T) {
	editTemp(t)
	srv := sftpSrv(t)
	writeRemote(t, srv, "real.txt", "a\n")
	if err := os.Chmod(srv.SFTPDir("real.txt"), 0o640); err != nil { //nolint:gosec // a mode the upload must keep
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", srv.SFTPDir("link.txt")); err != nil {
		t.Fatal(err)
	}
	b := openBrowser(t, srv)
	c := fetch(t, b, "link.txt")
	if c.Remote != "/home/pi/real.txt" || c.Name != "link.txt" {
		t.Fatalf("copy %+v", c)
	}
	appendTo(t, c.Path, "b\n")
	if res, err := b.EditFinish(bg, c); err != nil || res.Outcome != EditSaved {
		t.Fatalf("%+v, %v", res, err)
	}
	fi, err := os.Lstat(srv.SFTPDir("real.txt"))
	if err != nil || fi.Mode().Perm() != 0o640 {
		t.Fatalf("real.txt: %v, %v", fi.Mode(), err)
	}
	if li, err := os.Lstat(srv.SFTPDir("link.txt")); err != nil || li.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("link.txt is no longer a link: %v", err)
	}
	if got := readFile(t, srv.SFTPDir("real.txt")); got != "a\nb\n" {
		t.Fatalf("real.txt = %q", got)
	}
	if _, err := os.Lstat(srv.SFTPDir("real.txt" + PartSuffix)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("part file left: %v", err)
	}
}

// FR-SFTP-019: directories and files over 64 MiB are refused before
// anything is downloaded or created.
func TestFR_SFTP_019_TooLargeRefused(t *testing.T) {
	tmp := editTemp(t)
	srv := sftpSrv(t, sshtest.WithSFTPZeroFile("big.bin", EditMaxSize+1), sshtest.WithSFTPZeroFile("ok.bin", 10))
	if err := os.MkdirAll(srv.SFTPDir("dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	b := openBrowser(t, srv)
	if _, err := b.EditFetch(bg, "/home/pi", "big.bin"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big.bin: %v", err)
	}
	if n := srv.SFTPBytesRead("big.bin"); n != 0 {
		t.Fatalf("%d bytes of big.bin read", n)
	}
	if _, err := b.EditFetch(bg, "/home/pi", "dir"); !errors.Is(err, ErrIsDir) {
		t.Fatalf("dir: %v", err)
	}
	if _, err := b.EditFetch(bg, "/home/pi", "../etc"); !errors.Is(err, ErrBadName) {
		t.Fatalf("..: %v", err)
	}
	noEditDirs(t, tmp, "refused")
	c := fetch(t, b, "ok.bin")
	if got := readFile(t, c.Path); got != strings.Repeat("\x00", 10) {
		t.Fatalf("ok.bin copy %q", got)
	}
	if _, err := b.EditResolve(bg, c, EditDiscard, ""); err != nil {
		t.Fatal(err)
	}
	noEditDirs(t, tmp, "discarded")
}

// FR-SFTP-020: each answer to the remote-changed question, and "save
// mine" picking a free name.
func TestFR_SFTP_020_RemoteChangedChoices(t *testing.T) {
	tmp := editTemp(t)
	srv := sftpSrv(t)
	writeRemote(t, srv, "notes.txt", "v1\n")
	writeRemote(t, srv, "notes.txt.local-edit", "older\n")
	b := openBrowser(t, srv)
	c := fetch(t, b, "notes.txt")
	changeRemote(t, srv, b, c)
	res, err := b.EditResolve(bg, c, EditSaveRemote, "")
	if err != nil || res.SavedAs != "notes.txt.local-edit (1)" {
		t.Fatalf("%+v, %v", res, err)
	}
	if got := readFile(t, srv.SFTPDir("notes.txt.local-edit (1)")); got != "v1\nmine\n" {
		t.Fatalf("saved %q", got)
	}
	if got := readFile(t, srv.SFTPDir("notes.txt.local-edit")); got != "older\n" {
		t.Fatalf("the existing .local-edit changed: %q", got)
	}

	// Save to the local pane's directory (the upload-failed question).
	local := t.TempDir()
	c = fetch(t, b, "notes.txt")
	appendTo(t, c.Path, "local\n")
	res, err = b.EditResolve(bg, c, EditSaveLocal, local)
	if err != nil || res.SavedAs != "notes.txt.local-edit" || !res.Local {
		t.Fatalf("%+v, %v", res, err)
	}
	if got := readFile(t, filepath.Join(local, "notes.txt.local-edit")); got != "v1\ntheirs\nlocal\n" {
		t.Fatalf("saved locally %q", got)
	}
	noEditDirs(t, tmp, "saved")
}

// FR-SFTP-020: a remote file deleted while it was edited is asked about
// the same way; Overwrite recreates it.
func TestFR_SFTP_020_RemoteDeleted(t *testing.T) {
	tmp := editTemp(t)
	srv := sftpSrv(t)
	writeRemote(t, srv, "notes.txt", "v1\n")
	b := openBrowser(t, srv)
	c := fetch(t, b, "notes.txt")
	appendTo(t, c.Path, "mine\n")
	if err := os.Remove(srv.SFTPDir("notes.txt")); err != nil {
		t.Fatal(err)
	}
	res, err := b.EditFinish(bg, c)
	if err != nil || res.Outcome != EditRemoteGone {
		t.Fatalf("%+v, %v", res, err)
	}
	if res, err = b.EditResolve(bg, c, EditOverwrite, ""); err != nil || res.Outcome != EditSaved {
		t.Fatalf("%+v, %v", res, err)
	}
	if got := readFile(t, srv.SFTPDir("notes.txt")); got != "v1\nmine\n" {
		t.Fatalf("recreated %q", got)
	}
	noEditDirs(t, tmp, "recreated")
}
