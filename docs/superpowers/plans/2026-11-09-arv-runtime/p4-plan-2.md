# Phase 4, task p4.2: Errors carry their kind through the evaluator

> **For agentic workers:** this file is one task, p4.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p4.1 is committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.2: Errors carry their kind through the evaluator

`value.Error` gets a `Kind`. `ErrKind()` reads an unset kind as `runtime`, so every existing `newError` call is already right: it is the program being wrong.
- `newErrorKind(kind, format, ...)` makes an error of another kind. `errorOf(err, fallback)` turns a Go error into an Arv error of the kind `runtime.ErrorKind` finds.
- Budget errors (steps, alloc, timeout, live tasks) become `budget`. A cancelled task's errors (`tick`, a call, `spawn`, `await`) become `cancelled`. A denied `spawn ... with` is `capability`; an OS error while canonicalising a grant is `effect`.
- `failure`, which carries an Arv error through `runtime.Scope`, implements `runtime.Kinded`, so the runtime can read the kind in p4.10. This is the 16-notes item: `Scope.Fail` keeps the `*value.Error`, not just its text. A panic or a timeout that did not start as an Arv error keeps the kind it has.
- `at` and `placeAtEnd` copied only the message, stack and position. They now copy the whole struct, or a kind would be lost each time an error passes a node.

Effects get their kinds in p4.4.

**Files:**
- Modify: `arv/evaluator/alloc.go`
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/evaluator.go`
- Modify: `arv/evaluator/scope_options.go`
- Modify: `arv/value/value.go`
- Create (test): `arv/evaluator/errkind_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/errkind_test.go`:

```go
package evaluator

import (
	"bytes"
	"context"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// wantKind fails unless got is an error of the given kind.
func wantKind(t *testing.T, got value.Object, kind string) *value.Error {
	t.Helper()
	e, ok := got.(*value.Error)
	if !ok {
		t.Fatalf("got %s %s, want a %s error", got.Type(), got.Inspect(), kind)
	}
	if e.ErrKind() != kind {
		t.Fatalf("error %q has kind %q, want %q", e.Message, e.ErrKind(), kind)
	}
	return e
}

// Errors made by the evaluator itself carry the kind of their cause.
func TestEvaluatorErrorKinds(t *testing.T) {
	for _, tt := range []struct {
		name, src string
		l         runtime.Limits
		kind      string
	}{
		{"type error", `1 + "a"`, runtime.Limits{}, runtime.KindRuntime},
		{"division by zero", `1 / 0`, runtime.Limits{}, runtime.KindRuntime},
		{"unknown identifier", `nope`, runtime.Limits{}, runtime.KindRuntime},
		{"stack overflow", `let f = fn(n) { f(n + 1) }; f(0)`, runtime.Limits{}, runtime.KindRuntime},
		{"bad scope option", `scope (bogus: 1) { 1 }`, runtime.Limits{}, runtime.KindRuntime},
		{"steps", `scope (steps: 10) { for x in [1, 2, 3, 4, 5, 6] { x } }`, runtime.Limits{}, runtime.KindBudget},
		{"root steps", `let a = 1; let b = 2; a + b`, runtime.Limits{Steps: 3}, runtime.KindBudget},
		{"alloc", `let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } }; scope (alloc_mb: 1) { d("aaaaaaaaaaaaaaaa", 20) }`, runtime.Limits{}, runtime.KindBudget},
		{"timeout", `scope (timeout_ms: 20) { sleep(5000) }`, runtime.Limits{}, runtime.KindBudget},
		{"live tasks", `let f = fn() { sleep(5000) }; spawn f(); spawn f()`, runtime.Limits{LiveTasks: 1}, runtime.KindBudget},
		{"spawn denied", `let f = fn() { 1 }; spawn f() with [env("ARV_KIND_X")]`, runtime.Limits{}, runtime.KindCapability},
		{"child's timeout, through await", `let f = fn() { scope (timeout_ms: 10) { sleep(5000) } }; let t = spawn f(); await t`, runtime.Limits{}, runtime.KindBudget},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantKind(t, runLimited(t, tt.src, tt.l), tt.kind)
		})
	}
}

// A cancelled task's errors are of kind cancelled.
func TestCancelledKind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := runtime.NewRoot(ctx, runtime.NewCapSet(), &bytes.Buffer{})
	wantKind(t, New(root).Run(parse(t, `puts(1)`), value.NewEnvironment()), runtime.KindCancelled)
}

// A panic in a task is a runtime error, not a lost kind.
func TestPanicKind(t *testing.T) {
	got, _ := runEnv(t, `let t = spawn boom(); await t`, map[string]value.BuiltinFunction{"boom": panics})
	wantKind(t, got, runtime.KindRuntime)
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestEvaluatorErrorKinds|TestCancelledKind|TestPanicKind'`

Expected: FAIL. The build fails: (the first lines; ports and times vary)

```text
arv/evaluator/errkind_test.go:19:7: e.ErrKind undefined (type *value.Error has no field or method ErrKind)
arv/evaluator/errkind_test.go:20:58: e.ErrKind undefined (type *value.Error has no field or method ErrKind)
```

- [ ] **Step 3: Implement**

In `arv/evaluator/alloc.go`, replace:

```go
package evaluator

import "github.com/delavalom/arvlang/arv/value"

// Rough Go sizes (measured on darwin/arm64, Go 1.26) of what evaluation
```

with:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Rough Go sizes (measured on darwin/arm64, Go 1.26) of what evaluation
```

In `arv/evaluator/alloc.go`, replace:

```go
func (in *Interpreter) charge(n int64) *value.Error {
	if err := in.chargeBytes(n); err != nil {
		return newError("%s", err)
	}
	return nil
```

with:

```go
func (in *Interpreter) charge(n int64) *value.Error {
	if err := in.chargeBytes(n); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	return nil
```

In `arv/evaluator/concurrency.go`, replace:

```go
// placeAtEnd stamps e with the position of node's last statement if node
// is a program, else of node; "1:1" for an empty program.
func placeAtEnd(e *value.Error, node ast.Node) *value.Error {
	if p, ok := node.(*ast.Program); ok && len(p.Statements) > 0 {
```

with:

```go
// placeAtEnd stamps e with the position of node's last statement if node
// is a program, else of node; "1:1" for an empty program. Like at, it
// copies and keeps the kind.
func placeAtEnd(e *value.Error, node ast.Node) *value.Error {
	if p, ok := node.(*ast.Program); ok && len(p.Statements) > 0 {
```

In `arv/evaluator/concurrency.go`, replace:

```go
	}
	if line, _ := ast.Pos(node); line == 0 {
		return &value.Error{Message: e.Message, StackTrace: e.StackTrace, Pos: "1:1"}
	}
	return at(e, node)
```

with:

```go
	}
	if line, _ := ast.Pos(node); line == 0 {
		c := *e
		c.Pos = "1:1"
		return &c
	}
	return at(e, node)
```

In `arv/evaluator/concurrency.go`, replace:

```go
			in.depth, in.evalDepth = depth, evalDepth // the panic skipped their unwinding
			s.Fail(fmt.Errorf("task %s panicked: %v", in.Task.ID, r))
			result = newError("%s", s.Wait())
		}
	}()
```

with:

```go
			in.depth, in.evalDepth = depth, evalDepth // the panic skipped their unwinding
			s.Fail(fmt.Errorf("task %s panicked: %v", in.Task.ID, r))
			result = errorValue(s.Wait())
		}
	}()
```

In `arv/evaluator/concurrency.go`, replace:

```go
		// timeout itself, even if everything in it succeeded late, where
		// the body was when time ran out (if it got that far).
		e := newError("%s", s.Expired())
		if bodyErr != nil {
			e.Pos = result.(*value.Error).Pos
```

with:

```go
		// timeout itself, even if everything in it succeeded late, where
		// the body was when time ran out (if it got that far).
		e := errorOf(s.Expired(), runtime.KindBudget)
		if bodyErr != nil {
			e.Pos = result.(*value.Error).Pos
```

In `arv/evaluator/concurrency.go`, replace:

```go
// failure carries an Arv error value through runtime.Scope, which stores
// Go errors, so the value (with its position) comes out the other side.
type failure struct{ v *value.Error }

func (f *failure) Error() string { return f.v.Message }

// errorValue turns a scope's failure back into an Arv error value.
func errorValue(err error) *value.Error {
	var f *failure
```

with:

```go
// failure carries an Arv error value through runtime.Scope, which stores
// Go errors, so the value (with its position and kind) comes out the other
// side. It tells the runtime the value's kind (runtime.Kinded), so a
// supervisor can decide whether to restart.
type failure struct{ v *value.Error }

func (f *failure) Error() string     { return f.v.Message }
func (f *failure) ErrorKind() string { return f.v.ErrKind() }

// errorValue turns a scope's failure back into an Arv error value. A Go
// error that did not start as one (a panic, a timeout) keeps the kind it
// has, if any: a panic is a runtime error, a timeout a budget one.
func errorValue(err error) *value.Error {
	var f *failure
```

In `arv/evaluator/concurrency.go`, replace:

```go
		return f.v
	}
	return newError("%s", err)
}
```

with:

```go
		return f.v
	}
	return errorOf(err, runtime.KindRuntime)
}
```

In `arv/evaluator/concurrency.go`, replace:

```go
	caps, err := in.Task.Caps.AttenuateEach(len(reqs), req, charge, func(c runtime.Capability) error {
		if err := in.Task.Err(); err != nil {
			stop = newError("spawn in task %s cancelled: %s", in.Task.ID, err)
		} else {
			stop = in.charge(capabilitySize + int64(len(c.Scope)))
```

with:

```go
	caps, err := in.Task.Caps.AttenuateEach(len(reqs), req, charge, func(c runtime.Capability) error {
		if err := in.Task.Err(); err != nil {
			stop = newErrorKind(runtime.KindCancelled, "spawn in task %s cancelled: %s", in.Task.ID, err)
		} else {
			stop = in.charge(capabilitySize + int64(len(c.Scope)))
```

In `arv/evaluator/concurrency.go`, replace:

```go
		return stop
	}
	if err != nil {
		return newError("spawn in task %s: %s", in.Task.ID, err)
	}
```

with:

```go
		return stop
	}
	if err != nil { // denied, or an OS error canonicalising inside a grant
		return newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "spawn in task %s: %s", in.Task.ID, err)
	}
```

In `arv/evaluator/concurrency.go`, replace:

```go
		var ex *runtime.ExceededError
		if errors.As(err, &ex) {
			return newError("%s", ex)
		}
		return newError("spawn in task %s cancelled: %s", in.Task.ID, err)
	}
	scope := top.scope
```

with:

```go
		var ex *runtime.ExceededError
		if errors.As(err, &ex) {
			return errorOf(ex, runtime.KindBudget)
		}
		return newErrorKind(runtime.KindCancelled, "spawn in task %s cancelled: %s", in.Task.ID, err)
	}
	scope := top.scope
```

In `arv/evaluator/concurrency.go`, replace:

```go
		return h.Result()
	case <-in.Task.Ctx.Done():
		return newError("task %s cancelled while awaiting %s: %s", in.Task.ID, h.ID, context.Cause(in.Task.Ctx))
	}
}
```

with:

```go
		return h.Result()
	case <-in.Task.Ctx.Done():
		return newErrorKind(runtime.KindCancelled, "task %s cancelled while awaiting %s: %s", in.Task.ID, h.ID, context.Cause(in.Task.Ctx))
	}
}
```

In `arv/evaluator/evaluator.go`, replace:

```go
// at returns e stamped with where node starts, or e itself if node has no
// position. It copies: an error value may already be shared (a task's
// result is read by every task that awaits it), so it is never mutated.
func at(e *value.Error, node ast.Node) *value.Error {
	line, col := ast.Pos(node)
```

with:

```go
// at returns e stamped with where node starts, or e itself if node has no
// position. It copies, keeping every other field (the kind among them):
// an error value may already be shared (a task's result is read by every
// task that awaits it), so it is never mutated.
func at(e *value.Error, node ast.Node) *value.Error {
	line, col := ast.Pos(node)
```

In `arv/evaluator/evaluator.go`, replace:

```go
		return e
	}
	return &value.Error{Message: e.Message, StackTrace: e.StackTrace, Pos: fmt.Sprintf("%d:%d", line, col)}
}
```

with:

```go
		return e
	}
	c := *e
	c.Pos = fmt.Sprintf("%d:%d", line, col)
	return &c
}
```

In `arv/evaluator/evaluator.go`, replace:

```go
func (in *Interpreter) tick() *value.Error {
	if err := in.Task.Tick(); err != nil {
		return newError("task %s cancelled: %s", in.Task.ID, err)
	}
	if in.steps++; in.steps >= stepChunk {
```

with:

```go
func (in *Interpreter) tick() *value.Error {
	if err := in.Task.Tick(); err != nil {
		return newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", in.Task.ID, err)
	}
	if in.steps++; in.steps >= stepChunk {
```

In `arv/evaluator/evaluator.go`, replace:

```go
	}
	if err := in.top().budget.Steps(n); err != nil {
		return newError("%s", err)
	}
	return nil
```

with:

```go
	}
	if err := in.top().budget.Steps(n); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	return nil
```

In `arv/evaluator/evaluator.go`, replace:

```go
		}
	} else if err := b.StepsSpent(); err != nil {
		return newError("%s", err)
	}
	if err := b.AllocSpent(); err != nil {
		return newError("%s", err)
	}
	return nil
```

with:

```go
		}
	} else if err := b.StepsSpent(); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	if err := b.AllocSpent(); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	return nil
```

In `arv/evaluator/evaluator.go`, replace:

```go
		}
		if err := in.Task.Err(); err != nil {
			return newError("task %s cancelled: %s", in.Task.ID, err)
		}
		if fn == putsBuiltin {
```

with:

```go
		}
		if err := in.Task.Err(); err != nil {
			return newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", in.Task.ID, err)
		}
		if fn == putsBuiltin {
```

In `arv/evaluator/evaluator.go`, replace:

```go
}

func newError(format string, a ...interface{}) *value.Error {
	return &value.Error{Message: fmt.Sprintf(format, a...)}
}
```

with:

```go
}

// newError is a runtime error: the program is wrong (types, arguments,
// overflow). Errors with another cause use newErrorKind or errorOf.
func newError(format string, a ...interface{}) *value.Error {
	return newErrorKind(runtime.KindRuntime, format, a...)
}

// newErrorKind is an error of the given kind (a runtime.Kind* constant).
func newErrorKind(kind, format string, a ...interface{}) *value.Error {
	return &value.Error{Message: fmt.Sprintf(format, a...), Kind: kind}
}

// errorOf is err as an Arv error value with err's message, of err's kind
// (runtime.ErrorKind), or fallback if err does not say.
func errorOf(err error, fallback string) *value.Error {
	return &value.Error{Message: err.Error(), Kind: runtime.ErrorKind(err, fallback)}
}
```

In `arv/evaluator/scope_options.go`, replace:

```go
// optionError is an error positioned at the option's name.
func optionError(o ast.ScopeOption, format string, a ...interface{}) *value.Error {
	return &value.Error{Message: fmt.Sprintf(format, a...), Pos: fmt.Sprintf("%d:%d", o.Token.Line, o.Token.Col)}
}
```

with:

```go
// optionError is an error positioned at the option's name.
func optionError(o ast.ScopeOption, format string, a ...interface{}) *value.Error {
	e := newError(format, a...)
	e.Pos = fmt.Sprintf("%d:%d", o.Token.Line, o.Token.Col)
	return e
}
```

In `arv/value/value.go`, replace:

```go
// Error is an Arv error value. Pos is "line:col" of the innermost node it
// arose in or passed through ("" if none); the evaluator sets it once, on
// a copy, and never changes it after.
type Error struct {
	Message    string
	StackTrace string
	Pos        string
}

func (e *Error) Type() ObjectType { return ERROR_VAL }
func (e *Error) Inspect() string  { return "ERROR: " + e.Message }

type Function struct {
```

with:

```go
// Error is an Arv error value. Pos is "line:col" of the innermost node it
// arose in or passed through ("" if none); the evaluator sets it once, on
// a copy, and never changes it after. Kind is one of the runtime.Kind*
// constants ("" reads as runtime.KindRuntime, see ErrKind).
type Error struct {
	Message    string
	StackTrace string
	Pos        string
	Kind       string
}

func (e *Error) Type() ObjectType { return ERROR_VAL }
func (e *Error) Inspect() string  { return "ERROR: " + e.Message }

// ErrKind is e's kind, runtime.KindRuntime if none was set.
func (e *Error) ErrKind() string {
	if e.Kind == "" {
		return runtime.KindRuntime
	}
	return e.Kind
}

type Function struct {
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestEvaluatorErrorKinds|TestCancelledKind|TestPanicKind'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/alloc.go arv/evaluator/concurrency.go arv/evaluator/errkind_test.go arv/evaluator/evaluator.go arv/evaluator/scope_options.go arv/value/value.go && git commit -F - <<'MSG'
feat(evaluator): errors carry a kind: runtime, budget, cancelled, capability

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
