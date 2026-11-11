package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"
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
	abort context.Context // ends when the host cancels the run or it diverges
	stop  context.CancelCauseFunc

	failure  atomic.Pointer[error] // the first divergence or write failure
	mu       sync.Mutex
	consumed map[string]uint64 // per task: records answered from the log
}

// NewRecorder starts a log on w for a run described by h. Every line is
// one write to w; sync, unless nil, runs after each (fsync). Output goes
// to out.
func NewRecorder(w io.Writer, h Header, out io.Writer, sync func() error) (*EffectLog, error) {
	l := &EffectLog{mode: Record, w: &logWriter{w: w, sync: sync}, out: newOutput(out, nil)}
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
	return &EffectLog{mode: Replay, log: log, out: newOutput(out, log.outs), consumed: map[string]uint64{}}, nil
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
	return &EffectLog{mode: Resume, log: log, w: lw, out: newOutput(out, log.outs), consumed: map[string]uint64{}}
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
// the run and is not logged, as if it had been killed here.
func (l *EffectLog) aborted() bool { return l.abort != nil && l.abort.Err() != nil }

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

// diverge fails the run with a DivergedError.
func (l *EffectLog) diverge(format string, a ...any) error {
	return l.fail(&DivergedError{Msg: fmt.Sprintf(format, a...)})
}

// Finish ends the run, whose value was result (value.EncodeRequest of
// it): it prints what output is still held, then closes the log of a
// recorded or resumed run with End, unless the host cancelled it; a
// replay checks that it used every record and ended as the recorded run
// did. It returns why the run failed, if the log made it fail.
func (l *EffectLog) Finish(result []byte) error {
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
			return l.diverge("at the end: the recorded run ended with %s, this run with %s", clip(l.log.End.Result), clip(result))
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

func clip(b []byte) string {
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
	return kind + "(" + clip(inner) + ")"
}

// describe is a record as a divergence error names it.
func describe(r *LogRecord) string {
	if r.Check != 0 {
		return fmt.Sprintf("%s at check %d", r.Kind, r.Check)
	}
	return call(r.Kind, r.Req)
}
