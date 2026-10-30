package sftpview

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/conflict"
)

// The transfer queue as the tab shows it (FR-SFTP-014 … FR-SFTP-016): the
// progress line, the transfers view and the conflict question. The view
// only reads snapshots; every queue change is a Browser call in a command.

// SnapshotMsg carries a snapshot of the tab's transfers (Browser.Transfers,
// taken in a command).
type SnapshotMsg struct {
	Tab       int
	Transfers []app.Transfer
}

// TabNum implements TabMsg.
func (m SnapshotMsg) TabNum() int { return m.Tab }

// answerMsg answers a waiting transfer; the conflict modal delivers it.
type answerMsg struct {
	tab    int
	id     app.TransferID
	answer app.Answer
	all    bool
}

func (m answerMsg) TabNum() int { return m.tab }

// Bar is the width of the progress bar in cells.
const Bar = 10

// Transfers is the last snapshot.
func (m Model) Transfers() []app.Transfer { return m.transfers }

// ShowingTransfers reports whether the transfers view replaces the panes.
func (m Model) ShowingTransfers() bool { return m.showTransfers }

// Running reports whether a transfer is running, so the progress line
// needs redrawing (at most 4 Hz, ARCH-SFTP-003).
func (m Model) Running() bool {
	for _, t := range m.transfers {
		if t.State == app.Running {
			return true
		}
	}
	return false
}

// Pending counts the transfers that closing the tab would stop: queued,
// running, waiting or paused (FR-SFTP-025).
func (m Model) Pending() int {
	n := 0
	for _, t := range m.transfers {
		if t.State.Pending() {
			n++
		}
	}
	return n
}

// Finished counts done and failed transfers, for the tab's activity
// marker (FR-TAB-003 M3 revision).
func (m Model) Finished() int {
	n := 0
	for _, t := range m.transfers {
		if t.State == app.Done || t.State == app.Failed {
			n++
		}
	}
	return n
}

// snapshot takes a new snapshot: a finished transfer refreshes the pane
// showing its target directory, and a finished copy says how it went
// (FR-SFTP-001, FR-SFTP-013).
func (m Model) snapshot(ts []app.Transfer) (Model, tea.Cmd) {
	m.transfers = ts
	m.tcursor = min(m.tcursor, max(len(ts)-1, 0))
	var cmds []tea.Cmd
	refreshed := map[app.Side]bool{}
	batches := map[app.BatchID][]app.Transfer{}
	for _, t := range ts {
		batches[t.Batch] = append(batches[t.Batch], t)
		if t.State != app.Done || m.doneSeen[t.ID] {
			continue
		}
		m.doneSeen[t.ID] = true
		side := t.From.Other()
		if p := m.panes[side]; p.loaded && p.dir == t.DstDir && !refreshed[side] {
			refreshed[side] = true
			name, _ := p.cursorName()
			cmds = append(cmds, m.list(side, p.dir, name))
		}
	}
	for id, bt := range batches {
		if m.reported[id] || !allFinished(bt) {
			continue
		}
		m.reported[id] = true
		m.setMessage(batchSummary(bt))
	}
	return m, tea.Batch(cmds...)
}

// allFinished reports whether every transfer of a batch has finished.
func allFinished(ts []app.Transfer) bool {
	for _, t := range ts {
		if !t.State.Finished() {
			return false
		}
	}
	return true
}

// batchSummary is the status message of a finished copy: "copied 3
// files, skipped 1".
func batchSummary(ts []app.Transfer) string {
	var done, skipped, failed, cancelled int
	for _, t := range ts {
		switch t.State {
		case app.Done:
			done++
		case app.Skipped:
			skipped++
		case app.Failed:
			failed++
		case app.Cancelled:
			cancelled++
		}
	}
	parts := []string{"copied " + strconv.Itoa(done) + " " + plural(done, "file", "files")}
	if skipped > 0 {
		parts = append(parts, "skipped "+strconv.Itoa(skipped))
	}
	if failed > 0 {
		parts = append(parts, strconv.Itoa(failed)+" failed")
	}
	if cancelled > 0 {
		parts = append(parts, strconv.Itoa(cancelled)+" cancelled")
	}
	s := strings.Join(parts, ", ")
	if failed > 0 {
		return ErrorMark + " " + s
	}
	return s
}

// copySelection queues a copy of the marked entries, or the one under
// the cursor, into the other pane's directory, and clears the marks
// (FR-SFTP-001, FR-SFTP-009).
func (m Model) copySelection(p *pane) tea.Cmd {
	ts := p.targets()
	if len(ts) == 0 || !m.panes[p.side.Other()].loaded {
		return nil
	}
	names := make([]string, len(ts))
	for i, e := range ts {
		names[i] = e.Name
	}
	clear(p.marks)
	return m.copyNames(p, names)
}

// copyNames queues a copy of names from p's directory into the other
// pane's directory (FR-SFTP-001, FR-SFTP-022).
func (m Model) copyNames(p *pane, names []string) tea.Cmd {
	other := m.panes[p.side.Other()]
	if len(names) == 0 || !other.loaded {
		return nil
	}
	tab, b, from, src, dst := m.tab, m.b, p.side, p.dir, other.dir
	return func() tea.Msg {
		b.Copy(from, src, names, dst)
		return SnapshotMsg{Tab: tab, Transfers: b.Transfers()}
	}
}

// transferKey handles a key in the transfers view (FR-SFTP-015).
func (m Model) transferKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	var sel *app.Transfer
	if m.tcursor < len(m.transfers) {
		sel = &m.transfers[m.tcursor]
	}
	tab, b := m.tab, m.b
	act := func(f func(app.Browser)) tea.Cmd {
		return func() tea.Msg {
			f(b)
			return SnapshotMsg{Tab: tab, Transfers: b.Transfers()}
		}
	}
	switch {
	case key.Matches(k, keys.TransfersBack):
		m.showTransfers = false
	case key.Matches(k, keys.Up):
		m.tcursor = max(m.tcursor-1, 0)
	case key.Matches(k, keys.Down):
		m.tcursor = min(m.tcursor+1, max(len(m.transfers)-1, 0))
	case key.Matches(k, keys.TransferCancel):
		if sel != nil && sel.State.Pending() {
			id := sel.ID
			return m, act(func(b app.Browser) { b.Cancel(id) })
		}
	case key.Matches(k, keys.TransferRetry):
		if sel != nil && (sel.State == app.Failed || sel.State == app.Cancelled) {
			if m.disconnected {
				m.setMessage(ErrorMark + " not connected")
				return m, nil
			}
			id := sel.ID
			return m, act(func(b app.Browser) { b.Retry(id) })
		}
	case key.Matches(k, keys.TransferAnswer):
		if sel != nil && sel.State == app.Waiting {
			return m, m.ask(*sel)
		}
	case key.Matches(k, keys.TransferClear):
		return m, act(func(b app.Browser) { b.Clear() })
	}
	return m, nil
}

// ask opens the conflict question of waiting transfer t (FR-SFTP-016).
func (m Model) ask(t app.Transfer) tea.Cmd {
	c := app.Conflict{}
	if t.Conflict != nil {
		c = *t.Conflict
	}
	tab, id := m.tab, t.ID
	q := conflict.Question{
		Name:   domain.Sanitize(t.Name),
		Remote: FormatSize(c.Remote.Size) + ", " + FormatTime(c.Remote.ModTime, m.opts.Now()),
		Local:  FormatSize(c.Local.Size) + ", " + FormatTime(c.Local.ModTime, m.opts.Now()),
	}
	modal := conflict.New(q, func(a app.Answer, all bool) tea.Msg {
		return answerMsg{tab: tab, id: id, answer: a, all: all}
	})
	return showModal(modal)
}

// answer sends an answer in a command.
func (m Model) answer(msg answerMsg) tea.Cmd {
	tab, b := m.tab, m.b
	return func() tea.Msg {
		b.Answer(msg.id, msg.answer, msg.all)
		return SnapshotMsg{Tab: tab, Transfers: b.Transfers()}
	}
}

// statusLine is the progress line's text (FR-SFTP-014): the oldest
// running transfer; else a message shown less than MessageHold ago; else
// paused transfers, a waiting question, or the last message.
func (m Model) statusLine(width int) string {
	var running []app.Transfer
	paused, queued := 0, 0
	var waiting *app.Transfer
	for i, t := range m.transfers {
		switch t.State {
		case app.Running:
			running = append(running, t)
		case app.Paused:
			paused++
		case app.Queued:
			queued++
		case app.Waiting:
			if waiting == nil {
				waiting = &m.transfers[i]
			}
		}
	}
	switch {
	case len(running) > 0:
		return progressText(running[0], len(running)-1, queued, width)
	case m.msg != "" && m.opts.Now().Sub(m.msgAt) < MessageHold:
		return m.msg
	case paused > 0:
		return fmt.Sprintf("⏸ %d %s paused: connection lost · r reconnect", paused, plural(paused, "transfer", "transfers"))
	case waiting != nil:
		return "? " + domain.Sanitize(waiting.Name) + " exists · t to answer"
	}
	return m.msg
}

// progressText is the running transfer's line (FR-SFTP-014):
// "↓ backup.tar.gz  412/840 MB  █████░░░░░ 49%  12 MB/s  +1 running, 3
// queued". When it does not fit width, the counts, then the bar, then
// the rate are dropped, and the name is cut with "…".
func progressText(t app.Transfer, moreRunning, queued, width int) string {
	arrow := "↑"
	if t.From == app.Remote {
		arrow = "↓"
	}
	name := domain.Sanitize(t.Name)
	pct := 0
	if t.Total > 0 {
		pct = int(min(t.Done*100/t.Total, 100))
	}
	bytes := BytesPair(t.Done, t.Total)
	cells := (pct*Bar + 50) / 100
	bar := strings.Repeat("█", cells) + strings.Repeat("░", Bar-cells) + " "
	rate := ""
	if t.Rate > 0 {
		rate = FormatSize(int64(t.Rate)) + "/s"
	}
	var counts []string
	if moreRunning > 0 {
		counts = append(counts, "+"+strconv.Itoa(moreRunning)+" running")
	}
	if queued > 0 {
		counts = append(counts, strconv.Itoa(queued)+" queued")
	}
	count := strings.Join(counts, ", ")
	build := func(withBar, withRate, withCount bool, n string) string {
		parts := []string{arrow + " " + n, bytes}
		if withBar {
			parts = append(parts, bar+strconv.Itoa(pct)+"%")
		} else {
			parts = append(parts, strconv.Itoa(pct)+"%")
		}
		if withRate && rate != "" {
			parts = append(parts, rate)
		}
		if withCount && count != "" {
			parts = append(parts, count)
		}
		return strings.Join(parts, "  ")
	}
	for _, opt := range [][3]bool{{true, true, true}, {true, true, false}, {false, true, false}, {false, false, false}} {
		if s := build(opt[0], opt[1], opt[2], name); ansi.StringWidth(s) <= width {
			return s
		}
	}
	rest := ansi.StringWidth(build(false, false, false, ""))
	return build(false, false, false, cut(name, width-rest))
}

// BytesPair is "412/840 MB": done in the unit of total.
func BytesPair(done, total int64) string {
	if total <= 0 {
		return FormatSize(done)
	}
	t := FormatSize(total)
	unit := t[strings.LastIndex(t, " ")+1:]
	div := map[string]float64{"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "PB": 1e15, "EB": 1e18}[unit]
	v := float64(done) / div
	var d string
	if v < 9.95 && unit != "B" {
		d = strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
	} else {
		d = strconv.FormatFloat(math.Floor(v), 'f', 0, 64)
	}
	return d + "/" + t
}

// formatETA is the estimated time left: "—" while the rate is 0.
func formatETA(t app.Transfer) string {
	if t.State != app.Running {
		return ""
	}
	if t.Rate <= 0 || t.ETA <= 0 {
		return "—"
	}
	d := t.ETA.Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// stateText is a transfer's state as the view writes it (FR-SFTP-015).
func stateText(t app.Transfer) string {
	s := t.State.String()
	if t.Reason != "" && t.State != app.Done && t.State != app.Running {
		s += ": " + domain.Sanitize(t.Reason)
	}
	return s
}

// transfersBox draws the transfers view in r, one full-width box.
func (m Model) transfersBox(r Rect) []string {
	inner := max(r.W-2, 0)
	out := make([]string, 0, r.H)
	title := "Transfers"
	if n := len(m.transfers); n > 0 {
		title += " (" + strconv.Itoa(n) + ")"
	}
	out = append(out, "┌ "+titleStyle.Render(title)+" "+strings.Repeat("─", max(inner-2-ansi.StringWidth(title), 0))+"┐")
	rows := max(r.H-2, 0)
	top := max(m.tcursor-rows+1, 0)
	const bytesW, rateW, etaW = 13, 9, 6
	stateW := 14 + max(inner-78, 0)/2
	nameW := max(inner-4-1-stateW-1-bytesW-1-rateW-1-etaW, 4)
	for i := range rows {
		idx := top + i
		s := strings.Repeat(" ", inner)
		switch {
		case idx < len(m.transfers):
			t := m.transfers[idx]
			cur := " "
			if idx == m.tcursor {
				cur = ">"
			}
			arrow := "↑"
			if t.From == app.Remote {
				arrow = "↓"
			}
			bytes := ""
			if t.Total > 0 || t.Done > 0 {
				bytes = BytesPair(t.Done, t.Total)
			}
			rate := ""
			if t.State == app.Running && t.Rate > 0 {
				rate = FormatSize(int64(t.Rate)) + "/s"
			}
			s = cur + " " + arrow + " " + padRight(cut(domain.Sanitize(t.Name), nameW), nameW) + " " +
				padRight(cut(stateText(t), stateW), stateW) + " " + padLeft(bytes, bytesW) + " " + padLeft(rate, rateW) + " " + padLeft(formatETA(t), etaW)
			s = padRight(ansi.Truncate(s, inner, ""), inner)
			if idx == m.tcursor {
				s = cursorStyle.Render(s)
			}
		case i == 0 && len(m.transfers) == 0:
			s = dimStyle.Render(padRight("   No transfers", inner))
		}
		out = append(out, "│"+s+"│")
	}
	out = append(out, "└"+strings.Repeat("─", inner)+"┘")
	return out
}
