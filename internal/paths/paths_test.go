package paths

import (
	"errors"
	"slices"
	"testing"
)

func TestConfigDir(t *testing.T) {
	home := func() (string, error) { return "/home/u", nil }
	for _, tc := range []struct {
		name, goos, xdg, want string
	}{
		{"linux default", "linux", "", "/home/u/.config/sshelf"},
		{"linux XDG", "linux", "/x/cfg", "/x/cfg/sshelf"},
		{"linux relative XDG ignored", "linux", "rel", "/home/u/.config/sshelf"},
		{"macOS default", "darwin", "", "/home/u/Library/Application Support/sshelf"},
		{"macOS explicit XDG", "darwin", "/x/cfg", "/x/cfg/sshelf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := func(k string) string {
				if k == "XDG_CONFIG_HOME" {
					return tc.xdg
				}
				return ""
			}
			got, err := configDir(tc.goos, env, home)
			if err != nil || got != tc.want {
				t.Fatalf("configDir() = %q, %v; want %q", got, err, tc.want)
			}
		})
	}

	noHome := func() (string, error) { return "", errors.New("no home") }
	if _, err := configDir("linux", func(string) string { return "" }, noHome); err == nil {
		t.Fatal("no error without a home directory")
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
	if ConfigFile("/cfg") != "/cfg/config.json" {
		t.Fatal("ConfigFile")
	}
}
