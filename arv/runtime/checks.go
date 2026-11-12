package runtime

import (
	"context"
	"errors"
	"fmt"
)

// observe is a check: a point where t sees whether something whose timing
// the run does not control has happened (its cancellation, the live-task
// cap, a deadline). Checks are numbered per task, in the order t makes
// them. With no log, or live, probe looks and says what it saw, "" for
// nothing; a recording logs what it saw, and nothing when it saw nothing.
// Replayed, nothing is looked at: replay gets the outcome the log has for
// this check ("" if none) and acts it out. probe's argument says whether
// the check is quiet (Task.quiet): it then looks at checkCtx or waitCtx.
func (t *Task) observe(probe func(quiet bool) (string, error), replay func(outcome string) error) error {
	l := t.log
	if l == nil {
		_, err := probe(false)
		return err
	}
	t.checks++
	if err := l.failed(); err != nil {
		return err
	}
	rec, replaying := t.next()
	if replaying {
		if rec == nil || rec.Check == 0 || rec.Check > t.checks {
			return replay("")
		}
		if rec.Check < t.checks {
			return l.diverge("at task %s, check %d: the log has %s, which this run passed without seeing", t.ID, t.checks, describe(rec))
		}
		t.consume()
		t.by = rec.By
		return replay(rec.Kind)
	}
	outcome, err := probe(t.quiet())
	var by string
	if outcome == CheckCancelled {
		t.saw(err)
		by = t.deadlineOf(err)
	}
	if outcome != "" {
		if aerr := t.appendRecord(&LogRecord{Kind: outcome, Check: t.checks, By: by}); aerr != nil {
			return aerr
		}
	}
	return err
}

// unexpected fails a replay whose log has outcome at a check that cannot
// have it.
func (t *Task) unexpected(outcome string) error {
	return t.log.diverge("at task %s, check %d: the log has %s, which this check cannot see", t.ID, t.checks, outcome)
}

// cancelled is the replay of a cancellation check.
func (t *Task) cancelled(outcome string) error {
	switch outcome {
	case "":
		return nil
	case CheckCancelled:
		return t.awaitCancel(t.by)
	}
	return t.unexpected(outcome)
}

// Err reports why the task must stop: its context's cause once cancelled.
// Once a scope's deadline has passed but the timer that cancels it has
// not fired yet (it can be late on a loaded machine), it cancels the
// scope itself and reports the timeout; context.DeadlineExceeded for
// another deadline on its context. nil while the task may go on. Unlike Tick, it checks
// every time, so the evaluator calls it before each builtin: no effect
// (a write, an output line) runs in a task whose scope has already ended.
//
// It is a check (see observe): with a log, the recorded run's answer is
// replayed, so a replayed task stops exactly where it stopped when
// recorded. Code inside an effect, which runs only live, uses LiveErr.
func (t *Task) Err() error {
	if t.log == nil {
		return t.LiveErr()
	}
	return t.observe(func(quiet bool) (string, error) {
		if err := liveErr(t.checkCtx(quiet)); err != nil {
			return CheckCancelled, err
		}
		return "", nil
	}, t.cancelled)
}

// WaitFor blocks until done is closed or t is cancelled, and returns nil
// or why t was cancelled. It is a check: replayed, it ends as the recorded
// wait did, whatever this run's timing.
func (t *Task) WaitFor(done <-chan struct{}) error {
	live := func(quiet bool) (string, error) {
		ctx, release := t.waitCtx(quiet)
		defer release()
		select {
		case <-done:
			return "", nil
		case <-ctx.Done():
			return CheckCancelled, context.Cause(ctx)
		}
	}
	if t.log == nil {
		_, err := live(false)
		return err
	}
	return t.observe(live, func(outcome string) error {
		if outcome != "" {
			return t.cancelled(outcome)
		}
		select {
		case <-done:
			return nil
		case <-t.log.abort.Done():
			return context.Cause(t.log.abort)
		}
	})
}

// Acquire is b.Acquire for a spawn by t. It is a check: replayed, the
// spawn fails at the live-task cap, or is cancelled, exactly when the
// recorded one was, and otherwise takes its slot without the cap.
//
// Resumed and live, the check looks at the cap as the recorded run would
// have: if the log has records of the task this spawn starts, the
// recorded spawn got its slot, so this one takes it without the cap.
// Otherwise it first waits for every task the crashed run had finished
// (Task.Finished) to finish here too: their slots were free by then.
func (t *Task) Acquire(b *Budget) error {
	if t.log == nil {
		return b.Acquire(t.Ctx)
	}
	return t.observe(func(quiet bool) (string, error) {
		if t.log.mode == Resume {
			if t.hasRecords(fmt.Sprintf("%s/%d", t.ID, t.children.Load())) {
				return "", b.acquire(t.log.abort, false)
			}
			if err := t.log.waitDone(); err != nil {
				return CheckCancelled, err
			}
		}
		ctx, release := t.waitCtx(quiet)
		defer release()
		err := b.Acquire(ctx)
		var ex *ExceededError
		switch {
		case err == nil:
			return "", nil
		case errors.As(err, &ex):
			return CheckExceeded, err
		}
		return CheckCancelled, err
	}, func(outcome string) error {
		switch outcome {
		case "":
			return b.acquire(t.log.abort, false)
		case CheckExceeded:
			return &ExceededError{Kind: "tasks", Limit: b.live.limit}
		}
		return t.cancelled(outcome)
	})
}

// Expired is s.Expired for t, the task that opened s, as s closes. For a
// scope with a deadline it is a check: replayed, the scope has timed out
// exactly when the recorded one had.
func (t *Task) Expired(s *Scope) error {
	if t.log == nil || s.timeout == nil {
		return s.Expired()
	}
	return t.observe(func(bool) (string, error) {
		if err := s.Expired(); err != nil {
			return CheckExpired, err
		}
		return "", nil
	}, func(outcome string) error {
		switch outcome {
		case "":
			return nil
		case CheckExpired:
			return s.timeout
		}
		return t.unexpected(outcome)
	})
}

// errStopped is a restart backoff cut short.
var errStopped = errors.New("backoff cut short")

// Backoff waits (wait reports whether its whole delay passed with the
// scope still running) before t's task runs again under restart, as the
// task named next. It is a check: replayed, it does not wait, and is cut
// short exactly when the recorded one was. Quiet (Task.quiet), it does not
// wait either when the log has records for next: the recorded wait ended.
func (t *Task) Backoff(next string, wait func() bool) bool {
	if t.log == nil {
		return wait()
	}
	err := t.observe(func(quiet bool) (string, error) {
		if quiet && t.hasRecords(next) {
			return "", nil
		}
		if wait() {
			return "", nil
		}
		return CheckCancelled, errStopped
	}, func(outcome string) error {
		if outcome == "" {
			return nil
		}
		if err := t.cancelled(outcome); err != nil {
			return err
		}
		return errStopped
	})
	return err == nil
}
