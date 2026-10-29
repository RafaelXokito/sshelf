package main

import (
	"bytes"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/paths"
)

// useLogging runs setupLogging with SSHELF_DEBUG set to debug and restores
// the default logger afterwards.
func useLogging(t *testing.T, debug, stateDir string) func() error {
	t.Helper()
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	getenv := func(k string) string {
		if k == debugEnv {
			return debug
		}
		return ""
	}
	closeLog, err := setupLogging(getenv, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeLog() })
	return closeLog
}

func TestSEC_LOG_003_NoFileByDefault(t *testing.T) {
	for _, v := range []string{"", "0", "true", "yes", " 1"} {
		stateDir := filepath.Join(t.TempDir(), "state", "sshelf")
		useLogging(t, v, stateDir)
		slog.Error("connect: something", "host", "h")
		log.Print("from the log package")
		if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
			t.Errorf("SSHELF_DEBUG=%q: state dir exists (err %v)", v, err)
		}
	}
}

func TestSEC_LOG_003_EnabledBySSHELF_DEBUG(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state", "sshelf")
	closeLog := useLogging(t, "1", stateDir)
	slog.Debug("connect: dialling", "addr", "example:22")
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(paths.LogFile(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "connect: dialling") || !strings.Contains(string(b), "addr=example:22") {
		t.Fatalf("log = %q", b)
	}
	st, err := os.Stat(stateDir)
	if err != nil || st.Mode().Perm() != paths.DirMode {
		t.Fatalf("state dir: %v, mode %v", err, st.Mode())
	}
}

func TestSEC_LOG_003_Mode0600(t *testing.T) {
	stateDir := t.TempDir()
	// An existing log with a loose mode is tightened too.
	if err := os.WriteFile(paths.LogFile(stateDir), []byte("old\n"), 0o644); err != nil { //nolint:gosec // deliberately loose
		t.Fatal(err)
	}
	useLogging(t, "1", stateDir)
	slog.Info("hello")
	checkMode(t, paths.LogFile(stateDir), paths.FileMode)
}

func TestSEC_LOG_003_RotatesAt5MiB(t *testing.T) {
	stateDir := t.TempDir()
	closeLog := useLogging(t, "1", stateDir)
	logFile := paths.LogFile(stateDir)
	pad := strings.Repeat("x", 1000)
	write := func(until func() bool) {
		for i := 0; !until(); i++ {
			if i > 20000 {
				t.Fatal("log never rotated")
			}
			slog.Info("filler", "pad", pad)
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	write(func() bool { return exists(logFile + ".1") })
	first, err := os.ReadFile(logFile + ".1") //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if n := len(first); n > maxLogSize || n < maxLogSize-2000 {
		t.Fatalf("backup is %d bytes, want just under %d", n, maxLogSize)
	}
	if st, err := os.Stat(logFile); err != nil || st.Size() > 2000 {
		t.Fatalf("new log: %v, size %d", err, st.Size())
	}
	checkMode(t, logFile, paths.FileMode)
	checkMode(t, logFile+".1", paths.FileMode)

	// A second rotation replaces the backup: one .1 only.
	slog.Info("marker")
	var prev int64
	write(func() bool {
		st, err := os.Stat(logFile)
		if err != nil {
			t.Fatal(err)
		}
		rotated := st.Size() < prev
		prev = st.Size()
		return rotated
	})
	if b, err := os.ReadFile(logFile + ".1"); err != nil || !bytes.Contains(b, []byte("msg=marker")) { //nolint:gosec // test temp dir
		t.Fatalf("second backup lacks the marker (err %v)", err)
	}
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(logFile + "*")
	if len(matches) != 2 {
		t.Fatalf("log files = %v, want the log and one backup", matches)
	}
}

func checkMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != want {
		t.Fatalf("%s mode %04o, want %04o", filepath.Base(path), got, want)
	}
}
