package runtime

import "sync/atomic"

// supervised counts the tasks a scope has started (SuperviseWith): a
// scope that never started one has no failure but its body's to race.
type supervised struct{ n atomic.Int64 }

// Fail records err, t's failure, as s's failure unless one is already
// recorded (Scope.Fail). t is a task of s, or s's opener, whose body
// failed. Which failure of a scope comes first is timing, so with a log
// it is part of the log (I7):
//
//   - Recording, the task whose failure is first logs CheckFailed, and
//     does so before the failure cancels the scope, so whatever the
//     cancellation causes (a sibling's logged cancellation) is logged
//     after it. By names s if its deadline had passed first: the failure
//     recorded is then the timeout.
//   - Replaying, the failure the log marks first is recorded, and only
//     then is its record used, so a cancellation logged after it waits
//     for it (awaitCancel). Any other failure waits until s has one, as
//     it did when recorded.
//   - Resuming, a live failure waits likewise while the log still has
//     records: every logged failure came before the crash, so before it.
//
// A scope that never started a task has nothing to race, and logs nothing.
func (t *Task) Fail(s *Scope, err error) {
	l := t.log
	if l == nil || s.tasks.n.Load() == 0 {
		s.Fail(err)
		return
	}
	t.checks++
	if l.failed() != nil {
		s.Fail(err)
		return
	}
	rec, replaying := t.next()
	if replaying {
		switch {
		case rec != nil && rec.Check == t.checks && rec.Kind == CheckFailed:
			if rec.By != "" {
				if ferr := t.fireLogged(rec.By); ferr != nil {
					s.Fail(err)
					return
				}
			}
			s.Fail(err)
			t.consume()
			return
		case rec != nil && rec.Check != 0 && rec.Check <= t.checks:
			l.diverge("at task %s, check %d: the log has %s where this run's task failed", t.ID, t.checks, describe(rec))
			s.Fail(err)
			return
		}
		t.awaitFailure(s, nil)
		s.Fail(err)
		return
	}
	if l.mode == Resume && !l.isDrained() {
		t.idle(func() error { t.awaitFailure(s, l.drained); return nil })
	}
	s.fail(err, func(expired bool) {
		var by string
		if expired {
			by = s.id
		}
		t.appendRecord(&LogRecord{Kind: CheckFailed, Check: t.checks, By: by})
	})
}

// awaitFailure waits until s has a failure, or drained (unless nil) is
// closed, or the run is aborted.
func (t *Task) awaitFailure(s *Scope, drained <-chan struct{}) {
	var aborted <-chan struct{}
	if t.log.abort != nil {
		aborted = t.log.abort.Done()
	}
	select {
	case <-s.failed:
	case <-drained:
	case <-aborted:
	}
}
