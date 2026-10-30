// Command sshelf is a terminal UI SSH host manager.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	tea "charm.land/bubbletea/v2"
	xterm "golang.org/x/term"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/term"
	"github.com/RafaelXokito/sshelf/internal/ui"
)

const usage = `Usage: sshelf [--help] [--version]

sshelf is a terminal UI SSH host manager. Run it without arguments in an
interactive terminal to open the host list.

Options:
  --help     show this help and exit
  --version  show the version, commit and build date and exit
`

func main() {
	// SEC-MEM-003: first, so that nothing sshelf later holds can reach a
	// core dump.
	if err := harden(); err != nil {
		fmt.Fprintln(os.Stderr, "sshelf: process hardening:", err)
		os.Exit(1)
	}
	os.Exit(run(os.Args[1:]))
}

// run parses the command line (FR-CLI-001) and starts the TUI. It returns
// the process exit status.
func run(args []string) (code int) {
	fs := flag.NewFlagSet("sshelf", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // usage goes to the right stream below
	showHelp := fs.Bool("help", false, "")
	showVersion := fs.Bool("version", false, "")
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp), err == nil && *showHelp:
		_, _ = io.WriteString(os.Stdout, usage)
		return 0
	case err != nil:
		fmt.Fprintf(os.Stderr, "sshelf: %v\n\n%s", err, usage)
		return 2
	case fs.NArg() > 0:
		fmt.Fprintf(os.Stderr, "sshelf: unknown argument %q\n\n%s", fs.Arg(0), usage)
		return 2
	case *showVersion:
		_, _ = io.WriteString(os.Stdout, versionString()+"\n")
		return 0
	}

	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		fmt.Fprintln(os.Stderr, "sshelf needs an interactive terminal")
		return 1
	}

	dirs, err := paths.Resolve("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		return 1
	}
	closeLog, err := setupLogging(os.Getenv, dirs.State)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sshelf: debug log:", err)
		return 1
	}
	defer func() { _ = closeLog() }()
	slog.Info("sshelf starting", "version", versionString())

	// ARCH-SEC-004: from here on a panic restores the startup terminal
	// state, writes the reset sequence and a crash report, and exits 70.
	// Panics in goroutines started with term.SafeGo get a report too.
	restoreTerm, err := term.SaveState(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		return 1
	}
	crash := crashHandler{stateDir: dirs.State, restore: restoreTerm, stdout: os.Stdout, stderr: os.Stderr}
	term.SetPanicReporter(func(r any, stack []byte) { _, _ = crash.report(r, stack) })
	defer func() {
		if r := recover(); r != nil {
			code = crash.crash(r, debug.Stack())
		}
	}()

	home, _ := os.UserHomeDir() // "" skips ~/.ssh/known_hosts
	svc := app.New(app.Config{
		Dir:             dirs.Config,
		KnownHostsFiles: paths.KnownHostsFiles(dirs.Config, home),
		Secrets:         newSecretStore(), // in-memory in sshelf_faultinject test builds
	})

	p := tea.NewProgram(guard{model: ui.New(svc)}, tea.WithoutCatchPanics())
	svc.SetSend(func(msg any) { p.Send(msg) }) // prompts reach the UI (design §8.3)
	// Bubble Tea quits on SIGTERM; SIGHUP (the terminal went away) quits
	// the same way instead of killing the process.
	stopHangup := quitOnHangup(p)
	final, err := p.Run()
	stopHangup()
	// Whatever ended the program, no session outlives it (ARCH-SESS-010).
	if g, ok := final.(guard); ok {
		if m, ok := g.model.(ui.Model); ok {
			m.CloseSessions()
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sshelf:", err)
		return 1
	}
	return 0
}

// quitOnHangup quits p when sshelf receives SIGHUP, until stop is called.
func quitOnHangup(p *tea.Program) (stop func()) {
	sig := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sig, syscall.SIGHUP)
	term.SafeGo(func() {
		select {
		case <-sig:
			p.Quit()
		case <-done:
		}
	}, func(any) {})
	return func() {
		signal.Stop(sig)
		close(done)
	}
}

func isTerminal(f *os.File) bool {
	return xterm.IsTerminal(int(f.Fd())) //nolint:gosec // a file descriptor fits in an int
}
