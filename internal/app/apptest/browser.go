package apptest

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"slices"
	"sync"

	"github.com/RafaelXokito/sshelf/internal/app"
)

// FakeBrowser is the "fake browser" of the M3 tasks (tasks.md): an
// app.Browser with canned listings that the file operations change in
// memory, and a scripted transfer queue. Its zero value is not usable;
// call NewFakeBrowser. It is safe for concurrent use.
type FakeBrowser struct {
	// Check, when set, is called with the method name at the start of
	// every method that may block, so a UI test can fail when one runs on
	// the Update goroutine (ARCH-UI-002).
	Check func(method string)

	mu        sync.Mutex
	connID    uint64
	start     [2]string
	dirs      [2]map[string]*fakeDir
	errs      [2]map[string]error // by path, for List, Stat, Rename, Mkdir and Delete
	stats     [2]map[string]app.Entry
	counts    [2]map[string]int // by dir; a missing dir counts its canned entries
	countGate chan struct{}
	countDirs []string
	transfers []app.Transfer
	copies    []Copy
	answers   []Answered
	cancels   []app.TransferID
	retries   []app.TransferID
	clears    int
	nextBatch app.BatchID
	state     app.ConnState
	lists     int
	closes    int
	editErr   error      // EditFetch's error
	editSteps []EditStep // what EditFinish and EditResolve return next
	edits     []string   // the recorded edit calls

	updates chan struct{}
	done    chan struct{}
}

type fakeDir struct {
	title   string
	entries []app.Entry
	skipped int
	trunc   bool
}

// Copy is one recorded Browser.Copy call.
type Copy struct {
	From   app.Side
	SrcDir string
	Names  []string
	DstDir string
}

// Answered is one recorded Browser.Answer call.
type Answered struct {
	ID     app.TransferID
	Answer app.Answer
	All    bool
}

// NewFakeBrowser returns a connected browser on connection connID whose
// panes start in localStart and remoteStart.
func NewFakeBrowser(connID uint64, localStart, remoteStart string) *FakeBrowser {
	f := &FakeBrowser{
		connID:  connID,
		start:   [2]string{localStart, remoteStart},
		updates: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	for i := range 2 {
		f.dirs[i] = map[string]*fakeDir{}
		f.errs[i] = map[string]error{}
		f.stats[i] = map[string]app.Entry{}
		f.counts[i] = map[string]int{}
	}
	return f
}

// SetDir sets the listing of dir on side, shown with title.
func (f *FakeBrowser) SetDir(side app.Side, dir, title string, entries ...app.Entry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs[side][dir] = &fakeDir{title: title, entries: slices.Clone(entries)}
}

// SetListingNotes marks dir's listing with skipped unsafe names and a
// cut (FR-SFTP-024, SEC-SFTP-004).
func (f *FakeBrowser) SetListingNotes(side app.Side, dir string, skipped int, truncated bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d := f.dirs[side][dir]; d != nil {
		d.skipped, d.trunc = skipped, truncated
	}
}

// SetErr makes operations on p (a directory for List, Mkdir and
// Delete, a path for Stat and Rename) fail with err; nil clears it.
func (f *FakeBrowser) SetErr(side app.Side, p string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.errs[side], p)
		return
	}
	f.errs[side][p] = err
}

// SetStat sets what Stat returns for p (the followed link).
func (f *FakeBrowser) SetStat(side app.Side, p string, e app.Entry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats[side][p] = e
}

// SetCount sets what Count returns for dir.
func (f *FakeBrowser) SetCount(side app.Side, dir string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[side][dir] = n
}

// GateCounts makes every Count call wait until the returned function is
// called or its context ends.
func (f *FakeBrowser) GateCounts() (release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	gate := make(chan struct{})
	f.countGate = gate
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

// SetTransfers replaces the queue snapshot and notifies Updates.
func (f *FakeBrowser) SetTransfers(ts ...app.Transfer) {
	f.mu.Lock()
	f.transfers = slices.Clone(ts)
	f.mu.Unlock()
	f.Notify()
}

// Notify sends a coalesced update, never blocking.
func (f *FakeBrowser) Notify() {
	select {
	case f.updates <- struct{}{}:
	default:
	}
}

// Drop simulates a lost connection: State becomes Disconnected, running
// and queued transfers become paused, and Done is closed.
func (f *FakeBrowser) Drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state == app.Disconnected {
		return
	}
	f.state = app.Disconnected
	for i, t := range f.transfers {
		if t.State == app.Running || t.State == app.Queued {
			f.transfers[i].State = app.Paused
		}
	}
	close(f.done)
}

// Reattach is what a successful ReconnectSFTP does to the fake: State
// becomes Connected with a new Done channel, and paused transfers are
// queued again.
func (f *FakeBrowser) Reattach() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = app.Connected
	f.done = make(chan struct{})
	for i, t := range f.transfers {
		if t.State == app.Paused {
			f.transfers[i].State = app.Queued
		}
	}
}

// CountCalls returns the directories passed to Count, in order.
func (f *FakeBrowser) CountCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.countDirs)
}

// Copies returns the recorded Copy calls.
func (f *FakeBrowser) Copies() []Copy {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.copies)
}

// Answers returns the recorded Answer calls.
func (f *FakeBrowser) Answers() []Answered {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.answers)
}

// Cancels returns the IDs passed to Cancel.
func (f *FakeBrowser) Cancels() []app.TransferID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.cancels)
}

// Retries returns the IDs passed to Retry.
func (f *FakeBrowser) Retries() []app.TransferID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.retries)
}

// Clears returns how many times Clear was called.
func (f *FakeBrowser) Clears() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clears
}

// Lists returns how many times List was called.
func (f *FakeBrowser) Lists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

// Closes returns how many times Close was called.
func (f *FakeBrowser) Closes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

// Entries returns the current canned entries of dir.
func (f *FakeBrowser) Entries(side app.Side, dir string) []app.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d := f.dirs[side][dir]; d != nil {
		return slices.Clone(d.entries)
	}
	return nil
}

func (f *FakeBrowser) check(method string) {
	if f.Check != nil {
		f.Check(method)
	}
}

// connected returns ErrNotConnected while the connection is lost; the
// local side always works. The caller holds mu.
func (f *FakeBrowser) connected(side app.Side) error {
	if side == app.Remote && f.state == app.Disconnected {
		return app.ErrNotConnected
	}
	return nil
}

// ConnID implements app.Browser.
func (f *FakeBrowser) ConnID() uint64 { return f.connID }

// Start implements app.Browser.
func (f *FakeBrowser) Start(side app.Side) string {
	f.check("Start")
	return f.start[side]
}

// List implements app.Browser.
func (f *FakeBrowser) List(_ context.Context, side app.Side, dir string) (app.Listing, error) {
	f.check("List")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	if err := f.connected(side); err != nil {
		return app.Listing{}, err
	}
	if err := f.errs[side][dir]; err != nil {
		return app.Listing{}, err
	}
	d := f.dirs[side][dir]
	if d == nil {
		return app.Listing{}, fs.ErrNotExist
	}
	return app.Listing{Side: side, Dir: dir, Title: d.title, Entries: slices.Clone(d.entries), Skipped: d.skipped, Truncated: d.trunc}, nil
}

// Count implements app.Browser.
func (f *FakeBrowser) Count(ctx context.Context, side app.Side, dir string, deep bool) (int, error) {
	f.check("Count")
	f.mu.Lock()
	gate := f.countGate
	f.countDirs = append(f.countDirs, dir)
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.connected(side); err != nil {
		return 0, err
	}
	if n, ok := f.counts[side][dir]; ok {
		return n, nil
	}
	if d := f.dirs[side][dir]; d != nil {
		return len(d.entries), nil
	}
	return 0, nil
}

// Stat implements app.Browser.
func (f *FakeBrowser) Stat(_ context.Context, side app.Side, p string) (app.Entry, error) {
	f.check("Stat")
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.connected(side); err != nil {
		return app.Entry{}, err
	}
	if err := f.errs[side][p]; err != nil {
		return app.Entry{}, err
	}
	if e, ok := f.stats[side][p]; ok {
		return e, nil
	}
	if _, ok := f.dirs[side][p]; ok {
		return app.Entry{Name: path.Base(p), Display: path.Base(p), Kind: app.KindDir}, nil
	}
	return app.Entry{}, fs.ErrNotExist
}

// find returns the index of name in d, or -1.
func (d *fakeDir) find(name string) int {
	return slices.IndexFunc(d.entries, func(e app.Entry) bool { return e.Name == name })
}

// Rename implements app.Browser. It refuses an existing target, as
// sshelf never overwrites by renaming (FR-SFTP-010).
func (f *FakeBrowser) Rename(_ context.Context, side app.Side, dir, from, to string) error {
	f.check("Rename")
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.connected(side); err != nil {
		return err
	}
	if err := f.errs[side][path.Join(dir, from)]; err != nil {
		return err
	}
	d := f.dirs[side][dir]
	if d == nil {
		return fs.ErrNotExist
	}
	i := d.find(from)
	if i < 0 {
		return fs.ErrNotExist
	}
	if d.find(to) >= 0 {
		return fs.ErrExist
	}
	d.entries[i].Name, d.entries[i].Display = to, to
	return nil
}

// Mkdir implements app.Browser.
func (f *FakeBrowser) Mkdir(_ context.Context, side app.Side, dir, name string) error {
	f.check("Mkdir")
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.connected(side); err != nil {
		return err
	}
	if err := f.errs[side][dir]; err != nil {
		return err
	}
	d := f.dirs[side][dir]
	if d == nil {
		return fs.ErrNotExist
	}
	if d.find(name) >= 0 {
		return fs.ErrExist
	}
	d.entries = append(d.entries, app.Entry{Name: name, Display: name, Kind: app.KindDir, Mode: 0o755})
	f.dirs[side][path.Join(dir, name)] = &fakeDir{title: path.Join(d.title, name)}
	return nil
}

// Delete implements app.Browser: it removes names in order and stops at
// the first one whose path has an error set.
func (f *FakeBrowser) Delete(_ context.Context, side app.Side, dir string, names []string) (int, error) {
	f.check("Delete")
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.connected(side); err != nil {
		return 0, err
	}
	d := f.dirs[side][dir]
	if d == nil {
		return 0, fs.ErrNotExist
	}
	for n, name := range names {
		if err := f.errs[side][path.Join(dir, name)]; err != nil {
			return n, err
		}
		i := d.find(name)
		if i < 0 {
			return n, fs.ErrNotExist
		}
		d.entries = slices.Delete(d.entries, i, i+1)
	}
	return len(names), nil
}

// Copy implements app.Browser: it records the call and returns a new
// batch ID; the test scripts the transfers with SetTransfers.
func (f *FakeBrowser) Copy(from app.Side, srcDir string, names []string, dstDir string) app.BatchID {
	f.check("Copy")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.copies = append(f.copies, Copy{From: from, SrcDir: srcDir, Names: slices.Clone(names), DstDir: dstDir})
	f.nextBatch++
	return f.nextBatch
}

// Transfers implements app.Browser.
func (f *FakeBrowser) Transfers() []app.Transfer {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.transfers)
	for i, t := range out {
		if t.Conflict != nil {
			q := *t.Conflict
			out[i].Conflict = &q
		}
	}
	return out
}

// Answer implements app.Browser.
func (f *FakeBrowser) Answer(id app.TransferID, a app.Answer, all bool) {
	f.check("Answer")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, Answered{ID: id, Answer: a, All: all})
}

// Cancel implements app.Browser.
func (f *FakeBrowser) Cancel(id app.TransferID) {
	f.check("Cancel")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, id)
}

// Retry implements app.Browser.
func (f *FakeBrowser) Retry(id app.TransferID) {
	f.check("Retry")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries = append(f.retries, id)
}

// Clear implements app.Browser: it removes the finished rows.
func (f *FakeBrowser) Clear() {
	f.check("Clear")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clears++
	f.transfers = slices.DeleteFunc(f.transfers, func(t app.Transfer) bool { return t.State.Finished() })
}

// Updates implements app.Browser.
func (f *FakeBrowser) Updates() <-chan struct{} { return f.updates }

// State implements app.Browser.
func (f *FakeBrowser) State() app.ConnState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

// Done implements app.Browser.
func (f *FakeBrowser) Done() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.done
}

// Close implements app.Browser. It is idempotent.
func (f *FakeBrowser) Close() error {
	f.check("Close")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return nil
}

// EditStep is what one EditFinish or EditResolve call returns.
type EditStep struct {
	Result app.EditResult
	Err    error
}

// SetEditErr makes EditFetch fail with err; nil clears it.
func (f *FakeBrowser) SetEditErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editErr = err
}

// QueueEdit sets what the next EditFinish and EditResolve calls return,
// in order. Without a step, EditFinish reports EditUnchanged and
// EditResolve EditDiscarded.
func (f *FakeBrowser) QueueEdit(steps ...EditStep) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editSteps = append(f.editSteps, steps...)
}

// EditCalls returns the recorded edit calls: "fetch <path>", "finish
// <name>" and "resolve <choice> <name> <localDir>".
func (f *FakeBrowser) EditCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.edits)
}

// editStep pops the next step, or returns def.
func (f *FakeBrowser) editStep(def app.EditOutcome) (app.EditResult, error) {
	if len(f.editSteps) == 0 {
		return app.EditResult{Outcome: def}, nil
	}
	s := f.editSteps[0]
	f.editSteps = f.editSteps[1:]
	return s.Result, s.Err
}

// EditFetch implements app.Browser: it records the call and returns a
// copy whose Path does not exist; UI tests never run an editor on it.
func (f *FakeBrowser) EditFetch(_ context.Context, dir, name string) (*app.EditCopy, error) {
	f.check("EditFetch")
	f.mu.Lock()
	defer f.mu.Unlock()
	p := path.Join(dir, name)
	f.edits = append(f.edits, "fetch "+p)
	if err := f.connected(app.Remote); err != nil {
		return nil, err
	}
	if f.editErr != nil {
		return nil, f.editErr
	}
	return &app.EditCopy{Name: name, Path: path.Join("/fake-tmp/sshelf-edit-1", name), Remote: p}, nil
}

// EditFinish implements app.Browser.
func (f *FakeBrowser) EditFinish(_ context.Context, c *app.EditCopy) (app.EditResult, error) {
	f.check("EditFinish")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits = append(f.edits, "finish "+c.Name)
	return f.editStep(app.EditUnchanged)
}

// EditResolve implements app.Browser.
func (f *FakeBrowser) EditResolve(_ context.Context, c *app.EditCopy, choice app.EditChoice, localDir string) (app.EditResult, error) {
	f.check("EditResolve")
	f.mu.Lock()
	defer f.mu.Unlock()
	names := map[app.EditChoice]string{app.EditOverwrite: "overwrite", app.EditKeepRemote: "keep", app.EditSaveRemote: "save-remote",
		app.EditSaveLocal: "save-local", app.EditDiscard: "discard", app.EditRetry: "retry"}
	f.edits = append(f.edits, "resolve "+names[choice]+" "+c.Name+" "+localDir)
	return f.editStep(app.EditDiscarded)
}

// ErrFake is a convenient error for tests.
var ErrFake = errors.New("permission denied")

var _ app.Browser = (*FakeBrowser)(nil)
