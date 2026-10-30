// Package editor runs the user's editor for edit-locally (FR-SFTP-019,
// ADR-019). It is the only sshelf package that imports os/exec
// (SEC-MEM-008 and ARCH-SESS-001 M3 revisions, SEC-SFTP-006): the program
// named by $VISUAL, $EDITOR or vi runs directly, without a shell, while
// Bubble Tea has released the terminal.
package editor

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Fallback is the editor used when neither $VISUAL nor $EDITOR is set.
const Fallback = "vi"

// Words returns the words of the first non-empty of $VISUAL, $EDITOR and
// vi, split on white space: "code -w" is the program code with the
// argument -w. There is no shell and no quoting.
func Words(getenv func(string) string) []string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if w := strings.Fields(getenv(name)); len(w) > 0 {
			return w
		}
	}
	return []string{Fallback}
}

// Command is the editor command for path: the program words[0] with the
// other words and path as separate arguments. Env is left nil, so the
// editor inherits sshelf's environment, which never holds a secret
// (SEC-MEM-008).
func Command(words []string, path string) *exec.Cmd {
	args := append(append([]string(nil), words[1:]...), path)
	return exec.Command(words[0], args...) //nolint:gosec // the user's own editor, without a shell (SEC-SFTP-006)
}

// Run returns the command that runs the user's editor on path with the
// terminal released (tea.ExecProcess) and then delivers done's message
// with the editor's error. An editor that cannot be found is reported
// without releasing the terminal. The command line is worked out when
// the command runs, off the Update goroutine.
func Run(path string, done func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		c := Command(Words(os.Getenv), path)
		if c.Err != nil {
			return done(c.Err)
		}
		return tea.ExecProcess(c, done)()
	}
}

// Name is the program the editor command runs, for messages.
func Name(getenv func(string) string) string { return Words(getenv)[0] }

// NotFound reports whether err means the editor could not be started
// because its program does not exist.
func NotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}

// ExitStatus returns the exit status of an editor that ran and failed.
func ExitStatus(err error) (int, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}
