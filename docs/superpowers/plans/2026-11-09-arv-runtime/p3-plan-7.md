# Phase 3, task p3.7: Scope options: evaluation and budget frames

> **For agentic workers:** this file is one task, p3.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.6 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.7: Scope options: evaluation and budget frames

`in.scopes` becomes `[]frame{scope, budget}`, so per-scope state lives in the interpreter and never in exported `Task` fields (16-notes). Each scope gets its budget level where the scope is created (16-notes):
- `Run` opens the root scope with `runtime.Root(runtime.Limits{})`.
- `evalScope` opens a scope with `top().budget.Child(limits)`.
- `runTask` opens a task body with the spawning frame's `budget.Task()`.

`scopeLimits` evaluates each option in the enclosing environment, in order, and validates it:
- The known names are `timeout_ms`, `tasks`, `steps` and `alloc_mb`; an unknown name is an error, so a typo never means "no limit".
- A name may appear once.
- A value must be a positive INTEGER, and 0 is rejected.
- Values that would overflow are rejected: `timeout_ms` above `MaxInt64/1e6` (the bound `sleep` uses), `alloc_mb` above `MaxInt64>>20`, `tasks` above `MaxInt32`.

Errors are positioned at the option's name, and the body does not run. The limits are recorded here but enforced from p3.8 on.

**Files:**
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/evaluator.go`
- Create: `arv/evaluator/scope_options.go`
- Create (test): `arv/evaluator/scope_options_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/scope_options_test.go`:

```go
package evaluator

import "testing"

func TestValidScopeOptions(t *testing.T) {
	tests := []struct{ input, want string }{
		{`scope (timeout_ms: 1000, tasks: 2, steps: 100000, alloc_mb: 64) { 1 + 1 }`, `2`},
		{`scope () { 3 }`, `3`},
		// option values are expressions, evaluated in the enclosing scope
		{`let n = 3; scope (tasks: n, steps: n * 1000) { n }`, `3`},
		{`let f = fn(x) { x * 2 }; scope (tasks: 5) { await for i in [1, 2] { spawn f(i) } }`, `[2, 4]`},
	}
	for _, tt := range tests {
		got, _ := runWith(t, tt.input)
		if got.Inspect() != tt.want {
			t.Errorf("%s\n  = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// Bad options fail before the body runs, positioned at the option's name.
func TestScopeOptionErrors(t *testing.T) {
	tests := []struct{ input, msg, pos string }{
		{"scope (\n  timout_ms: 100) { 1 }", `unknown scope option "timout_ms"`, "2:3"},
		{"scope (tasks: 2, tasks: 3) { 1 }", `duplicate scope option "tasks"`, "1:18"},
		{`scope (tasks: "2") { 1 }`, "scope option tasks: want a positive INTEGER, got STRING", "1:8"},
		{`scope (steps: 0) { 1 }`, "scope option steps: want a positive INTEGER, got 0", "1:8"},
		{`scope (alloc_mb: -1) { 1 }`, "scope option alloc_mb: want a positive INTEGER, got -1", "1:8"},
		{`scope (timeout_ms: 9223372036855) { 1 }`,
			"scope option timeout_ms: 9223372036855 is too large (max 9223372036854)", "1:8"},
		{`scope (alloc_mb: 8796093022208) { 1 }`,
			"scope option alloc_mb: 8796093022208 is too large (max 8796093022207)", "1:8"},
		{`scope (tasks: 2147483648) { 1 }`, "scope option tasks: 2147483648 is too large (max 2147483647)", "1:8"},
		// an error in the value is the scope's error
		{"scope (steps: nope) { 1 }", "identifier not found: nope", "1:15"},
	}
	for _, tt := range tests {
		got, out := runWith(t, tt.input+`; puts("after")`)
		wantErrorAt(t, got, tt.msg, tt.pos)
		if out != "" {
			t.Errorf("%s: printed %q; the program went on after a bad option", tt.input, out)
		}
	}
}

// The body never runs when an option is bad.
func TestBadOptionSkipsBody(t *testing.T) {
	_, out := runWith(t, `scope (bogus: 1) { puts("body ran") }`)
	if out != "" {
		t.Fatalf("output = %q, want the body skipped", out)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test ./arv/evaluator/ -run 'ScopeOption|BadOption'`

Expected: `TestScopeOptionErrors` and `TestBadOptionSkipsBody` fail, because bad options are ignored and the body runs (`got nil, want error "unknown scope option \"timout_ms\"" at 2:3` and `output = "body ran\n", want the body skipped`). The other new tests already pass.

- [ ] **Step 3: Implement**

In `arv/evaluator/concurrency.go`, replace

```go
// Run evaluates a whole program inside the root scope, so spawn works at
// top level and the program does not finish before its tasks do.
func (in *Interpreter) Run(node ast.Node, env *value.Environment) value.Object {
	return in.withScope(runtime.NewScope(in.Task.Ctx), func() value.Object {
		return in.Eval(node, env)
	})
}
```

with

```go
// Run evaluates a whole program inside the root scope, so spawn works at
// top level and the program does not finish before its tasks do.
func (in *Interpreter) Run(node ast.Node, env *value.Environment) value.Object {
	root := frame{runtime.NewScope(in.Task.Ctx), runtime.Root(runtime.Limits{})}
	return in.withScope(root, func() value.Object {
		return in.Eval(node, env)
	})
}
```

In `arv/evaluator/concurrency.go`, replace

```go
//
// A Go panic in body cancels s, waits for its tasks (I4: nothing started
// inside s outlives it) and becomes an Arv error naming the task.
func (in *Interpreter) withScope(s *runtime.Scope, body func() value.Object) (result value.Object) {
	prev := in.Task.Ctx
	in.Task.Ctx = s.Context()
	in.scopes = append(in.scopes, s)
	defer func() {
		in.scopes = in.scopes[:len(in.scopes)-1]
		in.Task.Ctx = prev
```

with

```go
//
// A Go panic in body cancels s, waits for its tasks (I4: nothing started
// inside s outlives it) and becomes an Arv error naming the task.
func (in *Interpreter) withScope(f frame, body func() value.Object) (result value.Object) {
	s := f.scope
	prev := in.Task.Ctx
	in.Task.Ctx = s.Context()
	in.scopes = append(in.scopes, f)
	defer func() {
		in.scopes = in.scopes[:len(in.scopes)-1]
		in.Task.Ctx = prev
```

In `arv/evaluator/concurrency.go`, replace

```go
	return newError("%s", err)
}

func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	return in.withScope(runtime.NewScope(in.Task.Ctx), func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
}

func (in *Interpreter) evalSpawn(node *ast.SpawnExpression, env *value.Environment) value.Object {
	if len(in.scopes) == 0 {
		return newError("spawn outside a scope")
```

with

```go
	return newError("%s", err)
}

// evalScope opens a scope with the limits its options set, as a new level
// of the budget tree under the enclosing scope's (I6).
func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	limits, errv := in.scopeLimits(node.Options, env)
	if errv != nil {
		return errv
	}
	f := frame{runtime.NewScope(in.Task.Ctx), in.top().budget.Child(limits)}
	return in.withScope(f, func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
}

// top is the innermost open scope. Run opens the first, so evaluation
// always has one; only spawn checks, to fail cleanly if misused.
func (in *Interpreter) top() frame { return in.scopes[len(in.scopes)-1] }

func (in *Interpreter) evalSpawn(node *ast.SpawnExpression, env *value.Environment) value.Object {
	if len(in.scopes) == 0 {
		return newError("spawn outside a scope")
```

In `arv/evaluator/concurrency.go`, replace

```go
		return newError("spawn in task %s: %s", in.Task.ID, err)
	}

	scope := in.scopes[len(in.scopes)-1]
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	scope.Go(func() error {
		// Finish runs even if runTask panics, so no awaiter is left waiting.
		var result value.Object = newError("task %s did not finish", child.ID)
		defer func() { handle.Finish(result) }()
		result = runTask(child, fn, args)
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node)
```

with

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
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node)
```

In `arv/evaluator/concurrency.go`, replace

```go
	return handle
}

// runTask runs fn(args) as task t on a fresh interpreter. The task body is
// its own implicit scope, so a task never finishes before its children, and
// withScope turns a panic in the body into "task <id> panicked: ..." there.
func runTask(t *runtime.Task, fn value.Object, args []value.Object) value.Object {
	in := New(t)
	return in.withScope(runtime.NewScope(t.Ctx), func() value.Object {
		return in.applyFunction(fn, args)
	})
}
```

with

```go
	return handle
}

// runTask runs fn(args) as task t on a fresh interpreter, with b as the
// budget level of its body. The task body is its own implicit scope, so a
// task never finishes before its children, and withScope turns a panic in
// the body into "task <id> panicked: ..." there.
func runTask(t *runtime.Task, b *runtime.Budget, fn value.Object, args []value.Object) value.Object {
	in := New(t)
	return in.withScope(frame{runtime.NewScope(t.Ctx), b}, func() value.Object {
		return in.applyFunction(fn, args)
	})
}
```


In `arv/evaluator/evaluator.go`, replace

```go
// It is not safe for concurrent use; every spawned task gets its own.
type Interpreter struct {
	Task      *runtime.Task
	depth     int              // Arv call depth, bounded by runtime.MaxDepth
	evalDepth int              // nested Eval calls, bounded by runtime.MaxEvalDepth
	scopes    []*runtime.Scope // innermost last; spawn adds tasks to the top one
}

// New returns an interpreter bound to task t.
```

with

```go
// It is not safe for concurrent use; every spawned task gets its own.
type Interpreter struct {
	Task      *runtime.Task
	depth     int     // Arv call depth, bounded by runtime.MaxDepth
	evalDepth int     // nested Eval calls, bounded by runtime.MaxEvalDepth
	scopes    []frame // innermost last; spawn adds tasks to the top one
}

// frame is the per-scope state of one open scope: its tasks and its level
// of the budget tree. It lives here, not in the Task, because it changes
// as scopes open and close.
type frame struct {
	scope  *runtime.Scope
	budget *runtime.Budget
}

// New returns an interpreter bound to task t.
```


Create `arv/evaluator/scope_options.go`:

```go
package evaluator

import (
	"fmt"
	"math"
	"time"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Largest values the scope options accept: beyond them a Duration, a byte
// count or a slot count would overflow.
const (
	maxTimeoutMS = math.MaxInt64 / int64(time.Millisecond)
	maxAllocMB   = math.MaxInt64 >> 20
	maxTasks     = math.MaxInt32
)

// scopeLimits evaluates a scope's options in env, in order, and turns them
// into limits. Each option may appear once and must be a positive INTEGER;
// an unknown name is an error, so a typo never silently means "no limit".
func (in *Interpreter) scopeLimits(opts []ast.ScopeOption, env *value.Environment) (runtime.Limits, *value.Error) {
	var l runtime.Limits
	seen := map[string]bool{}
	for _, o := range opts {
		var max int64
		switch o.Name {
		case "timeout_ms":
			max = maxTimeoutMS
		case "tasks":
			max = maxTasks
		case "steps":
			max = math.MaxInt64
		case "alloc_mb":
			max = maxAllocMB
		default:
			return l, optionError(o, "unknown scope option %q", o.Name)
		}
		if seen[o.Name] {
			return l, optionError(o, "duplicate scope option %q", o.Name)
		}
		seen[o.Name] = true
		v := in.Eval(o.Value, env)
		if e, ok := v.(*value.Error); ok {
			return l, e
		}
		n, ok := v.(*value.Integer)
		if !ok {
			return l, optionError(o, "scope option %s: want a positive INTEGER, got %s", o.Name, v.Type())
		}
		if n.Value <= 0 {
			return l, optionError(o, "scope option %s: want a positive INTEGER, got %d", o.Name, n.Value)
		}
		if n.Value > max {
			return l, optionError(o, "scope option %s: %d is too large (max %d)", o.Name, n.Value, max)
		}
		switch o.Name {
		case "timeout_ms":
			l.Timeout = time.Duration(n.Value) * time.Millisecond
		case "tasks":
			l.Tasks = int(n.Value)
		case "steps":
			l.Steps = n.Value
		case "alloc_mb":
			l.AllocBytes = n.Value << 20
		}
	}
	return l, nil
}

// optionError is an error positioned at the option's name.
func optionError(o ast.ScopeOption, format string, a ...interface{}) *value.Error {
	return &value.Error{Message: fmt.Sprintf(format, a...), Pos: fmt.Sprintf("%d:%d", o.Token.Line, o.Token.Col)}
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/evaluator/ -run 'ScopeOption|BadOption'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/concurrency.go arv/evaluator/evaluator.go arv/evaluator/scope_options.go arv/evaluator/scope_options_test.go && git commit -F - <<'MSG'
feat(evaluator): validate scope options into per-scope budget frames

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
