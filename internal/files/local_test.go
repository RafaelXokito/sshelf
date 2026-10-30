package files

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// tree makes a temp dir with a target folder dl and, beside it, a folder
// outside holding secret.txt, the file a symlink in dl points at.
func tree(t *testing.T) (dl, outside, secret string) {
	t.Helper()
	base := t.TempDir()
	dl = filepath.Join(base, "dl")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{dl, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	secret = filepath.Join(outside, "secret.txt")
	writeFile(t, secret, "do not touch")
	return dl, outside, secret
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func openDir(t *testing.T, path string) *LocalDir {
	t.Helper()
	d, err := OpenLocalDir(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// download writes content to name in d the way a transfer does.
func download(d *LocalDir, name, content string, mode fs.FileMode, mtime time.Time) error {
	f, err := d.CreatePart(name)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	return d.Finish(f, name, mode, mtime)
}

func withUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

// SEC-SFTP-002 AC1: overwriting a symlink replaces the link and leaves its
// target alone.
func TestSEC_SFTP_002_SymlinkTargetUnchanged(t *testing.T) {
	dl, _, secret := tree(t)
	symlink(t, secret, filepath.Join(dl, "notes.txt"))
	d := openDir(t, dl)

	if err := download(d, "notes.txt", "new notes", 0o644, time.Now()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dl, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("notes.txt is %v, want a regular file", info.Mode())
	}
	if got := readFile(t, filepath.Join(dl, "notes.txt")); got != "new notes" {
		t.Errorf("notes.txt = %q", got)
	}
	if got := readFile(t, secret); got != "do not touch" {
		t.Errorf("link target changed: %q", got)
	}
}

// SEC-SFTP-002 AC2: a symlink where a directory is expected is never entered.
func TestSEC_SFTP_002_SymlinkDirNotEntered(t *testing.T) {
	dl, outside, _ := tree(t)
	symlink(t, outside, filepath.Join(dl, "photos"))
	// A link to a directory inside the root is refused too.
	if err := os.Mkdir(filepath.Join(dl, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	symlink(t, "real", filepath.Join(dl, "inside"))
	writeFile(t, filepath.Join(dl, "plain"), "x")
	d := openDir(t, dl)

	for _, name := range []string{"photos", "inside", "plain"} {
		sub, err := d.Subdir(name)
		if !errors.Is(err, ErrNotDir) {
			if sub != nil {
				_ = sub.Close()
			}
			t.Errorf("Subdir(%s) = %v, want %v", name, err, ErrNotDir)
		}
	}
	ents, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Errorf("outside has %d entries, want only secret.txt", len(ents))
	}
	if ents, _ := os.ReadDir(filepath.Join(dl, "real")); len(ents) != 0 {
		t.Errorf("real has %d entries, want 0", len(ents))
	}

	// A new subdirectory gets its own root; an existing one is reused.
	for range 2 {
		sub, err := d.Subdir("new")
		if err != nil {
			t.Fatal(err)
		}
		if err := download(sub, "a.txt", "a", 0o600, time.Now()); err != nil && !errors.Is(err, fs.ErrExist) {
			t.Fatal(err)
		}
		_ = sub.Close()
	}
	if got := readFile(t, filepath.Join(dl, "new", "a.txt")); got != "a" {
		t.Errorf("new/a.txt = %q", got)
	}
	// Names that are not one element are refused.
	for _, name := range []string{"..", ".", "", "a/b", "../outside", "x\x00"} {
		if sub, err := d.Subdir(name); err == nil {
			_ = sub.Close()
			t.Errorf("Subdir(%q) succeeded", name)
		}
		if f, err := d.CreatePart(name); err == nil {
			_ = f.Close()
			t.Errorf("CreatePart(%q) succeeded", name)
		}
	}
}

// SEC-SFTP-002 AC3: a part file that is a symlink is not resumed through
// nor overwritten.
func TestSEC_SFTP_002_PartSymlinkRefused(t *testing.T) {
	dl, _, secret := tree(t)
	symlink(t, secret, filepath.Join(dl, "big.iso"+PartSuffix))
	d := openDir(t, dl)

	if f, err := d.OpenPart("big.iso"); !errors.Is(err, ErrUnsafePart) {
		if f != nil {
			_ = f.Close()
		}
		t.Fatalf("OpenPart = %v, want %v", err, ErrUnsafePart)
	}
	if ErrUnsafePart.Error() != "unsafe part file" {
		t.Errorf("message %q", ErrUnsafePart)
	}
	if f, err := d.CreatePart("big.iso"); err == nil {
		_ = f.Close()
		t.Fatal("CreatePart wrote over a symlink")
	}
	// A dangling link is refused by O_EXCL too.
	symlink(t, filepath.Join(dl, "nowhere"), filepath.Join(dl, "x"+PartSuffix))
	if f, err := d.CreatePart("x"); err == nil {
		_ = f.Close()
		t.Fatal("CreatePart followed a dangling link")
	}
	if _, err := os.Lstat(filepath.Join(dl, "nowhere")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("dangling link target was created")
	}
	// Removing the part removes the link only.
	if err := d.RemovePart("big.iso"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, secret); got != "do not touch" {
		t.Errorf("link target changed: %q", got)
	}
	if err := d.RemovePart("big.iso"); err != nil {
		t.Errorf("RemovePart of a missing part: %v", err)
	}
}

// SEC-SFTP-002 AC4: swapping the part for a symlink, or for another file,
// between the check and the open fails the open.
func TestSEC_SFTP_002_SwapRace(t *testing.T) {
	dl, _, secret := tree(t)
	part := filepath.Join(dl, "big.iso"+PartSuffix)
	inner := filepath.Join(dl, "inner.txt")
	writeFile(t, inner, "inner")
	d := openDir(t, dl)
	t.Cleanup(func() { testHookOpenPart = nil })

	swaps := map[string]func(){
		"symlink": func() {
			if err := os.Remove(part); err != nil {
				t.Error(err)
			}
			symlink(t, secret, part)
		},
		"symlink inside the root": func() {
			if err := os.Remove(part); err != nil {
				t.Error(err)
			}
			symlink(t, "inner.txt", part)
		},
		"other file": func() {
			if err := os.Remove(part); err != nil {
				t.Error(err)
			}
			writeFile(t, part, "other")
		},
	}
	for name, swap := range swaps {
		t.Run(name, func(t *testing.T) {
			_ = os.Remove(part)
			writeFile(t, part, "half")
			testHookOpenPart = swap
			f, err := d.OpenPart("big.iso")
			testHookOpenPart = nil
			if !errors.Is(err, ErrUnsafePart) {
				if f != nil {
					_, _ = f.WriteString("evil")
					_ = f.Close()
				}
				t.Fatalf("OpenPart = %v, want %v", err, ErrUnsafePart)
			}
			if got := readFile(t, secret); got != "do not touch" {
				t.Errorf("link target changed: %q", got)
			}
			if got := readFile(t, inner); got != "inner" {
				t.Errorf("inner link target changed: %q", got)
			}
		})
	}

	// Without a swap the part is resumed at its end.
	_ = os.Remove(part)
	writeFile(t, part, "half")
	f, err := d.OpenPart("big.iso")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("+rest"); err != nil {
		t.Fatal(err)
	}
	if err := d.Finish(f, "big.iso", 0o644, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dl, "big.iso")); got != "half+rest" {
		t.Errorf("big.iso = %q", got)
	}
}

// SEC-SFTP-002 AC5, FR-SFTP-011: deleting a directory removes a link to a
// directory inside it, not the link's target.
func TestSEC_SFTP_002_DeleteKeepsLinkTarget(t *testing.T) {
	dl, outside, secret := tree(t)
	victim := filepath.Join(dl, "victim")
	if err := os.MkdirAll(filepath.Join(victim, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(victim, "sub", "f"), "f")
	symlink(t, outside, filepath.Join(victim, "link"))
	symlink(t, secret, filepath.Join(dl, "filelink"))
	writeFile(t, filepath.Join(dl, "keep"), "k")

	n, err := LocalFS{}.Delete(context.Background(), dl, []string{"victim", "filelink"})
	if err != nil || n != 2 {
		t.Fatalf("Delete = %d, %v", n, err)
	}
	for _, name := range []string{"victim", "filelink"} {
		if _, err := os.Lstat(filepath.Join(dl, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s still exists", name)
		}
	}
	if got := readFile(t, secret); got != "do not touch" {
		t.Errorf("link target changed: %q", got)
	}
	if got := readFile(t, filepath.Join(dl, "keep")); got != "k" {
		t.Errorf("keep = %q", got)
	}
	// Unsafe names stop the delete before anything outside is touched.
	n, err = LocalFS{}.Delete(context.Background(), dl, []string{"keep", "../outside"})
	if n != 1 || !errors.Is(err, ErrBadName) {
		t.Errorf("Delete = %d, %v; want 1, %v", n, err, ErrBadName)
	}
	if got := readFile(t, secret); got != "do not touch" {
		t.Errorf("outside changed: %q", got)
	}
}

// SEC-SFTP-003: the part file is 0600 while written, the final name does
// not exist until the end, and the end sets mode & 0777 and the mtime.
func TestSEC_SFTP_003_PartModeAndRename(t *testing.T) {
	withUmask(t, 0)
	dl, _, _ := tree(t)
	d := openDir(t, dl)
	final := filepath.Join(dl, "report.pdf")
	writeFile(t, final, "old")

	f, err := d.CreatePart("report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dl, "report.pdf"+PartSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if m := info.Mode().Perm(); m != PartMode {
		t.Errorf("part mode %o, want %o", m, PartMode)
	}
	if _, err := f.WriteString("new content"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, final); got != "old" {
		t.Errorf("final name changed before the end: %q", got)
	}
	if again, err := d.CreatePart("report.pdf"); err == nil {
		_ = again.Close()
		t.Error("a second CreatePart succeeded")
	}

	mtime := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	if err := d.Finish(f, "report.pdf", fs.ModeSetuid|0o751, mtime); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(final)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != 0o751 {
		t.Errorf("final mode %v, want -rwxr-x--x", info.Mode())
	}
	if !info.ModTime().Equal(mtime) {
		t.Errorf("mtime %v, want %v", info.ModTime(), mtime)
	}
	if got := readFile(t, final); got != "new content" {
		t.Errorf("final = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dl, "report.pdf"+PartSuffix)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("part file left behind")
	}
}

// FR-SFTP-005: the local pane starts in the working directory.
func TestFR_SFTP_005_LocalStartsInWorkingDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if got := (LocalFS{}).Start(); !sameDir(t, got, dir) {
		t.Errorf("Start = %q, want %q", got, dir)
	}
}

// FR-SFTP-005: when the working directory is gone, the local pane starts
// in $HOME.
func TestFR_SFTP_005_FallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if got := (LocalFS{}).Start(); got != home {
		t.Errorf("Start = %q, want %q", got, home)
	}
	t.Setenv("HOME", filepath.Join(home, "missing"))
	if got := (LocalFS{}).Start(); got != "/" {
		t.Errorf("Start = %q, want /", got)
	}
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	return err == nil && os.SameFile(ia, ib)
}

// FR-SFTP-012: a local mkdir is 0755 before the umask and refuses an
// existing name.
func TestFR_SFTP_012_LocalMkdirMode(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		umask int
		want  fs.FileMode
	}{{0o000, 0o755}, {0o022, 0o755}, {0o077, 0o700}} {
		withUmask(t, tc.umask)
		dir := t.TempDir()
		if err := (LocalFS{}).Mkdir(ctx, dir, "new"); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(filepath.Join(dir, "new"))
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != tc.want {
			t.Errorf("umask %03o: mode %v, want %o", tc.umask, info.Mode(), tc.want)
		}
		if err := (LocalFS{}).Mkdir(ctx, dir, "new"); !errors.Is(err, ErrExists) || err.Error() != "already exists" {
			t.Errorf("second Mkdir = %v, want %v", err, ErrExists)
		}
		// A dangling link counts as existing and is not followed.
		symlink(t, filepath.Join(dir, "target"), filepath.Join(dir, "dangling"))
		if err := (LocalFS{}).Mkdir(ctx, dir, "dangling"); !errors.Is(err, ErrExists) {
			t.Errorf("Mkdir over a dangling link = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "target")); !errors.Is(err, fs.ErrNotExist) {
			t.Error("Mkdir followed a dangling link")
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", "bell\x07", "esc\x1b[31m", "c1\u009b", "nul\x00"} {
		if err := (LocalFS{}).Mkdir(ctx, t.TempDir(), name); !errors.Is(err, ErrBadName) {
			t.Errorf("Mkdir(%q) = %v, want %v", name, err, ErrBadName)
		}
	}
}

// FR-SFTP-010 (local back end): rename never replaces an existing entry.
func TestFR_SFTP_010_LocalRenameRefusesExisting(t *testing.T) {
	ctx := context.Background()
	dl, _, secret := tree(t)
	writeFile(t, filepath.Join(dl, "a"), "a")
	writeFile(t, filepath.Join(dl, "b"), "b")
	symlink(t, secret, filepath.Join(dl, "link"))

	for _, to := range []string{"b", "link"} {
		if err := (LocalFS{}).Rename(ctx, dl, "a", to); !errors.Is(err, ErrExists) {
			t.Errorf("Rename(a, %s) = %v, want %v", to, err, ErrExists)
		}
	}
	if err := (LocalFS{}).Rename(ctx, dl, "a", "../outside/a"); !errors.Is(err, ErrBadName) {
		t.Errorf("Rename out of dir = %v", err)
	}
	if err := (LocalFS{}).Rename(ctx, dl, "a", "c"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dl, "c")); got != "a" {
		t.Errorf("c = %q", got)
	}
	if got := readFile(t, filepath.Join(dl, "b")); got != "b" {
		t.Errorf("b = %q", got)
	}
}

// ARCH-SFTP-002 (local side): a listing is Lstat-based, sanitised and
// capped.
func TestARCH_SFTP_002_LocalListing(t *testing.T) {
	ctx := context.Background()
	dl, outside, _ := tree(t)
	writeFile(t, filepath.Join(dl, "file.txt"), "12345")
	if err := os.Mkdir(filepath.Join(dl, "dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(dl, "link"))
	writeFile(t, filepath.Join(dl, "esc\x1b[31mred\ttab"), "")

	l, err := LocalFS{}.List(ctx, dl)
	if err != nil {
		t.Fatal(err)
	}
	if l.Side != Local || l.Dir != dl || l.Truncated || l.Skipped != 0 {
		t.Errorf("listing header %+v", l)
	}
	got := map[string]Entry{}
	for _, e := range l.Entries {
		got[e.Name] = e
	}
	if e := got["file.txt"]; e.Kind != File || e.Size != 5 || e.Mode != 0o600 || e.Display != "file.txt" {
		t.Errorf("file.txt = %+v", e)
	}
	if e := got["dir"]; e.Kind != Dir || e.Mode != 0o750 {
		t.Errorf("dir = %+v", e)
	}
	if e := got["link"]; e.Kind != Symlink || e.Size != int64(len(outside)) {
		t.Errorf("link = %+v (want the link's own size)", e)
	}
	if e := got["esc\x1b[31mred\ttab"]; e.Display != "escred�tab" {
		t.Errorf("display = %q", e.Display)
	}

	t.Setenv("HOME", filepath.Dir(dl))
	if l, _ := (LocalFS{}).List(ctx, dl); l.Title != "~/dl" {
		t.Errorf("title %q, want ~/dl", l.Title)
	}

	old := listCap
	listCap = 3
	t.Cleanup(func() { listCap = old })
	l, err = LocalFS{}.List(ctx, dl)
	if err != nil || len(l.Entries) != 3 || !l.Truncated {
		t.Errorf("capped listing: %d entries, truncated %v, %v", len(l.Entries), l.Truncated, err)
	}
	listCap = 4
	if l, _ := (LocalFS{}).List(ctx, dl); len(l.Entries) != 4 || l.Truncated {
		t.Errorf("exactly at cap: %d entries, truncated %v", len(l.Entries), l.Truncated)
	}

	if e, err := (LocalFS{}).Stat(ctx, filepath.Join(dl, "link")); err != nil || e.Kind != Dir || e.Name != "link" {
		t.Errorf("Stat(link) = %+v, %v; want a dir named link", e, err)
	}
}

// FR-SFTP-011 (local back end): the count does not follow links, caps the
// depth and stops early.
func TestFR_SFTP_011_LocalCount(t *testing.T) {
	ctx := context.Background()
	dl, outside, _ := tree(t)
	for i := range 5 {
		writeFile(t, filepath.Join(outside, "o"+strconv.Itoa(i)), "")
	}
	deep := dl
	for range MaxDepth + 3 {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(dl, "link"))
	writeFile(t, filepath.Join(dl, "f"), "")

	if n, err := (LocalFS{}).Count(ctx, dl, 1000, false); err != nil || n != 3 {
		t.Errorf("shallow count = %d, %v; want 3", n, err)
	}
	// d, link, f, then 63 more levels of d.
	if n, err := (LocalFS{}).Count(ctx, dl, 1000, true); err != nil || n != 3+MaxDepth-1 {
		t.Errorf("deep count = %d, %v; want %d", n, err, 3+MaxDepth-1)
	}
	if n, err := (LocalFS{}).Count(ctx, dl, 10, true); err != nil || n != 10 {
		t.Errorf("stopped count = %d, %v; want 10", n, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := (LocalFS{}).Count(cctx, dl, 1000, true); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled count = %v", err)
	}
}

// pathWrites lists the calls in a Go source file that write by path
// through os, syscall or unix instead of through an os.Root.
func pathWrites(t *testing.T, name string, src any) []string {
	t.Helper()
	writes := map[string]bool{
		"OpenFile": true, "Create": true, "WriteFile": true, "Mkdir": true,
		"MkdirAll": true, "MkdirTemp": true, "CreateTemp": true, "Rename": true,
		"Remove": true, "RemoveAll": true, "Chmod": true, "Chtimes": true,
		"Chown": true, "Lchown": true, "Symlink": true, "Link": true, "Truncate": true,
		"Open": true, "Unlink": true, "Rmdir": true, "Utimes": true,
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && (id.Name == "syscall" || id.Name == "unix" || id.Name == "os" && sel.Sel.Name != "Open") && writes[sel.Sel.Name] {
			found = append(found, fset.Position(sel.Pos()).String()+": "+id.Name+"."+sel.Sel.Name)
		}
		return true
	})
	return found
}

// T-047 done-when: no write in local.go takes a path string to an os
// function; every write goes through an os.Root (SEC-SFTP-002).
func TestSEC_SFTP_002_WritesOnlyThroughRoot(t *testing.T) {
	for _, w := range pathWrites(t, "local.go", nil) {
		t.Errorf("%s writes by path; use the os.Root", w)
	}
	bad := "package p\nimport \"os\"\nfunc f() { _ = os.Remove(\"x\"); _, _ = os.OpenFile(\"y\", 0, 0) }\n"
	if got := pathWrites(t, "bad.go", bad); len(got) != 2 {
		t.Errorf("the check found %v in a file with two path writes", got)
	}
}
