# Phase 6, task p6.7: Checks: the live-task cap, deadlines and backoffs

> **For agentic workers:** this file is one task, p6.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.6 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.7: Checks: the live-task cap, deadlines and backoffs

Three more checks, used by `spawn`, `scope` and `restart` in p6.10:
- `Task.Acquire(b)`: a spawn's slot. Replayed, it fails at the live-task cap exactly where the recording did, and otherwise takes its slot without the cap (`Budget.acquire(ctx, false)`), so this run's timing cannot fail a spawn that succeeded.
- `Task.Expired(s)`: a timed scope's deadline as it closes. Replayed, the scope has timed out exactly when the recorded one had.
- `Task.Backoff(wait)`: a restart's backoff. Replayed, it does not wait; it is cut short only where the recording's was.

`Scope.SuperviseWith(id, attempt, backoff, done)` is `Supervise` with a hook that decides each backoff; `Supervise` passes nil and behaves as before.

**Files:**
- Modify: `arv/runtime/budget.go`
- Modify: `arv/runtime/checks.go`
- Modify: `arv/runtime/supervisor.go`
- Modify (test): `arv/runtime/checks_test.go`

- [ ] **Step 1: Write the failing tests**

In `arv/runtime/checks_test.go`, replace:

```go
	"context"
	"errors"
	"testing"
	"time"
```

with:

```go
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
```

In `arv/runtime/checks_test.go`, replace:

```go
}

// A wait is a check too: replayed, a wait the recorded run saw end still
// ends when its task does, though this run has cancelled the waiter, and
```

with:

```go
}

// The live-task cap and a restart backoff are checks too: replayed, a
// spawn fails at the cap exactly where it did, and a backoff waits for
// nothing unless it was cut short.
func TestReplayedAcquireAndBackoff(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	b := Root(Limits{LiveTasks: 1})
	if err := root.Acquire(b); err != nil {
		t.Fatal(err)
	}
	if err := root.Acquire(b); err == nil {
		t.Fatal("past the cap")
	}
	if !root.Backoff(func() bool { return true }) {
		t.Fatal("backoff cut short")
	}
	l.Finish([]byte(`[null]`))

	l, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	b = Root(Limits{LiveTasks: 1})
	b.live.n.Add(1) // another task holds the only live slot in this run
	if err := root.Acquire(b); err != nil {
		t.Fatalf("replayed spawn hit this run's cap: %v", err)
	}
	var ex *ExceededError
	if err := root.Acquire(b); !errors.As(err, &ex) || ex.Limit != 1 {
		t.Fatalf("second spawn = %v, want the tasks budget error", err)
	}
	if !root.Backoff(func() bool { t.Error("a replayed backoff waited"); return true }) {
		t.Fatal("replayed backoff cut short")
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
}

// A wait is a check too: replayed, a wait the recorded run saw end still
// ends when its task does, though this run has cancelled the waiter, and
```

In `arv/runtime/checks_test.go`, replace:

```go
		t.Fatalf("replayed second wait = %v", err)
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
}
```

with:

```go
		t.Fatalf("replayed second wait = %v", err)
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
}

// SuperviseWith hands each restart's backoff to its caller, which may
// skip the wait (as a replay does) or stop the restarts.
func TestSuperviseWithDecidesEachBackoff(t *testing.T) {
	s := restartScope(5, time.Hour, KindEffect) // waiting out a backoff would time the test out
	var count atomic.Int32
	var asked []int
	s.SuperviseWith("root/0", failing(t, KindEffect, 100, &count), func(n int, wait func() bool) bool {
		asked = append(asked, n)
		return n < 2
	}, nil)
	if err := s.Wait(); err == nil || count.Load() != 2 || !slices.Equal(asked, []int{1, 2}) {
		t.Fatalf("Wait = %v after %d attempts, backoffs asked %v; want an error after 2, [1 2]", err, count.Load(), asked)
	}
}

// A scope's deadline at close is a check: replayed, a scope times out
// exactly when the recorded one did, whatever this run's clock says.
func TestReplayedExpiry(t *testing.T) {
	late, early := errors.New("late"), errors.New("early")
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	s := NewTimedScope(root.Ctx, time.Nanosecond, late)
	<-s.Context().Done()
	s.Wait()
	if err := root.Expired(s); err != late {
		t.Fatalf("first scope: Expired = %v", err)
	}
	s = NewTimedScope(root.Ctx, time.Hour, early)
	s.Wait()
	if err := root.Expired(s); err != nil {
		t.Fatalf("second scope: Expired = %v", err)
	}
	l.Finish([]byte(`[null]`))

	l, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	s = NewTimedScope(root.Ctx, time.Hour, late) // has not expired in this run
	s.Wait()
	if err := root.Expired(s); err != late {
		t.Fatalf("replayed first scope: Expired = %v", err)
	}
	s = NewTimedScope(root.Ctx, time.Nanosecond, early) // has expired in this run
	<-s.Context().Done()
	s.Wait()
	if err := root.Expired(s); err != nil {
		t.Fatalf("replayed second scope: Expired = %v", err)
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestReplayedAcquireAndBackoff|TestSuperviseWithDecidesEachBackoff|TestReplayedExpiry'`

Expected: the package fails to build: `root.Acquire undefined`, `root.Backoff undefined`, `s.SuperviseWith undefined`, `root.Expired undefined`.

- [ ] **Step 3: Implement**

In `arv/runtime/budget.go`, replace:

```go
// It gives up with ctx's cause if ctx ends first. Call Release when the
// task finishes, on every path.
func (b *Budget) Acquire(ctx context.Context) error {
	if err := context.Cause(ctx); err != nil {
		return err
```

with:

```go
// It gives up with ctx's cause if ctx ends first. Call Release when the
// task finishes, on every path.
func (b *Budget) Acquire(ctx context.Context) error { return b.acquire(ctx, true) }

// acquire is Acquire, without the live-task cap unless capped: a replayed
// spawn that the recorded run let through must not fail on this run's
// timing (Task.Acquire).
func (b *Budget) acquire(ctx context.Context, capped bool) error {
	if err := context.Cause(ctx); err != nil {
		return err
```

In `arv/runtime/budget.go`, replace:

```go
		}
	}
	if b.live.n.Add(1) > b.live.limit {
		b.live.n.Add(-1)
		b.releaseSlots(nil)
```

with:

```go
		}
	}
	if b.live.n.Add(1) > b.live.limit && capped {
		b.live.n.Add(-1)
		b.releaseSlots(nil)
```

In `arv/runtime/checks.go`, replace:

```go
package runtime

import "context"

// observe is a check: a point where t sees whether something whose timing
```

with:

```go
package runtime

import (
	"context"
	"errors"
)

// observe is a check: a point where t sees whether something whose timing
```

In `arv/runtime/checks.go`, replace:

```go
	})
}
```

with:

```go
	})
}

// Acquire is b.Acquire for a spawn by t. It is a check: replayed, the
// spawn fails at the live-task cap, or is cancelled, exactly when the
// recorded one was, and otherwise takes its slot without the cap.
func (t *Task) Acquire(b *Budget) error {
	if t.log == nil {
		return b.Acquire(t.Ctx)
	}
	return t.observe(func() (string, error) {
		err := b.Acquire(t.Ctx)
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
	return t.observe(func() (string, error) {
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
// scope still running) before t's task runs again under restart. It is a
// check: replayed, it does not wait, and is cut short exactly when the
// recorded one was.
func (t *Task) Backoff(wait func() bool) bool {
	if t.log == nil {
		return wait()
	}
	err := t.observe(func() (string, error) {
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
```

In `arv/runtime/supervisor.go`, replace:

```go
// the scope when done runs.
func (s *Scope) Supervise(id string, attempt func(n int) error, done func()) {
	s.wg.Add(1)
	go func() {
```

with:

```go
// the scope when done runs.
func (s *Scope) Supervise(id string, attempt func(n int) error, done func()) {
	s.SuperviseWith(id, attempt, nil, done)
}

// SuperviseWith is Supervise with backoff, unless nil, deciding each wait
// before a restart: backoff(n, wait) is called after attempt n failed,
// where wait() waits the policy's Delay and reports whether the scope was
// still running at the end of it, and it reports whether attempt n+1 runs.
// The evaluator passes the wait through the effect log (Task.Backoff).
func (s *Scope) SuperviseWith(id string, attempt func(n int) error, backoff func(n int, wait func() bool) bool, done func()) {
	s.wg.Add(1)
	go func() {
```

In `arv/runtime/supervisor.go`, replace:

```go
				return
			case s.policy.restarts(err, n):
				if s.backoff(s.policy.Delay(id, n)) {
					continue
				}
```

with:

```go
				return
			case s.policy.restarts(err, n):
				wait := func() bool { return s.backoff(s.policy.Delay(id, n)) }
				if backoff == nil && wait() || backoff != nil && backoff(n, wait) {
					continue
				}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestReplayedAcquireAndBackoff|TestSuperviseWithDecidesEachBackoff|TestReplayedExpiry'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/runtime/budget.go arv/runtime/checks.go arv/runtime/checks_test.go arv/runtime/supervisor.go && git commit -F - <<'MSG'
feat(runtime): live-task cap, scope deadlines and backoffs are logged checks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
