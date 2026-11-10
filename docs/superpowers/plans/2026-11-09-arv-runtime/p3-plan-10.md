# Phase 3, task p3.10: `tasks:` slots and the live-task cap

> **For agentic workers:** this file is one task, p3.10. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.9 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.10: `tasks:` slots and the live-task cap

`evalSpawn` calls `top.budget.Acquire(in.Task.Ctx)` before `scope.Go`, and `defer top.budget.Release()` is the child goroutine's first statement. That defer runs on every path: a value, an error, a panic, or cancellation (16-notes).
- When a slot is full, `spawn` blocks; this is the backpressure. Cancelling the scope ends the wait with an error.
- Past the run's live-task cap, `spawn` fails with `budget exceeded: tasks (limit N live tasks)`, positioned at the spawn. That bounds spawn recursion, which nothing bounded before.

**Files:**
- Modify: `arv/evaluator/concurrency.go`
- Create (test): `arv/evaluator/livetasks_test.go`
- Create (test): `arv/evaluator/tasks_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/livetasks_test.go`:

```go
package evaluator

import (
	"context"
	"fmt"
	"io"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// spawnDeep nests tasks n deep, each awaiting the next, so n tasks are
// alive at once at the bottom. It stops by itself, so a missing cap shows
// up as "no cap", not as a run that eats all memory.
const spawnDeep = "let f = fn(n) { if (n == 0) { \"no cap\" } else { await spawn f(n - 1) } };\nf(%d)"

// Each task starts at call depth 0, so spawn recursion is bounded only by
// the run's live-task cap: past it, spawn is an error, not a crash.
func TestSpawnRecursionHitsTheLiveTaskCap(t *testing.T) {
	before := goruntime.NumGoroutine()
	p := parser.New(lexer.New(fmt.Sprintf(spawnDeep, 200)))
	program := p.ParseProgram()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	got := New(root).RunLimited(program, value.NewEnvironment(), runtime.Limits{LiveTasks: 50})
	wantErrorAt(t, got, "budget exceeded: tasks (limit 50 live tasks)", "1:55")
	settled(t, before)
}

// With no limits set, the default cap still applies.
func TestDefaultLiveTaskCap(t *testing.T) {
	got := runBounded(t, 60*time.Second, fmt.Sprintf(spawnDeep, 2000), nil)
	wantErrorHas(t, got, "budget exceeded: tasks (limit 1000 live tasks)")
}

// runIn evaluates input as the root task with b as the root scope's budget,
// so a test can inspect b afterwards.
func runIn(t *testing.T, b *runtime.Budget, input string, binds map[string]value.BuiltinFunction) value.Object {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	env := value.NewEnvironment()
	for name, fn := range binds {
		env.Set(name, &value.Builtin{Fn: fn})
	}
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	in := New(root)
	return in.withScope(frame{runtime.NewScope(root.Ctx), b}, func() value.Object {
		return in.Eval(program, env)
	})
}

// A slot and a live task are given back however the task ends: a value,
// an error, a panic, or a spawn that gave up waiting for a slot.
func TestSlotsReleasedOnEveryPath(t *testing.T) {
	binds := map[string]value.BuiltinFunction{"boom": panics}
	for _, input := range []string{
		`let ok = fn() { 1 }; await spawn ok()`,
		`await spawn len(1)`,
		`await spawn boom()`,
		`scope (timeout_ms: 50) { spawn sleep(10000); spawn sleep(1) }`,
	} {
		b := runtime.Root(runtime.Limits{Tasks: 1, LiveTasks: 1})
		runIn(t, b, input, binds)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := b.Acquire(ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: slot or live task still held afterwards: %v", input, err)
		}
	}
}
```

Create `arv/evaluator/tasks_test.go`:

```go
package evaluator

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// inflight returns enter() and leave() builtins that count the tasks
// between them, and a func reporting the peak count.
func inflight() (map[string]value.BuiltinFunction, func() int) {
	var mu sync.Mutex
	n, peak := 0, 0
	binds := map[string]value.BuiltinFunction{
		"enter": func(*runtime.Task, ...value.Object) value.Object {
			mu.Lock()
			defer mu.Unlock()
			if n++; n > peak {
				peak = n
			}
			return NIL
		},
		"leave": func(*runtime.Task, ...value.Object) value.Object {
			mu.Lock()
			defer mu.Unlock()
			n--
			return NIL
		},
	}
	return binds, func() int { mu.Lock(); defer mu.Unlock(); return peak }
}

// zeros is an Arv array literal of n zeros, to loop over.
func zeros(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0, ", n), ", ") + "]" }

// 50 tasks of 100 ms under tasks: 5 run at most five at a time, so they
// take at least ten rounds. Both bounds hold however loaded the machine
// is: sleeps never end early, and the peak is counted, not timed.
func TestTasksBackpressure(t *testing.T) {
	binds, peak := inflight()
	start := time.Now()
	got := runBounded(t, 30*time.Second, `
		let work = fn() { enter(); sleep(100); leave() };
		scope (tasks: 5) { for i in `+zeros(50)+` { spawn work() } }; "done"`, binds)
	if got.Inspect() != "done" {
		t.Fatalf("got %s", got.Inspect())
	}
	if p := peak(); p > 5 {
		t.Fatalf("peak %d tasks in flight, limit 5", p)
	}
	if d := time.Since(start); d < time.Second {
		t.Fatalf("50 x 100 ms at 5 at a time took %s, want at least 1 s", d)
	}
}

// The limit is reached, not just respected: five tasks that each wait for
// all five to arrive can only finish if all five run at once.
func TestTasksRunUpToTheLimit(t *testing.T) {
	binds, peak := inflight()
	var mu sync.Mutex
	arrived, all := 0, make(chan struct{})
	binds["rendezvous"] = func(t *runtime.Task, _ ...value.Object) value.Object {
		mu.Lock()
		if arrived++; arrived == 5 {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
			return NIL
		case <-t.Ctx.Done():
			return newError("rendezvous cancelled")
		}
	}
	got := runBounded(t, 30*time.Second, `
		let work = fn() { enter(); rendezvous(); leave() };
		scope (tasks: 5) { for i in `+zeros(6)+` { spawn work() } }; "done"`, binds)
	if got.Inspect() != "done" || peak() != 5 {
		t.Fatalf("got %s with peak %d, want done with peak 5", got.Inspect(), peak())
	}
}

// tasks counts direct children: two children holding both slots can still
// spawn and await grandchildren.
func TestTasksNoDeadlock(t *testing.T) {
	got := runBounded(t, 10*time.Second, `
		let leaf = fn(n) { n * 10 };
		let mid = fn(n) { await spawn leaf(n) };
		scope (tasks: 2) { await [spawn mid(1), spawn mid(2)] }`, nil)
	if got.Inspect() != "[10, 20]" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// I6 within a task: a nested scope's tasks also take the enclosing
// scope's slots, so tasks: 10 inside tasks: 2 runs two at a time.
func TestNestedScopeTasksShrink(t *testing.T) {
	binds, peak := inflight()
	runBounded(t, 30*time.Second, `
		let work = fn() { enter(); sleep(20); leave() };
		scope (tasks: 2) { scope (tasks: 10) { for i in `+zeros(10)+` { spawn work() } } }`, binds)
	if p := peak(); p > 2 {
		t.Fatalf("peak %d tasks in flight, outer limit 2", p)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test ./arv/evaluator/ -run 'Tasks|LiveTask|SpawnRecursion|Slots'`

Expected: `TestTasksBackpressure` fails with `peak N tasks in flight, limit 5`, `TestNestedScopeTasksShrink` fails the same way, and both live-task tests fail because the recursion finishes (`got no cap, want error "budget exceeded: tasks (limit 50 live tasks)" at 1:55`). `TestTasksRunUpToTheLimit`, `TestTasksNoDeadlock` and `TestSlotsReleasedOnEveryPath` already pass, because nothing is limited yet; they guard against regressions.

**Never run unbounded spawn recursion** (`let f = fn() { await spawn f() }; f()`) without the cap in place. It grows until the OS kills the test binary. That is why `spawnDeep` stops by itself.

- [ ] **Step 3: Implement**

In `arv/evaluator/concurrency.go`, replace

```go
		return newError("spawn in task %s: %s", in.Task.ID, err)
	}

	top := in.top()
	scope := top.scope
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	scope.Go(func() error {
		// Finish runs even if runTask panics, so no awaiter is left waiting.
		var result value.Object = newError("task %s did not finish", child.ID)
		defer func() { handle.Finish(result) }()
		result = runTask(child, top.budget.Task(), fn, args)
```

with

```go
		return newError("spawn in task %s: %s", in.Task.ID, err)
	}

	// A full scope (tasks:) makes spawn wait for a slot; cancellation ends
	// the wait. Past the run's live-task cap, spawn fails instead.
	top := in.top()
	if err := top.budget.Acquire(in.Task.Ctx); err != nil {
		var ex *runtime.ExceededError
		if errors.As(err, &ex) {
			return newError("%s", ex)
		}
		return newError("spawn in task %s cancelled: %s", in.Task.ID, err)
	}
	scope := top.scope
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	scope.Go(func() error {
		// Both run even if runTask panics: no slot or live task leaks, and
		// no awaiter is left waiting.
		defer top.budget.Release()
		var result value.Object = newError("task %s did not finish", child.ID)
		defer func() { handle.Finish(result) }()
		result = runTask(child, top.budget.Task(), fn, args)
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/evaluator/ -run 'Tasks|LiveTask|SpawnRecursion|Slots'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

All the bounds in `TestTasksBackpressure` are load-independent. Sleeps never end early, so 50 sleeps of 100 ms at 5 at a time take at least 1 s, and the peak is counted rather than timed.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/concurrency.go arv/evaluator/livetasks_test.go arv/evaluator/tasks_test.go && git commit -F - <<'MSG'
feat(evaluator): tasks: slots and the live-task cap bound spawn

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
