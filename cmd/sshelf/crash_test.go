package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/paths"
	"github.com/RafaelXokito/sshelf/internal/secrets"
	"github.com/RafaelXokito/sshelf/internal/term"
)

const crashCanary = "CANARY-pw-7f3a9c"

// panicWith panics with v from a frame whose argument is the canary, so
// the stack trace has the canary's address in it.
//
//go:noinline
func panicWith(secret string, v any) {
	if secret != "" {
		panic(v)
	}
}

// recovered runs f and returns what it panicked with and the stack at the
// recover, as main's handler sees them.
func recovered(f func()) (r any, stack []byte) {
	defer func() {
		r = recover()
		stack = debug.Stack()
	}()
	f()
	return nil, nil
}

func readReports(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "crash-*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, m := range matches {
		b, err := os.ReadFile(m) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatal(err)
		}
		out[m] = b
	}
	return out
}

func TestARCH_SEC_004_CrashReportHasNoCanary(t *testing.T) {
	values := map[string]any{
		"string":    crashCanary,
		"error":     fmt.Errorf("auth with %q failed", crashCanary),
		"bytes":     []byte(crashCanary),
		"secret":    secrets.NewSecret([]byte(crashCanary)),
		"cmd panic": cmdPanic{value: errors.New(crashCanary), stack: []byte("goroutine 9 [running]:\n")},
	}
	for name, v := range values {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			var stdout, stderr bytes.Buffer
			restored := false
			h := crashHandler{stateDir: dir, restore: func() error { restored = true; return nil }, stdout: &stdout, stderr: &stderr}
			r, stack := recovered(func() { panicWith(crashCanary, v) })
			if code := h.crash(r, stack); code != 70 {
				t.Fatalf("exit status %d, want 70", code)
			}
			if !restored || stdout.String() != term.ResetSequence {
				t.Fatalf("terminal not restored: restored=%v stdout=%q", restored, stdout.String())
			}
			reports := readReports(t, dir)
			if len(reports) != 1 {
				t.Fatalf("%d crash reports", len(reports))
			}
			for path, b := range reports {
				checkMode(t, path, paths.FileMode)
				if !strings.Contains(stderr.String(), path) {
					t.Errorf("stderr %q does not name the report", stderr.String())
				}
				if !bytes.HasPrefix(b, []byte("sshelf crash report")) || !bytes.Contains(b, []byte("goroutine ")) {
					t.Errorf("report lacks header or stack:\n%s", b)
				}
				if !bytes.Contains(b, []byte("panic: value of type")) {
					t.Errorf("report does not describe the panic by type:\n%s", b)
				}
				if bytes.Contains(b, []byte(crashCanary)) {
					t.Errorf("report contains the canary:\n%s", b)
				}
			}
			if strings.Contains(stderr.String(), crashCanary) {
				t.Errorf("stderr contains the canary: %q", stderr.String())
			}
		})
	}
}

func TestARCH_SEC_004_CrashReportKeepsRuntimeErrors(t *testing.T) {
	dir := t.TempDir()
	h := crashHandler{stateDir: dir, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	r, stack := recovered(func() {
		var m map[string]int
		m["x"] = 1 //nolint:staticcheck // deliberate nil-map panic
	})
	h.crash(r, stack)
	for _, b := range readReports(t, dir) {
		if !bytes.Contains(b, []byte("panic: runtime error: assignment to entry in nil map")) {
			t.Fatalf("report:\n%s", b)
		}
	}
}

// fakeModel panics in Update when told to.
type fakeModel struct{ cmd tea.Cmd }

func (m fakeModel) Init() tea.Cmd                       { return m.cmd }
func (m fakeModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, m.cmd }
func (m fakeModel) View() tea.View                      { return tea.NewView("") }

func TestARCH_SEC_004_CmdPanicReachesUpdate(t *testing.T) {
	boom := func() tea.Msg { panic(crashCanary) }
	ok := func() tea.Msg { return "fine" }
	g := guard{model: fakeModel{cmd: tea.Batch(ok, boom)}}

	msg := g.Init()()
	batch, isBatch := msg.(tea.BatchMsg)
	if !isBatch || len(batch) != 2 {
		t.Fatalf("Init cmd returned %#v", msg)
	}
	if got := batch[0](); got != "fine" {
		t.Fatalf("first command = %v", got)
	}
	p, isPanic := batch[1]().(cmdPanic)
	if !isPanic || p.value != crashCanary || !bytes.Contains(p.stack, []byte("goroutine ")) {
		t.Fatalf("second command = %#v, want a cmdPanic", p)
	}
	// Bubble Tea's sequence message is unexported, but also a []tea.Cmd.
	sq := reflect.ValueOf(guard{model: fakeModel{cmd: tea.Sequence(ok, boom)}}.Init()())
	if sq.Kind() != reflect.Slice || sq.Len() != 2 {
		t.Fatalf("sequence message is %v", sq.Type())
	}
	if _, isPanic := sq.Index(1).Interface().(tea.Cmd)().(cmdPanic); !isPanic {
		t.Fatal("a command in a sequence was not guarded")
	}

	r, _ := recovered(func() { g.Update(p) })
	if r == nil {
		t.Fatal("Update did not re-panic with the command's panic")
	}
	if _, ok := r.(cmdPanic); !ok {
		t.Fatalf("Update panicked with %#v", r)
	}
}
