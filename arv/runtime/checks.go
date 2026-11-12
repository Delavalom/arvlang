package runtime

import "context"

// observe is a check: a point where t sees whether something whose timing
// the run does not control has happened (its cancellation, the live-task
// cap, a deadline). Checks are numbered per task, in the order t makes
// them. With no log, or live, probe looks and says what it saw, "" for
// nothing; a recording logs what it saw, and nothing when it saw nothing.
// Replayed, nothing is looked at: replay gets the outcome the log has for
// this check ("" if none) and acts it out.
func (t *Task) observe(probe func() (string, error), replay func(outcome string) error) error {
	l := t.log
	if l == nil {
		_, err := probe()
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
		return replay(rec.Kind)
	}
	outcome, err := probe()
	if outcome != "" {
		if aerr := t.appendRecord(&LogRecord{Kind: outcome, Check: t.checks}); aerr != nil {
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
		return t.awaitCancel()
	}
	return t.unexpected(outcome)
}

// Err reports why the task must stop: its context's cause once cancelled,
// or context.DeadlineExceeded once a deadline on its context has passed
// but the timer that cancels it has not fired yet (it can be late on a
// loaded machine). nil while the task may go on. Unlike Tick, it checks
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
	return t.observe(func() (string, error) {
		if err := t.LiveErr(); err != nil {
			return CheckCancelled, err
		}
		return "", nil
	}, t.cancelled)
}

// WaitFor blocks until done is closed or t is cancelled, and returns nil
// or why t was cancelled. It is a check: replayed, it ends as the recorded
// wait did, whatever this run's timing.
func (t *Task) WaitFor(done <-chan struct{}) error {
	live := func() (string, error) {
		select {
		case <-done:
			return "", nil
		case <-t.Ctx.Done():
			return CheckCancelled, context.Cause(t.Ctx)
		}
	}
	if t.log == nil {
		_, err := live()
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
