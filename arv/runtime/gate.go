package runtime

import (
	"bytes"
	"context"
)

// Outcome is what one effect did, encoded for the log: its result, the
// bytes it charged to alloc_mb, whether its result is a cancellation, and,
// for puts, each line's index in the run's output. Replayed is set when
// the log answered it: the effect did not run.
type Outcome struct {
	Res       []byte
	Charged   int64
	Cancelled bool
	Out       []uint64
	Replayed  bool
}

// SetLog makes t, a root task not yet running, log its run's effects to
// l. Every task it spawns shares l.
func (t *Task) SetLog(l *EffectLog) { t.log = l }

// Log is the run's effect log, nil if the run keeps none.
func (t *Task) Log() *EffectLog { return t.log }

// next is the record that answers t's next effect or check, and whether
// the log answers t at all: in a replay always (nil once t's records are
// spent), in a resumed run while t has records left, in a recording never.
func (t *Task) next() (*LogRecord, bool) {
	l := t.log
	if l.mode == Record {
		return nil, false
	}
	if recs := l.log.tasks[t.ID]; t.seq < uint64(len(recs)) {
		return &recs[t.seq], true
	}
	return nil, l.mode == Replay
}

// consume marks t's next record used.
func (t *Task) consume() {
	t.seq++
	t.log.mu.Lock()
	t.log.consumed[t.ID] = t.seq
	t.log.mu.Unlock()
}

// appendRecord logs r as t's next record.
func (t *Task) appendRecord(r *LogRecord) error {
	t.seq++
	r.Task, r.Seq = t.ID, t.seq
	return t.log.w.append(r)
}

// awaitCancel blocks until t is cancelled and returns why: a replayed
// cancellation happens only once its cause (a sibling's failure, a
// deadline) has happened in this run too, so the first failure is the
// same as when it was recorded (I5).
func (t *Task) awaitCancel() error {
	<-t.Ctx.Done()
	return context.Cause(t.Ctx)
}

// Effect runs do, one effect of t named kind with arguments req (I7). With
// no log it just runs do. Recording, it runs do and logs what it did.
// Replaying, it runs nothing: it returns the logged outcome of t's next
// effect if that effect has the same kind and req, and otherwise fails
// the run with a DivergedError. Resuming, it replays while t has records
// left, then records. A replayed cancellation is returned only once t is
// cancelled.
func (t *Task) Effect(kind string, req []byte, do func() Outcome) (Outcome, error) {
	l := t.log
	if l == nil {
		return do(), nil
	}
	if err := l.failed(); err != nil {
		return Outcome{}, err
	}
	rec, replaying := t.next()
	if replaying {
		if rec == nil {
			return Outcome{}, l.diverge("at task %s, effect %d: the log has no more records for it; the program asked for %s",
				t.ID, t.seq+1, call(kind, req))
		}
		if rec.Check != 0 || rec.Kind != kind || !bytes.Equal(rec.Req, req) {
			return Outcome{}, l.diverge("at task %s, effect %d: the log has %s, the program asked for %s",
				t.ID, rec.Seq, describe(rec), call(kind, req))
		}
		t.consume()
		if rec.Cancelled {
			t.awaitCancel()
		}
		return Outcome{Res: rec.Res, Charged: rec.Charged, Cancelled: rec.Cancelled, Out: rec.Out, Replayed: true}, nil
	}
	o := do()
	err := t.appendRecord(&LogRecord{Kind: kind, Req: req, Res: o.Res, Charged: o.Charged, Cancelled: o.Cancelled, Out: o.Out})
	return o, err
}
