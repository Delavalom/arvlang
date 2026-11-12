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
// seen, or by a deadline. A deadline is never hidden: in a resume it
// fires on the recorded run's clock or where the log saw it (see
// deadline), never early, so a quiet check sees it as the recorded run
// would have.
func (t *Task) seenCancel() bool {
	return t.Ctx.Err() != nil && visible(t.seen, context.Cause(t.Ctx))
}

// visible reports that a quiet check sees cause: a deadline, or one of
// seen.
func visible(seen []error, cause error) bool {
	return isDeadline(cause) || slices.Contains(seen, cause)
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
	own, seen, drained := t.Ctx, slices.Clone(t.seen), t.log.drained
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-drained:
		case <-own.Done():
			if !visible(seen, context.Cause(own)) {
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

// hasRecords reports that the log holds records for the task named id.
func (t *Task) hasRecords(id string) bool {
	return t.log.log != nil && len(t.log.log.tasks[id]) > 0
}
