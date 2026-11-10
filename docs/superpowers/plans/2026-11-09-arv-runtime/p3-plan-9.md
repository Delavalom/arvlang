# Phase 3, task p3.9: Steps budget

> **For agentic workers:** this file is one task, p3.9. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.8 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.9: Steps budget

Every evaluated node counts against `steps`. The interpreter counts nodes locally (`in.steps`) and charges them to the innermost frame's budget every 256 nodes. It also charges them when a scope opens (the nodes so far belong to the enclosing scope) and when a scope closes, so one task's total is checked exactly. A task can run at most 255 nodes past a limit before it notices, which is the overshoot the design allows.

This charges after use; the design took fuel before use. Charging after use means a task can't hold unspent fuel that starves its siblings. 20 concurrent tasks under `steps: 5000` would otherwise reserve 20×256 steps and fail spuriously. `tick()` replaces the bare `Task.Tick()` calls, and `Task.Tick` keeps doing cancellation only.

**Files:**
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/evaluator.go`
- Create (test): `arv/evaluator/steps_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/steps_test.go`:

```go
package evaluator

import (
	"context"
	"fmt"
	"io"
	goruntime "runtime"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// countdown recurses n times; each level costs 13 steps.
const countdown = `let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };`

func TestStepsLimit(t *testing.T) {
	got, _ := runWith(t, countdown+"\nscope (steps: 1000) { f(5000) }")
	wantErrorHas(t, got, "budget exceeded: steps (limit 1000)")
	if e := got.(*value.Error); e.Message != "budget exceeded: steps (limit 1000)" || e.Pos == "" {
		t.Fatalf("got %q at %q, want the bare budget error with a position", e.Message, e.Pos)
	}
	got, _ = runWith(t, countdown+"scope (steps: 1000000) { f(5000) }") // f(5000) takes 65016
	if got.Inspect() != "0" {
		t.Fatalf("under a large enough limit: got %s", got.Inspect())
	}
}

// Steps count every task under the scope: four tasks of about 400 steps
// each pass 1000 together, but fit in 5000.
func TestStepsSharedByTasks(t *testing.T) {
	prog := countdown + `scope (steps: %d) { await for i in [1, 2, 3, 4] { spawn f(30) } }`
	got, _ := runWith(t, fmt.Sprintf(prog, 1000))
	wantErrorHas(t, got, "budget exceeded: steps (limit 1000)")
	got, _ = runWith(t, fmt.Sprintf(prog, 5000))
	if got.Inspect() != "[0, 0, 0, 0]" {
		t.Fatalf("under steps: 5000: got %s", got.Inspect())
	}
}

// I6: the smaller limit wins, whichever scope sets it.
func TestNestedStepsShrink(t *testing.T) {
	got, _ := runWith(t, countdown+`scope (steps: 1000) { scope (steps: 1000000) { f(5000) } }`)
	wantErrorHas(t, got, "budget exceeded: steps (limit 1000)")
	got, _ = runWith(t, countdown+`scope (steps: 100000) { scope (steps: 2000) { f(5000) } }`)
	wantErrorHas(t, got, "budget exceeded: steps (limit 2000)")
	// a task spawned inside counts against the scope it was spawned in
	got, _ = runWith(t, countdown+`scope (steps: 2000) { await spawn f(5000) }`)
	wantErrorHas(t, got, "budget exceeded: steps (limit 2000)")
}

// Closing a scope charges its last partial chunk, so one task's total is
// exact: the smallest limit a body fits in passes, one less fails.
func TestStepsExactAtScopeExit(t *testing.T) {
	body := `let a = [1, 2, 3]; a[0] + a[1] + a[2]`
	fits := 0
	for n := 1; n < 200 && fits == 0; n++ {
		if got, _ := runWith(t, fmt.Sprintf("scope (steps: %d) { %s }", n, body)); got.Inspect() == "6" {
			fits = n
		}
	}
	if fits < 5 {
		t.Fatalf("body fit in %d steps; it needs more than that", fits)
	}
	got, _ := runWith(t, fmt.Sprintf("scope (steps: %d) { %s }", fits-1, body))
	wantErrorHas(t, got, fmt.Sprintf("budget exceeded: steps (limit %d)", fits-1))
}

// Opening and closing scopes charges what was used, never a whole chunk,
// so many small scopes fit in a small limit.
func TestManySmallScopesFit(t *testing.T) {
	got, _ := runWith(t, `scope (steps: 600) { len(for i in [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20] { scope { i } }) }`)
	if got.Inspect() != "20" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// The run's own steps limit (Limits.Steps at the root).
func TestRunLimitedSteps(t *testing.T) {
	p := parser.New(lexer.New(countdown + "f(5000)"))
	program := p.ParseProgram()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	got := New(root).RunLimited(program, value.NewEnvironment(), runtime.Limits{Steps: 500})
	wantErrorHas(t, got, "budget exceeded: steps (limit 500)")
}

// I4: running out of steps fails the scope, which cancels and waits for
// the other tasks, so nothing outlives the run.
func TestStepsExhaustionCancelsTasks(t *testing.T) {
	before := goruntime.NumGoroutine()
	got, _ := runWith(t, countdown+`scope (steps: 3000) { spawn sleep(10000); spawn sleep(10000); f(5000) }`)
	wantErrorHas(t, got, "budget exceeded: steps (limit 3000)")
	settled(t, before)
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test ./arv/evaluator/ -run 'Steps|SmallScopes'`

Expected: every new test fails because nothing counts steps, for example `got INTEGER 0, want an error mentioning "budget exceeded: steps (limit 1000)"`, and `TestStepsExactAtScopeExit` fails with `body fit in 1 steps`. `TestStepsExhaustionCancelsTasks` takes 10 s to fail, because its sleeping tasks are never cancelled.

- [ ] **Step 3: Implement**

In `arv/evaluator/concurrency.go`, replace

```go
// inside s outlives it) and becomes an Arv error naming the task.
func (in *Interpreter) withScope(f frame, body func() value.Object) (result value.Object) {
	s := f.scope
	prev := in.Task.Ctx
	in.Task.Ctx = s.Context()
	in.scopes = append(in.scopes, f)
```

with

```go
// inside s outlives it) and becomes an Arv error naming the task.
func (in *Interpreter) withScope(f frame, body func() value.Object) (result value.Object) {
	s := f.scope
	// Steps evaluated so far belong to the enclosing scope, not to this one.
	if e := in.chargeSteps(); e != nil {
		s.Wait() // nothing started yet: this only releases the scope
		return e
	}
	prev := in.Task.Ctx
	in.Task.Ctx = s.Context()
	in.scopes = append(in.scopes, f)
```

In `arv/evaluator/concurrency.go`, replace

```go
	}()

	result = body()
	var bodyErr error
	if e, ok := result.(*value.Error); ok {
		bodyErr = &failure{e}
```

with

```go
	}()

	result = body()
	if e := in.chargeSteps(); e != nil && !isError(result) {
		result = e // the last few steps passed the limit
	}
	var bodyErr error
	if e, ok := result.(*value.Error); ok {
		bodyErr = &failure{e}
```


In `arv/evaluator/evaluator.go`, replace

```go
	depth     int     // Arv call depth, bounded by runtime.MaxDepth
	evalDepth int     // nested Eval calls, bounded by runtime.MaxEvalDepth
	scopes    []frame // innermost last; spawn adds tasks to the top one
}

// frame is the per-scope state of one open scope: its tasks and its level
// of the budget tree. It lives here, not in the Task, because it changes
// as scopes open and close.
```

with

```go
	depth     int     // Arv call depth, bounded by runtime.MaxDepth
	evalDepth int     // nested Eval calls, bounded by runtime.MaxEvalDepth
	scopes    []frame // innermost last; spawn adds tasks to the top one
	steps     int64   // nodes evaluated since the last charge to a steps budget
}

// stepChunk is how many nodes a task evaluates between charges to its
// steps budget: an atomic add per limited level every stepChunk nodes, not
// every node. A task may run up to stepChunk-1 nodes past a limit before
// it notices; closing a scope charges the rest, so a single task's total
// is checked exactly.
const stepChunk = 256

// frame is the per-scope state of one open scope: its tasks and its level
// of the budget tree. It lives here, not in the Task, because it changes
// as scopes open and close.
```

In `arv/evaluator/evaluator.go`, replace

```go
	return &value.Error{Message: e.Message, StackTrace: e.StackTrace, Pos: fmt.Sprintf("%d:%d", line, col)}
}

func (in *Interpreter) eval(node ast.Node, env *value.Environment) value.Object {
	if err := in.Task.Tick(); err != nil {
		return newError("task %s cancelled: %s", in.Task.ID, err)
	}
	switch node := node.(type) {
	case *ast.Program:
		return in.evalProgram(node, env)
```

with

```go
	return &value.Error{Message: e.Message, StackTrace: e.StackTrace, Pos: fmt.Sprintf("%d:%d", line, col)}
}

// tick is called once per evaluated node. It stops a cancelled task and
// counts the node against the steps budget.
func (in *Interpreter) tick() *value.Error {
	if err := in.Task.Tick(); err != nil {
		return newError("task %s cancelled: %s", in.Task.ID, err)
	}
	if in.steps++; in.steps >= stepChunk {
		return in.chargeSteps()
	}
	return nil
}

// chargeSteps charges the nodes evaluated since the last charge to the
// innermost scope's level of the budget tree (and so to every level above).
func (in *Interpreter) chargeSteps() *value.Error {
	n := in.steps
	in.steps = 0
	if len(in.scopes) == 0 {
		return nil
	}
	if err := in.top().budget.Steps(n); err != nil {
		return newError("%s", err)
	}
	return nil
}

func (in *Interpreter) eval(node ast.Node, env *value.Environment) value.Object {
	if e := in.tick(); e != nil {
		return e
	}
	switch node := node.(type) {
	case *ast.Program:
		return in.evalProgram(node, env)
```

In `arv/evaluator/evaluator.go`, replace

```go
		if !ok {
			break
		}
		if err := in.Task.Tick(); err != nil {
			return newError("task %s cancelled: %s", in.Task.ID, err)
		}
		spine = append(spine, next)
	}
```

with

```go
		if !ok {
			break
		}
		if e := in.tick(); e != nil {
			return e
		}
		spine = append(spine, next)
	}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/evaluator/ -run 'Steps|SmallScopes'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/concurrency.go arv/evaluator/evaluator.go arv/evaluator/steps_test.go && git commit -F - <<'MSG'
feat(evaluator): steps budget

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
