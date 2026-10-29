package app

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/store"
)

func permOf(t *testing.T, path string) fs.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestFR_CFG_001_FirstLaunchCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "xdg", "sshelf")
	a := New(Config{Dir: dir, Terminal: stillTerm{}})
	l, err := a.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !l.FirstRun || len(l.Hosts) != 0 || len(l.Notices) != 0 {
		t.Fatalf("Load() = %+v", l)
	}
	if m := permOf(t, dir); m != paths.DirMode {
		t.Fatalf("config dir mode %o, want 0700", m)
	}
	// Only the directory: config.json is written by the first save.
	if _, err := os.Stat(paths.ConfigFile(dir)); err == nil {
		t.Fatal("config.json created on first launch")
	}
}

func TestARCH_PKG_002_PermissionsAfterFirstSave(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sshelf")
	a := New(Config{Dir: dir, Terminal: stillTerm{}})
	if _, err := a.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap := &store.Snapshot{Data: domain.Data{Hosts: []domain.Host{{Meta: domain.Meta{ID: "h1"}, Label: "a", Hostname: "a"}}}}
	if err := store.Save(paths.ConfigFile(dir), snap); err != nil {
		t.Fatal(err)
	}
	if m := permOf(t, dir); m != paths.DirMode {
		t.Fatalf("dir mode %o", m)
	}
	if m := permOf(t, paths.ConfigFile(dir)); m != paths.FileMode {
		t.Fatalf("config.json mode %o", m)
	}
	// The startup check finds nothing to repair after a save.
	if fixed, err := paths.CheckModes(dir); err != nil || len(fixed) != 0 {
		t.Fatalf("CheckModes after save = %q, %v", fixed, err)
	}
	l, err := a.Load(context.Background())
	if err != nil || l.FirstRun || len(l.Hosts) != 1 || len(l.Notices) != 0 {
		t.Fatalf("Load after save = %+v, %v", l, err)
	}
}

func TestSEC_FILE_002_NoticeShownOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(paths.ConfigFile(dir), []byte(`{"version": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.ConfigFile(dir), 0o644); err != nil { //nolint:gosec // a deliberately loose mode to repair
		t.Fatal(err)
	}
	a := New(Config{Dir: dir, Terminal: stillTerm{}})
	l, err := a.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Notices) != 1 || !strings.Contains(l.Notices[0], "config.json") || strings.Contains(l.Notices[0], "\n") {
		t.Fatalf("notices = %q, want one line naming config.json", l.Notices)
	}
	if m := permOf(t, paths.ConfigFile(dir)); m != paths.FileMode {
		t.Fatalf("config.json mode %o after start", m)
	}
	l, err = New(Config{Dir: dir, Terminal: stillTerm{}}).Load(context.Background())
	if err != nil || len(l.Notices) != 0 {
		t.Fatalf("second start: notices = %q, %v", l.Notices, err)
	}
}
