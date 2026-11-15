# Phase 3, task p3.8: `timeout_ms` and the run's timeout

> **For agentic workers:** this file is one task, p3.8. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.7 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.8: `timeout_ms` and the run's timeout

A scope with `timeout_ms` runs under `runtime.NewTimedScope`, which is `context.WithTimeoutCause` with an `*ExceededError` cause. Nested deadlines take the earlier one, which gives I6 for free. `Scope.Expired()` reports whether the scope's *own* deadline ended it. If so, `withScope` returns the clean `budget exceeded: timeout (limit N ms)` instead of the body's `sleep cancelled: …`. The error is positioned where the body was when time ran out, or at the scope if the body had finished.
- A failure before the deadline still wins (I5).
- The scope still waits for its tasks (I4), even one that ignores cancellation.
- `Interpreter.RunLimited(node, env, l)` gives the root scope the same treatment; `Run` calls it with zero limits.
- `Wait` stops the deadline's timer.

**Files:**
- Modify: `arv/evaluator/concurrency.go`
- Create (test): `arv/evaluator/goroutines_test.go`
- Create (test): `arv/evaluator/timeout_test.go`
- Modify: `arv/runtime/scope.go`
- Create (test): `arv/runtime/timed_scope_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/goroutines_test.go`:

```go
package evaluator

import (
	goruntime "runtime"
	"testing"
	"time"
)

// settled fails the test unless the goroutine count comes back to at most
// before within 10 s: nothing a run started may outlive it.
func settled(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := goruntime.NumGoroutine()
		if n <= before {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines still running, %d before the run:\n%s",
				n, before, buf[:goruntime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}
```

Create `arv/evaluator/timeout_test.go`:

```go
package evaluator

import (
	"context"
	"io"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// timed runs input and fails if it takes longer than limit. Bounds are far
// above the timeouts under test (a 5 s sleep must not run out), so they
// hold on a heavily loaded machine.
func timed(t *testing.T, limit time.Duration, input string) value.Object {
	t.Helper()
	start := time.Now()
	got, _ := runWith(t, input)
	if d := time.Since(start); d > limit {
		t.Fatalf("took %s, want under %s:\n%s", d, limit, input)
	}
	return got
}

func TestScopeTimeout(t *testing.T) {
	got := timed(t, 3*time.Second, "scope (timeout_ms: 100) {\n  sleep(5000) }")
	wantErrorAt(t, got, "budget exceeded: timeout (limit 100 ms)", "2:3")
}

// I6: the earlier deadline wins, whichever scope sets it.
func TestNestedTimeoutsShrink(t *testing.T) {
	got := timed(t, 3*time.Second, `scope (timeout_ms: 100) { scope (timeout_ms: 10000) { sleep(5000) } }`)
	wantErrorHas(t, got, "budget exceeded: timeout (limit 100 ms)")

	got = timed(t, 3*time.Second, `scope (timeout_ms: 10000) { scope (timeout_ms: 100) { sleep(5000) } }`)
	wantErrorHas(t, got, "budget exceeded: timeout (limit 100 ms)")
}

// I4: a timeout cancels the scope's tasks and still waits for them, even a
// task that ignores cancellation.
func TestTimeoutWaitsForTasks(t *testing.T) {
	var done atomic.Bool
	before := goruntime.NumGoroutine()
	got, _ := runEnv(t, `scope (timeout_ms: 50) { spawn stubborn(); spawn sleep(5000); 1 }`,
		map[string]value.BuiltinFunction{"stubborn": stubborn(300*time.Millisecond, &done)})
	wantErrorHas(t, got, "budget exceeded: timeout (limit 50 ms)")
	if !done.Load() {
		t.Fatal("the scope returned before its stubborn task finished")
	}
	settled(t, before)
}

func TestFastScopeBeatsItsTimeout(t *testing.T) {
	got := timed(t, 3*time.Second, `let f = fn() { 21 }; scope (timeout_ms: 5000) { let t = spawn f(); await t * 2 }`)
	if got.Inspect() != "42" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// I5: a failure before the deadline is the scope's error, not the timeout.
func TestFailureBeforeTimeoutWins(t *testing.T) {
	got := timed(t, 3*time.Second, `scope (timeout_ms: 2000) { spawn len(1); sleep(5000) }`)
	wantErrorHas(t, got, "argument to `len` not supported")
}

// The run's own timeout (Limits.Timeout at the root) ends the program the
// same way.
func TestRunLimitedTimeout(t *testing.T) {
	p := parser.New(lexer.New("let f = fn() { sleep(5000) };\nspawn f();\nsleep(5000)"))
	program := p.ParseProgram()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	start := time.Now()
	got := New(root).RunLimited(program, value.NewEnvironment(), runtime.Limits{Timeout: 100 * time.Millisecond})
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %s", d)
	}
	wantErrorAt(t, got, "budget exceeded: timeout (limit 100 ms)", "3:1")
}
```

Create `arv/runtime/timed_scope_test.go`:

```go
package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTimedScopeExpires(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 50}
	s := NewTimedScope(context.Background(), 50*time.Millisecond, cause)
	s.Go(func() error {
		<-s.Context().Done()
		return context.Cause(s.Context())
	})
	if err := s.Wait(); err != cause {
		t.Fatalf("Wait = %v, want the timeout cause", err)
	}
	if s.Expired() != cause {
		t.Fatalf("Expired = %v, want the timeout cause", s.Expired())
	}
}

func TestTimedScopeFailureFirst(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 10_000}
	s := NewTimedScope(context.Background(), 10*time.Second, cause)
	boom := errors.New("boom")
	s.Go(func() error { return boom })
	if err := s.Wait(); err != boom || s.Expired() != nil {
		t.Fatalf("Wait = %v, Expired = %v; want boom and nil", err, s.Expired())
	}
}

// I6: an enclosing deadline that comes first ends the inner scope, but it
// is the enclosing scope's timeout, not the inner one's.
func TestTimedScopeNested(t *testing.T) {
	outerCause := &ExceededError{Kind: "timeout", Limit: 50}
	outer := NewTimedScope(context.Background(), 50*time.Millisecond, outerCause)
	inner := NewTimedScope(outer.Context(), 10*time.Second, &ExceededError{Kind: "timeout", Limit: 10_000})
	start := time.Now()
	<-inner.Context().Done()
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("inner scope ended after %s, want the outer 50 ms deadline", d)
	}
	inner.Wait()
	outer.Wait()
	if inner.Expired() != nil || outer.Expired() != outerCause {
		t.Fatalf("inner.Expired = %v, outer.Expired = %v", inner.Expired(), outer.Expired())
	}
}

func TestUntimedScopeNeverExpires(t *testing.T) {
	s := NewScope(context.Background())
	s.Fail(errors.New("stop"))
	s.Wait()
	if s.Expired() != nil {
		t.Fatal("a scope without a deadline reported Expired")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ ./arv/evaluator/`

Expected: build failures `undefined: NewTimedScope`, `s.Expired undefined` (runtime) and `New(root).RunLimited undefined` (evaluator).

- [ ] **Step 3: Implement**

In `arv/evaluator/concurrency.go`, replace

```go
// Run evaluates a whole program inside the root scope, so spawn works at
// top level and the program does not finish before its tasks do.
func (in *Interpreter) Run(node ast.Node, env *value.Environment) value.Object {
	root := frame{runtime.NewScope(in.Task.Ctx), runtime.Root(runtime.Limits{})}
	return in.withScope(root, func() value.Object {
		return in.Eval(node, env)
	})
}

// withScope runs body with s as the innermost scope and the task's context
// switched to s's context, so a failing child also stops body. It returns
// the scope's first failure (I5), whether that was a child or body itself,
// else body's value. body's error goes through s.Fail like a child's: if a
// child failed first, body's error is usually just the cancellation, and
// the child's error wins.
//
// A Go panic in body cancels s, waits for its tasks (I4: nothing started
// inside s outlives it) and becomes an Arv error naming the task.
```

with

```go
// Run evaluates a whole program inside the root scope, so spawn works at
// top level and the program does not finish before its tasks do.
func (in *Interpreter) Run(node ast.Node, env *value.Environment) value.Object {
	return in.RunLimited(node, env, runtime.Limits{})
}

// RunLimited is Run with l as the whole run's budget: the root of the
// budget tree, with l.Timeout as a deadline on the root scope.
func (in *Interpreter) RunLimited(node ast.Node, env *value.Environment, l runtime.Limits) value.Object {
	root := frame{newScope(in.Task.Ctx, l), runtime.Root(l)}
	return in.withScope(root, func() value.Object {
		return in.Eval(node, env)
	})
}

// newScope opens a scope under ctx, with a deadline if l sets a timeout.
func newScope(ctx context.Context, l runtime.Limits) *runtime.Scope {
	if l.Timeout <= 0 {
		return runtime.NewScope(ctx)
	}
	cause := &runtime.ExceededError{Kind: "timeout", Limit: l.Timeout.Milliseconds()}
	return runtime.NewTimedScope(ctx, l.Timeout, cause)
}

// withScope runs body with f as the innermost scope and the task's context
// switched to f's scope's context, so a failing child also stops body. It
// returns the scope's first failure (I5), whether that was a child or body
// itself, else body's value; if the scope's own deadline ended it, the
// timeout error instead. body's error goes through s.Fail like a child's:
// if a child failed first, body's error is usually just the cancellation,
// and the child's error wins.
//
// A Go panic in body cancels s, waits for its tasks (I4: nothing started
// inside s outlives it) and becomes an Arv error naming the task.
```

In `arv/evaluator/concurrency.go`, replace

```go
	switch err := s.Wait(); {
	case err == nil:
		return result
	case err == bodyErr:
		return result // body failed first: keep its error value as is
	default:
```

with

```go
	switch err := s.Wait(); {
	case err == nil:
		return result
	case s.Expired() != nil:
		// The scope's own deadline ended it: report the timeout itself,
		// where the body was when time ran out (if it got that far).
		e := newError("%s", s.Expired())
		if bodyErr != nil {
			e.Pos = result.(*value.Error).Pos
		}
		return e
	case err == bodyErr:
		return result // body failed first: keep its error value as is
	default:
```

In `arv/evaluator/concurrency.go`, replace

```go
	if errv != nil {
		return errv
	}
	f := frame{runtime.NewScope(in.Task.Ctx), in.top().budget.Child(limits)}
	return in.withScope(f, func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
```

with

```go
	if errv != nil {
		return errv
	}
	f := frame{newScope(in.Task.Ctx, limits), in.top().budget.Child(limits)}
	return in.withScope(f, func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
```


In `arv/runtime/scope.go`, replace

```go
	"context"
	"fmt"
	"sync"
)

// Scope owns a group of tasks. Wait does not return until every task has
```

with

```go
	"context"
	"fmt"
	"sync"
	"time"
)

// Scope owns a group of tasks. Wait does not return until every task has
```

In `arv/runtime/scope.go`, replace

```go
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
```

with

```go
// tasks (and the code that opened the scope) stop early. Phase 3 adds
// budgets here, Phase 4 adds supervision strategies.
type Scope struct {
	ctx     context.Context
	cancel  context.CancelCauseFunc
	stop    context.CancelFunc // releases the deadline's timer; nil without one
	timeout error              // the cause the deadline cancels with
	wg      sync.WaitGroup
	once    sync.Once
	err     error
}

// NewScope opens a scope whose context is derived from parent.
```

In `arv/runtime/scope.go`, replace

```go
	return &Scope{ctx: ctx, cancel: cancel}
}

// Context is cancelled when the scope fails or is cancelled.
func (s *Scope) Context() context.Context { return s.ctx }
```

with

```go
	return &Scope{ctx: ctx, cancel: cancel}
}

// NewTimedScope is NewScope with a deadline: d after it opens, the scope
// is cancelled with cause. Deadlines nest: inside a scope that ends
// sooner, this one ends then too, so a nested timeout can only shrink (I6).
func NewTimedScope(parent context.Context, d time.Duration, cause error) *Scope {
	tctx, stop := context.WithTimeoutCause(parent, d, cause)
	s := NewScope(tctx)
	s.stop, s.timeout = stop, cause
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
```

In `arv/runtime/scope.go`, replace

```go
func (s *Scope) Wait() error {
	s.wg.Wait()
	s.cancel(context.Canceled) // release the context's resources
	return s.err
}
```

with

```go
func (s *Scope) Wait() error {
	s.wg.Wait()
	s.cancel(context.Canceled) // release the context's resources
	if s.stop != nil {
		s.stop()
	}
	return s.err
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ ./arv/evaluator/`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

The time bounds in these tests (3 s for a 100 ms timeout racing a 5 s sleep) are deliberately loose: the machine runs at load averages above 300. `settled` in `goroutines_test.go` is the leak check that later tasks reuse.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/concurrency.go arv/evaluator/goroutines_test.go arv/evaluator/timeout_test.go arv/runtime/scope.go arv/runtime/timed_scope_test.go && git commit -F - <<'MSG'
feat: timeout_ms and the run's timeout

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
