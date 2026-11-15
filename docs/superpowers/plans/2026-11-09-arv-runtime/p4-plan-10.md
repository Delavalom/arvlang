# Phase 4, task p4.10: `runtime.Policy` and `Scope.Supervise`

> **For agentic workers:** this file is one task, p4.10. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.9 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.10: `runtime.Policy` and `Scope.Supervise`

A scope gets a `Policy`: `FailFast` (the zero value), `Collect` or `Restart` with `Retries`, `Backoff` and `RetryOn` (error kinds). `Scope.Supervise(id, attempt, done)` runs `attempt(1)`, `attempt(2)`, ... on one goroutine owned by the scope.
- **FailFast:** an error fails the scope.
- **Collect:** an error does not; the caller reports it (p4.13).
- **Restart:** an error whose kind (`ErrorKind`, through `failure` from p4.2) is in `RetryOn` runs again, up to `Retries` more times. It waits `Delay(id, n)` first: `Backoff` doubled each attempt, plus up to 10% jitter from an FNV hash of `id#n` (no random source, so Phase 6 replay waits the same), capped at 30 s. Cancellation ends the wait, and nothing runs again. Anything else, and the last failure, fails the scope.
- A panic fails the scope whatever the policy. `done` runs once, after `Fail`: so when an awaiter wakes, a failed scope is already cancelled (p4.13 relies on it).

`Go(fn)` becomes `Supervise` with a single attempt. Nothing sets a policy until p4.11, so behaviour is unchanged. The backoff test needs at least 300 ms; the timing tests use lower bounds and generous upper bounds, to survive a heavily loaded machine.

**Files:**
- Modify: `arv/runtime/scope.go`
- Create: `arv/runtime/supervisor.go`
- Create (test): `arv/runtime/supervisor_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/supervisor_test.go`:

```go
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// kindErr is an error of a given kind, as the evaluator's are.
type kindErr struct{ kind, msg string }

func (e *kindErr) Error() string     { return e.msg }
func (e *kindErr) ErrorKind() string { return e.kind }

// failing returns an attempt func that fails with kind for its first
// fails attempts, then succeeds, counting attempts and checking that they
// are numbered 1, 2, 3, ...
func failing(t *testing.T, kind string, fails int, count *atomic.Int32) func(int) error {
	return func(n int) error {
		if got := count.Add(1); int(got) != n {
			t.Errorf("attempt numbered %d, want %d", n, got)
		}
		if n <= fails {
			return &kindErr{kind, fmt.Sprintf("attempt %d failed", n)}
		}
		return nil
	}
}

func restartScope(retries int, backoff time.Duration, kinds ...string) *Scope {
	s := NewScope(context.Background())
	p := Policy{Strategy: Restart, Retries: retries, Backoff: backoff, RetryOn: map[string]bool{}}
	for _, k := range kinds {
		p.RetryOn[k] = true
	}
	s.SetPolicy(p)
	return s
}

func TestFailFastIsTheDefault(t *testing.T) {
	s := NewScope(context.Background())
	var count atomic.Int32
	var done atomic.Int32
	s.Supervise("root/0", failing(t, KindEffect, 5, &count), func() { done.Add(1) })
	if err := s.Wait(); err == nil || err.Error() != "attempt 1 failed" {
		t.Fatalf("Wait = %v, want attempt 1's error", err)
	}
	if count.Load() != 1 || done.Load() != 1 || s.Context().Err() == nil {
		t.Fatalf("attempts %d, done %d, cancelled %v; want 1, 1, true", count.Load(), done.Load(), s.Context().Err() != nil)
	}
}

// Under Collect a failure cancels nothing and is not the scope's.
func TestCollectDoesNotFailTheScope(t *testing.T) {
	s := NewScope(context.Background())
	s.SetPolicy(Policy{Strategy: Collect})
	var count atomic.Int32
	cancelled := make(chan bool, 1)
	s.Supervise("root/0", failing(t, KindEffect, 1, &count), nil)
	s.Go(func() error {
		time.Sleep(50 * time.Millisecond)
		cancelled <- s.Context().Err() != nil
		return nil
	})
	if err := s.Wait(); err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	if <-cancelled {
		t.Fatal("a collected failure cancelled its sibling")
	}
}

func TestRestartRecovers(t *testing.T) {
	s := restartScope(3, time.Millisecond, KindEffect)
	var count atomic.Int32
	s.Supervise("root/0", failing(t, KindEffect, 2, &count), nil)
	if err := s.Wait(); err != nil || count.Load() != 3 {
		t.Fatalf("Wait = %v after %d attempts, want nil after 3", err, count.Load())
	}
}

// After Retries reruns the last error fails the scope.
func TestRestartGivesUp(t *testing.T) {
	s := restartScope(2, time.Millisecond, KindEffect)
	var count atomic.Int32
	s.Supervise("root/0", failing(t, KindEffect, 100, &count), nil)
	if err := s.Wait(); err == nil || err.Error() != "attempt 3 failed" || count.Load() != 3 {
		t.Fatalf("Wait = %v after %d attempts, want attempt 3's error after 3", err, count.Load())
	}
}

// Only the kinds in RetryOn restart; a capability or budget error would
// fail the same way again, and an error without a kind is a runtime one.
func TestRestartOnlyRetryableKinds(t *testing.T) {
	for _, err := range []error{
		&kindErr{KindUser, "user"}, &DeniedError{Want: Capability{Kind: Env, Scope: "X"}},
		&ExceededError{Kind: "steps", Limit: 1}, errors.New("no kind"),
	} {
		s := restartScope(3, time.Millisecond, KindEffect)
		var count atomic.Int32
		s.Supervise("root/0", func(int) error { count.Add(1); return err }, nil)
		if got := s.Wait(); got != err || count.Load() != 1 {
			t.Errorf("%v: Wait = %v after %d attempts, want it after 1", err, got, count.Load())
		}
	}
	s := restartScope(3, time.Millisecond, KindEffect, KindUser)
	var count atomic.Int32
	s.Supervise("root/0", failing(t, KindUser, 1, &count), nil)
	if err := s.Wait(); err != nil || count.Load() != 2 {
		t.Fatalf("retry_on user: Wait = %v after %d attempts", err, count.Load())
	}
}

// Backoff doubles, plus at most a tenth, capped at MaxBackoff; the same
// task and attempt always wait the same.
func TestDelayBounds(t *testing.T) {
	p := Policy{Strategy: Restart, Backoff: 100 * time.Millisecond}
	for n, base := range map[int]time.Duration{1: 100, 2: 200, 3: 400, 4: 800} {
		base *= time.Millisecond
		d := p.Delay("root/0", n)
		if d < base || d > base+base/10 {
			t.Errorf("Delay(attempt %d) = %s, want in [%s, %s]", n, d, base, base+base/10)
		}
		if d != p.Delay("root/0", n) {
			t.Errorf("Delay(attempt %d) is not deterministic", n)
		}
	}
	for _, n := range []int{10, 64, 1000} {
		if d := p.Delay("root/0", n); d != MaxBackoff {
			t.Errorf("Delay(attempt %d) = %s, want the cap %s", n, d, MaxBackoff)
		}
	}
	if d := (Policy{Backoff: time.Hour}).Delay("x", 1); d != MaxBackoff {
		t.Errorf("an hour's backoff waits %s, want the cap", d)
	}
	if d := (Policy{}).Delay("x", 5); d != 0 {
		t.Errorf("no backoff waits %s, want 0", d)
	}
}

// The waits add up: two failures with 100 ms backoff take at least 300 ms.
func TestRestartWaits(t *testing.T) {
	s := restartScope(3, 100*time.Millisecond, KindEffect)
	var count atomic.Int32
	start := time.Now()
	s.Supervise("root/0", failing(t, KindEffect, 2, &count), nil)
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("took %s, want at least 300ms", elapsed)
	}
}

// Cancelling the scope ends a backoff at once, and nothing runs again.
func TestCancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := NewScope(ctx)
	s.SetPolicy(Policy{Strategy: Restart, Retries: 3, Backoff: time.Hour, RetryOn: map[string]bool{KindEffect: true}})
	var count atomic.Int32
	failed := make(chan struct{})
	s.Supervise("root/0", func(n int) error {
		count.Add(1)
		close(failed)
		return &kindErr{KindEffect, "boom"}
	}, nil)
	<-failed
	start := time.Now()
	cancel()
	s.Wait()
	if elapsed := time.Since(start); elapsed > 10*time.Second || count.Load() != 1 {
		t.Fatalf("Wait took %s after %d attempts, want prompt after 1", elapsed, count.Load())
	}
}

// A scope's deadline also ends a backoff, and the scope reports it.
func TestDeadlineDuringBackoff(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 50}
	s := NewTimedScope(context.Background(), 50*time.Millisecond, cause)
	s.SetPolicy(Policy{Strategy: Restart, Retries: 3, Backoff: time.Hour, RetryOn: map[string]bool{KindEffect: true}})
	s.Supervise("root/0", func(int) error { return &kindErr{KindEffect, "boom"} }, nil)
	start := time.Now()
	if err := s.Wait(); err != cause {
		t.Fatalf("Wait = %v, want the timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Wait took %s", elapsed)
	}
}

// A panic fails the scope under any policy, and done still runs, after
// the scope is cancelled.
func TestSupervisePanic(t *testing.T) {
	for _, st := range []Strategy{FailFast, Collect, Restart} {
		s := NewScope(context.Background())
		s.SetPolicy(Policy{Strategy: st, Retries: 3, RetryOn: map[string]bool{KindEffect: true, KindRuntime: true}})
		var cancelledAtDone atomic.Bool
		s.Supervise("root/0", func(int) error { panic("boom") }, func() {
			cancelledAtDone.Store(s.Context().Err() != nil)
		})
		if err := s.Wait(); err == nil || err.Error() != "panic in task: boom" {
			t.Errorf("strategy %d: Wait = %v, want the panic", st, err)
		}
		if !cancelledAtDone.Load() {
			t.Errorf("strategy %d: done ran before the scope was cancelled", st)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestFailFastIsTheDefault|TestCollectDoesNotFailTheScope|TestRestart|TestDelayBounds|TestCancelDuringBackoff|TestDeadlineDuringBackoff|TestSupervisePanic'`

Expected: FAIL. The build fails: (the first lines; ports and times vary)

```text
arv/runtime/supervisor_test.go:35:7: undefined: Policy
arv/runtime/supervisor_test.go:35:24: undefined: Restart
arv/runtime/supervisor_test.go:39:4: s.SetPolicy undefined (type *Scope has no field or method SetPolicy)
arv/runtime/supervisor_test.go:47:4: s.Supervise undefined (type *Scope has no field or method Supervise)
```

- [ ] **Step 3: Implement**

In `arv/runtime/scope.go`, replace:

```go
import (
	"context"
	"fmt"
	"sync"
	"time"
```

with:

```go
import (
	"context"
	"sync"
	"time"
```

In `arv/runtime/scope.go`, replace:

```go
// Scope owns a group of tasks. Wait does not return until every task has
// finished; the first failure cancels the scope's context so the other
// tasks (and the code that opened the scope) stop early. Phase 3 adds
// budgets here, Phase 4 adds supervision strategies.
type Scope struct {
	ctx      context.Context
	cancel   context.CancelCauseFunc
```

with:

```go
// Scope owns a group of tasks. Wait does not return until every task has
// finished; by default the first failure cancels the scope's context so
// the other tasks (and the code that opened the scope) stop early. Its
// Policy can collect failures or restart failed tasks instead (Supervise).
type Scope struct {
	policy   Policy
	ctx      context.Context
	cancel   context.CancelCauseFunc
```

In `arv/runtime/scope.go`, replace:

```go
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
```

with:

```go
func (s *Scope) Context() context.Context { return s.ctx }

// Go runs fn on a new goroutine owned by the scope: Supervise, with the
// one attempt fn. Under the default policy, a returned error or a panic
// fails the scope.
func (s *Scope) Go(fn func() error) {
	s.Supervise("", func(int) error { return fn() }, nil)
}
```

Create `arv/runtime/supervisor.go`:

```go
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
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestFailFastIsTheDefault|TestCollectDoesNotFailTheScope|TestRestart|TestDelayBounds|TestCancelDuringBackoff|TestDeadlineDuringBackoff|TestSupervisePanic'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/runtime/scope.go arv/runtime/supervisor.go arv/runtime/supervisor_test.go && git commit -F - <<'MSG'
feat(runtime): scope supervision policies with a restart loop

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
