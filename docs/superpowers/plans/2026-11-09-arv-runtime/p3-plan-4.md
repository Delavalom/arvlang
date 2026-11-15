# Phase 3, task p3.4: `await` on an array of tasks

> **For agentic workers:** this file is one task, p3.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.3 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.4: `await` on an array of tasks

`await [t1, t2]` awaits each task in order and yields the array of results. If an element fails, the first error in array order is the result. Every element is type-checked and ownership-checked before anything is awaited, so a bad element fails at once instead of after the tasks before it finish. The single-handle path keeps its exact messages: e2e matches `await: want TASK, got INTEGER`. `evalAwait` is split into `checkAwait` (the structured ownership rule, moved verbatim) and `wait`.

**Files:**
- Create (test): `arv/evaluator/await_array_test.go`
- Modify: `arv/evaluator/concurrency.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/await_array_test.go`:

```go
package evaluator

import (
	"testing"
	"time"
)

func TestAwaitArray(t *testing.T) {
	tests := []struct{ input, want string }{
		{`let f = fn(n) { n * 10 }; await [spawn f(1), spawn f(2)]`, `[10, 20]`},
		{`await []`, `[]`},
		{`let f = fn(n) { n * 2 }; await for i in [1, 2, 3] { spawn f(i) }`, `[2, 4, 6]`},
		// results come back in array order, not finishing order
		{`let f = fn(ms) { sleep(ms); ms }; await [spawn f(60), spawn f(1), spawn f(30)]`, `[60, 1, 30]`},
		// a handle may be awaited again, alone or in an array
		{`let f = fn() { 7 }; let t = spawn f(); [await t, await [t, t]]`, `[7, [7, 7]]`},
	}
	for _, tt := range tests {
		got, _ := runWith(t, tt.input)
		if got.Inspect() != tt.want {
			t.Errorf("%s\n  = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

func TestAwaitArrayErrors(t *testing.T) {
	// the first error in array order is the result
	got, _ := runWith(t, "let f = fn(n) { 10 / n };\nawait [spawn f(1), spawn f(0)]")
	wantErrorAt(t, got, "division by zero", "1:17")

	// every element is checked before anything is awaited: a bad element
	// fails at once instead of after the 5 s sleep
	start := time.Now()
	got, _ = runWith(t, "let t = spawn sleep(5000);\n await [t, 5]")
	wantErrorAt(t, got, "await: want TASK, got INTEGER (element 1)", "2:2")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %s: the bad element was found only after awaiting t", d)
	}

	// the ownership rule applies to each element
	got, _ = runWith(t, `
		let a = spawn sleep(10);
		let f = fn() { await [a] };
		await spawn f()`)
	wantErrorHas(t, got, "await: task root/0 was not spawned by task root/1 or its descendants")
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test ./arv/evaluator/ -run Await`

Expected: every `TestAwaitArray` case fails with `ERROR: await: want TASK, got ARRAY`.

- [ ] **Step 3: Implement**

In `arv/evaluator/concurrency.go`, replace

```go
	})
}

func (in *Interpreter) evalAwait(node *ast.AwaitExpression, env *value.Environment) value.Object {
	v := in.Eval(node.Task, env)
	if isError(v) {
		return v
	}
	h, ok := v.(*value.TaskHandle)
	if !ok {
		return newError("await: want TASK, got %s", v.Type())
	}
	// Structured rule: a task may await only handles spawned by itself or
	// by one of its descendants. Awaiting itself, a sibling or an ancestor
	// could wait on a task that is itself waiting (via its scope) for the
	// awaiter: a cycle nothing would break. With this rule every await
	// points down the task tree, so no cycle can form.
	if h.Owner != in.Task && !in.Task.IsAncestorOf(h.Owner) {
		if h.ID == in.Task.ID {
			return newError("await: task %s cannot await itself", h.ID)
		}
		return newError("await: task %s was not spawned by task %s or its descendants", h.ID, in.Task.ID)
	}
	select {
	case <-h.Done():
		return h.Result()
```

with

```go
	})
}

// evalAwait evaluates await t, which yields t's result, and await [t1, t2],
// which awaits each in order and yields the array of results. Every element
// is checked before anything is awaited; the first error, in order, is the
// result.
func (in *Interpreter) evalAwait(node *ast.AwaitExpression, env *value.Environment) value.Object {
	v := in.Eval(node.Task, env)
	if isError(v) {
		return v
	}
	var handles []*value.TaskHandle
	switch v := v.(type) {
	case *value.TaskHandle:
		if err := in.checkAwait(v); err != nil {
			return err
		}
		return in.wait(v)
	case *value.Array:
		handles = make([]*value.TaskHandle, len(v.Elements))
		for i, el := range v.Elements {
			h, ok := el.(*value.TaskHandle)
			if !ok {
				return newError("await: want TASK, got %s (element %d)", el.Type(), i)
			}
			if err := in.checkAwait(h); err != nil {
				return err
			}
			handles[i] = h
		}
	default:
		return newError("await: want TASK, got %s", v.Type())
	}
	results := make([]value.Object, len(handles))
	for i, h := range handles {
		r := in.wait(h)
		if isError(r) {
			return r
		}
		results[i] = r
	}
	return &value.Array{Elements: results}
}

// checkAwait enforces the structured rule: a task may await only handles
// spawned by itself or by one of its descendants. Awaiting itself, a
// sibling or an ancestor could wait on a task that is itself waiting (via
// its scope) for the awaiter: a cycle nothing would break. With this rule
// every await points down the task tree, so no cycle can form.
func (in *Interpreter) checkAwait(h *value.TaskHandle) *value.Error {
	if h.Owner == in.Task || in.Task.IsAncestorOf(h.Owner) {
		return nil
	}
	if h.ID == in.Task.ID {
		return newError("await: task %s cannot await itself", h.ID)
	}
	return newError("await: task %s was not spawned by task %s or its descendants", h.ID, in.Task.ID)
}

// wait blocks until h finishes or the awaiting task is cancelled.
func (in *Interpreter) wait(h *value.TaskHandle) value.Object {
	select {
	case <-h.Done():
		return h.Result()
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/evaluator/ -run Await`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/await_array_test.go arv/evaluator/concurrency.go && git commit -F - <<'MSG'
feat(evaluator): await on an array of tasks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
