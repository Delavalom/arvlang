package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Scope owns a group of tasks. Wait does not return until every task has
// finished; the first failure cancels the scope's context so the other
// tasks (and the code that opened the scope) stop early. Phase 3 adds
// budgets here, Phase 4 adds supervision strategies.
type Scope struct {
	ctx      context.Context
	cancel   context.CancelCauseFunc
	stop     context.CancelFunc // releases the deadline's timer; nil without one
	timeout  error              // the cause the deadline cancels with
	deadline time.Time          // when the scope's own deadline passes
	wg       sync.WaitGroup
	once     sync.Once
	err      error
}

// NewScope opens a scope whose context is derived from parent.
func NewScope(parent context.Context) *Scope {
	ctx, cancel := context.WithCancelCause(parent)
	return &Scope{ctx: ctx, cancel: cancel}
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

// Go runs fn on a new goroutine owned by the scope. A returned error or a
// panic fails the scope.
func (s *Scope) Go(fn func() error) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.Fail(fmt.Errorf("panic in task: %v", r))
			}
		}()
		if err := fn(); err != nil {
			s.Fail(err)
		}
	}()
}

// Fail records err as the scope's failure unless one is already recorded,
// and cancels the scope. The first failure wins: Wait returns it, whatever
// fails after it (often because of the cancellation) is dropped (I5). If
// the scope's own deadline has already passed, the deadline came first:
// the failure recorded is the timeout, not err.
func (s *Scope) Fail(err error) {
	s.once.Do(func() {
		s.expireIfDue()
		if s.Expired() != nil {
			err = s.timeout
		}
		s.err = err
		s.cancel(err)
	})
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
	if s.timeout != nil && s.ctx.Err() == nil && !time.Now().Before(s.deadline) {
		s.cancel(s.timeout)
	}
}
