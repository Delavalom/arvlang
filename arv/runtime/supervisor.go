package runtime

import (
	"fmt"
	"hash/fnv"
	"time"
)

// Strategy is what a scope does when one of its tasks fails.
type Strategy int

const (
	FailFast Strategy = iota // the first failure cancels the scope (I5); the default
	Collect                  // a failure cancels nothing; it is that task's result
	Restart                  // a failed task runs again, up to Policy.Retries times
)

// Restart defaults, and the longest wait between two attempts.
const (
	DefaultRetries = 3
	DefaultBackoff = 100 * time.Millisecond
	MaxBackoff     = 30 * time.Second
)

// Policy is how a scope supervises its tasks. The zero value is FailFast.
type Policy struct {
	Strategy Strategy
	Retries  int             // Restart: how many times a failed task may run again
	Backoff  time.Duration   // Restart: the wait before the first rerun; it doubles after each
	RetryOn  map[string]bool // Restart: the error kinds (Kind*) that are restarted
}

// RestartPolicy is Restart with the defaults: 3 retries, 100 ms backoff,
// and only effect errors restarted: the others would fail the same way
// again (capability, budget, runtime), or must not be retried (cancelled).
func RestartPolicy() Policy {
	return Policy{Strategy: Restart, Retries: DefaultRetries, Backoff: DefaultBackoff,
		RetryOn: map[string]bool{KindEffect: true}}
}

// Delay is how long task id waits after its attempt-th attempt failed:
// Backoff doubled attempt-1 times, plus up to a tenth of that, all capped
// at MaxBackoff. The extra part (jitter, so restarted siblings spread out)
// is derived from id and attempt, not from a random source, so a replayed
// run waits the same (Phase 6).
func (p Policy) Delay(id string, attempt int) time.Duration {
	d := min(p.Backoff, MaxBackoff)
	for i := 1; i < attempt && d < MaxBackoff; i++ {
		d *= 2
	}
	d = min(d, MaxBackoff)
	h := fnv.New64a()
	fmt.Fprintf(h, "%s#%d", id, attempt)
	return min(d+time.Duration(h.Sum64()%uint64(d/10+1)), MaxBackoff)
}

// restarts reports whether a task whose attempt-th attempt failed with
// err runs again.
func (p Policy) restarts(err error, attempt int) bool {
	return p.Strategy == Restart && attempt <= p.Retries && p.RetryOn[ErrorKind(err, KindRuntime)]
}

// SetPolicy sets how s supervises its tasks. Call it before the first Go
// or Supervise.
func (s *Scope) SetPolicy(p Policy) { s.policy = p }

// Policy is how s supervises its tasks.
func (s *Scope) Policy() Policy { return s.policy }

// Supervise runs attempt(1) on a new goroutine owned by the scope and
// handles its failure by the scope's Policy:
//   - FailFast: an error fails the scope.
//   - Collect: an error does not; reporting it is the caller's job.
//   - Restart: an error of a kind in RetryOn runs attempt(2), attempt(3),
//     ... up to Retries more times, each after Delay(id, n). Once the
//     scope is cancelled (a sibling failed, a deadline passed, the parent
//     was cancelled) the wait ends and nothing runs again. An error it
//     does not restart, or the last one, fails the scope as under FailFast.
//
// A panic fails the scope whatever the policy. done, unless nil, runs once
// after that, however the attempts ended: a failure has already cancelled
// the scope when done runs.
func (s *Scope) Supervise(id string, attempt func(n int) error, done func()) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if done != nil {
			defer done()
		}
		defer func() {
			if r := recover(); r != nil {
				s.Fail(fmt.Errorf("panic in task: %v", r))
			}
		}()
		for n := 1; ; n++ {
			err := attempt(n)
			switch {
			case err == nil, s.policy.Strategy == Collect:
				return
			case s.policy.restarts(err, n) && s.backoff(s.policy.Delay(id, n)):
				continue
			}
			s.Fail(err)
			return
		}
	}()
}

// backoff waits d, or until s is cancelled; it reports whether d passed
// with s still running.
func (s *Scope) backoff(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return s.ctx.Err() == nil
	case <-s.ctx.Done():
		return false
	}
}
