//go:build unix

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// runSshelf runs the built binary with args. stdin and stdout default to
// pipes (not terminals) when nil; stderr is always captured.
func runSshelf(t *testing.T, stdin, stdout *os.File, args ...string) (code int, out, errOut string) {
	t.Helper()
	bin := sshelfBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // the binary this test just built
	cmd.Env = []string{"HOME=" + t.TempDir(), "XDG_CONFIG_HOME=" + t.TempDir(), "TERM=xterm-256color"}
	var outBuf, errBuf bytes.Buffer
	if stdin != nil {
		cmd.Stdin = stdin
	} else {
		cmd.Stdin = strings.NewReader("")
	}
	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = &outBuf
	}
	cmd.Stderr = &errBuf
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("run sshelf %v: %v", args, err)
	}
	return code, outBuf.String(), errBuf.String()
}

func TestFR_CLI_001_Version(t *testing.T) {
	code, out, errOut := runSshelf(t, nil, nil, "--version")
	want := "sshelf " + e2eVersion + " (commit " + e2eCommit + ", built " + e2eDate + ")\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("--version: code %d, stdout %q, stderr %q; want 0, %q, empty", code, out, errOut, want)
	}

	t.Run("fallback to build info", func(t *testing.T) {
		info := &debug.BuildInfo{
			Main: debug.Module{Version: "v1.2.3"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
				{Key: "vcs.time", Value: "2026-10-29T10:00:00Z"},
				{Key: "vcs.modified", Value: "true"},
			},
		}
		for _, tc := range []struct {
			name, v, c, d string
			info          *debug.BuildInfo
			want          string
		}{
			{"ldflags win", "v9", "fff", "2027-01-01", info, "sshelf v9 (commit fff, built 2027-01-01)"},
			{"build info", "", "", "", info, "sshelf v1.2.3 (commit abc123-dirty, built 2026-10-29T10:00:00Z)"},
			{"devel build", "", "", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "sshelf dev (commit unknown, built unknown)"},
			{"no build info", "", "", "", nil, "sshelf dev (commit unknown, built unknown)"},
		} {
			if got := formatVersion(tc.v, tc.c, tc.d, tc.info); got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		}
	})
}

func TestFR_CLI_001_Help(t *testing.T) {
	for _, arg := range []string{"--help", "-help", "-h"} {
		code, out, errOut := runSshelf(t, nil, nil, arg)
		if code != 0 || out != usage || errOut != "" {
			t.Errorf("%s: code %d, stdout %q, stderr %q; want 0, usage, empty", arg, code, out, errOut)
		}
	}
}

func TestFR_CLI_001_UnknownArgExit2(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"--bogus"}, {"connect", "web-prod"}, {"--version", "extra"}} {
		code, out, errOut := runSshelf(t, nil, nil, args...)
		if code != 2 || out != "" || !strings.HasSuffix(errOut, usage) || !strings.HasPrefix(errOut, "sshelf: ") {
			t.Errorf("%v: code %d, stdout %q, stderr %q; want 2, empty, error then usage", args, code, out, errOut)
		}
	}
}

func TestFR_CLI_001_NotATerminalExit1(t *testing.T) {
	master, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no PTY available: %v", err)
	}
	defer func() { _ = master.Close(); _ = tty.Close() }()

	for _, tc := range []struct {
		name          string
		stdin, stdout *os.File
	}{
		{"neither", nil, nil},
		{"stdout not a terminal", tty, nil},
		{"stdin not a terminal", nil, tty},
	} {
		code, out, errOut := runSshelf(t, tc.stdin, tc.stdout)
		if code != 1 || out != "" || errOut != "sshelf needs an interactive terminal\n" {
			t.Errorf("%s: code %d, stdout %q, stderr %q; want 1 and the message", tc.name, code, out, errOut)
		}
	}
}
