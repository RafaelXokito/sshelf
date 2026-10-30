package sftpview

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/confirm"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/input"
)

// The file operations of the panes (FR-SFTP-010 … FR-SFTP-012): each
// asks in a modal, runs in a command, and refreshes the pane with the
// cursor on the name it changed.

// renameMsg renames From to To in Dir; the rename modal delivers it.
type renameMsg struct {
	tab      int
	side     app.Side
	dir      string
	from, to string
}

func (m renameMsg) TabNum() int { return m.tab }

// mkdirMsg makes directory name in dir; the mkdir modal delivers it.
type mkdirMsg struct {
	tab  int
	side app.Side
	dir  string
	name string
}

func (m mkdirMsg) TabNum() int { return m.tab }

// deleteCountedMsg carries the number of entries inside the directories
// about to be deleted.
type deleteCountedMsg struct {
	tab   int
	seq   int
	n     int
	err   error
	names []string
}

func (m deleteCountedMsg) TabNum() int { return m.tab }

// deleteMsg deletes names in dir; the confirmation delivers it on y.
type deleteMsg struct {
	tab   int
	side  app.Side
	dir   string
	names []string
}

func (m deleteMsg) TabNum() int { return m.tab }

// opDoneMsg reports a finished operation: the pane is listed again with
// the cursor on want.
type opDoneMsg struct {
	tab  int
	side app.Side
	dir  string
	want string
	text string // the message on success
	what string // what failed, for the error line
	err  error
}

func (m opDoneMsg) TabNum() int { return m.tab }

// counting is a delete waiting for its count (FR-SFTP-011).
type counting struct {
	seq   int
	side  app.Side
	dir   string
	names []string
	label string // what is counted: "photos/" or "3 items"
	stop  context.CancelFunc
}

// CheckName returns why name cannot be a file name, or "" (FR-SFTP-010):
// empty, "." or "..", or holding "/", NUL or a control character.
func CheckName(name string) string {
	switch {
	case name == "":
		return "the name is empty"
	case name == "." || name == "..":
		return "not a valid name"
	case strings.Contains(name, "/"):
		return "the name contains /"
	case strings.ContainsFunc(name, func(r rune) bool { return r == 0 || unicode.IsControl(r) }):
		return "the name contains a control character"
	}
	return ""
}

// nameCheck is CheckName plus "already exists" for the names of p's
// listing; same is a name that may stay (the one being renamed).
func nameCheck(p *pane, same string) func(string) string {
	names := map[string]bool{}
	for _, e := range p.entries {
		names[e.Name] = true
	}
	return func(name string) string {
		if r := CheckName(name); r != "" {
			return r
		}
		if name != same && names[name] {
			return "already exists"
		}
		return ""
	}
}

// targets are what d and c act on: the marked entries, or the entry under
// the cursor (FR-SFTP-009); never "..".
func (p *pane) targets() []app.Entry {
	var out []app.Entry
	if len(p.marks) > 0 {
		for _, e := range p.entries {
			if p.marks[e.Name] {
				out = append(out, e)
			}
		}
		return out
	}
	if r, ok := p.current(); ok && !r.parent {
		out = append(out, *r.e)
	}
	return out
}

// rename opens the rename modal for the entry under the cursor.
func (m Model) rename(p *pane) tea.Cmd {
	r, ok := p.current()
	if !ok || r.parent {
		return nil
	}
	tab, side, dir, from := m.tab, p.side, p.dir, r.e.Name
	title := "Rename " + displayName(*r.e) + " to:"
	modal := input.New(title, from, nameCheck(p, from), func(to string) tea.Msg {
		return renameMsg{tab: tab, side: side, dir: dir, from: from, to: to}
	})
	return showModal(modal)
}

// mkdir opens the mkdir modal.
func (m Model) mkdir(p *pane) tea.Cmd {
	if !p.loaded {
		return nil
	}
	tab, side, dir := m.tab, p.side, p.dir
	title := "New directory in " + p.title + ":"
	modal := input.New(title, "", nameCheck(p, ""), func(name string) tea.Msg {
		return mkdirMsg{tab: tab, side: side, dir: dir, name: name}
	})
	return showModal(modal)
}

// showModal asks the root model to open modal.
func showModal(modal components.Modal) tea.Cmd {
	return func() tea.Msg { return components.ShowModalMsg{Modal: modal} }
}

// delete starts the delete flow (FR-SFTP-011): with a directory among
// the targets, count their contents first; then ask.
func (m Model) delete(p *pane) (Model, tea.Cmd) {
	ts := p.targets()
	if len(ts) == 0 {
		return m, nil
	}
	names := make([]string, len(ts))
	var dirs []string
	for i, e := range ts {
		names[i] = e.Name
		if e.Kind == app.KindDir {
			dirs = append(dirs, p.join(p.dir, e.Name))
		}
	}
	label := strconv.Itoa(len(ts)) + " items"
	if len(ts) == 1 {
		label = displayName(ts[0])
	}
	if len(dirs) == 0 {
		return m, showModal(confirm.New(deleteQuestion(label, len(ts), false, 0, false), deleteMsg{tab: m.tab, side: p.side, dir: p.dir, names: names}))
	}
	m.stopCounting()
	ctx, stop := context.WithCancel(context.Background())
	m.countSeq++
	m.counting = &counting{seq: m.countSeq, side: p.side, dir: p.dir, names: names, label: label, stop: stop}
	tab, b, side, seq := m.tab, m.b, p.side, m.countSeq
	return m, func() tea.Msg {
		total := 0
		for _, d := range dirs {
			n, err := b.Count(ctx, side, d, true)
			if err != nil {
				return deleteCountedMsg{tab: tab, seq: seq, err: err, names: names}
			}
			total += n
			if total > app.DeleteCountLimit {
				break
			}
		}
		return deleteCountedMsg{tab: tab, seq: seq, n: total, names: names}
	}
}

// stopCounting cancels a delete's count.
func (m *Model) stopCounting() {
	if m.counting != nil {
		m.counting.stop()
		m.counting = nil
	}
}

// deleteCounted asks to confirm once the count is known.
func (m Model) deleteCounted(msg deleteCountedMsg) (Model, tea.Cmd) {
	c := m.counting
	if c == nil || c.seq != msg.seq {
		return m, nil // cancelled
	}
	m.counting = nil
	c.stop()
	if msg.err != nil {
		m.setError("cannot count "+domain.Sanitize(c.label), msg.err)
		return m, nil
	}
	q := deleteQuestion(c.label, len(c.names), true, msg.n, msg.n > app.DeleteCountLimit)
	return m, showModal(confirm.New(q, deleteMsg{tab: m.tab, side: c.side, dir: c.dir, names: c.names}))
}

// deleteQuestion is the confirmation (FR-SFTP-011): "Delete notes.txt?",
// "Delete photos/ and the 1,234 entries in it? This cannot be undone.",
// "Delete 3 items and the 1,234 entries in them?".
func deleteQuestion(label string, items int, dirs bool, n int, over bool) string {
	if !dirs {
		return "Delete " + label + "?"
	}
	what := "the " + thousands(n) + " " + plural(n, "entry", "entries")
	if over {
		what = "more than " + thousands(app.DeleteCountLimit) + " entries"
	}
	if items == 1 {
		return "Delete " + label + " and " + what + " in it? This cannot be undone."
	}
	return "Delete " + label + " and " + what + " in them?"
}

// thousands formats n with commas: 1,234.
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// runRename renames in a command.
func (m Model) runRename(msg renameMsg) tea.Cmd {
	if msg.to == msg.from {
		return nil
	}
	b := m.b
	return func() tea.Msg {
		err := b.Rename(context.Background(), msg.side, msg.dir, msg.from, msg.to)
		done := opDoneMsg{tab: msg.tab, side: msg.side, dir: msg.dir, want: msg.to, err: err, what: "cannot rename " + domain.Sanitize(msg.from)}
		if err != nil {
			done.want = msg.from
		} else {
			done.text = "renamed " + domain.Sanitize(msg.from) + " to " + domain.Sanitize(msg.to)
		}
		return done
	}
}

// runMkdir makes a directory in a command.
func (m Model) runMkdir(msg mkdirMsg) tea.Cmd {
	b := m.b
	return func() tea.Msg {
		err := b.Mkdir(context.Background(), msg.side, msg.dir, msg.name)
		done := opDoneMsg{tab: msg.tab, side: msg.side, dir: msg.dir, want: msg.name, err: err, what: "cannot make " + domain.Sanitize(msg.name)}
		if err == nil {
			done.text = "made " + domain.Sanitize(msg.name) + "/"
		}
		return done
	}
}

// runDelete deletes in a command; it stops at the first error and says
// how many were deleted (FR-SFTP-011).
func (m Model) runDelete(msg deleteMsg, want string) tea.Cmd {
	b := m.b
	return func() tea.Msg {
		n, err := b.Delete(context.Background(), msg.side, msg.dir, msg.names)
		done := opDoneMsg{tab: msg.tab, side: msg.side, dir: msg.dir, want: want, err: err}
		switch {
		case err != nil && n < len(msg.names):
			done.what = "cannot delete " + domain.Sanitize(msg.names[n]) + " (" + strconv.Itoa(n) + " deleted)"
		case err != nil:
			done.what = "delete"
		case len(msg.names) == 1:
			done.text = "deleted " + domain.Sanitize(msg.names[0])
		default:
			done.text = "deleted " + strconv.Itoa(n) + " items"
		}
		return done
	}
}

// opDone shows how an operation went and lists the pane again with the
// cursor on the name it changed (FR-SFTP-010 … FR-SFTP-012).
func (m Model) opDone(msg opDoneMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		if errors.Is(msg.err, fs.ErrExist) {
			msg.err = errors.New("already exists")
		}
		m.setError(msg.what, msg.err)
	} else {
		m.setMessage(msg.text)
	}
	p := m.panes[msg.side]
	if !p.loaded || p.dir != msg.dir {
		return m, nil
	}
	return m, m.list(msg.side, msg.dir, msg.want)
}

// deleteWant is the name the cursor goes to after deleting names: the
// first entry after the cursor that is not deleted, else before it.
func (p *pane) deleteWant(names []string) string {
	gone := map[string]bool{}
	for _, n := range names {
		gone[n] = true
	}
	for i := p.cursor; i < len(p.rows); i++ {
		if r := p.rows[i]; !r.parent && !gone[r.e.Name] {
			return r.e.Name
		}
	}
	for i := min(p.cursor, len(p.rows)-1); i >= 0; i-- {
		if r := p.rows[i]; !r.parent && !gone[r.e.Name] {
			return r.e.Name
		}
	}
	return ""
}
