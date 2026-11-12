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
	if l.mode == Resume && !t.live {
		t.goLive()
	}
	return nil, l.mode == Replay
}

// consume marks t's next record used. In a resume, t's clock moves to it.
//
// A record that saw a deadline stop t (a cancellation whose By names a
// timed scope) is counted toward the log draining only once that deadline
// has fired (awaitCancel, release). In a resume a live task's failure
// waits for the log to drain (Task.Fail), and the record says the deadline
// came first: counted earlier, the last record could drain the log while
// its deadline waited to fire, and a live failure would become the
// scope's first failure ahead of it (review round 4).
func (t *Task) consume() {
	rec := &t.log.log.tasks[t.ID][t.seq]
	t.lastAt = max(t.lastAt, rec.At)
	t.vbase = max(t.vbase, t.lastAt)
	t.log.usedLine(rec.line)
	t.seq++
	t.log.mu.Lock()
	t.log.consumed[t.ID] = t.seq
	t.log.mu.Unlock()
	if rec.By != "" && (rec.Kind == CheckCancelled || rec.Check == 0 && rec.Cancelled) {
		t.held = true
		return
	}
	t.log.used()
}

// release counts the record consume held, if any, toward the log
// draining.
func (t *Task) release() {
	if t.held {
		t.held = false
		t.log.used()
	}
}

// appendRecord logs r as t's next record, unless the run is being torn
// down: from then on nothing is logged, as if the run had been killed.
func (t *Task) appendRecord(r *LogRecord) error {
	if t.log.aborted() {
		return nil
	}
	t.seq++
	r.Task, r.Seq, r.At = t.ID, t.seq, t.now()
	if err := t.log.w.append(r); err != nil {
		return t.log.fail(err)
	}
	return nil
}

// awaitCancel blocks until t is cancelled and returns why: a replayed
// cancellation happens only once its cause (a sibling's failure) has
// happened in this run too, so the first failure is the same as when it
// was recorded (I5). A deadline is time, which a replay does not wait
// for: by, unless "", names the timed scope whose deadline the recorded
// run saw here, and it fires once every record logged before the first
// that saw it has been used (see deadline). Those happened before it,
// and none of them depends on it: a task that saw it logged that.
func (t *Task) awaitCancel(by string) error {
	if by != "" {
		err := t.fireLogged(by)
		t.release()
		if err != nil {
			return err
		}
	}
	t.release()
	t.log.Park(func() { <-t.Ctx.Done() })
	err := context.Cause(t.Ctx)
	t.saw(err)
	return err
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
				t.ID, t.effects+1, call(kind, req))
		}
		if rec.Check != 0 || rec.Kind != kind || !bytes.Equal(rec.Req, req) {
			return Outcome{}, l.diverge("at task %s, effect %d: the log has %s, the program asked for %s",
				t.ID, t.effects+1, describe(rec), call(kind, req))
		}
		t.consume()
		t.effects++
		if rec.Cancelled {
			if err := t.awaitCancel(rec.By); err != nil && l.failed() != nil {
				return Outcome{}, err
			}
		}
		t.release()
		return Outcome{Res: rec.Res, Charged: rec.Charged, Cancelled: rec.Cancelled, Out: rec.Out, Replayed: true}, nil
	}
	began := t.now()
	t.effects++
	o := do()
	var by string
	if o.Cancelled && t.Ctx.Err() != nil {
		cause := context.Cause(t.Ctx)
		t.saw(cause)
		by = t.deadlineOf(cause)
	}
	err := t.appendRecord(&LogRecord{Kind: kind, Req: req, Res: o.Res, Charged: o.Charged, Cancelled: o.Cancelled, Out: o.Out, Began: began, By: by})
	return o, err
}

// Finished ends t, a spawned task, and then runs release (unless nil),
// which frees the task's slot (tasks:) and its place under the live-task
// cap; under restart, an attempt ends once the next one starts
// (Task.Attempt), with a nil release: the slot stays the task's. With a
// log, the end is a record (KindDone): a resumed run then knows which
// tasks the crashed run had finished, so it replays every one of them to
// its end, pure work after its last effect included, instead of running
// that work live against the replay's state; and a live check of the
// live-task cap waits for them (Task.Acquire). A replay checks that t
// ends where the recorded task did.
//
// The end is logged before release runs, and a replayed end is counted
// used (doneUsed) only after it has run: no task can take the slot, and
// log a record, before the end of the task that held it is in the log
// (review round 6). Otherwise a crash between the two left a log with
// records of the next task in the slot but no end for the holder, and a
// resume ran the holder live while the spawn of that next task waited
// for its slot: if the holder failed, it waited for the log to drain
// (Task.Fail), which needed that next task, and the resume hung.
func (t *Task) Finished(release func()) {
	defer t.retire()
	free := func() {
		if release != nil {
			r := release
			release = nil
			r()
		}
	}
	defer free() // on every path, a divergence or a panic included
	l := t.log
	if l == nil || l.failed() != nil {
		return
	}
	rec, replaying := t.next()
	switch {
	case !replaying:
		t.appendRecord(&LogRecord{Kind: KindDone})
	case rec == nil:
		l.diverge("at task %s: it ended, and the log has no end for it", t.ID)
	case rec.Kind != KindDone || rec.Check != 0:
		l.diverge("at task %s: it ended, and the log has %s next", t.ID, describe(rec))
	default:
		t.consume()
		free()
		l.doneUsed(rec.At)
	}
}

// fireLogged fires the deadline of the timed scope named by, which t is
// inside, where the log has it fire (awaitCancel).
func (t *Task) fireLogged(by string) error {
	s := t.timedScope(by)
	if s == nil || s.dl == nil {
		return t.log.diverge("at task %s: the log has it stopped by the deadline of scope %s, which it is not inside", t.ID, by)
	}
	if err := t.log.waitFront(t.log.log.sighted[by]); err != nil {
		return err
	}
	s.dl.fire(s.timeout)
	return nil
}
