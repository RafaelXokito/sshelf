//go:build unix

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/creack/pty"
)

// startFresh starts sshelf in a PTY with an empty HOME and no sshelf config
// directory. setup, when not nil, prepares HOME first.
func startFresh(t *testing.T, setup func(home string)) *slice {
	t.Helper()
	home, xdg := t.TempDir(), t.TempDir()
	if setup != nil {
		setup(home)
	}
	master, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no PTY available: %v", err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	s := &slice{t: t, master: master, tty: tty, out: &syncBuffer{}, exited: make(chan struct{}), cfgDir: filepath.Join(xdg, "sshelf"), home: home}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(s.out, master)
	}()
	s.cmd = exec.Command(sshelfBinary(t)) //nolint:gosec // the binary this test just built
	s.cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + xdg, "TERM=xterm-256color", "PATH=" + os.Getenv("PATH")}
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
	s := startFresh(t, nil)
	if _, err := os.Stat(s.cfgDir); !os.IsNotExist(err) {
		t.Fatalf("config dir exists before launch: %v", err)
	}
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
