package paths

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestARCH_PKG_002_PathTable(t *testing.T) {
	home := func() (string, error) { return "/home/u", nil }
	for _, tc := range []struct {
		name, goos, override string
		env                  map[string]string
		want                 Dirs
	}{
		{"linux default", "linux", "", nil,
			Dirs{"/home/u/.config/sshelf", "/home/u/.local/state/sshelf"}},
		{"linux XDG", "linux", "", map[string]string{"XDG_CONFIG_HOME": "/x/cfg", "XDG_STATE_HOME": "/x/state"},
			Dirs{"/x/cfg/sshelf", "/x/state/sshelf"}},
		{"linux relative XDG ignored", "linux", "", map[string]string{"XDG_CONFIG_HOME": "rel", "XDG_STATE_HOME": "rel"},
			Dirs{"/home/u/.config/sshelf", "/home/u/.local/state/sshelf"}},
		{"macOS default", "darwin", "", nil,
			Dirs{"/home/u/Library/Application Support/sshelf", "/home/u/Library/Logs/sshelf"}},
		{"macOS explicit XDG", "darwin", "", map[string]string{"XDG_CONFIG_HOME": "/x/cfg", "XDG_STATE_HOME": "/x/state"},
			Dirs{"/x/cfg/sshelf", "/home/u/Library/Logs/sshelf"}},
		{"override on linux", "linux", "/o/cfg", map[string]string{"XDG_CONFIG_HOME": "/x/cfg"},
			Dirs{"/o/cfg", "/home/u/.local/state/sshelf"}},
		{"override on macOS", "darwin", "/o/cfg", nil,
			Dirs{"/o/cfg", "/home/u/Library/Logs/sshelf"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolve(tc.goos, func(k string) string { return tc.env[k] }, home, tc.override)
			if err != nil || got != tc.want {
				t.Fatalf("resolve() = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}

	// A relative override is made absolute.
	got, err := resolve("linux", func(string) string { return "" }, home, "rel")
	if err != nil || !filepath.IsAbs(got.Config) || filepath.Base(got.Config) != "rel" {
		t.Fatalf("relative override: %+v, %v", got, err)
	}

	noHome := func() (string, error) { return "", errors.New("no home") }
	if _, err := resolve("linux", func(string) string { return "" }, noHome, ""); err == nil {
		t.Fatal("no error without a home directory")
	}
	emptyHome := func() (string, error) { return "", nil }
	if _, err := resolve("linux", func(string) string { return "" }, emptyHome, ""); err == nil {
		t.Fatal("no error with an empty home directory")
	}
}

func TestKnownHostsFiles(t *testing.T) {
	got := KnownHostsFiles("/cfg", "/home/u")
	want := []string{"/cfg/known_hosts", "/home/u/.ssh/known_hosts", "/etc/ssh/ssh_known_hosts"}
	if !slices.Equal(got, want) {
		t.Fatalf("KnownHostsFiles() = %q, want %q", got, want)
	}
	if got := KnownHostsFiles("/cfg", ""); len(got) != 2 {
		t.Fatalf("without home: %q", got)
	}
	if ConfigFile("/cfg") != "/cfg/config.json" || KnownHostsFile("/cfg") != "/cfg/known_hosts" {
		t.Fatal("ConfigFile or KnownHostsFile")
	}
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestEnsureDirLeavesExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sshelf")
	if created, err := EnsureDir(dir); err != nil || !created {
		t.Fatalf("EnsureDir(new) = %v, %v", created, err)
	}
	if created, err := EnsureDir(dir); err != nil || created {
		t.Fatalf("EnsureDir(existing) = %v, %v", created, err)
	}
}

func TestSEC_FILE_002_RestoresLooseModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sshelf")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{ConfigFile(dir), KnownHostsFile(dir)} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Set the loose modes explicitly, past the umask.
	for path, m := range map[string]fs.FileMode{dir: 0o755, ConfigFile(dir): 0o644, KnownHostsFile(dir): 0o640} {
		if err := os.Chmod(path, m); err != nil {
			t.Fatal(err)
		}
	}
	fixed, err := CheckModes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{dir, ConfigFile(dir), KnownHostsFile(dir)}; !slices.Equal(fixed, want) {
		t.Fatalf("fixed = %q, want %q", fixed, want)
	}
	if m := mode(t, dir); m != DirMode {
		t.Errorf("dir mode %o", m)
	}
	for _, f := range []string{ConfigFile(dir), KnownHostsFile(dir)} {
		if m := mode(t, f); m != FileMode {
			t.Errorf("%s mode %o", f, m)
		}
	}
	// Nothing to repair the second time, and a stricter mode is kept.
	if err := os.Chmod(ConfigFile(dir), 0o400); err != nil {
		t.Fatal(err)
	}
	if fixed, err := CheckModes(dir); err != nil || len(fixed) != 0 {
		t.Fatalf("second CheckModes = %q, %v", fixed, err)
	}
	if m := mode(t, ConfigFile(dir)); m != 0o400 {
		t.Fatalf("0400 changed to %o", m)
	}
}

func TestCheckModesSkipsMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if fixed, err := CheckModes(dir); err != nil || len(fixed) != 0 {
		t.Fatalf("CheckModes(missing) = %q, %v", fixed, err)
	}
}
