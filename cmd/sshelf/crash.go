package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/term"
	"github.com/RafaelXokito/sshelf/internal/ui"
)

// exitCrash is the exit status after a panic (EX_SOFTWARE).
const exitCrash = 70

// crashHandler deals with panics (ARCH-SEC-004, design §8.6).
type crashHandler struct {
	stateDir string
	restore  func() error // restores the terminal state saved at startup
	stdout   io.Writer    // the terminal
	stderr   io.Writer
	now      func() time.Time
}

// crash handles a panic recovered in main: it restores the startup
// terminal state, writes the reset sequence, then writes a crash report and
// says where it is. It returns the exit status.
func (h crashHandler) crash(recovered any, stack []byte) int {
	if p, ok := recovered.(cmdPanic); ok {
		recovered, stack = p.value, p.stack
	}
	if h.restore != nil {
		_ = h.restore()
	}
	_, _ = io.WriteString(h.stdout, term.ResetSequence)
	path, err := h.report(recovered, stack)
	if err != nil {
		_, _ = fmt.Fprintln(h.stderr, "sshelf: internal error; writing the crash report failed:", err)
	} else {
		_, _ = fmt.Fprintln(h.stderr, "sshelf: internal error; crash report:", path)
	}
	return exitCrash
}

// report writes a crash report, mode 0600, to the state directory and
// returns its path. term.SafeGo calls it for panics it recovered, which
// leave the program running.
func (h crashHandler) report(recovered any, stack []byte) (string, error) {
	desc := describePanic(recovered)
	if _, err := paths.EnsureDir(h.stateDir); err != nil {
		slog.Error("panic", "panic", desc, "err", err)
		return "", err
	}
	now := time.Now
	if h.now != nil {
		now = h.now
	}
	// CreateTemp opens with O_EXCL and mode 0600.
	f, err := os.CreateTemp(h.stateDir, "crash-"+now().UTC().Format("20060102T150405Z")+"-*.txt")
	if err != nil {
		slog.Error("panic", "panic", desc, "err", err)
		return "", err
	}
	_, werr := fmt.Fprintf(f, "sshelf crash report\n\ntime: %s\nversion: %s\ngo: %s %s/%s\npanic: %s\n\n%s",
		now().UTC().Format(time.RFC3339), versionString(), runtime.Version(), runtime.GOOS, runtime.GOARCH, desc, stack)
	cerr := f.Close()
	slog.Error("panic", "panic", desc, "report", f.Name())
	if werr != nil {
		return f.Name(), werr
	}
	return f.Name(), cerr
}

// describePanic says what panicked without risking a secret in the crash
// report (SEC-LOG-001): a runtime error (nil dereference, index out of
// range, …) carries only the runtime's own text and is kept; any other
// value may hold data, so only its type is written. The stack trace shows
// arguments as raw words, never the bytes they point to.
func describePanic(r any) string {
	if re, ok := r.(runtime.Error); ok {
		return "runtime error: " + re.Error()
	}
	return fmt.Sprintf("value of type %T (not recorded: it may contain secrets)", r)
}

// cmdPanic carries a panic recovered in a tea.Cmd, which Bubble Tea runs on
// its own goroutines, to Update; guard re-panics there, on the goroutine
// that runs the program, so main's handler sees it.
type cmdPanic struct {
	value any
	stack []byte // of the goroutine that panicked
}

// guard wraps the root model so that every panic reaches main's handler.
// The program runs with tea.WithoutCatchPanics: Bubble Tea's own recovery
// prints the panic value to the terminal and would hide it from main.
type guard struct {
	model tea.Model
}

// Init implements tea.Model.
func (g guard) Init() tea.Cmd { return guardCmd(g.model.Init()) }

// Update implements tea.Model.
func (g guard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p, ok := msg.(cmdPanic); ok {
		panic(p)
	}
	switch msg.(type) {
	case tea.KeyPressMsg:
		term.Fault("update")
	case ui.SessionUpdateMsg:
		term.Fault("session-update") // a crash while a session is open
	}
	m, cmd := g.model.Update(msg)
	return guard{model: m}, guardCmd(cmd)
}

// View implements tea.Model.
func (g guard) View() tea.View { return g.model.View() }

var cmdType = reflect.TypeFor[tea.Cmd]()

// guardCmd returns cmd with a recover that turns a panic into a cmdPanic
// message. The commands inside a batch or sequence message are guarded the
// same way.
func guardCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = cmdPanic{value: r, stack: debug.Stack()}
			}
		}()
		msg = cmd()
		// tea.BatchMsg and Bubble Tea's unexported sequence message are
		// both []tea.Cmd.
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == cmdType {
			for i := range v.Len() {
				c, _ := v.Index(i).Interface().(tea.Cmd)
				v.Index(i).Set(reflect.ValueOf(guardCmd(c)))
			}
		}
		return msg
	}
}
