package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDir returns sshelf's config directory (ADR-005). It does not create
// it.
//
//	Linux: $XDG_CONFIG_HOME/sshelf, default ~/.config/sshelf
//	macOS: ~/Library/Application Support/sshelf, or $XDG_CONFIG_HOME/sshelf
//	       when that variable is explicitly set
func ConfigDir() (string, error) {
	return configDir(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

func configDir(goos string, getenv func(string) string, home func() (string, error)) (string, error) {
	// The XDG spec says relative values are invalid and must be ignored.
	if x := getenv("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "sshelf"), nil
	}
	h, err := home()
	if err != nil {
		return "", err
	}
	if h == "" {
		return "", errors.New("paths: home directory unknown")
	}
	if goos == "darwin" {
		return filepath.Join(h, "Library", "Application Support", "sshelf"), nil
	}
	return filepath.Join(h, ".config", "sshelf"), nil
}

// ConfigFile returns the path of config.json in dir.
func ConfigFile(dir string) string { return filepath.Join(dir, "config.json") }

// KnownHostsFiles returns the known_hosts files used to verify host keys, in
// order: sshelf's own file in dir, then ~/.ssh/known_hosts (skipped when
// home is ""), then /etc/ssh/ssh_known_hosts (ADR-004). sshelf only ever
// writes the first.
func KnownHostsFiles(dir, home string) []string {
	files := []string{filepath.Join(dir, "known_hosts")}
	if home != "" {
		files = append(files, filepath.Join(home, ".ssh", "known_hosts"))
	}
	return append(files, "/etc/ssh/ssh_known_hosts")
}
