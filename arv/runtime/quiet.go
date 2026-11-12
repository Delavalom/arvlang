package runtime

import (
	"context"
	"slices"
)

// maxSeen is how many cancellation causes a task remembers (see saw): a
// constant, however many scopes the program fails.
const maxSeen = 8

// quiet reports that t, in a resumed run, is past its own records while
// other tasks still have records to use. Its checks then see neither a
// cancellation it has not seen before (saw), unless a deadline caused
// it (seenCancel), nor the live-task cap's waits: this run's replay races ahead of the recorded run (a sibling's
// logged failure is replayed at once), and a live check that saw what
// the replay did early could stop t short of what the log says happened:
// a spawn whose child has records, a result an ancestor's logged puts
// prints. Every logged record happened before the crash, and a check
// that saw something before the crash was logged, so a check that sees
// nothing new until the log is used up sees what the recorded run saw.
func (t *Task) quiet() bool {
	return t.log.mode == Resume && !t.log.isDrained()
}

// saw notes that t has seen cancellation cause err, from a logged or
// replayed check or effect. Cancellation does not undo itself: a later
// check of t whose context was cancelled by a cause t has already seen
// would see it in any run, so even a quiet check sees it.
func (t *Task) saw(err error) {
	if err == nil || t.log == nil || slices.Contains(t.seen, err) {
		return
	}
	if len(t.seen) == maxSeen {
		t.seen = slices.Delete(t.seen, 0, 1)
	}
	t.seen = append(t.seen, err)
}

// seenCancel reports that t's context was cancelled by a cause t has
// seen, or by a deadline t has reached. A deadline fires on the recorded
// run's clock or where the log saw it (see deadline), never early for a
// task at the log's frontier, so a quiet check sees it as the recorded
// run would have. A task still behind the log (Task.behind) does not
// see it yet.
func (t *Task) seenCancel() bool {
	return t.Ctx.Err() != nil && visible(t.seen, context.Cause(t.Ctx), !t.behind())
}

// visible reports that a quiet check sees cause: one of seen, or a
// deadline if deadlines are seen.
func visible(seen []error, cause error, deadlines bool) bool {
	return isDeadline(cause) && deadlines || slices.Contains(seen, cause)
}

// behind reports that t, resumed and live, is still behind the log: a
// task it has still to spawn, the next one or a later one, has records
// (it, or a task under it). The recorded t reached that spawn, and its
// checks saw no cancellation before it, or the spawn would not have run;
// so t's checks must not see this run's deadlines, which the replay can
// fire early, before it.
func (t *Task) behind() bool {
	if t.log.mode != Resume {
		return false
	}
	_, ok := t.log.first(t.ID, t.children.Load())
	return ok
}

// checkCtx is the context a live check looks at: t's own, or, quiet, the
// run's abort, unless t has already seen what cancelled it.
func (t *Task) checkCtx(quiet bool) context.Context {
	if quiet && t.log.abort != nil && !t.seenCancel() {
		return t.log.abort
	}
	return t.Ctx
}

// waitCtx is checkCtx for a check that blocks. Quiet, it ends when the
// run is aborted, or when t is cancelled by a cause it has seen, or once
// the log is used up, so a wait that began quiet does not outlast the
// quiet. release frees it; the check calls it before it returns. The
// goroutine that watches works on copies of t's context and seen causes:
// t changes both once the check is over.
func (t *Task) waitCtx(quiet bool) (ctx context.Context, release func()) {
	if !quiet || t.log.abort == nil || t.seenCancel() {
		return t.Ctx, func() {}
	}
	ctx, cancel := context.WithCancelCause(t.log.abort)
	own, seen, drained, deadlines := t.Ctx, slices.Clone(t.seen), t.log.drained, !t.behind()
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-drained:
		case <-own.Done():
			if !visible(seen, context.Cause(own), deadlines) {
				select {
				case <-ctx.Done():
					return
				case <-drained:
				}
			}
		}
		select {
		case <-ctx.Done():
		case <-own.Done():
			cancel(context.Cause(own))
		}
	}()
	return ctx, func() { cancel(context.Canceled) }
}

// hasRecords reports that the log holds records for the task named id,
// or for a task under it (its restart attempts, its descendants): the
// task was started in the recorded run.
func (t *Task) hasRecords(id string) bool {
	return t.log.under[id]
}
