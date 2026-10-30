package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/editor"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/editconflict"
	"github.com/RafaelXokito/sshelf/internal/ui/sftpview"
)

// Edit locally (FR-SFTP-019, FR-SFTP-020, design §8.10): e on a remote
// file downloads a private copy, the editor runs on it with the UI
// suspended (ADR-019), and the copy is uploaded, or the user is asked,
// when the editor exits. Every step runs in a command; the copy is
// removed at the end of every path by the back end (SEC-SFTP-005).

// EditFetchedMsg reports that the copy of a remote file is ready; the
// editor runs next.
type EditFetchedMsg struct {
	Tab  int
	Copy *app.EditCopy
}

// editFetchFailedMsg reports that the copy could not be made.
type editFetchFailedMsg struct {
	tab  int
	name string
	err  error
}

// editorDoneMsg reports that the editor exited, with its error.
type editorDoneMsg struct {
	tab  int
	copy *app.EditCopy
	err  error
}

// editResultMsg carries the outcome of EditFinish or EditResolve. text,
// when set, is the status line's message for EditDiscarded.
type editResultMsg struct {
	tab  int
	copy *app.EditCopy
	res  app.EditResult
	err  error
	text string
}

// editChoiceMsg is the answer to an edit question.
type editChoiceMsg struct {
	tab    int
	copy   *app.EditCopy
	choice app.EditChoice
}

// updateEdit handles the edit-locally messages; ok is false for others.
func (m Model) updateEdit(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case sftpview.EditRequestMsg:
		t := m.sftpTabNum(msg.Tab)
		// Never while a connection attempt runs: its prompter would wait
		// for the suspended UI (ADR-019).
		if t == nil || m.cancel != nil {
			return m, nil, true
		}
		if m.editing {
			m.setEditMessage(t, "already editing a file")
			return m, nil, true
		}
		m.editing = true
		name := domain.Sanitize(msg.Name)
		m.setEditMessage(t, "downloading "+name+" to edit…")
		b, tab, dir, raw := t.sftp.browser, t.num, msg.Dir, msg.Name
		return m, func() tea.Msg {
			c, err := b.EditFetch(context.Background(), dir, raw)
			if err != nil {
				return editFetchFailedMsg{tab: tab, name: name, err: err}
			}
			return EditFetchedMsg{Tab: tab, Copy: c}
		}, true

	case editFetchFailedMsg:
		m.editing = false
		if t := m.sftpTabNum(msg.tab); t != nil {
			m.setEditMessage(t, sftpview.ErrorMark+" "+msg.name+": "+sftpview.ErrText(msg.err))
		}
		return m, nil, true

	case EditFetchedMsg:
		t := m.sftpTabNum(msg.Tab)
		if t == nil {
			m.editing = false // closing the tab removed the copy
			return m, nil, true
		}
		if m.cancel != nil {
			return m, m.resolveEdit(t, msg.Copy, app.EditDiscard, "edit cancelled: a connection attempt is in progress"), true
		}
		tab, c := t.num, msg.Copy
		return m, m.runEditor(c.Path, func(err error) tea.Msg { return editorDoneMsg{tab: tab, copy: c, err: err} }), true

	case editorDoneMsg:
		// The terminal may have changed size while the editor had it:
		// ask, and the answer resizes every session (ARCH-SFTP-006).
		size := tea.RequestWindowSize
		t := m.sftpTabNum(msg.tab)
		if t == nil {
			m.editing = false
			return m, size, true
		}
		if msg.err != nil {
			return m, tea.Batch(size, m.resolveEdit(t, msg.copy, app.EditDiscard, editorErrText(msg.err))), true
		}
		b, tab, c := t.sftp.browser, t.num, msg.copy
		m.setEditMessage(t, "uploading "+domain.Sanitize(c.Name)+"…")
		return m, tea.Batch(size, func() tea.Msg {
			res, err := b.EditFinish(context.Background(), c)
			return editResultMsg{tab: tab, copy: c, res: res, err: err}
		}), true

	case editResultMsg:
		t := m.sftpTabNum(msg.tab)
		if t == nil {
			m.editing = false
			return m, nil, true
		}
		return m.editResult(t, msg)

	case editChoiceMsg:
		t := m.sftpTabNum(msg.tab)
		if t == nil {
			m.editing = false
			return m, nil, true
		}
		m.setEditMessage(t, "saving "+domain.Sanitize(msg.copy.Name)+"…")
		return m, m.resolveEdit(t, msg.copy, msg.choice, ""), true
	}
	return m, nil, false
}

// editResult shows the outcome of an edit step, or asks the next
// question.
func (m Model) editResult(t *sessionTab, msg editResultMsg) (Model, tea.Cmd, bool) {
	name, tab, c := domain.Sanitize(msg.copy.Name), t.num, msg.copy
	answer := func(ch app.EditChoice) tea.Msg { return editChoiceMsg{tab: tab, copy: c, choice: ch} }
	if msg.err != nil {
		local := filepath.Join(t.sftp.view.Dir(app.Local), name+app.LocalEditSuffix)
		m.setEditMessage(t, sftpview.ErrorMark+" "+name+": upload failed: "+sftpview.ErrText(msg.err))
		m.modals = append(m.modals, editconflict.NewUploadFailed(name, sftpview.ErrText(msg.err), domain.Sanitize(local), answer))
		return m, nil, true
	}
	var refresh tea.Cmd
	switch msg.res.Outcome {
	case app.EditRemoteChanged, app.EditRemoteGone:
		m.setEditMessage(t, "")
		m.modals = append(m.modals, editconflict.NewChanged(name, msg.res.Outcome == app.EditRemoteGone, answer))
		return m, nil, true
	case app.EditUnchanged:
		m.setEditMessage(t, name+": no changes")
	case app.EditSaved:
		m.setEditMessage(t, fmt.Sprintf("saved %s (%s)", name, sftpview.FormatSize(msg.res.Size)))
		refresh = t.sftp.view.Refresh()
	case app.EditSavedAs:
		m.setEditMessage(t, fmt.Sprintf("saved %s (%s)", domain.Sanitize(msg.res.SavedAs), sftpview.FormatSize(msg.res.Size)))
		refresh = t.sftp.view.Refresh()
	case app.EditDiscarded:
		text := msg.text
		if text == "" {
			text = name + ": edit discarded"
		}
		m.setEditMessage(t, text)
	}
	m.editing = false
	return m, refresh, true
}

// resolveEdit ends the edit of c on tab t with choice; text replaces the
// status message of a discarded edit.
func (m Model) resolveEdit(t *sessionTab, c *app.EditCopy, choice app.EditChoice, text string) tea.Cmd {
	b, tab, localDir := t.sftp.browser, t.num, t.sftp.view.Dir(app.Local)
	return func() tea.Msg {
		res, err := b.EditResolve(context.Background(), c, choice, localDir)
		return editResultMsg{tab: tab, copy: c, res: res, err: err, text: text}
	}
}

// editorErrText is the status message after the editor failed; nothing
// is uploaded (FR-SFTP-019).
func editorErrText(err error) string {
	if code, ok := editor.ExitStatus(err); ok {
		return fmt.Sprintf("editor exited with status %d; nothing uploaded", code)
	}
	if editor.NotFound(err) {
		return "editor not found: " + domain.Sanitize(editor.Name(os.Getenv))
	}
	return "editor could not start: " + sftpview.ErrText(err) + "; nothing uploaded"
}

// setEditMessage puts text on tab t's progress line.
func (m Model) setEditMessage(t *sessionTab, text string) {
	t.sftp.view = t.sftp.view.WithMessage(text)
}
