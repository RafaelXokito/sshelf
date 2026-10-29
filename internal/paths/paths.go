package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Modes of everything sshelf creates (SEC-FILE-001).
const (
	DirMode  fs.FileMode = 0o700
	FileMode fs.FileMode = 0o600
)

// Dirs are sshelf's directories (ADR-005).
type Dirs struct {
	Config string // config.json and known_hosts
	State  string // log and crash reports
}

// Resolve returns sshelf's directories for the current user. It creates
// nothing. A non-empty override replaces the config directory; it is the
// only override (tests, and --config in v1).
//
//	Config, Linux: $XDG_CONFIG_HOME/sshelf, default ~/.config/sshelf
//	Config, macOS: ~/Library/Application Support/sshelf, or
//	               $XDG_CONFIG_HOME/sshelf when that variable is explicitly set
//	State, Linux:  $XDG_STATE_HOME/sshelf, default ~/.local/state/sshelf
//	State, macOS:  ~/Library/Logs/sshelf
func Resolve(override string) (Dirs, error) {
	return resolve(runtime.GOOS, os.Getenv, os.UserHomeDir, override)
}

func resolve(goos string, getenv func(string) string, home func() (string, error), override string) (Dirs, error) {
	h, err := home()
	if err == nil && h == "" {
		err = errors.New("home directory unknown")
	}
	if err != nil {
		return Dirs{}, fmt.Errorf("paths: %w", err)
	}
	// The XDG spec says relative values are invalid and must be ignored.
	xdg := func(name string) string {
		if v := getenv(name); filepath.IsAbs(v) {
			return filepath.Join(v, "sshelf")
		}
		return ""
	}

	var d Dirs
	if goos == "darwin" {
		d.Config = filepath.Join(h, "Library", "Application Support", "sshelf")
		d.State = filepath.Join(h, "Library", "Logs", "sshelf")
	} else {
		d.Config = filepath.Join(h, ".config", "sshelf")
		d.State = filepath.Join(h, ".local", "state", "sshelf")
		if s := xdg("XDG_STATE_HOME"); s != "" {
			d.State = s
		}
	}
	if c := xdg("XDG_CONFIG_HOME"); c != "" {
		d.Config = c
	}
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return Dirs{}, fmt.Errorf("paths: %w", err)
		}
		d.Config = abs
	}
	return d, nil
}

// ConfigFile returns the path of config.json in dir.
func ConfigFile(dir string) string { return filepath.Join(dir, "config.json") }

// KnownHostsFile returns the path of sshelf's own known_hosts in dir, the
// only known_hosts file sshelf writes (D-102).
func KnownHostsFile(dir string) string { return filepath.Join(dir, "known_hosts") }

// KnownHostsFiles returns the known_hosts files used to verify host keys, in
// order: sshelf's own file in dir, then ~/.ssh/known_hosts (skipped when
// home is ""), then /etc/ssh/ssh_known_hosts (ADR-004). sshelf only ever
// writes the first.
func KnownHostsFiles(dir, home string) []string {
	files := []string{KnownHostsFile(dir)}
	if home != "" {
		files = append(files, filepath.Join(home, ".ssh", "known_hosts"))
	}
	return append(files, "/etc/ssh/ssh_known_hosts")
}

// EnsureDir creates dir, and any missing parent, with mode 0700 whatever the
// umask (SEC-FILE-001). It reports whether dir was created. An existing
// directory is left alone; CheckModes repairs its mode.
func EnsureDir(dir string) (created bool, err error) {
	if _, err := os.Stat(dir); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return false, err
	}
	// MkdirAll applies the umask; set the mode explicitly.
	return true, os.Chmod(dir, DirMode)
}

// CheckModes makes the config directory 0700 and its config.json and
// known_hosts 0600 when group or others have any access (SEC-FILE-002).
// Missing files are skipped. It returns the paths it repaired, so the caller
// can show a one-line notice; a second call returns none.
func CheckModes(dir string) (fixed []string, err error) {
	for _, f := range []struct {
		path string
		mode fs.FileMode
	}{
		{dir, DirMode},
		{ConfigFile(dir), FileMode},
		{KnownHostsFile(dir), FileMode},
	} {
		st, err := os.Stat(f.path) // follows symlinks, like Chmod
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fixed, err
		}
		if st.Mode().Perm()&0o077 == 0 {
			continue
		}
		if err := os.Chmod(f.path, f.mode); err != nil {
			return fixed, err
		}
		fixed = append(fixed, f.path)
	}
	return fixed, nil
}
