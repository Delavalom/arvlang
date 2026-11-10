package runtime

import (
	"context"
	"fmt"
	"sync"
)

// Scope owns a group of tasks. Wait does not return until every task has
// finished; the first failure cancels the scope's context so the other
// tasks (and the code that opened the scope) stop early. Phase 3 adds
// budgets here, Phase 4 adds supervision strategies.
type Scope struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup
	once   sync.Once
	err    error
}

// NewScope opens a scope whose context is derived from parent.
func NewScope(parent context.Context) *Scope {
	ctx, cancel := context.WithCancelCause(parent)
	return &Scope{ctx: ctx, cancel: cancel}
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
				s.fail(fmt.Errorf("panic in task: %v", r))
			}
		}()
		if err := fn(); err != nil {
			s.fail(err)
		}
	}()
}

// Cancel stops every task in the scope with cause, without recording a failure.
func (s *Scope) Cancel(cause error) { s.cancel(cause) }

func (s *Scope) fail(err error) {
	s.once.Do(func() {
		s.err = err
		s.cancel(err)
	})
}

// Wait blocks until all tasks finish and returns the first failure.
func (s *Scope) Wait() error {
	s.wg.Wait()
	s.cancel(context.Canceled) // release the context's resources
	return s.err
}
