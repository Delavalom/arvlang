package runtime

import (
	"context"
	"sync"
	"time"
)

// Scope owns a group of tasks. Wait does not return until every task has
// finished; by default the first failure cancels the scope's context so
// the other tasks (and the code that opened the scope) stop early. Its
// Policy can collect failures or restart failed tasks instead (Supervise).
type Scope struct {
	policy   Policy
	ctx      context.Context
	cancel   context.CancelCauseFunc
	stop     context.CancelFunc // releases the deadline's timer; nil without one
	timeout  error              // the cause the deadline cancels with
	deadline time.Time          // when the scope's own deadline passes; zero if unknown (see due)
	wg       sync.WaitGroup
	once     sync.Once
	err      error
	failed   chan struct{} // closed once a failure is recorded (Fail)
	tasks    supervised    // tasks started (SuperviseWith)

	outer *Scope    // the scope whose context s's is derived from, nil if none
	id    string    // a timed scope's name in an effect log (see Task.OpenTimed)
	dl    *deadline // a logged replay's or resume's deadline, nil otherwise
}

// scopeKey finds, in a context, the innermost scope it belongs to.
type scopeKey struct{}

// scopeOf is the innermost scope whose context ctx is, or is derived
// from; nil if none.
func scopeOf(ctx context.Context) *Scope {
	s, _ := ctx.Value(scopeKey{}).(*Scope)
	return s
}

// NewScope opens a scope whose context is derived from parent.
func NewScope(parent context.Context) *Scope {
	ctx, cancel := context.WithCancelCause(parent)
	s := &Scope{cancel: cancel, outer: scopeOf(parent), failed: make(chan struct{})}
	s.ctx = context.WithValue(ctx, scopeKey{}, s)
	return s
}

// NewTimedScope is NewScope with a deadline: d after it opens, the scope
// is cancelled with cause. Deadlines nest: inside a scope that ends
// sooner, this one ends then too, so a nested timeout can only shrink (I6).
//
// A scope that closes after its deadline has timed out even if nothing
// noticed in time (the work ignored the context, or the timer fired
// late): Wait checks the deadline itself.
func NewTimedScope(parent context.Context, d time.Duration, cause error) *Scope {
	deadline := time.Now().Add(d)
	tctx, stop := context.WithTimeoutCause(parent, d, cause)
	s := NewScope(tctx)
	s.stop, s.timeout, s.deadline = stop, cause, deadline
	return s
}

// Expired returns the deadline's cause if the scope's own deadline is what
// cancelled it, and nil if it was not cancelled, or was cancelled first by
// a failure or by an enclosing scope.
func (s *Scope) Expired() error {
	if s.timeout != nil && context.Cause(s.ctx) == s.timeout {
		return s.timeout
	}
	return nil
}

// Context is cancelled when the scope fails or is cancelled.
func (s *Scope) Context() context.Context { return s.ctx }

// Go runs fn on a new goroutine owned by the scope: Supervise, with the
// one attempt fn. Under the default policy, a returned error or a panic
// fails the scope.
func (s *Scope) Go(fn func() error) {
	s.Supervise("", func(int) error { return fn() }, nil)
}

// Fail records err as the scope's failure unless one is already recorded,
// and cancels the scope. The first failure wins: Wait returns it, whatever
// fails after it (often because of the cancellation) is dropped (I5). If
// the scope's own deadline has already passed, the deadline came first:
// the failure recorded is the timeout, not err.
func (s *Scope) Fail(err error) { s.fail(err, nil) }

// fail is Fail. won, unless nil, runs if err is the first failure, after
// it is recorded and before the scope is cancelled, so whatever the
// cancellation causes comes after what won does; its argument says that
// the deadline had passed first, so the failure recorded is the timeout.
// fail reports whether err was the first.
func (s *Scope) fail(err error, won func(expired bool)) (first bool) {
	s.once.Do(func() {
		first = true
		s.expireIfDue()
		expired := s.Expired() != nil
		if expired {
			err = s.timeout
		}
		s.err = err
		if won != nil {
			won(expired)
		}
		s.cancel(err)
		close(s.failed)
	})
	return first
}

// hasFailed reports whether a failure has been recorded.
func (s *Scope) hasFailed() bool {
	select {
	case <-s.failed:
		return true
	default:
		return false
	}
}

// Wait blocks until all tasks finish and returns the first failure. If
// the scope's own deadline passed before anything failed, or before the
// scope closed, the failure is the timeout.
func (s *Scope) Wait() error {
	s.wg.Wait()
	s.expireIfDue()
	if s.Expired() != nil {
		s.Fail(s.timeout) // a no-op if a failure was recorded first
	}
	s.cancel(context.Canceled) // release the context's resources
	if s.stop != nil {
		s.stop()
	}
	return s.err
}

// expireIfDue cancels s with its timeout if its deadline has passed but
// the timer has not fired yet (it can be late on a loaded machine), so
// what happened after the deadline is not mistaken for what came first.
func (s *Scope) expireIfDue() {
	if s.ctx.Err() == nil && s.due() {
		s.cancel(s.timeout)
	}
}
