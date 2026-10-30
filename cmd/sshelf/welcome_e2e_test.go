//go:build unix

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/creack/pty"
)

// startFresh starts sshelf in a PTY with an empty HOME and no sshelf config
// directory. setup, when not nil, prepares HOME first.
func startFresh(t *testing.T, setup func(home string)) *slice {
	t.Helper()
	var setupDirs func(home, xdg string)
	if setup != nil {
		setupDirs = func(home, _ string) { setup(home) }
	}
	return startFreshBin(t, sshelfBinary(t), setupDirs)
}

// startFreshBin is startFresh for bin, with env added to sshelf's minimal
// environment. setup, when not nil, prepares HOME and XDG_CONFIG_HOME
// first.
func startFreshBin(t *testing.T, bin string, setup func(home, xdg string), env ...string) *slice {
	t.Helper()
	home, xdg := t.TempDir(), t.TempDir()
	if setup != nil {
		setup(home, xdg)
	}
	master, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no PTY available: %v", err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	s := &slice{t: t, master: master, tty: tty, out: &syncBuffer{}, exited: make(chan struct{}), cfgDir: filepath.Join(xdg, "sshelf"), home: home, state: stateDir(home)}
	if _, err := exec.LookPath("stty"); err == nil {
		s.before = stty(t, tty) // for checkCrash
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(s.out, master)
	}()
	s.cmd = exec.Command(bin) //nolint:gosec // a binary this test run built
	s.cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + xdg, "TERM=xterm-256color", "PATH=" + os.Getenv("PATH")}
	s.cmd.Env = append(s.cmd.Env, env...)
	s.cmd.Stdin, s.cmd.Stdout, s.cmd.Stderr = tty, tty, tty
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.exitErr = s.cmd.Wait(); close(s.exited) }()
	t.Cleanup(func() {
		select {
		case <-s.exited:
		default:
			_ = s.cmd.Process.Kill()
			<-s.exited
		}
		_ = tty.Close()
		_ = master.Close()
		<-drained
	})
	return s
}

func TestFR_CFG_001_StartsWithoutConfigDir(t *testing.T) {
	// The absence is checked in setup, before the launch: after it, sshelf
	// may already have created the directory (this raced on CI).
	s := startFreshBin(t, sshelfBinary(t), func(_, xdg string) {
		if _, err := os.Stat(filepath.Join(xdg, "sshelf")); !os.IsNotExist(err) {
			t.Fatalf("config dir exists before launch: %v", err)
		}
	})
	s.waitText(0, "Import from ~/.ssh/config")
	s.waitText(0, "Add host")
	st, err := os.Stat(s.cfgDir)
	if err != nil {
		t.Fatalf("config dir not created: %v", err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("config dir mode %04o, want 0700", st.Mode().Perm())
	}
	s.quit()
}

// TestFR_IMPORT_001_E2EImportFromWelcome imports a fixture ~/.ssh/config
// from the Welcome screen and lands on a populated host list.
func TestFR_IMPORT_001_E2EImportFromWelcome(t *testing.T) {
	const sshConfig = "Host web\n  HostName 10.0.0.10\n  User ubuntu\n  IdentityFile ~/.ssh/id_ed25519\n\n" +
		"Host db\n  HostName db.example\n  Port 5432\n\nHost bastion\n  ProxyJump jump\n"
	s := startFresh(t, func(home string) {
		if err := os.Mkdir(filepath.Join(home, ".ssh"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(sshConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	off := s.waitText(0, "Import from ~/.ssh/config")
	s.typeKeys("I")
	s.waitText(off, "Created 3 hosts and 1 identity.")
	off = s.waitText(off, "bastion: ProxyJump")
	s.typeKeys("\r")
	s.waitText(off, "db db.example:5432")
	s.waitText(off, "web ubuntu@10.0.0.10")
	s.quit()
	cfg, err := os.ReadFile(filepath.Join(s.cfgDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), filepath.Join(s.home, ".ssh", "id_ed25519")) {
		t.Fatalf("config.json lacks the key path:\n%s", cfg)
	}
}
