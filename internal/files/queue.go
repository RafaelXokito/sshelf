package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/RafaelXokito/sshelf/internal/term"
)

// The transfer queue (ADR-018, design §8.9): FIFO, two transfers at a
// time, each in its own goroutine started through term.SafeGo. Its state
// lives in the Browser under b.mu; the UI reads it through Transfers and
// is told about state changes through Updates (ARCH-SFTP-003).

// TransferID names one transfer of a tab.
type TransferID uint64

// BatchID names the transfers queued by one copy (one c or drag).
type BatchID uint64

// TransferState is where a transfer is (design §8.9).
type TransferState int

// Transfer states.
const (
	Queued TransferState = iota
	Running
	Waiting // the target exists; Conflict says what to ask
	Paused  // the connection was lost
	Done
	Failed // Reason says why
	Cancelled
	Skipped
)

// String is the state as the transfers view writes it (FR-SFTP-015).
func (s TransferState) String() string {
	return [...]string{"queued", "running", "waiting", "paused", "done", "failed", "cancelled", "skipped"}[s]
}

// Finished reports whether the transfer will not run again unless it is
// retried.
func (s TransferState) Finished() bool {
	return s == Done || s == Failed || s == Cancelled || s == Skipped
}

// Pending reports whether the transfer still counts as work to stop:
// queued, running, waiting or paused (FR-SFTP-025, FR-TAB-009).
func (s TransferState) Pending() bool { return !s.Finished() }

// FileInfo is the size and modification time of one side of a conflict.
type FileInfo struct {
	Size    int64
	ModTime time.Time
}

// Conflict is what a waiting transfer asks (FR-SFTP-016): the target
// exists.
type Conflict struct {
	Local, Remote FileInfo
}

// Transfer is one row of the transfers snapshot.
type Transfer struct {
	ID    TransferID
	Batch BatchID
	// From is the side the data comes from: Remote is a download (↓),
	// Local an upload (↑).
	From   Side
	Name   string // displayed, sanitised; a path relative to the copy's source dir
	State  TransferState
	Done   int64         // bytes transferred
	Total  int64         // bytes in all; 0 when unknown (a directory)
	Rate   float64       // bytes per second, a 5 s moving average
	ETA    time.Duration // 0 when the rate is 0
	Reason string        // why it failed, was skipped or restarted; sanitised
	// DstDir is the directory the transfer writes into, so the UI can
	// refresh a pane showing it when the transfer is done.
	DstDir   string
	Conflict *Conflict // set while Waiting
}

// Answer is the reply to a waiting transfer's question (FR-SFTP-016).
type Answer int

// Answers.
const (
	Overwrite Answer = iota
	Skip
	KeepBoth
)

const (
	// Workers is how many transfers run at once (ADR-018: a constant,
	// not a setting).
	Workers = 2
	// HistoryLimit is the most rows the transfers list keeps
	// (FR-SFTP-015).
	HistoryLimit = 1000
	// rateWindow is the span of the moving-average rate (FR-SFTP-014).
	rateWindow = 5 * time.Second
	// sampleEvery spaces the rate samples.
	sampleEvery = 250 * time.Millisecond
)

// Errors of the queue. Their texts are what the transfers view shows.
var (
	ErrNotFile     = errors.New("exists and is not a file")
	ErrInternal    = errors.New("internal error")
	errSizeChanged = errors.New("size changed during transfer")
	errLost        = errors.New("connection lost")
)

// kindUnknown marks a job named in Copy: its kind is read when it starts,
// so Copy does no I/O.
const kindUnknown Kind = -1

// job is one transfer: a file, or a directory that lists its source and
// queues its children when it runs. Every field is guarded by b.mu; a
// running job's goroutine works on a copy (task) and reports through the
// Browser's methods.
type job struct {
	id     TransferID
	batch  *batch
	parent *job
	from   Side
	srcDir string   // the source's directory, exact bytes
	name   string   // the source's name, one element
	rel    string   // the source relative to the copy's source dir, for display
	dstTop string   // the copy's target directory
	dstRel []string // the directories below dstTop the job writes into
	target string   // the target's name; KeepBoth changes it
	depth  int      // 1 for the names of Copy
	kind   Kind

	state     TransferState
	done      int64
	total     int64
	reason    string
	conflict  *Conflict
	overwrite bool // answered Overwrite
	keepBoth  bool // answered KeepBoth
	cancelReq bool
	cancel    context.CancelFunc
	samples   []sample
	rec       *record     // the source when the transfer started (ARCH-SFTP-005)
	cp        *checkpoint // an upload's last checkpoint
}

// record is the source's size and modification time when a transfer
// started; a resume requires them unchanged (FR-SFTP-017).
type record struct {
	size  int64
	mtime time.Time
}

// checkpoint is what an upload's part file looked like at an
// acknowledged offset: the server had every byte before off
// (ARCH-SFTP-005).
type checkpoint struct {
	off   int64
	size  int64
	mtime time.Time
}

// sample is the bytes done at one moment, for the rate.
type sample struct {
	at   time.Time
	done int64
}

// batch is the transfers of one copy; "apply to all" is stored on it
// (FR-SFTP-016).
type batch struct {
	id  BatchID
	all *Answer
}

// dstDir is the directory the job writes into, as its side spells it.
func (j *job) dstDir() string {
	if j.from == Remote { // a download writes locally
		return filepath.Join(append([]string{j.dstTop}, j.dstRel...)...)
	}
	return path.Join(append([]string{j.dstTop}, j.dstRel...)...)
}

// srcPath is the source, as its side spells it.
func (j *job) srcPath() string {
	if j.from == Remote {
		return path.Join(j.srcDir, j.name)
	}
	return filepath.Join(j.srcDir, j.name)
}

// under reports whether j is below dir in the tree of a copy.
func (j *job) under(dir *job) bool {
	for p := j.parent; p != nil; p = p.parent {
		if p == dir {
			return true
		}
	}
	return false
}

// Copy queues a copy of names from srcDir on side from into dstDir on the
// other side and returns at once (FR-SFTP-001): it does no I/O. Each name
// becomes one transfer; directories queue their contents when they run.
func (b *Browser) Copy(from Side, srcDir string, names []string, dstDir string) BatchID {
	b.mu.Lock()
	b.nextBatch++
	bt := &batch{id: b.nextBatch}
	for _, name := range names {
		j := b.newJob(bt, nil, from, srcDir, name, name, dstDir, nil, 1, kindUnknown)
		if err := CheckName(name); err != nil {
			j.state, j.reason = Failed, err.Error()
		} else {
			j.state = b.queued()
			b.pending = append(b.pending, j)
		}
		b.jobs = append(b.jobs, j)
	}
	b.trimHistory()
	b.schedule()
	b.mu.Unlock()
	b.notify()
	return bt.id
}

// newJob makes a queued job. The caller holds b.mu and places it.
func (b *Browser) newJob(bt *batch, parent *job, from Side, srcDir, name, rel, top string, dstRel []string, depth int, kind Kind) *job {
	b.nextID++
	return &job{
		id: b.nextID, batch: bt, parent: parent, from: from,
		srcDir: srcDir, name: name, rel: rel, dstTop: top, dstRel: dstRel,
		target: name, depth: depth, kind: kind,
	}
}

// child is an entry a directory job found in its source.
type child struct {
	name string
	kind Kind
	size int64
}

// addChildren queues the entries of the directory job j right after it,
// in name order, so trees are copied depth-first (ADR-018). Links and
// special files become skipped rows (FR-SFTP-001); directories past
// MaxDepth too (SEC-SFTP-004). unsafe counts names left out of the
// listing (SEC-SFTP-001).
func (b *Browser) addChildren(j *job, children []child, unsafe int, truncated bool) {
	slices.SortFunc(children, func(a, c child) int { return strings.Compare(a.name, c.name) })
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || j.cancelReq {
		return
	}
	srcDir := j.srcPath()
	dstRel := append(slices.Clone(j.dstRel), j.target)
	var rows, run []*job
	for _, c := range children {
		k := b.newJob(j.batch, j, j.from, srcDir, c.name, path.Join(j.rel, c.name), j.dstTop, dstRel, j.depth+1, c.kind)
		k.total = c.size
		switch {
		case c.kind == Symlink:
			k.state, k.reason, k.total = Skipped, "symbolic link", 0
		case c.kind == Other:
			k.state, k.reason, k.total = Skipped, "special file", 0
		case c.kind == Dir && k.depth > MaxDepth:
			k.state, k.reason = Skipped, ErrTooDeep.Error()
		case c.kind == kindUnknown:
			k.state, k.reason, k.total = Skipped, ErrBadName.Error(), 0
		default:
			run = append(run, k)
		}
		rows = append(rows, k)
	}
	if unsafe > 0 {
		k := b.newJob(j.batch, j, j.from, srcDir, "", j.rel, j.dstTop, dstRel, j.depth+1, Other)
		k.state, k.reason = Skipped, plural(unsafe, "unsafe name")+" skipped"
		rows = append(rows, k)
	}
	if truncated {
		j.reason = "listing stopped at 100,000 entries"
	}
	i := slices.Index(b.jobs, j) + 1
	if i == 0 { // cleared from the list while it ran
		i = len(b.jobs)
	}
	b.jobs = slices.Insert(b.jobs, i, rows...)
	b.pending = slices.Insert(b.pending, 0, run...)
	b.trimHistory()
}

// plural is "1 thing" or "n things".
func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
}

// trimHistory drops the oldest finished rows past HistoryLimit. The
// caller holds b.mu.
func (b *Browser) trimHistory() {
	extra := len(b.jobs) - HistoryLimit
	if extra <= 0 {
		return
	}
	b.jobs = slices.DeleteFunc(b.jobs, func(j *job) bool {
		if extra > 0 && j.state.Finished() {
			extra--
			return true
		}
		return false
	})
}

// queued is the state of a job put in the queue: Paused while the
// connection is lost. The caller holds b.mu.
func (b *Browser) queued() TransferState {
	if b.state == Disconnected {
		return Paused
	}
	return Queued
}

// setRecord records what a transfer started from, and why it started
// over when it could not resume.
func (b *Browser) setRecord(j *job, rec *record, restarted string) {
	b.mu.Lock()
	j.rec = rec
	if restarted != "" {
		j.cp = nil
		j.reason = "restarted: " + restarted
	}
	b.mu.Unlock()
}

// setCheckpoint records an upload's checkpoint.
func (b *Browser) setCheckpoint(j *job, cp *checkpoint) {
	b.mu.Lock()
	j.cp = cp
	b.mu.Unlock()
}

// schedule starts queued jobs while fewer than Workers run and the
// connection is up. The caller holds b.mu.
func (b *Browser) schedule() {
	for b.running < Workers && !b.closed && b.state == Connected && len(b.pending) > 0 {
		j := b.pending[0]
		b.pending = b.pending[1:]
		if j.state != Queued {
			continue
		}
		b.start(j)
	}
}

// start runs j in a new goroutine. The caller holds b.mu.
func (b *Browser) start(j *job) {
	ctx, cancel := context.WithCancel(b.ctx)
	j.state, j.cancel, j.cancelReq = Running, cancel, false
	j.samples = append(j.samples[:0], sample{at: time.Now(), done: j.done})
	t := b.task(j)
	b.running++
	b.wg.Add(1)
	if testHookStart != nil {
		testHookStart(j.rel)
	}
	term.SafeGo(func() {
		err := b.run(ctx, t)
		if err != nil && ctx.Err() == nil && t.remote.lost(err) {
			err = errLost
		}
		b.finish(j, err)
	}, func(any) {
		b.finish(j, ErrInternal)
	})
}

// testHookStart sees every job start, by its relative name.
var testHookStart func(rel string)

// finish records how a run of j ended and starts the next jobs.
func (b *Browser) finish(j *job, err error) {
	b.mu.Lock()
	j.cancel()
	b.running--
	var cf *conflictError
	var sk *skipError
	switch {
	case err == nil:
		j.state = Done
	case j.cancelReq || b.closed:
		j.state = Cancelled
		if !errors.Is(err, context.Canceled) {
			j.reason = errText(err)
		}
	case errors.As(err, &cf):
		j.state, j.conflict = Waiting, &cf.c
		if a := j.batch.all; a != nil && b.answer(j, *a) { // answered for all while it ran
			b.pending = slices.Insert(b.pending, 0, j)
		}
	case errors.Is(err, errLost):
		// Attach queues it again, unless Attach already ran while it failed.
		j.state = Paused
		if b.state == Connected {
			j.state = Queued
			b.pending = slices.Insert(b.pending, 0, j)
		}
	case errors.As(err, &sk):
		j.state, j.reason = Skipped, sk.reason
	default:
		j.state, j.reason = Failed, errText(err)
	}
	j.samples = nil
	b.schedule()
	b.mu.Unlock()
	b.wg.Done()
	b.notify()
}

// conflictError parks a job as Waiting.
type conflictError struct{ c Conflict }

func (e *conflictError) Error() string { return ErrExists.Error() }

// skipError skips a job with a reason.
type skipError struct{ reason string }

func (e *skipError) Error() string { return e.reason }

// errText is err as the transfers view shows it: a local error without
// its path, a server message as sanitised by remoteErr, on one line.
func errText(err error) string {
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		err = pe.Err
	case errors.As(err, &le):
		err = le.Err
	}
	return Reason(err.Error())
}

// progress adds n bytes to j's count.
func (b *Browser) progress(j *job, n int64) {
	if n <= 0 {
		return
	}
	now := time.Now()
	b.mu.Lock()
	j.done += n
	if s := j.samples; len(s) == 0 || now.Sub(s[len(s)-1].at) >= sampleEvery {
		cut := 0
		for cut < len(s)-1 && now.Sub(s[cut+1].at) >= rateWindow {
			cut++
		}
		j.samples = append(s[cut:], sample{at: now, done: j.done})
	}
	b.mu.Unlock()
}

// setTotal records j's size once its source is open, and the bytes it
// starts from.
func (b *Browser) setTotal(j *job, total, done int64) {
	b.mu.Lock()
	j.total, j.done = total, done
	j.samples = append(j.samples[:0], sample{at: time.Now(), done: done})
	b.mu.Unlock()
	b.notify()
}

// setTarget records the name KeepBoth chose.
func (b *Browser) setTarget(j *job, name string) {
	b.mu.Lock()
	j.target = name
	b.mu.Unlock()
}

// batchAnswer is the answer stored for j's batch by "apply to all", or
// what j itself was answered.
func (b *Browser) batchAnswer(j *job) (Answer, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case j.overwrite:
		return Overwrite, true
	case j.keepBoth:
		return KeepBoth, true
	case j.batch.all != nil:
		return *j.batch.all, true
	}
	return 0, false
}

// Transfers is a snapshot of the queue, oldest first, with each running
// transfer's rate over the last 5 s and its ETA (FR-SFTP-014,
// FR-SFTP-015).
func (b *Browser) Transfers() []Transfer {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Transfer, len(b.jobs))
	for i, j := range b.jobs {
		t := Transfer{
			ID: j.id, Batch: j.batch.id, From: j.from, Name: Display(j.rel),
			State: j.state, Done: j.done, Total: j.total, Reason: j.reason, DstDir: j.dstDir(),
		}
		if j.conflict != nil {
			c := *j.conflict
			t.Conflict = &c
		}
		if j.state == Running {
			t.Rate = rate(j.samples, j.done, now)
			if t.Rate > 0 && j.total > j.done {
				t.ETA = time.Duration(float64(j.total-j.done) / t.Rate * float64(time.Second))
			}
		}
		out[i] = t
	}
	return out
}

// rate is the bytes per second since the oldest sample within
// rateWindow.
func rate(s []sample, done int64, now time.Time) float64 {
	for _, x := range s {
		if now.Sub(x.at) <= rateWindow {
			if el := now.Sub(x.at).Seconds(); el > 0 {
				return float64(done-x.done) / el
			}
			return 0
		}
	}
	return 0
}

// find returns the job with id. The caller holds b.mu.
func (b *Browser) find(id TransferID) *job {
	for _, j := range b.jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}

// Answer answers a waiting transfer's question (FR-SFTP-016). With all,
// the answer also covers every other conflict of the same copy, the
// waiting ones and those still to come.
func (b *Browser) Answer(id TransferID, a Answer, all bool) {
	b.mu.Lock()
	j := b.find(id)
	if j == nil || j.state != Waiting {
		b.mu.Unlock()
		return
	}
	var front []*job
	if b.answer(j, a) {
		front = append(front, j)
	}
	if all {
		j.batch.all = &a
		for _, k := range b.jobs {
			if k.batch == j.batch && k.state == Waiting && b.answer(k, a) {
				front = append(front, k)
			}
		}
	}
	b.pending = slices.Insert(b.pending, 0, front...)
	b.schedule()
	b.mu.Unlock()
	b.notify()
}

// answer applies a to the waiting job j and reports whether j must run
// again, at the front of the queue. The caller holds b.mu.
func (b *Browser) answer(j *job, a Answer) bool {
	j.conflict = nil
	switch a {
	case Overwrite:
		j.state, j.overwrite = b.queued(), true
	case KeepBoth:
		j.state, j.keepBoth = b.queued(), true
	default:
		j.state, j.reason = Skipped, ErrExists.Error()
		return false
	}
	return true
}

// Cancel stops a queued, running, waiting or paused transfer; a running
// one's part file is removed (FR-SFTP-015). Cancelling a directory
// transfer also cancels everything still to do below it.
func (b *Browser) Cancel(id TransferID) {
	b.mu.Lock()
	j := b.find(id)
	if j == nil {
		b.mu.Unlock()
		return
	}
	var parts []task
	b.cancelJob(j, &parts)
	if j.kind == Dir || j.kind == kindUnknown {
		for _, k := range b.jobs {
			if k.under(j) {
				b.cancelJob(k, &parts)
			}
		}
	}
	b.mu.Unlock()
	b.removeParts(parts)
	b.notify()
}

// cancelJob cancels j if it is pending, adding to parts the jobs whose
// part file must be removed here (a running job removes its own). The
// caller holds b.mu.
func (b *Browser) cancelJob(j *job, parts *[]task) {
	switch j.state {
	case Running:
		j.cancelReq = true
		j.cancel()
	case Queued, Waiting, Paused:
		if j.state == Paused || j.done > 0 {
			*parts = append(*parts, b.task(j))
		}
		j.state, j.conflict = Cancelled, nil
		b.pending = slices.DeleteFunc(b.pending, func(k *job) bool { return k == j })
	}
}

// Retry queues a failed or cancelled transfer again, at the end of the
// queue; it resumes where that is safe (FR-SFTP-017).
func (b *Browser) Retry(id TransferID) {
	b.mu.Lock()
	j := b.find(id)
	if j == nil || (j.state != Failed && j.state != Cancelled) {
		b.mu.Unlock()
		return
	}
	j.state, j.reason = b.queued(), ""
	b.pending = append(b.pending, j)
	b.schedule()
	b.mu.Unlock()
	b.notify()
}

// Clear removes the finished rows (C in the transfers view).
func (b *Browser) Clear() {
	b.mu.Lock()
	b.jobs = slices.DeleteFunc(b.jobs, func(j *job) bool { return j.state.Finished() })
	b.mu.Unlock()
	b.notify()
}

// Updates delivers coalesced state changes: a capacity-1 channel written
// without blocking (ARCH-SFTP-003). Byte counts change without a
// notification; the UI reads them on its tick.
func (b *Browser) Updates() <-chan struct{} { return b.updates }

// notify sends one pending notification at most.
func (b *Browser) notify() {
	select {
	case b.updates <- struct{}{}:
	default:
	}
}
