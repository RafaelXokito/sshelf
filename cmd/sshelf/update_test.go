//go:build unix

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RafaelXokito/sshelf/internal/update"
)

func TestFR_UPD_003_E2EDevBuildRefuses(t *testing.T) {
	// The E2E binary is stamped v0.0.0-e2e, which is not a release.
	code, out, errOut := runSshelf(t, nil, nil, "update")
	if code != 1 || out != "" || !strings.Contains(errOut, "development build (v0.0.0-e2e)") || !strings.Contains(errOut, update.ReleasesURL) {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	for _, args := range [][]string{{"update", "now"}, {"--version", "update"}} {
		if code, _, _ := runSshelf(t, nil, nil, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}

func TestFR_UPD_003_UsageListsUpdate(t *testing.T) {
	for _, want := range []string{"sshelf update", "SSHELF_NO_UPDATE_CHECK=1"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage lacks %q", want)
		}
	}
}

func TestFR_UPD_003_RunUpdate(t *testing.T) {
	exe := func() (string, error) { return "/opt/sshelf/bin/sshelf", nil }
	t.Run("success", func(t *testing.T) {
		var got update.Target
		apply := func(_ context.Context, tg update.Target, out io.Writer) error {
			got = tg
			_, _ = io.WriteString(out, "Updated.\n")
			return nil
		}
		var out, errOut bytes.Buffer
		if code := runUpdate("v0.1.0", exe, apply, &out, &errOut); code != 0 || out.String() != "Updated.\n" || errOut.Len() != 0 {
			t.Fatalf("code %d, stdout %q, stderr %q", code, out.String(), errOut.String())
		}
		if got != (update.Target{Exe: "/opt/sshelf/bin/sshelf", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}) {
			t.Errorf("target %+v", got)
		}
	})
	t.Run("not writable", func(t *testing.T) {
		apply := func(context.Context, update.Target, io.Writer) error {
			return fmt.Errorf("%w: /usr/local/bin is not writable", update.ErrNotWritable)
		}
		var out, errOut bytes.Buffer
		code := runUpdate("v0.1.0", exe, apply, &out, &errOut)
		for _, want := range []string{"not writable", "brew upgrade sshelf", "go install github.com/RafaelXokito/sshelf/cmd/sshelf@latest", update.ReleasesURL} {
			if !strings.Contains(errOut.String(), want) {
				t.Errorf("stderr %q lacks %q", errOut.String(), want)
			}
		}
		if code == 0 {
			t.Error("exit status 0")
		}
	})
	t.Run("failure is sanitised", func(t *testing.T) {
		apply := func(context.Context, update.Target, io.Writer) error {
			return errors.New("bad \x1b]52;c;ZXZpbA==\x07thing")
		}
		var errOut bytes.Buffer
		if code := runUpdate("v0.1.0", exe, apply, io.Discard, &errOut); code != 1 || strings.Contains(errOut.String(), "\x1b") || !strings.Contains(errOut.String(), "Nothing was changed.") {
			t.Fatalf("code %d, stderr %q", code, errOut.String())
		}
	})
	t.Run("dev build", func(t *testing.T) {
		called := false
		apply := func(context.Context, update.Target, io.Writer) error { called = true; return nil }
		var errOut bytes.Buffer
		if code := runUpdate("dev", exe, apply, io.Discard, &errOut); code != 1 || called || !strings.Contains(errOut.String(), "development build") {
			t.Fatalf("code %d, called %v, stderr %q", code, called, errOut.String())
		}
	})
}

func TestFR_UPD_001_RunningVersion(t *testing.T) {
	for _, tc := range []struct {
		v    string
		info *debug.BuildInfo
		want string
	}{
		{"v0.2.0", &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, "v0.2.0"},
		{"", &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, "v0.1.0"},
		{"", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "dev"},
		{"", nil, "dev"},
	} {
		if got := versionOf(tc.v, tc.info); got != tc.want {
			t.Errorf("versionOf(%q, %+v) = %q, want %q", tc.v, tc.info, got, tc.want)
		}
	}
}

// TestFR_UPD_001_CheckOffStartsNothing is also ARCH-UPD-002: with the
// check off, no goroutine is started.
func TestFR_UPD_001_CheckOffStartsNothing(t *testing.T) {
	var sent atomic.Int32
	send := func(any) { sent.Add(1) }
	off := func(k string) string {
		if k == update.DisableEnv {
			return "1"
		}
		return ""
	}
	// Neither starts a check, so nothing reaches the network.
	checkForUpdate(off, "v0.1.0", t.TempDir(), send)()
	checkForUpdate(func(string) string { return "" }, "dev", t.TempDir(), send)()
	if sent.Load() != 0 {
		t.Fatal("a disabled check sent a message")
	}
}
