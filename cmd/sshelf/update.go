package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/term"
	"github.com/RafaelXokito/sshelf/internal/ui"
	"github.com/RafaelXokito/sshelf/internal/update"
)

// runningVersion is the version this binary reports: the -X main.version
// stamp, else the module version the Go toolchain recorded (go install
// ...@vX.Y.Z), else "dev".
func runningVersion() string {
	info, _ := debug.ReadBuildInfo()
	return versionOf(version, info)
}

func versionOf(v string, info *debug.BuildInfo) string {
	if v == "" && info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	if v == "" {
		v = "dev"
	}
	return v
}

// applyFunc replaces the binary; update.Client.Apply in production.
type applyFunc func(ctx context.Context, t update.Target, out io.Writer) error

// runUpdate is `sshelf update` (FR-UPD-003). It returns the exit status.
func runUpdate(current string, exe func() (string, error), apply applyFunc, stdout, stderr io.Writer) int {
	if _, ok := update.Parse(current); !ok {
		_, _ = fmt.Fprintf(stderr, "sshelf update: this is a development build (%s), not a release; it cannot update itself.\nInstall a release from %s\n",
			domain.Sanitize(current), update.ReleasesURL)
		return 1
	}
	path, err := exe()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sshelf update: cannot find the running binary:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err = apply(ctx, update.Target{Exe: path, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}, stdout)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, update.ErrNotWritable):
		_, _ = fmt.Fprintf(stderr, "sshelf update: %s\n%s\n", domain.Sanitize(err.Error()), update.HowToUpdate)
	default:
		_, _ = fmt.Fprintf(stderr, "sshelf update: %s\nNothing was changed.\n", domain.Sanitize(err.Error()))
	}
	return 1
}

// checkVersion is the version the startup update check uses and whether
// the check runs (FR-UPD-001, D-029): only a release binary, whose version
// the release workflow sets with -X main.version (stamp), checks. A source
// build (go build, go install) never does, even when the Go toolchain
// recorded a release version such as v0.1.1 for it.
func checkVersion(getenv func(string) string, stamp string, info *debug.BuildInfo) (current string, ok bool) {
	current = versionOf(stamp, info)
	return current, stamp != "" && update.Enabled(getenv, current)
}

// checkForUpdate starts the background update check (FR-UPD-001): when a
// newer release exists, send gets ui.UpdateAvailableMsg. stamp is
// main.version and info the binary's build info (see checkVersion). It
// never blocks the caller and never reports a failure; the returned func
// stops it.
func checkForUpdate(getenv func(string) string, stamp string, info *debug.BuildInfo, configDir string, send func(any)) (cancel func()) {
	current, ok := checkVersion(getenv, stamp, info)
	if !ok {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	term.SafeGo(func() {
		if v, ok := update.New(current).Check(ctx, configDir, time.Now()); ok {
			send(ui.UpdateAvailableMsg{Version: v.String()})
		}
	}, func(any) {})
	return cancel
}
