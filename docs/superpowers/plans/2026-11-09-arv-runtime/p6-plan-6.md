# Phase 6, task p6.6: Checks: cancellation and waits

> **For agentic workers:** this file is one task, p6.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.5 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.6: Checks: cancellation and waits

Some outcomes depend on timing, not on the program: whether a task sees its cancellation at this `Tick` or the next, whether an `await` ends with the result or with the waiter's cancellation. Each such look is a **check**, numbered per task (`Task.checks`). `observe(probe, replay)`:
- with no log, or live, `probe` looks; a recording logs what it saw, and nothing when it saw nothing;
- replayed, nothing is looked at: the outcome is the log's record for this check number, or "" if the next record is for a later check (a record for an earlier one means the run diverged).

Built on it: `Task.Err` (before each builtin) and `Tick` (every 256 nodes) are checks; a replayed `cancelled` waits for this run's real cancellation before returning it. The old `Err` body becomes `LiveErr`, for code inside an effect, which runs only live: `readError` and `http_get` switch to it. `WaitFor(done)` is the check for `await`: a replayed wait that ended normally waits for `done`, even if this run has already cancelled the waiter.

**Files:**
- Modify: `arv/evaluator/effects.go`
- Modify: `arv/evaluator/httpget.go`
- Create: `arv/runtime/checks.go`
- Modify: `arv/runtime/task.go`
- Create (test): `arv/runtime/checks_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/checks_test.go`:

```go
package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// A task records where it saw its cancellation (the third check here);
// replayed, it does not see it before that check, and at that check it
// waits until this run cancels it too.
func TestReplayedCancellationIsSeenAtTheSameCheck(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	ctx := l.Start(context.Background())
	sctx, cancel := context.WithCancelCause(ctx)
	task := NewRoot(ctx, NewCapSet(), &bytes.Buffer{})
	task.SetLog(l)
	task = task.Child(sctx, NewCapSet())
	if task.Err() != nil || task.Err() != nil {
		t.Fatal("cancelled too early")
	}
	cancel(errors.New("sibling failed"))
	if err := task.Err(); err == nil || err.Error() != "sibling failed" {
		t.Fatalf("Err = %v", err)
	}
	l.Finish([]byte(`[1]`))

	_, root := replayer(t, log.Bytes(), &bytes.Buffer{})
	rctx, rcancel := context.WithCancelCause(root.Ctx)
	rcancel(errors.New("sibling failed")) // already cancelled: the first two checks still pass
	task = root.Child(rctx, NewCapSet())
	if task.Err() != nil || task.Err() != nil {
		t.Fatal("replay saw the cancellation before the recorded check")
	}
	if err := task.Err(); err == nil || err.Error() != "sibling failed" {
		t.Fatalf("third check = %v", err)
	}

	_, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	rctx, rcancel = context.WithCancelCause(root.Ctx)
	task = root.Child(rctx, NewCapSet())
	task.Err()
	task.Err()
	done := make(chan error, 1)
	go func() { done <- task.Err() }()
	select {
	case err := <-done:
		t.Fatalf("the third check returned %v before this run was cancelled", err)
	case <-time.After(50 * time.Millisecond):
	}
	rcancel(errors.New("now"))
	if err := <-done; err == nil || err.Error() != "now" {
		t.Fatalf("third check = %v", err)
	}
}

// A wait is a check too: replayed, a wait the recorded run saw end still
// ends when its task does, though this run has cancelled the waiter, and
// a wait the recorded run saw cut short is cut short.
func TestReplayedWait(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	ctx, cancel := context.WithCancelCause(root.Ctx)
	task := root.Child(ctx, NewCapSet())
	finished := make(chan struct{})
	close(finished)
	if err := task.WaitFor(finished); err != nil {
		t.Fatal(err)
	}
	cancel(errors.New("sibling failed"))
	if err := task.WaitFor(make(chan struct{})); err == nil || err.Error() != "sibling failed" {
		t.Fatalf("second wait = %v", err)
	}
	l.Finish([]byte(`[null]`))

	l, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	ctx, cancel = context.WithCancelCause(root.Ctx)
	cancel(errors.New("sibling failed")) // before the first wait, this time
	task = root.Child(ctx, NewCapSet())
	if err := task.WaitFor(finished); err != nil {
		t.Fatalf("replayed first wait = %v", err)
	}
	if err := task.WaitFor(make(chan struct{})); err == nil || err.Error() != "sibling failed" {
		t.Fatalf("replayed second wait = %v", err)
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestReplayedCancellationIsSeenAtTheSameCheck|TestReplayedWait'`

Expected: the package fails to build: `task.WaitFor undefined`. (Without `observe`, `TestReplayedCancellationIsSeenAtTheSameCheck` would also fail: the replay sees the cancellation at the first check.)

- [ ] **Step 3: Implement**

In `arv/evaluator/effects.go`, replace:

```go
	if kind == runtime.KindEffect || kind == runtime.KindCancelled {
		kind = runtime.KindEffect
		if t.Err() != nil {
			kind = runtime.KindCancelled
		}
```

with:

```go
	if kind == runtime.KindEffect || kind == runtime.KindCancelled {
		kind = runtime.KindEffect
		if t.LiveErr() != nil {
			kind = runtime.KindCancelled
		}
```

In `arv/evaluator/httpget.go`, replace:

```go
		// the task was cancelled first.
		kind := runtime.KindEffect
		if t.Err() != nil {
			kind = runtime.KindCancelled
		}
```

with:

```go
		// the task was cancelled first.
		kind := runtime.KindEffect
		if t.LiveErr() != nil {
			kind = runtime.KindCancelled
		}
```

Create `arv/runtime/checks.go`:

```go
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
```

In `arv/runtime/task.go`, replace:

```go
	log      *EffectLog // the run's effect log, nil if it keeps none
	seq      uint64     // records of this task logged or replayed so far
	children atomic.Uint64
	horizon  []uint64 // spawn times from the root's child down to this task (see Horizon)
```

with:

```go
	log      *EffectLog // the run's effect log, nil if it keeps none
	seq      uint64     // records of this task logged or replayed so far
	checks   uint64     // checks made so far (see observe)
	children atomic.Uint64
	horizon  []uint64 // spawn times from the root's child down to this task (see Horizon)
```

In `arv/runtime/task.go`, replace:

```go

// Tick is called once per evaluated node. It returns the context error once
// the task has been cancelled. Budgets (Phase 3) hook in here.
func (t *Task) Tick() error {
	t.steps++
	if t.steps%checkEvery == 0 {
		return context.Cause(t.Ctx)
	}
	return nil
}

// Err reports why the task must stop: its context's cause once cancelled,
// or context.DeadlineExceeded once a deadline on its context has passed
// but the timer that cancels it has not fired yet (it can be late on a
// loaded machine). nil while the task may go on. Unlike Tick, it checks
// every time, so the evaluator calls it before each builtin: no effect
// (a write, an output line) runs in a task whose scope has already ended.
func (t *Task) Err() error {
	if t.Ctx.Err() != nil {
		return context.Cause(t.Ctx)
```

with:

```go

// Tick is called once per evaluated node. It returns the context error once
// the task has been cancelled, looking every checkEvery nodes; each look
// is a check (see Err).
func (t *Task) Tick() error {
	t.steps++
	if t.steps%checkEvery != 0 {
		return nil
	}
	if t.log == nil {
		return context.Cause(t.Ctx)
	}
	return t.observe(func() (string, error) {
		if err := context.Cause(t.Ctx); err != nil {
			return CheckCancelled, err
		}
		return "", nil
	}, t.cancelled)
}

// LiveErr is Err as the world has it now, never logged or replayed: for
// code inside an effect, which runs only live (Effect's do).
func (t *Task) LiveErr() error {
	if t.Ctx.Err() != nil {
		return context.Cause(t.Ctx)
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestReplayedCancellationIsSeenAtTheSameCheck|TestReplayedWait'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/effects.go arv/evaluator/httpget.go arv/runtime/checks.go arv/runtime/checks_test.go arv/runtime/task.go && git commit -F - <<'MSG'
feat(runtime): cancellation and waits are logged checks; Task.LiveErr

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
