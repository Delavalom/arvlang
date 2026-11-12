package runtime

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Mode is what a run does with its effect log.
type Mode int

const (
	Record Mode = iota + 1 // run live and append every effect to the log
	Replay                 // answer every effect from the log; nothing runs live
	Resume                 // answer each task from the log while it has records, then run it live and append
)

// KindReplay is the kind of a replay that diverged from its log, or of a
// log that could not be written.
const KindReplay = "replay"

// DivergedError is why a replayed run stopped: it asked for an effect the
// log does not have, or ended other than the recorded run did.
type DivergedError struct{ Msg string }

func (e *DivergedError) Error() string   { return "replay diverged " + e.Msg }
func (*DivergedError) ErrorKind() string { return KindReplay }

// EffectLog is one run's effect log, shared by all its tasks (I7): every
// effect and every timing-dependent check a task makes goes through it
// (Task.Effect, Task.Err), to be recorded, or answered from the log.
type EffectLog struct {
	mode  Mode
	log   *Log       // Replay, Resume
	w     *logWriter // Record, Resume
	out   *output
	start time.Time       // when the run started; a resumed run's, as far back as the crashed run had run
	abort context.Context // ends when the host cancels the run or it diverges
	stop  context.CancelCauseFunc

	failure  atomic.Pointer[error] // the first divergence or write failure
	mu       sync.Mutex
	consumed map[string]uint64 // per task: records answered from the log

	// left counts the records no task has used yet (Replay, Resume);
	// drained is closed once it reaches 0. Until then a resumed task past
	// its own records checks quietly (see Task.quiet).
	left      atomic.Int64
	drained   chan struct{}
	drainOnce sync.Once
	// doneAts holds the At of each KindDone record no task has used yet,
	// sorted; doneUsed closes doneCh, and replaces it, each time one is
	// used (see Task.Acquire).
	doneMu  sync.Mutex
	doneAts []int64
	doneCh  chan struct{}
	// lines marks the log's records used; front is the first unused one,
	// and frontCh is closed, and replaced, each time front moves (see
	// awaitCancel).
	lines   []bool
	front   int
	frontCh chan struct{}
	// under holds every task ID that the log has records of, or of a task
	// under it: an attempt (root/0#2), a descendant (root/0/1).
	under map[string]bool
	// later holds, for each task ID, the children of it that the log has
	// records of (or of a task under them), by number, each with the
	// earliest start (Began, or At for a check or an end) of a record
	// under it or under a later child (later.first).
	later map[string][]childStart

	st stall // a replay's running tasks (stall.go)

	clockOnce sync.Once // ClockOffset
	clock     int64
	clockOK   bool
}

// NewRecorder starts a log on w for a run described by h. Every line is
// one write to w; sync, unless nil, runs after each (fsync). Output goes
// to out.
func NewRecorder(w io.Writer, h Header, out io.Writer, sync func() error) (*EffectLog, error) {
	l := &EffectLog{mode: Record, w: &logWriter{w: w, sync: sync}, out: newOutput(out, nil), start: time.Now()}
	if err := l.w.append(&LogRecord{V: LogVersion, Header: &h}); err != nil {
		return nil, err
	}
	return l, nil
}

// ErrUnfinished is a replay of a log with no end line.
var ErrUnfinished = errors.New("effect log: the recorded run did not finish (it was killed or cancelled); resume it instead")

// NewReplayer answers a run from log, which must be complete: a run that
// did not finish can only be resumed.
func NewReplayer(log *Log, out io.Writer) (*EffectLog, error) {
	if log.End == nil {
		return nil, ErrUnfinished
	}
	l := &EffectLog{mode: Replay, log: log, out: newOutput(out, log.outs), consumed: map[string]uint64{}, start: time.Now()}
	l.countRecords()
	l.st.active.Store(1) // the root task
	return l, nil
}

// countRecords starts left at the log's records, and doneLeft at its
// KindDone records.
func (l *EffectLog) countRecords() {
	l.drained, l.doneCh, l.frontCh = make(chan struct{}), make(chan struct{}), make(chan struct{})
	l.lines = make([]bool, l.log.n)
	l.left.Store(int64(l.log.n))
	if l.log.n == 0 {
		close(l.drained)
	}
	l.under = map[string]bool{}
	for id := range l.log.tasks {
		l.under[id] = true
		for i := 0; i < len(id); i++ {
			if id[i] == '/' || id[i] == '#' {
				l.under[id[:i]] = true
			}
		}
	}
	l.later = laterChildren(l.log.tasks)
	for _, recs := range l.log.tasks {
		if r := &recs[len(recs)-1]; r.Kind == KindDone && r.Check == 0 {
			l.doneAts = append(l.doneAts, r.At)
		}
	}
	slices.Sort(l.doneAts)
}

// childStart is a child's number, and the earliest start of a record
// under it or under a later child of the same task.
type childStart struct {
	n     uint64
	start int64
}

// laterChildren builds EffectLog.later.
func laterChildren(tasks map[string][]LogRecord) map[string][]childStart {
	first := map[string]map[uint64]int64{}
	for id, recs := range tasks {
		start := recs[0].At
		if recs[0].Check == 0 && recs[0].Kind != KindDone {
			start = recs[0].Began
		}
		for p := id; ; {
			i := strings.LastIndexByte(p, '/')
			if i < 0 {
				break
			}
			seg := p[i+1:]
			if j := strings.IndexByte(seg, '#'); j >= 0 {
				seg = seg[:j] // an attempt counts as its task
			}
			if n, err := strconv.ParseUint(seg, 10, 64); err == nil {
				if first[p[:i]] == nil {
					first[p[:i]] = map[uint64]int64{}
				}
				if old, ok := first[p[:i]][n]; !ok || start < old {
					first[p[:i]][n] = start
				}
			}
			p = p[:i]
		}
	}
	later := make(map[string][]childStart, len(first))
	for id, kids := range first {
		cs := make([]childStart, 0, len(kids))
		for n, start := range kids {
			cs = append(cs, childStart{n, start})
		}
		slices.SortFunc(cs, func(a, b childStart) int { return cmp.Compare(a.n, b.n) })
		for i := len(cs) - 2; i >= 0; i-- {
			cs[i].start = min(cs[i].start, cs[i+1].start)
		}
		later[id] = cs
	}
	return later
}

// first reports the earliest start of a record under a child of the task
// named id numbered n or more, and whether the log has one: that child,
// spawned later, started after everything id did before spawning it.
func (l *EffectLog) first(id string, n uint64) (int64, bool) {
	cs := l.later[id]
	i, _ := slices.BinarySearchFunc(cs, n, func(c childStart, n uint64) int { return cmp.Compare(c.n, n) })
	if i == len(cs) {
		return 0, false
	}
	return cs[i].start, true
}

// usedLine marks the record at line used, and moves front past every
// used record.
func (l *EffectLog) usedLine(line int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines[line] = true
	moved := false
	for l.front < len(l.lines) && l.lines[l.front] {
		l.front++
		moved = true
	}
	if moved {
		close(l.frontCh)
		l.frontCh = make(chan struct{})
	}
}

// waitFront waits until every record before line has been used, or the
// run is aborted.
func (l *EffectLog) waitFront(line int) error {
	for {
		l.mu.Lock()
		if l.front >= line {
			l.mu.Unlock()
			return nil
		}
		ch := l.frontCh
		l.mu.Unlock()
		if l.abort == nil {
			<-ch
			continue
		}
		var aborted bool
		l.Park(func() {
			select {
			case <-ch:
			case <-l.abort.Done():
				aborted = true
			}
		})
		if aborted {
			return context.Cause(l.abort)
		}
	}
}

// doneUsed counts the KindDone record logged at at used.
func (l *EffectLog) doneUsed(at int64) {
	l.doneMu.Lock()
	defer l.doneMu.Unlock()
	if i, ok := slices.BinarySearch(l.doneAts, at); ok {
		l.doneAts = slices.Delete(l.doneAts, i, i+1)
	}
	close(l.doneCh)
	l.doneCh = make(chan struct{})
}

// waitDone waits until every task whose end the log has before before
// (ms on the recorded run's clock) has ended in this run too, or the run
// is aborted. Such a task cannot be waiting for anything the waiter does
// from before on: on the recorded run's clock it had ended by then.
func (l *EffectLog) waitDone(before int64) error {
	for {
		l.doneMu.Lock()
		if len(l.doneAts) == 0 || l.doneAts[0] >= before {
			l.doneMu.Unlock()
			return nil
		}
		ch := l.doneCh
		l.doneMu.Unlock()
		if l.abort == nil {
			<-ch
			continue
		}
		select {
		case <-ch:
		case <-l.abort.Done():
			return context.Cause(l.abort)
		}
	}
}

// used counts one record used.
func (l *EffectLog) used() {
	if l.left.Add(-1) == 0 {
		l.drainOnce.Do(func() { close(l.drained) })
	}
}

// isDrained reports that every record has been used.
func (l *EffectLog) isDrained() bool {
	select {
	case <-l.drained:
		return true
	default:
		return false
	}
}

// NewResumer answers a run from log while each task has records, then
// runs it live and appends to w, which must be at log.Size (a partial
// last line cut off). A complete log is replayed instead.
func NewResumer(log *Log, w io.Writer, out io.Writer, sync func() error) *EffectLog {
	if log.End != nil {
		l, _ := NewReplayer(log, out)
		return l
	}
	lw := &logWriter{w: w, sync: sync, prev: log.last}
	l := &EffectLog{mode: Resume, log: log, w: lw, out: newOutput(out, log.outs), consumed: map[string]uint64{},
		start: time.Now().Add(-time.Duration(log.at) * time.Millisecond)}
	l.countRecords()
	l.st.active.Store(1) // the root task
	return l
}

// Mode is what the log does.
func (l *EffectLog) Mode() Mode { return l.mode }

// Log is the log read back (Replay, Resume), else nil.
func (l *EffectLog) Log() *Log { return l.log }

// Start binds the log to a run that the host can cancel through ctx. The
// run's root task must use the context it returns, which also ends when
// the replay diverges or the log cannot be written.
func (l *EffectLog) Start(ctx context.Context) context.Context {
	l.abort, l.stop = context.WithCancelCause(ctx)
	return l.abort
}

// aborted reports that the run is being torn down from outside (the host
// cancelled it, or it diverged): what it does from now on is not part of
// the run and is not logged, as if it had been killed here. A host's
// deadline that has passed is that host cancelling the run, even before
// its context's timer has fired: a check sees it then (liveErr), and no
// deadline in the log names it, so a replay could not wait for it.
func (l *EffectLog) aborted() bool {
	if l.abort == nil {
		return false
	}
	if l.abort.Err() != nil {
		return true
	}
	d, ok := l.abort.Deadline()
	return ok && !time.Now().Before(d)
}

// failed is the first divergence or write failure, or nil.
func (l *EffectLog) failed() error {
	if p := l.failure.Load(); p != nil {
		return *p
	}
	return nil
}

// fail records err as why the run stops, unless something already
// failed, and aborts the run. It returns the first failure.
func (l *EffectLog) fail(err error) error {
	if !l.failure.CompareAndSwap(nil, &err) {
		return l.failed()
	}
	if l.stop != nil {
		l.stop(err)
	}
	return err
}

// Diverge fails the run with a DivergedError: the log does not fit the
// program (for one, a record it cannot use).
func (l *EffectLog) Diverge(format string, a ...any) error { return l.diverge(format, a...) }

// diverge fails the run with a DivergedError. Once the host has
// cancelled the run, what it does is not the recorded run's (a replayed
// cancellation stops waiting for its cause, and the task goes on early),
// so the run fails with the host's cause instead, not a divergence.
func (l *EffectLog) diverge(format string, a ...any) error {
	if l.abort != nil && l.failed() == nil && l.aborted() {
		return l.fail(&hostCancelled{context.Cause(l.abort)})
	}
	return l.fail(&DivergedError{Msg: fmt.Sprintf(format, a...)})
}

// hostCancelled is why a logged run stopped when its host cancelled it.
type hostCancelled struct{ cause error }

func (e *hostCancelled) Error() string   { return "the host cancelled the run: " + e.cause.Error() }
func (*hostCancelled) ErrorKind() string { return KindCancelled }
func (e *hostCancelled) Unwrap() error   { return e.cause }

// Finish ends the run, whose value was result (value.EncodeRequest of
// it): it prints what output is still held, then closes the log of a
// recorded or resumed run with End, unless the host cancelled it; a
// replay checks that it used every record and ended as the recorded run
// did. It returns why the run failed, if the log made it fail.
func (l *EffectLog) Finish(result []byte) error {
	defer l.stopWatching()
	defer l.stop(context.Canceled) // release the run's context
	l.out.flush()
	if err := l.failed(); err != nil {
		return err
	}
	if l.mode != Record {
		if err := l.unconsumed(); err != nil {
			return err
		}
	}
	if l.mode == Replay {
		if string(l.log.End.Result) != string(result) {
			return l.diverge("at the end: the recorded run ended with %s, this run with %s", clipBytes(l.log.End.Result), clipBytes(result))
		}
		return nil
	}
	if l.aborted() {
		return nil
	}
	if err := l.w.append(&LogRecord{End: &End{Result: result}}); err != nil {
		return l.fail(err)
	}
	return nil
}

// unconsumed fails if some task never asked for records it has.
func (l *EffectLog) unconsumed() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	ids := make([]string, 0, len(l.log.tasks))
	for id := range l.log.tasks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		recs := l.log.tasks[id]
		if c := l.consumed[id]; c < uint64(len(recs)) {
			l.mu.Unlock()
			err := l.diverge("at task %s: the log has %d more records for it, from %s, which this run never reached",
				id, uint64(len(recs))-c, describe(&recs[c]))
			l.mu.Lock()
			return err
		}
	}
	return nil
}

// clipLen is how much of a request or result a divergence error quotes.
const clipLen = 200

func clipBytes(b []byte) string {
	if len(b) <= clipLen {
		return string(b)
	}
	return string(b[:clipLen]) + "..."
}

// call is an effect as a divergence error names it: http_get("url").
func call(kind string, req []byte) string {
	inner := req
	if len(inner) >= 2 && inner[0] == '[' && inner[len(inner)-1] == ']' {
		inner = inner[1 : len(inner)-1]
	}
	return kind + "(" + clipBytes(inner) + ")"
}

// describe is a record as a divergence error names it.
func describe(r *LogRecord) string {
	if r.Kind == KindDone && r.Check == 0 {
		return "the task's end"
	}
	if r.Check != 0 {
		return fmt.Sprintf("%s at check %d", r.Kind, r.Check)
	}
	return call(r.Kind, r.Req)
}
