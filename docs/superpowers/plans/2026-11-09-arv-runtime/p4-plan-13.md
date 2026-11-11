# Phase 4, task p4.13: `collect`: failures are results, never dropped

> **For agentic workers:** this file is one task, p4.13. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.12 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.13: `collect`: failures are results, never dropped

Under `supervise: "collect"` a failure cancels nothing; it is that task's result, for `await` and `match`. **Errors are never dropped silently**:
- `await` marks the handle awaited (`TaskHandle.MarkAwaited`).
- A collect scope keeps its tasks' handles (`Interpreter.collects`, keyed by scope, since only the interpreter that opened the scope spawns into it and closes it).
- When the scope would otherwise succeed, and failed tasks remain that nobody awaited, it fails with `N unawaited task failures: <first message>`, with the first failure's kind, position and task. The message is clipped, because error text is not charged. If the body fails, the body's error wins, under `fail_fast` rules.

Tests: the design's "collect", "collect never drops" and "escalation" rows (an inner `restart` scope that gives up, inside an outer `collect`), a property test over random failure patterns, and no leaked goroutines. `TestFailFastMatchDoesNotRescue` pins the p4.7 rule: under `fail_fast`, `match await t` on a failed task cannot rescue it, because the scope is cancelled before the handle finishes (p4.10's ordering).

**Files:**
- Create: `arv/evaluator/collect.go`
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/evaluator.go`
- Modify: `arv/value/value.go`
- Create (test): `arv/evaluator/collect_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/collect_test.go`:

```go
package evaluator

import (
	"fmt"
	"math/rand"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

const collectTasks = `let ok = fn(n) { wait(); n };
	let bad = fn() { getenv("ARV_COLLECT_X") };
`

// Acceptance: collect. The middle task fails; its siblings are not
// cancelled (they finish only after the failure has been seen), and every
// outcome is there to match.
func TestCollect(t *testing.T) {
	got := runBounded(t, 10*time.Second, collectTasks+`
		scope (supervise: "collect") {
			let ts = [spawn ok(1), spawn bad(), spawn ok(3)];
			let seen = match await ts[1] { ok(v) => v, err(e) => e["kind"] };
			open();
			for t in ts { match await t { ok(v) => v, err(e) => e["kind"] } }
		}`, gate())
	if got.Inspect() != "[1, capability, 3]" {
		t.Fatalf("got %s, want [1, capability, 3]", got.Inspect())
	}
}

// Acceptance: collect never drops. A failure nobody awaited fails the
// scope when it closes, with the count, and the first one's message,
// kind, position and task.
func TestCollectNeverDrops(t *testing.T) {
	got := runBounded(t, 10*time.Second, collectTasks+`scope (supervise: "collect") { spawn bad(); spawn bad(); open(); 1 }`, gate())
	e := wantKind(t, got, runtime.KindCapability)
	if !strings.HasPrefix(e.Message, "2 unawaited task failures: capability denied: env(\"ARV_COLLECT_X\") in task root/0") ||
		e.Task != "root/0" || e.Pos != "2:19" { // where getenv is, in bad
		t.Fatalf("got %q at %s in %s", e.Message, e.Pos, e.Task)
	}
	got = runBounded(t, 10*time.Second, collectTasks+`scope (supervise: "collect") { spawn bad(); 1 }`, gate())
	wantErrorHas(t, got, "1 unawaited task failures: ")
	// Awaited (here through match), a failure is not reported again.
	got = runBounded(t, 10*time.Second, collectTasks+`scope (supervise: "collect") {
		let t = spawn bad(); match await t { ok(v) => v, err(e) => "seen" } }`, gate())
	if got.Inspect() != "seen" {
		t.Fatalf("got %s, want seen", got.Inspect())
	}
	// A task that succeeded need not be awaited.
	got = runBounded(t, 10*time.Second, collectTasks+`scope (supervise: "collect") { spawn ok(1); open(); 2 }`, gate())
	if got.Inspect() != "2" {
		t.Fatalf("got %s, want 2", got.Inspect())
	}
}

// Acceptance: escalation. An inner restart scope that runs out of retries
// fails its task; the outer collect scope sees one failed task, and the
// others finish.
func TestEscalation(t *testing.T) {
	got, _ := runWith(t, `let flaky = fn() { error("down") };
		let inner = fn() { scope (supervise: "restart", retries: 2, backoff_ms: 1, retry_on: ["user"]) { await spawn flaky() } };
		let n = fn(x) { x };
		scope (supervise: "collect") {
			let a = spawn inner(); let b = spawn n(2); let c = spawn n(3);
			for t in [a, b, c] { match await t { ok(v) => v, err(e) => [e["kind"], e["task"]] } }
		}`)
	if got.Inspect() != "[[user, root/0/0#3], 2, 3]" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// The body's own error still fails a collect scope and cancels its tasks.
func TestCollectBodyErrorFailsFast(t *testing.T) {
	start := time.Now()
	got, _ := runWith(t, `let slow = fn() { sleep(10000) }; scope (supervise: "collect") { spawn slow(); 1 / 0 }`)
	wantErrorHas(t, got, "division by zero")
	if time.Since(start) > 5*time.Second {
		t.Fatal("the body's error did not cancel the collect scope's tasks")
	}
}

// Under fail_fast, match cannot rescue a task's failure: the scope is
// already cancelled when the awaiter sees it, and fails with it (I5).
func TestFailFastMatchDoesNotRescue(t *testing.T) {
	for i := 0; i < 20; i++ {
		got, out := runWith(t, `let bad = fn() { getenv("ARV_COLLECT_X") };
			scope { let t = spawn bad(); match await t { ok(v) => v, err(e) => puts("caught") } }`)
		wantKind(t, got, runtime.KindCapability)
		if out != "" {
			t.Fatalf("run %d: the err arm ran in a failed fail_fast scope", i)
		}
	}
}

// Property: a collect scope never drops an error. For random tasks, some
// failing, some awaited, the scope fails exactly when a failed task was
// not awaited, and counts those.
func TestCollectNeverDropsProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for i := 0; i < 150; i++ {
		n := 1 + rng.Intn(5)
		var spawns, awaits []string
		dropped := 0
		for j := 0; j < n; j++ {
			fails, awaited := rng.Intn(2) == 0, rng.Intn(2) == 0
			f := "good"
			if fails {
				f = "bad"
			}
			spawns = append(spawns, fmt.Sprintf("let t%d = spawn %s();", j, f))
			if awaited {
				awaits = append(awaits, fmt.Sprintf("match await t%d { ok(v) => v, err(e) => 0 };", j))
			} else if fails {
				dropped++
			}
		}
		src := `let good = fn() { 1 }; let bad = fn() { error("lost") };
			scope (supervise: "collect") { ` + strings.Join(spawns, " ") + strings.Join(awaits, " ") + ` "done" }`
		got, _ := runWith(t, src)
		if dropped == 0 && got.Inspect() != "done" {
			t.Fatalf("%s\n= %s, want done", src, got.Inspect())
		}
		if dropped > 0 {
			if e := wantKind(t, got, runtime.KindUser); e.Message != fmt.Sprintf("%d unawaited task failures: lost", dropped) {
				t.Fatalf("%s\n= %q, want %d unawaited", src, e.Message, dropped)
			}
		}
	}
}

// Collected failures leave no goroutine behind.
func TestCollectLeaksNothing(t *testing.T) {
	before := goruntime.NumGoroutine()
	got, _ := runWith(t, `let bad = fn(n) { error("x") }; let slow = fn() { sleep(20) };
		match scope (supervise: "collect") { for n in [1, 2, 3, 4, 5, 6] { spawn bad(n) }; spawn slow(); 1 } { ok(v) => v, err(e) => e["message"] }`)
	if got.Inspect() != "6 unawaited task failures: x" {
		t.Fatalf("got %s", got.Inspect())
	}
	settled(t, before)
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestCollect|TestEscalation|TestFailFastMatchDoesNotRescue'`

Expected: FAIL. The tests fail: (the first lines; ports and times vary)

```text
--- FAIL: TestCollectNeverDrops (0.00s)
    collect_test.go:39: got INTEGER 1, want a capability error
--- FAIL: TestCollectNeverDropsProperty (0.00s)
    collect_test.go:127: got STRING done, want a user error
--- FAIL: TestCollectLeaksNothing (0.02s)
    collect_test.go:140: got 1
```

- [ ] **Step 3: Implement**

Create `arv/evaluator/collect.go`:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// collected is what a collect scope keeps to report, when it closes, the
// failures nobody awaited: its tasks' handles, in spawn order. Only the
// interpreter that opened the scope spawns into it and closes it, so it
// needs no lock.
type collected struct {
	handles []*value.TaskHandle
}

// collecting starts keeping s's tasks if s collects (supervise:
// "collect"). withScope forgets them when s closes.
func (in *Interpreter) collecting(s *runtime.Scope) {
	if s.Policy().Strategy != runtime.Collect {
		return
	}
	if in.collects == nil {
		in.collects = map[*runtime.Scope]*collected{}
	}
	in.collects[s] = &collected{}
}

// track keeps h, just spawned into s, if s collects.
func (in *Interpreter) track(s *runtime.Scope, h *value.TaskHandle) {
	if c := in.collects[s]; c != nil {
		c.handles = append(c.handles, h)
	}
}

// unawaited is the error of a collect scope s that closed with failed
// tasks nobody awaited, or nil: under collect a failure cancels nothing
// and is only its task's result, so a failure nobody looked at would
// otherwise be lost. It says how many there were and gives the first
// one's message (clipped: error text is not charged), kind, position and
// task.
func (in *Interpreter) unawaited(s *runtime.Scope) *value.Error {
	c := in.collects[s]
	if c == nil {
		return nil
	}
	var first *value.Error
	n := 0
	for _, h := range c.handles {
		if e, ok := h.Result().(*value.Error); ok && !h.Awaited() {
			if n++; first == nil {
				first = e
			}
		}
	}
	if first == nil {
		return nil
	}
	e := newErrorKind(first.ErrKind(), "%d unawaited task failures: %s", n, clipped(first.Message))
	e.Pos, e.Task = first.Pos, first.Task
	return e
}
```

In `arv/evaluator/concurrency.go`, replace:

```go
		in.scopes = in.scopes[:len(in.scopes)-1]
		in.Task.Ctx = prev
	}()
	depth, evalDepth := in.depth, in.evalDepth
```

with:

```go
		in.scopes = in.scopes[:len(in.scopes)-1]
		in.Task.Ctx = prev
		delete(in.collects, s) // a no-op unless s collects
	}()
	depth, evalDepth := in.depth, in.evalDepth
```

In `arv/evaluator/concurrency.go`, replace:

```go
		return e
	case err == nil:
		return result
	case err == bodyErr:
```

with:

```go
		return e
	case err == nil:
		if e := in.unawaited(s); e != nil {
			return e // a collect scope's failures are never dropped
		}
		return result
	case err == bodyErr:
```

In `arv/evaluator/concurrency.go`, replace:

```go
	s := newScope(in.Task.Ctx, limits)
	s.SetPolicy(policy)
	f := frame{s, in.top().budget.Child(limits)}
	return in.withScope(f, func() value.Object {
```

with:

```go
	s := newScope(in.Task.Ctx, limits)
	s.SetPolicy(policy)
	in.collecting(s)
	f := frame{s, in.top().budget.Child(limits)}
	return in.withScope(f, func() value.Object {
```

In `arv/evaluator/concurrency.go`, replace:

```go
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	// The scope's policy decides what a failure does (runtime.Supervise).
	// Under restart, each attempt is a fresh task and interpreter (IDs
```

with:

```go
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	in.track(top.scope, handle)
	// The scope's policy decides what a failure does (runtime.Supervise).
	// Under restart, each attempt is a fresh task and interpreter (IDs
```

In `arv/evaluator/concurrency.go`, replace:

```go
	select {
	case <-h.Done():
		return h.Result()
	case <-in.Task.Ctx.Done():
```

with:

```go
	select {
	case <-h.Done():
		h.MarkAwaited()
		return h.Result()
	case <-in.Task.Ctx.Done():
```

In `arv/evaluator/evaluator.go`, replace:

```go
type Interpreter struct {
	Task      *runtime.Task
	depth     int     // Arv call depth, bounded by runtime.MaxDepth
	evalDepth int     // nested Eval calls, bounded by runtime.MaxEvalDepth
	scopes    []frame // innermost last; spawn adds tasks to the top one
	steps     int64   // nodes evaluated since the last charge to a steps budget
	held      int     // evaluator depth reserved from the run's pool (runtime.MaxRunEvalDepth)
}
```

with:

```go
type Interpreter struct {
	Task      *runtime.Task
	depth     int                           // Arv call depth, bounded by runtime.MaxDepth
	evalDepth int                           // nested Eval calls, bounded by runtime.MaxEvalDepth
	scopes    []frame                       // innermost last; spawn adds tasks to the top one
	steps     int64                         // nodes evaluated since the last charge to a steps budget
	held      int                           // evaluator depth reserved from the run's pool (runtime.MaxRunEvalDepth)
	collects  map[*runtime.Scope]*collected // the open collect scopes' tasks
}
```

In `arv/value/value.go`, replace:

```go
	"hash/fnv"
	"strings"

	"github.com/delavalom/arvlang/arv/ast"
```

with:

```go
	"hash/fnv"
	"strings"
	"sync/atomic"

	"github.com/delavalom/arvlang/arv/ast"
```

In `arv/value/value.go`, replace:

```go
// evalAwait).
type TaskHandle struct {
	ID     string
	Owner  *runtime.Task
	done   chan struct{}
	result Object
}
```

with:

```go
// evalAwait).
type TaskHandle struct {
	ID      string
	Owner   *runtime.Task
	done    chan struct{}
	result  Object
	awaited atomic.Bool
}
```

In `arv/value/value.go`, replace:

```go
// Result is valid only after Done is closed.
func (h *TaskHandle) Result() Object { return h.result }
```

with:

```go
// Result is valid only after Done is closed.
func (h *TaskHandle) Result() Object { return h.result }

// MarkAwaited records that an await has read the result. A collect scope
// reports the failures of tasks nobody awaited.
func (h *TaskHandle) MarkAwaited() { h.awaited.Store(true) }

// Awaited reports whether an await has read the result.
func (h *TaskHandle) Awaited() bool { return h.awaited.Load() }
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestCollect|TestEscalation|TestFailFastMatchDoesNotRescue'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/collect.go arv/evaluator/collect_test.go arv/evaluator/concurrency.go arv/evaluator/evaluator.go arv/value/value.go && git commit -F - <<'MSG'
feat(evaluator): collect scopes report unawaited failures

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
