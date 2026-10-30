package sftpview

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/RafaelXokito/sshelf/internal/app"
	"github.com/RafaelXokito/sshelf/internal/domain"
	"github.com/RafaelXokito/sshelf/internal/ui/components"
	"github.com/RafaelXokito/sshelf/internal/ui/keys"
	"github.com/RafaelXokito/sshelf/internal/ui/modals/help"
)

// MessageHold is how long a message stays on the progress line at least;
// after it, the next operation clears it (FR-SFTP-024).
const MessageHold = 10 * time.Second

// TabMsg is a message for one SFTP tab's view; the root model delivers it
// to the tab numbered TabNum.
type TabMsg interface{ TabNum() int }

// ListedMsg carries a listing of one pane (design §9.6).
type ListedMsg struct {
	Tab     int
	Side    app.Side
	Seq     int    // the pane's request it answers
	Dir     string // the directory asked for
	Start   string // set on the first listing: the pane's starting directory
	Listing app.Listing
	Err     error
}

// TabNum implements TabMsg.
func (m ListedMsg) TabNum() int { return m.Tab }

// CountedMsg carries the item count of directory Name in Dir.
type CountedMsg struct {
	Tab       int
	Side      app.Side
	Dir, Name string
	N         int
	Err       error
}

// TabNum implements TabMsg.
func (m CountedMsg) TabNum() int { return m.Tab }

// statMsg carries what a link under the cursor points to (enter on a
// symlink, FR-SFTP-007).
type statMsg struct {
	tab   int
	side  app.Side
	seq   int
	path  string
	entry app.Entry
	err   error
}

func (m statMsg) TabNum() int { return m.tab }

// EditRequestMsg asks the root model to edit a remote file locally
// (FR-SFTP-019, T-055).
type EditRequestMsg struct {
	Tab       int
	Dir, Name string
}

// Options configure a Model.
type Options struct {
	// Home is the local home directory, where ~ goes in the local pane
	// (FR-SFTP-007).
	Home string
	// Now is the clock for messages and modification times; nil means
	// time.Now. Times are shown in its location.
	Now func() time.Time
}

// Model is one SFTP tab's view.
type Model struct {
	tab   int
	b     app.Browser
	opts  Options
	panes [2]*pane
	focus app.Side
	// hidden hides names starting with "." in both panes (FR-SFTP-008);
	// they are shown when the tab opens.
	hidden        bool
	disconnected  bool
	width, height int

	msg   string // the progress line's message slot
	msgAt time.Time

	counting *counting // a delete counting what it will delete; nil if none
	countSeq int

	transfers     []app.Transfer // the last snapshot, oldest first
	showTransfers bool           // the transfers view replaces the panes
	tcursor       int            // the selected transfer
	doneSeen      map[app.TransferID]bool
	reported      map[app.BatchID]bool // finished copies already summed up

	drag      drag   // a press on a row and the drag it may start (FR-SFTP-022)
	lastPress *press // the last press on a row, for double-clicks
}

// New returns the view of SFTP tab number tab, browsing with b. The
// remote pane is focused (FR-SFTP-005). Init lists both panes.
func New(tab int, b app.Browser, opts Options) Model {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return Model{
		tab: tab, b: b, opts: opts, focus: app.Remote,
		panes:    [2]*pane{newPane(app.Local), newPane(app.Remote)},
		doneSeen: map[app.TransferID]bool{}, reported: map[app.BatchID]bool{},
	}
}

// WithMessage puts text on the progress line, such as why a reconnect
// failed (FR-SFTP-018).
func (m Model) WithMessage(text string) Model {
	m.setMessage(text)
	return m
}

// Init lists both panes' starting directories.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.listStart(app.Local), m.listStart(app.Remote))
}

// Refresh lists both panes again, keeping each cursor on its name
// (FR-SFTP-013, FR-SFTP-018 after a reconnect).
func (m Model) Refresh() tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range m.panes {
		if !p.loaded {
			cmds = append(cmds, m.listStart(p.side))
			continue
		}
		name, _ := p.cursorName()
		cmds = append(cmds, m.list(p.side, p.dir, name))
	}
	return tea.Batch(cmds...)
}

// Tab is the tab number the view reports with.
func (m Model) Tab() int { return m.tab }

// Focus is the focused pane.
func (m Model) Focus() app.Side { return m.focus }

// Dir is the directory pane side shows.
func (m Model) Dir(side app.Side) string { return m.panes[side].dir }

// Cursor is the name under pane side's cursor, ".." for the parent row.
func (m Model) Cursor(side app.Side) string {
	name, parent := m.panes[side].cursorName()
	if parent {
		return ".."
	}
	return name
}

// Marked returns pane side's marked names.
func (m Model) Marked(side app.Side) []string { return m.panes[side].marked() }

// Message is the progress line's message.
func (m Model) Message() string { return m.msg }

// Filtering reports whether the filter field has the keyboard.
func (m Model) Filtering() bool { return m.panes[m.focus].filtering }

// SetDisconnected sets the connection state the view refuses
// operations in (FR-SFTP-018).
func (m Model) SetDisconnected(d bool) Model {
	m.disconnected = d
	return m
}

// SetSize sets the terminal size the view lays itself out for.
func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	for _, p := range m.panes {
		p.scroll(m.visible(p.side))
	}
	return m
}

// size is the terminal size, 80×24 until it is known.
func (m Model) size() (int, int) {
	if m.width == 0 || m.height == 0 {
		return 80, 24
	}
	return m.width, m.height
}

// Layout is the view's layout at its size.
func (m Model) Layout() Regions { return Layout(m.size()) }

// visible is the number of entry rows pane side shows.
func (m Model) visible(side app.Side) int { return max(m.Layout().Rows[side].H, 1) }

// setMessage puts text on the progress line.
func (m *Model) setMessage(text string) {
	m.msg, m.msgAt = text, m.opts.Now()
}

// setError puts an error on the progress line: "✕ cannot open /root:
// permission denied" (FR-SFTP-024).
func (m *Model) setError(what string, err error) {
	m.setMessage(ErrorMark + " " + what + ": " + errText(err))
}

// expireMessage clears the message at the start of an operation once
// it has been shown for MessageHold (FR-SFTP-024).
func (m *Model) expireMessage() {
	if m.msg != "" && m.opts.Now().Sub(m.msgAt) >= MessageHold {
		m.msg = ""
	}
}

// listStart lists side's starting directory.
func (m Model) listStart(side app.Side) tea.Cmd {
	p := m.panes[side]
	p.seq++
	tab, b, seq := m.tab, m.b, p.seq
	return func() tea.Msg {
		dir := b.Start(side)
		l, err := b.List(context.Background(), side, dir)
		return ListedMsg{Tab: tab, Side: side, Seq: seq, Dir: dir, Start: dir, Listing: l, Err: err}
	}
}

// list lists dir in pane side and puts the cursor on want when the
// listing arrives.
func (m Model) list(side app.Side, dir, want string) tea.Cmd {
	p := m.panes[side]
	p.seq++
	p.want = want
	tab, b, seq := m.tab, m.b, p.seq
	return func() tea.Msg {
		l, err := b.List(context.Background(), side, dir)
		return ListedMsg{Tab: tab, Side: side, Seq: seq, Dir: dir, Listing: l, Err: err}
	}
}

// Update handles a key, a paste or one of the view's messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m, cmd = m.key(msg)
	case tea.PasteMsg:
		if p := m.panes[m.focus]; p.filtering {
			p.filter.Update(msg)
			p.rebuild(m.hidden)
		}
	case ListedMsg:
		m = m.listed(msg)
	case CountedMsg:
		m = m.counted(msg)
	case statMsg:
		m, cmd = m.statted(msg)
	case renameMsg:
		cmd = m.runRename(msg)
	case mkdirMsg:
		cmd = m.runMkdir(msg)
	case deleteCountedMsg:
		m, cmd = m.deleteCounted(msg)
	case deleteMsg:
		cmd = m.runDelete(msg, m.panes[msg.side].deleteWant(msg.names))
	case opDoneMsg:
		m, cmd = m.opDone(msg)
	case SnapshotMsg:
		m, cmd = m.snapshot(msg.Transfers)
	case answerMsg:
		cmd = m.answer(msg)
	case components.ClickMsg:
		m, cmd = m.click(msg)
	case components.WheelMsg:
		m = m.wheel(msg)
	case tea.MouseMotionMsg:
		m = m.motion(msg.Mouse())
	case tea.MouseReleaseMsg:
		m, cmd = m.release(msg.Mouse())
	default:
		return m, nil
	}
	for _, p := range m.panes {
		p.scroll(m.visible(p.side))
	}
	return m, tea.Batch(cmd, m.nextCount(app.Local), m.nextCount(app.Remote))
}

// listed applies a listing (design §9.6): the cursor stays on the same
// name, or goes to the name asked for; a new directory drops the marks,
// the filter and the counts; a failure keeps the listing shown and says
// why (FR-SFTP-007, FR-SFTP-024).
func (m Model) listed(msg ListedMsg) Model {
	p := m.panes[msg.Side]
	if msg.Seq != p.seq {
		return m // an older request
	}
	if msg.Start != "" {
		p.start = msg.Start
	}
	want := p.want
	p.want = ""
	if msg.Err != nil {
		if !p.loaded {
			p.dir, p.title = msg.Dir, domain.Sanitize(msg.Dir)
		}
		m.setError("cannot open "+domain.Sanitize(msg.Dir), msg.Err)
		return m
	}
	l := msg.Listing
	changed := !p.loaded || l.Dir != p.dir
	keep, _ := p.cursorName()
	if changed {
		p.leave()
		p.rows = nil
		keep = ""
	} else {
		p.stopCounts()
		clear(p.counts)
	}
	p.dir, p.title, p.loaded = l.Dir, domain.Sanitize(l.Title), true
	p.entries = slices.Clone(l.Entries)
	sortEntries(p.entries)
	names := map[string]bool{}
	for _, e := range p.entries {
		names[e.Name] = true
	}
	for n := range p.marks {
		if !names[n] {
			delete(p.marks, n) // gone after a refresh (FR-SFTP-009)
		}
	}
	p.rebuild(m.hidden)
	if !p.selectName(want) && !p.selectName(keep) && changed {
		p.cursor = 0
	}
	if w := listingWarning(l); w != "" {
		m.setMessage(WarningMark + " " + w)
	}
	return m
}

// listingWarning is the one warning of a listing that left entries out
// (FR-SFTP-024, SEC-SFTP-004).
func listingWarning(l app.Listing) string {
	var w string
	if l.Skipped > 0 {
		w = fmt.Sprintf("%d %s with unsafe names not shown", l.Skipped, plural(l.Skipped, "entry", "entries"))
	}
	if l.Truncated {
		if w != "" {
			w += "; "
		}
		w += "listing stopped at 100,000 entries"
	}
	return w
}

// plural picks the singular or plural word for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// nextCount starts the next item count of pane side: one at a time, for
// the directory rows on screen only (FR-SFTP-006).
func (m Model) nextCount(side app.Side) tea.Cmd {
	p := m.panes[side]
	if p.counting != "" || !p.loaded || (m.disconnected && side == app.Remote) {
		return nil
	}
	end := min(p.top+m.visible(side), len(p.rows))
	for i := p.top; i < end; i++ {
		r := p.rows[i]
		if r.parent || r.e.Kind != app.KindDir {
			continue
		}
		if _, ok := p.counts[r.e.Name]; ok {
			continue
		}
		if p.countStop == nil {
			var ctx context.Context
			ctx, p.countStop = context.WithCancel(context.Background())
			p.countCtx = ctx
		}
		ctx, tab, b, dir, name := p.countCtx, m.tab, m.b, p.dir, r.e.Name
		target := p.join(dir, name)
		p.counting = name
		return func() tea.Msg {
			n, err := b.Count(ctx, side, target, false)
			return CountedMsg{Tab: tab, Side: side, Dir: dir, Name: name, N: n, Err: err}
		}
	}
	return nil
}

// counted records an item count and starts the next.
func (m Model) counted(msg CountedMsg) Model {
	p := m.panes[msg.Side]
	if msg.Dir != p.dir || msg.Name != p.counting {
		return m // the pane left that directory
	}
	p.counting = ""
	switch {
	case errors.Is(msg.Err, context.Canceled):
	case msg.Err != nil:
		p.counts[msg.Name] = countFailed
	default:
		p.counts[msg.Name] = msg.N
	}
	return m
}

// statted opens a link's path when it points to a directory
// (FR-SFTP-007).
func (m Model) statted(msg statMsg) (Model, tea.Cmd) {
	p := m.panes[msg.side]
	if msg.seq != p.seq {
		return m, nil
	}
	if msg.err != nil {
		m.setError("cannot open "+domain.Sanitize(msg.path), msg.err)
		return m, nil
	}
	if msg.entry.Kind != app.KindDir {
		return m, nil
	}
	return m, m.list(msg.side, msg.path, "")
}

// key handles a key on the tab (design §9.6): the filter field while it
// has the keyboard, otherwise the panes' keys.
func (m Model) key(k tea.KeyPressMsg) (Model, tea.Cmd) {
	p := m.panes[m.focus]
	if p.filtering {
		switch {
		case key.Matches(k, keys.FilterKeep):
			p.filtering = false
			p.filter.Blur()
		case key.Matches(k, keys.FilterClear):
			p.filtering = false
			p.filter.Blur()
			p.filter.Reset()
			p.rebuild(m.hidden)
		default:
			p.filter.Update(k)
			p.rebuild(m.hidden)
		}
		return m, nil
	}
	if m.counting != nil && key.Matches(k, keys.Cancel) {
		m.stopCounting() // FR-SFTP-011: esc cancels the count, nothing is deleted
		return m, nil
	}
	m.expireMessage()
	if m.showTransfers {
		return m.transferKey(k)
	}
	if m.disconnected && !m.browsable(k) {
		m.setMessage(ErrorMark + " not connected")
		return m, nil
	}
	return m.paneKey(p, k)
}

// browsable reports whether k works while the connection is lost:
// switching panes, and browsing the local pane (FR-SFTP-018).
func (m Model) browsable(k tea.KeyPressMsg) bool {
	if key.Matches(k, keys.SwitchPane, keys.SFTPHelp, keys.Transfers) {
		return true
	}
	return m.focus == app.Local && key.Matches(k, keys.Up, keys.Down, keys.PageUp, keys.PageDown,
		keys.First, keys.Last, keys.OpenDir, keys.ParentDir, keys.HomeDir, keys.Hidden, keys.Filter, keys.Mark, keys.Refresh, keys.FilterClear)
}

// paneKey handles the panes' keys (FR-SFTP-007 … FR-SFTP-013).
func (m Model) paneKey(p *pane, k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(k, keys.Up):
		p.move(-1)
	case key.Matches(k, keys.Down):
		p.move(1)
	case key.Matches(k, keys.PageUp):
		p.move(-m.visible(p.side))
	case key.Matches(k, keys.PageDown):
		p.move(m.visible(p.side))
	case key.Matches(k, keys.First):
		p.cursor = 0
	case key.Matches(k, keys.Last):
		p.cursor = max(len(p.rows)-1, 0)
	case key.Matches(k, keys.SwitchPane):
		m.focus = m.focus.Other()
	case key.Matches(k, keys.OpenDir):
		return m.open(p)
	case key.Matches(k, keys.ParentDir):
		return m, m.parent(p)
	case key.Matches(k, keys.HomeDir):
		return m, m.home(p)
	case key.Matches(k, keys.Mark):
		if r, ok := p.current(); ok && !r.parent {
			p.marks[r.e.Name] = !p.marks[r.e.Name]
			if !p.marks[r.e.Name] {
				delete(p.marks, r.e.Name)
			}
			p.move(1)
		}
	case key.Matches(k, keys.Refresh):
		if m.disconnected {
			return m, m.refreshSide(app.Local)
		}
		return m, m.Refresh()
	case key.Matches(k, keys.Hidden):
		m.hidden = !m.hidden
		for _, q := range m.panes {
			q.rebuild(m.hidden)
		}
	case key.Matches(k, keys.Filter):
		p.filtering = true
		p.filter.Focus()
		p.filter.End()
	case key.Matches(k, keys.FilterClear):
		if p.filter.Value() != "" {
			p.filter.Reset()
			p.rebuild(m.hidden)
		}
	case key.Matches(k, keys.SFTPHelp):
		modal := help.NewList("SFTP tab keys", keys.SFTPPaneKeys())
		return m, func() tea.Msg { return components.ShowModalMsg{Modal: modal} }
	case key.Matches(k, keys.Transfers):
		m.showTransfers = true
	case key.Matches(k, keys.Copy):
		return m, m.copySelection(p)
	case key.Matches(k, keys.Rename):
		return m, m.rename(p)
	case key.Matches(k, keys.Mkdir):
		return m, m.mkdir(p)
	case key.Matches(k, keys.Delete):
		return m.delete(p)
	case key.Matches(k, keys.EditLocally):
		return m.edit(p)
	}
	return m, nil
}

// edit asks the root model to edit the remote file under the cursor
// (FR-SFTP-019); anything else only says why not. A link is sent on: the
// back end resolves it.
func (m Model) edit(p *pane) (Model, tea.Cmd) {
	r, ok := p.current()
	switch {
	case !ok:
		return m, nil
	case p.side == app.Local:
		m.setMessage("e edits remote files; this is the local pane")
	case r.parent || r.e.Kind == app.KindDir:
		m.setMessage("cannot edit a directory")
	case r.e.Kind == app.KindOther:
		m.setMessage(displayName(*r.e) + ": not a regular file")
	case r.e.Kind == app.KindFile && r.e.Size > app.EditMaxSize:
		m.setMessage(displayName(*r.e) + ": too large to edit (over 64 MiB)")
	default:
		tab, dir, name := m.tab, p.dir, r.e.Name
		return m, func() tea.Msg { return EditRequestMsg{Tab: tab, Dir: dir, Name: name} }
	}
	return m, nil
}

// refreshSide lists one pane again.
func (m Model) refreshSide(side app.Side) tea.Cmd {
	p := m.panes[side]
	if !p.loaded {
		return m.listStart(side)
	}
	name, _ := p.cursorName()
	return m.list(side, p.dir, name)
}

// open opens the row under the cursor: "..", a directory, or a link to
// a directory, by its own path (FR-SFTP-007). Files do nothing.
func (m Model) open(p *pane) (Model, tea.Cmd) {
	r, ok := m.panes[p.side].current()
	if !ok {
		return m, nil
	}
	switch {
	case r.parent:
		return m, m.parent(p)
	case r.e.Kind == app.KindDir:
		return m, m.list(p.side, p.join(p.dir, r.e.Name), "")
	case r.e.Kind == app.KindSymlink:
		p.seq++
		tab, b, side, seq, target := m.tab, m.b, p.side, p.seq, p.join(p.dir, r.e.Name)
		return m, func() tea.Msg {
			e, err := b.Stat(context.Background(), side, target)
			return statMsg{tab: tab, side: side, seq: seq, path: target, entry: e, err: err}
		}
	}
	return m, nil
}

// parent goes to the parent directory with the cursor on the directory
// just left (FR-SFTP-007).
func (m Model) parent(p *pane) tea.Cmd {
	dir, name, ok := p.parentDir()
	if !ok || p.dir == "" { // a start directory that failed to list still has a parent
		return nil
	}
	return m.list(p.side, dir, name)
}

// home goes to the local home directory, or the remote starting
// directory (FR-SFTP-007).
func (m Model) home(p *pane) tea.Cmd {
	dir := p.start
	if p.side == app.Local && m.opts.Home != "" {
		dir = m.opts.Home
	}
	if dir == "" {
		return nil
	}
	return m.list(p.side, dir, "")
}
