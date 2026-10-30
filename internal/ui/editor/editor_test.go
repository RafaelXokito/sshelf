package editor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestFR_SFTP_019_RunNotFoundKeepsTerminal: an editor that does not exist
// is reported through done without releasing the terminal.
func TestFR_SFTP_019_RunNotFoundKeepsTerminal(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "sshelf-no-such-editor-xyz -w")
	var got error
	msg := Run("/tmp/x", func(err error) tea.Msg { got = err; return "done" })()
	if msg != "done" || !NotFound(got) {
		t.Fatalf("msg %v, err %v", msg, got)
	}
}

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// TestARCH_SFTP_006_EditorWords: $VISUAL before $EDITOR before vi, split
// on spaces, empty values skipped.
func TestARCH_SFTP_006_EditorWords(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want []string
	}{
		{map[string]string{}, []string{"vi"}},
		{map[string]string{"EDITOR": "nano"}, []string{"nano"}},
		{map[string]string{"VISUAL": "code -w", "EDITOR": "nano"}, []string{"code", "-w"}},
		{map[string]string{"VISUAL": "   ", "EDITOR": "  emacs  -nw "}, []string{"emacs", "-nw"}},
		{map[string]string{"VISUAL": "", "EDITOR": "\t"}, []string{"vi"}},
		{map[string]string{"EDITOR": `sh -c 'touch x'`}, []string{"sh", "-c", "'touch", "x'"}}, // no quoting, no shell
	} {
		if got := Words(env(tc.env)); !slices.Equal(got, tc.want) {
			t.Errorf("Words(%q) = %q, want %q", tc.env, got, tc.want)
		}
	}
	if got := Name(env(map[string]string{"EDITOR": "nano -w"})); got != "nano" {
		t.Errorf("Name = %q", got)
	}
}

// TestARCH_SFTP_006_CommandArgsAndEnv: the arguments are the words plus
// the path, and Env is nil, so the editor inherits sshelf's environment.
func TestARCH_SFTP_006_CommandArgsAndEnv(t *testing.T) {
	c := Command([]string{"/usr/bin/true", "-w", "--wait"}, "/tmp/sshelf-edit-1/notes.txt")
	if want := []string{"/usr/bin/true", "-w", "--wait", "/tmp/sshelf-edit-1/notes.txt"}; !slices.Equal(c.Args, want) {
		t.Fatalf("Args = %q, want %q", c.Args, want)
	}
	if c.Path != "/usr/bin/true" || c.Env != nil || c.Dir != "" {
		t.Fatalf("Path %q, Env %q, Dir %q", c.Path, c.Env, c.Dir)
	}
	words := []string{"code", "-w"}
	_ = Command(words, "x")
	if !slices.Equal(words, []string{"code", "-w"}) {
		t.Fatalf("Command changed its words: %q", words)
	}
}

// TestSEC_SFTP_006_NoShell: a file named "$(touch pwned);x.txt" reaches a
// fake editor as one argument, and nothing runs it through a shell.
func TestSEC_SFTP_006_NoShell(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("testdata", "args.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	out := filepath.Join(dir, "args.out")
	t.Setenv("FAKE_EDITOR_OUT", out)
	name := filepath.Join(dir, "$(touch pwned);x.txt")
	if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Command(Words(env(map[string]string{"EDITOR": script + " --flag `touch pwned2`"})), name)
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out) //nolint:gosec // the test's temp dir
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if want := []string{"--flag", "`touch", "pwned2`", name}; !slices.Equal(got, want) {
		t.Fatalf("the editor received %q, want %q", got, want)
	}
	for _, p := range []string{"pwned", "pwned2"} {
		if _, err := os.Stat(filepath.Join(dir, p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s exists: %v", p, err)
		}
	}
}

// TestFR_SFTP_019_EditorErrors: a missing editor and a non-zero exit are
// told apart.
func TestFR_SFTP_019_EditorErrors(t *testing.T) {
	c := Command([]string{"sshelf-no-such-editor-xyz"}, "f")
	if c.Err == nil || !NotFound(c.Err) {
		t.Fatalf("Err = %v, want not found", c.Err)
	}
	if err := Command([]string{filepath.Join(t.TempDir(), "missing")}, "f").Run(); !NotFound(err) {
		t.Fatalf("absolute missing editor: %v", err)
	}
	sh, err := exec.LookPath("false")
	if err != nil {
		t.Skip("no false")
	}
	err = Command([]string{sh}, "f").Run()
	if code, ok := ExitStatus(err); !ok || code != 1 || NotFound(err) {
		t.Fatalf("ExitStatus(%v) = %d, %v", err, code, ok)
	}
	if _, ok := ExitStatus(errors.New("x")); ok {
		t.Fatal("ExitStatus of a plain error")
	}
}
