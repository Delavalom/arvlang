# Phase 2b: Scopes, tasks and fail-fast

**Goal:** `spawn` runs a function on a new goroutine as a child task with attenuated capabilities. `await` returns its result. A scope (explicit `scope { }`, every task body, and the whole program) doesn't finish until its tasks do. The first failure cancels siblings *and* the scope body.

**Invariants touched:** I2, I4, I5, I8 (memory safety part).

Semantics, precisely:
1. `spawn f(a, b) with caps` evaluates `f`, `a`, `b` and `caps` **in the parent**, then attenuates: every requested capability must be covered by the parent's set, or the spawn fails before anything runs. With no `with`, the child holds nothing.
2. The child runs `f(a, b)` inside its own implicit scope, so it can't finish before *its* children.
3. If the child's result is an error, its scope fails: the scope's context is cancelled with that error as the cause. Siblings stop at their next `Tick` or `sleep`. The code inside the scope stops too, because while a scope body runs, the task's context *is* the scope's context.
4. When a scope fails, it evaluates to an error. If the body itself failed, that's the body's error; otherwise it's the first child failure.
5. `await t` blocks until `t` finishes or the awaiting task is cancelled. It returns the child's result, which may be an error value that then propagates.

---

### Task 2.4: `runtime.Scope`

**Files:**
- Create: `arv/runtime/scope.go`
- Test: `arv/runtime/scope_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestScopeWaitsForAllTasks(t *testing.T) {
	s := NewScope(context.Background())
	var done atomic.Int32
	for i := 0; i < 10; i++ {
		s.Go(func() error {
			time.Sleep(10 * time.Millisecond)
			done.Add(1)
			return nil
		})
	}
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
	if done.Load() != 10 {
		t.Fatalf("Wait returned with %d/10 tasks done", done.Load())
	}
}

func TestScopeFailFast(t *testing.T) {
	s := NewScope(context.Background())
	boom := errors.New("boom")
	s.Go(func() error { return boom })
	s.Go(func() error {
		select {
		case <-s.Context().Done():
			return context.Cause(s.Context())
		case <-time.After(5 * time.Second):
			return errors.New("sibling was not cancelled")
		}
	})
	start := time.Now()
	if err := s.Wait(); !errors.Is(err, boom) {
		t.Fatalf("Wait = %v, want boom", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("sibling was not cancelled promptly")
	}
}

func TestScopeRecoversPanics(t *testing.T) {
	s := NewScope(context.Background())
	s.Go(func() error { panic("bad") })
	if err := s.Wait(); err == nil {
		t.Fatal("panic did not fail the scope")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./arv/runtime/ -run Scope`
Expected: build failure, `undefined: NewScope`.

- [ ] **Step 3: Implement**

```go
package runtime

import (
	"context"
	"fmt"
	"sync"
)

// Scope owns a group of tasks. Wait does not return until every task has
// finished; the first failure cancels the scope's context so the other
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
func NewScope(parent context.Context) *Scope {
	ctx, cancel := context.WithCancelCause(parent)
	return &Scope{ctx: ctx, cancel: cancel}
}

// Context is cancelled when the scope fails or is cancelled.
func (s *Scope) Context() context.Context { return s.ctx }

// Go runs fn on a new goroutine owned by the scope. A returned error or a
// panic fails the scope.
func (s *Scope) Go(fn func() error) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.fail(fmt.Errorf("panic in task: %v", r))
			}
		}()
		if err := fn(); err != nil {
			s.fail(err)
		}
	}()
}

// Cancel stops every task in the scope with cause, without recording a failure.
func (s *Scope) Cancel(cause error) { s.cancel(cause) }

func (s *Scope) fail(err error) {
	s.once.Do(func() {
		s.err = err
		s.cancel(err)
	})
}

// Wait blocks until all tasks finish and returns the first failure.
func (s *Scope) Wait() error {
	s.wg.Wait()
	s.cancel(context.Canceled) // release the context's resources
	return s.err
}
```

This is `errgroup` written out by hand on purpose. Phase 4 adds supervision strategies to `Go`, and Phase 3 adds a task semaphore. Both need to live here.

- [ ] **Step 4: Run with the race detector**

Run: `go test -race ./arv/runtime/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add arv/runtime
git commit -m "feat(runtime): Scope joins tasks and fails fast"
```

---

### Task 2.5: Thread-safe `Environment` and `TaskHandle`

A spawned closure reads its captured environment from the child goroutine while the parent may still `let` new names. Without a lock, that's a Go data race and can be a fatal concurrent map access.

**Files:**
- Replace: `arv/value/environment.go`
- Modify: `arv/value/value.go`

- [ ] **Step 1: Replace `arv/value/environment.go`**

```go
package value

import "sync"

func NewEnvironment() *Environment {
	return &Environment{store: make(map[string]Object)}
}

// Environment is a lexical scope. Closures passed to spawn are read by the
// child task while the parent may still bind names, so access is locked.
type Environment struct {
	mu    sync.RWMutex
	store map[string]Object
	outer *Environment
}

func (e *Environment) Get(name string) (Object, bool) {
	e.mu.RLock()
	obj, ok := e.store[name]
	e.mu.RUnlock()
	if !ok && e.outer != nil {
		return e.outer.Get(name)
	}
	return obj, ok
}

func (e *Environment) Set(name string, val Object) Object {
	e.mu.Lock()
	e.store[name] = val
	e.mu.Unlock()
	return val
}

func NewEnclosedEnvironment(outer *Environment) *Environment {
	env := NewEnvironment()
	env.outer = outer
	return env
}
```

- [ ] **Step 2: Add `TASK_VAL`** to the `const` block in `arv/value/value.go`, after `CAPABILITY_VAL`:

```go
	TASK_VAL         = "TASK"
```

- [ ] **Step 3: Append to `arv/value/value.go`**

```go
// TaskHandle is what spawn returns; await reads its result.
type TaskHandle struct {
	ID     string
	done   chan struct{}
	result Object
}

func NewTaskHandle(id string) *TaskHandle {
	return &TaskHandle{ID: id, done: make(chan struct{})}
}

func (h *TaskHandle) Type() ObjectType { return TASK_VAL }
func (h *TaskHandle) Inspect() string  { return "task(" + h.ID + ")" }

// Finish records the result and wakes every waiter. Call it exactly once.
func (h *TaskHandle) Finish(result Object) {
	h.result = result
	close(h.done)
}

// Done is closed once the task has finished.
func (h *TaskHandle) Done() <-chan struct{} { return h.done }

// Result is valid only after Done is closed.
func (h *TaskHandle) Result() Object { return h.result }
```

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test -race ./arv/value/ ./arv/evaluator/`
Expected: `ok`.

---

### Task 2.6: Evaluate `scope`, `spawn`, `await`; add `sleep`

**Files:**
- Create: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/evaluator.go`, `arv/evaluator/effects.go`, `arv/evaluator/interpreter_test.go`, `arv/arv.go`
- Test: `arv/evaluator/concurrency_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package evaluator

import (
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestTasksRunInParallel(t *testing.T) {
	start := time.Now()
	got, _ := runWith(t, `
		let work = fn(n) { sleep(200); n * 10 };
		scope {
			let a = spawn work(1);
			let b = spawn work(2);
			let c = spawn work(3);
			[await a, await b, await c]
		}`)
	if got.Inspect() != "[10, 20, 30]" {
		t.Fatalf("got %s", got.Inspect())
	}
	if elapsed := time.Since(start); elapsed > 450*time.Millisecond {
		t.Fatalf("took %s; tasks did not run in parallel", elapsed)
	}
}

func TestScopeWaitsForUnawaitedTasks(t *testing.T) {
	_, out := runWith(t, `
		let late = fn() { sleep(50); puts("child") };
		scope { spawn late(); };
		puts("after");`)
	if out != "child\nafter\n" {
		t.Fatalf("output = %q; scope returned before its child", out)
	}
}

func TestProgramWaitsForTopLevelTasks(t *testing.T) {
	_, out := runWith(t, `let late = fn() { sleep(30); puts("done") }; spawn late();`)
	if out != "done\n" {
		t.Fatalf("output = %q", out)
	}
}

func TestFailFastCancelsSiblings(t *testing.T) {
	t.Setenv("ARV_SECRET", "x")
	start := time.Now()
	got, _ := runWith(t, `
		let slow = fn() { sleep(10000); "finished" };
		let bad = fn() { getenv("ARV_SECRET") };
		scope {
			spawn slow();
			spawn bad();
			sleep(10000);
		}`)
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "capability denied") {
		t.Fatalf("got %v, want the child's capability error", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("sibling and scope body were not cancelled")
	}
}

func TestChildrenHoldNothingByDefault(t *testing.T) {
	t.Setenv("ARV_X", "x")
	envX := grant(t, runtime.Env, "ARV_X")

	got, _ := runWith(t, `let t = spawn getenv("ARV_X"); await t`, envX)
	wantDenied(t, got, "ARV_X")

	got, _ = runWith(t, `let t = spawn getenv("ARV_X") with [env("ARV_X")]; await t`, envX)
	if got.Inspect() != "x" {
		t.Fatalf("handed capability: got %s", got.Inspect())
	}
}

func TestSpawnCannotEscalate(t *testing.T) {
	got, _ := runWith(t, `spawn getenv("HOME") with [env("HOME")]`)
	wantDenied(t, got, `env("HOME")`)
}

func TestNestedTasksAttenuateFurther(t *testing.T) {
	t.Setenv("ARV_A", "a")
	t.Setenv("ARV_B", "b")
	caps := []runtime.Capability{grant(t, runtime.Env, "ARV_A"), grant(t, runtime.Env, "ARV_B")}
	got, _ := runWith(t, `
		let inner = fn() { getenv("ARV_B") };
		let outer = fn() {
			let t = spawn inner() with [env("ARV_B")];
			getenv("ARV_A") + await t
		};
		await spawn outer() with [env("ARV_A")]`, caps...)
	// outer never held ARV_B, so it cannot hand it to inner.
	wantDenied(t, got, "ARV_B")
}

func TestAwaitNeedsATask(t *testing.T) {
	got, _ := runWith(t, `await 5`)
	if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "want TASK") {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./arv/evaluator/ -run 'Parallel|Unawaited|TopLevel|FailFast|Default|Escalate|Nested|AwaitNeeds'`
Expected: FAIL. Results are `nil` or `identifier not found: sleep`, because the evaluator ignores the new nodes.

- [ ] **Step 3: Create `arv/evaluator/concurrency.go`**

```go
package evaluator

import (
	"context"
	"errors"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Run evaluates a whole program inside the root scope, so spawn works at
// top level and the program does not finish before its tasks do.
func (in *Interpreter) Run(node ast.Node, env *value.Environment) value.Object {
	return in.withScope(runtime.NewScope(in.Task.Ctx), func() value.Object {
		return in.Eval(node, env)
	})
}

// withScope runs body with s as the innermost scope and the task's context
// switched to s's context, so a failing child also stops body. It returns
// body's error if body failed, else the first child failure, else body's value.
func (in *Interpreter) withScope(s *runtime.Scope, body func() value.Object) value.Object {
	prev := in.Task.Ctx
	in.Task.Ctx = s.Context()
	in.scopes = append(in.scopes, s)
	defer func() {
		in.scopes = in.scopes[:len(in.scopes)-1]
		in.Task.Ctx = prev
	}()

	result := body()
	if e, ok := result.(*value.Error); ok {
		s.Cancel(errors.New(e.Message))
		s.Wait()
		return result
	}
	if err := s.Wait(); err != nil {
		return newError("%s", err)
	}
	return result
}

func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	return in.withScope(runtime.NewScope(in.Task.Ctx), func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
}

func (in *Interpreter) evalSpawn(node *ast.SpawnExpression, env *value.Environment) value.Object {
	if len(in.scopes) == 0 {
		return newError("spawn outside a scope")
	}
	fn := in.Eval(node.Call.Function, env)
	if isError(fn) {
		return fn
	}
	args := in.evalExpressions(node.Call.Arguments, env)
	if len(args) == 1 && isError(args[0]) {
		return args[0]
	}
	var reqs []runtime.Capability
	if node.Caps != nil {
		capsVal := in.Eval(node.Caps, env)
		if isError(capsVal) {
			return capsVal
		}
		arr, ok := capsVal.(*value.Array)
		if !ok {
			return newError("spawn ... with: want ARRAY of capabilities, got %s", capsVal.Type())
		}
		for _, el := range arr.Elements {
			c, ok := el.(*value.Capability)
			if !ok {
				return newError("spawn ... with: want CAPABILITY, got %s", el.Type())
			}
			reqs = append(reqs, c.Cap)
		}
	}
	caps, err := in.Task.Caps.Attenuate(reqs)
	if err != nil {
		return newError("spawn in task %s: %s", in.Task.ID, err)
	}

	scope := in.scopes[len(in.scopes)-1]
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID)
	scope.Go(func() error {
		result := runTask(child, fn, args)
		handle.Finish(result)
		if e, ok := result.(*value.Error); ok {
			return errors.New(e.Message)
		}
		return nil
	})
	return handle
}

// runTask runs fn(args) as task t on a fresh interpreter. The task body is
// its own implicit scope, so a task never finishes before its children.
func runTask(t *runtime.Task, fn value.Object, args []value.Object) value.Object {
	in := New(t)
	return in.withScope(runtime.NewScope(t.Ctx), func() value.Object {
		return in.applyFunction(fn, args)
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
	select {
	case <-h.Done():
		return h.Result()
	case <-in.Task.Ctx.Done():
		return newError("task %s cancelled while awaiting %s: %s", in.Task.ID, h.ID, context.Cause(in.Task.Ctx))
	}
}
```

- [ ] **Step 4: Wire it into `arv/evaluator/evaluator.go`.** Add the scope stack to the struct:

```go
type Interpreter struct {
	Task   *runtime.Task
	depth  int
	scopes []*runtime.Scope // innermost last; spawn adds tasks to the top one
}
```

Make the package-level `Eval` (used by the old tests and the REPL) go through `Run`, so top-level `spawn` works there too:

```go
// Eval runs node in a root scope with no capabilities, writing to stdout.
// Kept so the existing tests and the REPL keep working unchanged.
func Eval(node ast.Node, env *value.Environment) value.Object {
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), os.Stdout)
	return New(root).Run(node, env)
}
```

Add three cases to the `switch` in `(*Interpreter).Eval`, after `*ast.HashLiteral`:

```go
	case *ast.ScopeExpression:
		return in.evalScope(node, env)
	case *ast.SpawnExpression:
		return in.evalSpawn(node, env)
	case *ast.AwaitExpression:
		return in.evalAwait(node, env)
```

- [ ] **Step 5: Add `sleep` to `arv/evaluator/effects.go`.** Add `"context"` to the imports, register it at the end of `init()`:

```go
	builtins["sleep"] = &value.Builtin{Fn: sleep}
```

and append:

```go
// sleep(ms) pauses the task. It needs no capability, but it is an effect:
// it observes time, and it returns early with an error if cancelled.
func sleep(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 1 {
		return newError("sleep: want 1 argument, got %d", len(args))
	}
	ms, ok := args[0].(*value.Integer)
	if !ok || ms.Value < 0 {
		return newError("sleep: want a non-negative INTEGER of milliseconds")
	}
	timer := time.NewTimer(time.Duration(ms.Value) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return NIL
	case <-t.Ctx.Done():
		return newError("sleep cancelled: %s", context.Cause(t.Ctx))
	}
}
```

- [ ] **Step 6: Run programs inside the root scope.** Both `runWith` in `arv/evaluator/interpreter_test.go` and `Run` in `arv/arv.go` should call `.Run(program, value.NewEnvironment())` instead of `.Eval(program, value.NewEnvironment())`:

```bash
sed -i '' 's/)\.Eval(program, value\.NewEnvironment())/).Run(program, value.NewEnvironment())/' arv/evaluator/interpreter_test.go arv/arv.go
```

- [ ] **Step 7: Run everything with the race detector, several times**

Run: `go vet ./... && go test -race -count=5 ./...`
Expected: all `ok`, five times. `TestTasksRunInParallel` proves three 200 ms sleeps finish in under 450 ms. `TestFailFastCancelsSiblings` proves two 10-second sleeps are cut short in under 2 seconds.

- [ ] **Step 8: Commit**

```bash
git add arv
git commit -m "feat: structured concurrency - scope, spawn with attenuated caps, await

Every task body and the program are implicit scopes. The first failure
cancels siblings and the scope body (fail fast)."
```
