package runtime

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// A deadline firing is time, so in a logged run it is part of the log
// (I7), like every other outcome that depends on timing:
//
//   - Recording, a timed scope's deadline is a wall-clock timer, as in an
//     unlogged run. Each check or effect that sees the cancellation it
//     causes logs By, the scope's ID (Task.OpenTimed), and the scope's
//     opener logs "expired" if the scope closed timed out (Task.Expired).
//   - Replaying, no timer runs. A deadline fires when a replayed record
//     says it was seen (awaitCancel), and a scope closes timed out when
//     its opener's record says so. A replay as slow as you like reaches
//     the same deadlines, and a fast one is not held up by them.
//   - Resuming, a deadline the log has seen fires the replay's way. One it
//     has not is on the recorded run's clock: it passes vdl ms after that
//     run started. Each live task has its own reading of that clock
//     (Task.now): it goes on from the task's last record, so an effect
//     that was in flight at the crash, which the task runs again from the
//     start, is not counted twice. The deadline fires as soon as one live
//     task inside the scope reaches it (deadline.arm); a task that ends
//     stops counting (Task.retire).

// deadline is a timed scope's deadline in a replay or resume.
type deadline struct {
	cause error
	fire  context.CancelCauseFunc // cancels the scope with cause
	vdl   int64                   // resume: ms after the recorded run started
	done  atomic.Bool             // the scope has closed

	mu    sync.Mutex
	arms  map[*Task]time.Time // resume: when each live task inside reaches vdl
	at    time.Time           // the earliest of arms, zero if none
	timer *time.Timer         // fires at at
}

// OpenTimed opens a scope under t's context that times out with cause d
// after it opens (NewTimedScope). In a logged run the scope is named by
// t's ID and how many timed scopes t has opened, "root/0:2", so a log can
// say whose deadline a task saw. atStart says the scope opened when the
// run started (the root scope): in a resume, d counts from then.
func (t *Task) OpenTimed(d time.Duration, cause error, atStart bool) *Scope {
	if t.log == nil {
		return NewTimedScope(t.Ctx, d, cause)
	}
	t.timed++
	id := fmt.Sprintf("%s:%d", t.ID, t.timed)
	if t.log.mode == Record {
		s := NewTimedScope(t.Ctx, d, cause)
		s.id = id
		return s
	}
	var opened int64
	if !atStart && t.log.mode == Resume {
		opened = t.openedAt()
	}
	tctx, fire := context.WithCancelCause(t.Ctx)
	s := NewScope(tctx)
	s.id, s.timeout = id, cause
	s.dl = &deadline{cause: cause, fire: fire, vdl: opened + ceilMS(d)}
	s.stop = func() {
		s.dl.close()
		fire(context.Canceled)
	}
	if t.log.mode == Resume && t.live {
		s.dl.arm(t)
	}
	return s
}

// ceilMS is d in whole milliseconds, rounded up.
func ceilMS(d time.Duration) int64 {
	return int64((d + time.Millisecond - 1) / time.Millisecond)
}

// openedAt is when, on the recorded run's clock, the scope t opens now
// opened. It opened before t's next record began (an effect) or was
// logged (a check), and before any task t spawns after it started: the
// earliest of those is the bound, and it is late, so the deadline is
// never sooner than the recorded one, at worst later by the work between
// the scope opening and that record. With no such record, it opened at
// t's time now; t is live then.
func (t *Task) openedAt() int64 {
	bound, ok := t.log.first(t.ID, t.children.Load())
	if !t.live {
		if recs := t.log.log.tasks[t.ID]; t.seq < uint64(len(recs)) {
			r := &recs[t.seq]
			next := r.At
			if r.Check == 0 && r.Kind != KindDone {
				next = r.Began
			}
			if ok {
				next = min(next, bound)
			}
			return max(t.vbase, next)
		}
		t.goLive()
	}
	if ok {
		// t's clock can lag: it skipped any wait the log does not
		// time, such as a spawn's wait for a slot (Acquire).
		return max(t.vbase, bound)
	}
	return t.now()
}

// now is t's time on the run's clock, in ms since the run started: what
// its records log as At and Began. Recording, it is the run's own time.
// Resuming, a replaying task is at its last record's At; a live one goes
// on from there (Task.goLive).
func (t *Task) now() int64 {
	if t.log.mode != Resume {
		return t.log.elapsed()
	}
	if t.live {
		return t.vbase + time.Since(t.rbase).Milliseconds()
	}
	return t.vbase
}

// goLive marks t, in a resume, as past its records: from now on its clock
// runs, and every timed scope it is inside counts it.
func (t *Task) goLive() {
	t.live, t.rbase = true, time.Now()
	for s := scopeOf(t.Ctx); s != nil; s = s.outer {
		if s.dl != nil {
			s.dl.arm(t)
		}
	}
}

// idle runs wait, a wait that takes no time on the recorded run's clock,
// with t's clock stopped: it counts toward no deadline while it waits.
func (t *Task) idle(wait func() error) error {
	if !t.live {
		return wait()
	}
	armed := slices.Clone(t.armed)
	t.vbase = t.now()
	t.retire()
	err := wait()
	t.rbase = time.Now()
	for _, d := range armed {
		d.arm(t)
	}
	return err
}

// retire stops t counting toward its scopes' deadlines: it has ended, or
// a restart's next attempt has taken over.
func (t *Task) retire() {
	for _, d := range t.armed {
		d.unarm(t)
	}
	t.armed = nil
}

// arm counts t, live, toward d: d fires when t's clock reaches it, unless
// another task's does first.
func (d *deadline) arm(t *Task) {
	at := t.rbase.Add(time.Duration(d.vdl-t.vbase) * time.Millisecond)
	t.armed = slices.DeleteFunc(t.armed, func(x *deadline) bool { return x.done.Load() || x == d })
	t.armed = append(t.armed, d)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done.Load() {
		return
	}
	if d.arms == nil {
		d.arms = map[*Task]time.Time{}
	}
	d.arms[t] = at
	d.reset()
}

func (d *deadline) unarm(t *Task) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.arms[t]; ok {
		delete(d.arms, t)
		d.reset()
	}
}

// reset sets the timer to the earliest arm. d.mu is held.
func (d *deadline) reset() {
	var first time.Time
	for _, at := range d.arms {
		if first.IsZero() || at.Before(first) {
			first = at
		}
	}
	if first.Equal(d.at) || d.done.Load() {
		return
	}
	d.at = first
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if !first.IsZero() {
		d.timer = time.AfterFunc(time.Until(first), func() { d.fire(d.cause) })
	}
}

// when is the earliest arm, zero if none.
func (d *deadline) when() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.at
}

// close stops the timer: the scope has closed.
func (d *deadline) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.done.Store(true)
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.arms, d.at = nil, time.Time{}
}

// due reports that s's own deadline has passed, whether or not its timer
// has fired. A replay's deadline is never due: only the log fires it.
func (s *Scope) due() bool {
	if s.timeout == nil {
		return false
	}
	at := s.deadline
	if s.dl != nil {
		at = s.dl.when()
	}
	return !at.IsZero() && !time.Now().Before(at)
}

// timedScope is the timed scope named id that t is inside, nil if none.
func (t *Task) timedScope(id string) *Scope {
	for s := scopeOf(t.Ctx); s != nil; s = s.outer {
		if s.id == id && s.timeout != nil {
			return s
		}
	}
	return nil
}

// deadlineOf is the ID of the timed scope t is inside whose deadline is
// cause, "" if cause is no deadline's.
func (t *Task) deadlineOf(cause error) string {
	if cause == nil {
		return ""
	}
	for s := scopeOf(t.Ctx); s != nil; s = s.outer {
		if s.timeout == cause {
			return s.id
		}
	}
	return ""
}

// isDeadline reports that err is a deadline's cause (a scope's timeout,
// the run's among them), not a failure's.
func isDeadline(err error) bool {
	ex, ok := err.(*ExceededError)
	return ok && ex.Kind == "timeout"
}

// expireDue cancels, with its timeout, the scope around ctx whose deadline
// passed first, if one has passed but its timer has not fired yet (it can
// be late on a loaded machine). It reports whether it did.
func expireDue(ctx context.Context) bool {
	var first *Scope
	var firstAt time.Time
	for s := scopeOf(ctx); s != nil; s = s.outer {
		if s.ctx.Err() != nil || !s.due() {
			continue
		}
		at := s.deadline
		if s.dl != nil {
			at = s.dl.when()
		}
		if first == nil || at.Before(firstAt) {
			first, firstAt = s, at
		}
	}
	if first == nil {
		return false
	}
	first.expireIfDue()
	return true
}
