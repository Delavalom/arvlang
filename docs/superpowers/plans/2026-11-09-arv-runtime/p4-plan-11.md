# Phase 4, task p4.11: Restart spawned tasks, with attempt IDs

> **For agentic workers:** this file is one task, p4.11. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.10 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.11: Restart spawned tasks, with attempt IDs

`spawn` runs its task under `scope.Supervise`. Under `restart`, each attempt is a fresh `Task` and interpreter running `f(args)` again:
- `Task.Attempt(n)` names the attempt `root/0#2`, `root/0#3`, ... (the design's step 7). It keeps the first attempt's authority, context, parent and horizon, so every attempt sees the environments as they were at the spawn (I8), and the spawner still owns the handle (the await-ownership rule).
- Every attempt runs in the same task slot and against the same budgets. Nothing is refunded, so a restart never resets or escapes a budget (I6).
- `done` releases the slot, then finishes the handle with the last attempt's result.

The scope option that selects `restart` comes in p4.12. These tests set the root scope's policy directly through `runPolicy`. They cover the design's acceptance rows "restart recovers", "no retry on capability" and "backoff", plus: giving up with the last error, the spawn-time environment, budgets never reset, `timeout_ms` ending the retries, siblings unaffected, and no goroutine left behind.

**Files:**
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/runtime/task.go`
- Create (test): `arv/evaluator/restart_test.go`
- Create (test): `arv/runtime/attempt_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/restart_test.go`:

```go
package evaluator

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runPolicy runs input as the root task holding caps, with l as the run's
// budget and p as the root scope's policy, as RunLimited would if the
// root scope had a supervise option. It returns the result and what puts
// printed.
func runPolicy(t *testing.T, p runtime.Policy, l runtime.Limits, input string, binds map[string]value.BuiltinFunction, caps ...runtime.Capability) (value.Object, string) {
	t.Helper()
	program := parse(t, input)
	env := value.NewEnvironment()
	for name, fn := range binds {
		env.Set(name, &value.Builtin{Fn: fn})
	}
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(caps...), &out)
	in := New(root)
	s, b := newScope(root.Ctx, l), runtime.Root(l)
	s.SetPolicy(p)
	got := in.withScope(frame{s, b}, func() value.Object { return in.Eval(program, env) })
	in.releaseDepth(b, 0)
	return got, out.String()
}

// restartOn is a restart policy with a 1 ms backoff (unless set).
func restartOn(retries int, backoff time.Duration, kinds ...string) runtime.Policy {
	p := runtime.Policy{Strategy: runtime.Restart, Retries: retries, Backoff: backoff, RetryOn: map[string]bool{}}
	for _, k := range kinds {
		p.RetryOn[k] = true
	}
	return p
}

// flakyServer fails its first fails requests with 500, then answers "up".
func flakyServer(t *testing.T, fails int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= fails {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "up")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// check is an Arv function that turns a status other than 200 into a user
// error.
const check = `let check = fn(url) { let r = http_get(url); if (r["status"] == 200) { r["body"] } else { error("not ok") } };`

// Acceptance: restart recovers. Two 500s, then success: 3 hits.
func TestRestartRecovers(t *testing.T) {
	srv, hits := flakyServer(t, 2)
	got, _ := runPolicy(t, restartOn(3, time.Millisecond, "effect", "user"), runtime.Limits{},
		check+fmt.Sprintf(`await spawn check(%q) with [net("127.0.0.1")]`, srv.URL), nil, grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "up" || hits.Load() != 3 {
		t.Fatalf("got %s after %d hits, want up after 3", got.Inspect(), hits.Load())
	}
}

// Acceptance: no retry on capability. The task runs once.
func TestRestartSkipsCapabilityErrors(t *testing.T) {
	got, out := runPolicy(t, restartOn(3, time.Millisecond, "effect", "user"), runtime.Limits{},
		`let f = fn() { puts("attempt"); getenv("ARV_RESTART_X") }; await spawn f()`, nil)
	wantKind(t, got, runtime.KindCapability)
	if out != "attempt\n" {
		t.Fatalf("output %q, want one attempt", out)
	}
}

// Acceptance: backoff. 100 ms, two failures, then success: at least 300 ms.
func TestRestartBackoff(t *testing.T) {
	srv, _ := flakyServer(t, 2)
	start := time.Now()
	got, _ := runPolicy(t, restartOn(3, 100*time.Millisecond, "user"), runtime.Limits{},
		check+fmt.Sprintf(`await spawn check(%q) with [net("127.0.0.1")]`, srv.URL), nil, grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "up" {
		t.Fatalf("got %s", got.Inspect())
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("took %s, want at least 300ms (100 + 200)", elapsed)
	}
}

// Out of retries, the last attempt's error fails the scope, named after
// that attempt (root/0, root/0#2, ...); by default only effect errors
// restart.
func TestRestartGivesUpWithTheLastError(t *testing.T) {
	src := `let f = fn() { puts("attempt"); error("down") }; await spawn f()`
	got, out := runPolicy(t, restartOn(3, time.Millisecond, "user"), runtime.Limits{}, src, nil)
	if e := wantKind(t, got, runtime.KindUser); e.Message != "down" || e.Task != "root/0#4" {
		t.Fatalf("got %q in %s, want down in root/0#4", e.Message, e.Task)
	}
	if out != strings.Repeat("attempt\n", 4) {
		t.Fatalf("output %q, want 4 attempts", out)
	}
	if _, out = runPolicy(t, runtime.RestartPolicy(), runtime.Limits{}, src, nil); out != "attempt\n" {
		t.Fatalf("the default policy restarted a user error: %q", out)
	}
}

// Each attempt runs against the environment frozen at the spawn (I8), not
// the parent's later rebinding.
func TestRestartSeesTheSpawnTimeEnvironment(t *testing.T) {
	got, out := runPolicy(t, restartOn(2, time.Millisecond, "user"), runtime.Limits{}, `let x = 1;
		let f = fn() { wait(); puts(x); error("again") };
		let t = spawn f(); let x = 2; open(); await t`, gate())
	wantKind(t, got, runtime.KindUser)
	if out != "1\n1\n1\n" {
		t.Fatalf("output %q, want x = 1 in all 3 attempts", out)
	}
}

// Restarting never refunds a budget: attempts share the scope's steps and
// bytes, so a task that keeps failing runs out of them before its retries.
func TestRestartNeverResetsBudgets(t *testing.T) {
	for name, tt := range map[string]struct {
		l   runtime.Limits
		src string
	}{
		"steps": {runtime.Limits{Steps: 4000}, `let burn = fn(n) { if (n == 0) { 0 } else { burn(n - 1) } };
			let f = fn() { puts("attempt"); burn(100); error("again") }; await spawn f()`},
		"alloc": {runtime.Limits{AllocBytes: 1 << 20}, `let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } };
			let f = fn() { puts("attempt"); d("aaaaaaaaaaaaaaaa", 13); error("again") }; await spawn f()`},
	} {
		got, out := runPolicy(t, restartOn(20, time.Millisecond, "user"), tt.l, tt.src, nil)
		wantKind(t, got, runtime.KindBudget)
		if n := strings.Count(out, "attempt"); n < 2 || n > 10 {
			t.Errorf("%s: %d attempts, want a few, stopped by the budget", name, n)
		}
	}
}

// A timeout ends the restarts, backoff included.
func TestRestartWithinTimeout(t *testing.T) {
	start := time.Now()
	got, _ := runPolicy(t, restartOn(1000, 50*time.Millisecond, "user"), runtime.Limits{Timeout: 200 * time.Millisecond},
		`let f = fn() { error("again") }; await spawn f()`, nil)
	if e := wantKind(t, got, runtime.KindBudget); !strings.Contains(e.Message, "timeout") {
		t.Fatalf("got %q, want the timeout", e.Message)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("took %s", elapsed)
	}
}

// While one task restarts, its siblings carry on; an error that is not
// restarted fails the scope fast and cancels them (I5).
func TestRestartSiblings(t *testing.T) {
	var calls atomic.Int32
	flaky := func(*runtime.Task, ...value.Object) value.Object {
		if calls.Add(1) <= 2 {
			return &value.Error{Message: "flaky", Kind: runtime.KindEffect}
		}
		return &value.String{Value: "ok"}
	}
	got, _ := runPolicy(t, restartOn(3, time.Millisecond, "effect"), runtime.Limits{},
		`let n = fn(x) { x }; let a = spawn n(1); let b = spawn flaky(); let c = spawn n(3); [await a, await b, await c]`,
		map[string]value.BuiltinFunction{"flaky": flaky})
	if got.Inspect() != "[1, ok, 3]" || calls.Load() != 3 {
		t.Fatalf("got %s after %d calls, want [1, ok, 3] after 3", got.Inspect(), calls.Load())
	}
	start := time.Now()
	got, _ = runPolicy(t, restartOn(3, time.Millisecond, "effect", "user"), runtime.Limits{},
		`let slow = fn() { sleep(10000) }; let f = fn() { getenv("ARV_RESTART_X") }; spawn slow(); spawn f(); sleep(10000)`, nil)
	wantKind(t, got, runtime.KindCapability)
	if time.Since(start) > 5*time.Second {
		t.Fatal("a capability error did not cancel the restart scope")
	}
}

// Restarts leave no goroutine behind.
func TestRestartLeaksNothing(t *testing.T) {
	before := goruntime.NumGoroutine()
	got, _ := runPolicy(t, restartOn(3, time.Millisecond, "user"), runtime.Limits{},
		`let f = fn() { error("x") }; for n in [1, 2, 3, 4] { spawn f() }; 1`, nil)
	wantKind(t, got, runtime.KindUser)
	settled(t, before)
}
```

Create `arv/runtime/attempt_test.go`:

```go
package runtime

import (
	"context"
	"testing"
)

// A rerun is a fresh task in the same place: same authority, parent and
// horizon, its own ID and child numbering.
func TestAttempt(t *testing.T) {
	root := NewRoot(context.Background(), NewCapSet(), nil)
	before := Now()
	caps := NewCapSet(Capability{Kind: Env, Scope: "HOME"})
	child := root.Child(context.Background(), caps)
	if child.Attempt(1) != child {
		t.Fatal("the first attempt is not the task itself")
	}
	child.Child(context.Background(), NewCapSet()) // attempt 1 spawned root/0/0
	a := child.Attempt(2)
	if a.ID != "root/0#2" || !a.Caps.Allows(Capability{Kind: Env, Scope: "HOME"}) || !root.IsAncestorOf(a) {
		t.Fatalf("attempt 2 = %s holding %v", a.ID, a.Caps.List())
	}
	if a.Horizon(before) != child.Horizon(before) {
		t.Fatal("attempt 2 sees a later horizon than the task it reruns")
	}
	if g := a.Child(context.Background(), NewCapSet()); g.ID != "root/0#2/0" {
		t.Fatalf("attempt 2's first child is %s", g.ID)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ ./arv/evaluator/ -run 'TestAttempt|TestRestart'`

Expected: FAIL. The build fails: (the first lines; ports and times vary)

```text
arv/runtime/attempt_test.go:15:11: child.Attempt undefined (type *Task has no field or method Attempt)
arv/runtime/attempt_test.go:19:13: child.Attempt undefined (type *Task has no field or method Attempt)
```

- [ ] **Step 3: Implement**

In `arv/evaluator/concurrency.go`, replace:

```go
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	scope.Go(func() error {
		// Both run even if runTask panics: no slot or live task leaks, and
		// no awaiter is left waiting. Release runs first: an awaiter that
		// spawns again as soon as it wakes must not find this finished
		// task still counted against the live-task cap.
		var result value.Object = newError("task %s did not finish", child.ID)
		defer func() { handle.Finish(result) }()
		defer top.budget.Release()
		result = startTask(child, top.budget.Task(), fn, args)
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" || e.Task == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node, child.ID)
				result = e
			}
```

with:

```go
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	// The scope's policy decides what a failure does (runtime.Supervise).
	// Under restart, each attempt is a fresh task and interpreter (IDs
	// root/0, root/0#2, ...) running fn(args) again, against the
	// environments as they were at this spawn (I8), in the same slot and
	// the same budgets: nothing is refunded between attempts.
	var result value.Object
	scope.Supervise(child.ID, func(n int) error {
		t := child.Attempt(n)
		result = newError("task %s did not finish", t.ID) // if runTask panics
		result = startTask(t, top.budget.Task(), fn, args)
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" || e.Task == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node, t.ID)
				result = e
			}
```

In `arv/evaluator/concurrency.go`, replace:

```go
		}
		return nil
	})
	return handle
```

with:

```go
		}
		return nil
	}, func() {
		// Runs even if runTask panics: no slot or live task leaks, and no
		// awaiter is left waiting. Release comes first: an awaiter that
		// spawns again as soon as it wakes must not find this finished
		// task still counted against the live-task cap.
		top.budget.Release()
		handle.Finish(result)
	})
	return handle
```

In `arv/runtime/task.go`, replace:

```go
	horizon := append(t.horizon[:len(t.horizon):len(t.horizon)], clock.Add(1))
	return &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out, parent: t, horizon: horizon}
}
```

with:

```go
	horizon := append(t.horizon[:len(t.horizon):len(t.horizon)], clock.Add(1))
	return &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out, parent: t, horizon: horizon}
}

// Attempt is the task that makes t's nth attempt under a restart
// supervisor (t itself for the first): a fresh task with t's authority,
// context, output and parent, named t.ID#n, whose children are numbered
// from 0 again. It keeps t's horizon, so it sees the environments t was
// spawned with, not what its parent has bound since (I8).
func (t *Task) Attempt(n int) *Task {
	if n <= 1 {
		return t
	}
	return &Task{ID: fmt.Sprintf("%s#%d", t.ID, n), Caps: t.Caps, Ctx: t.Ctx, Out: t.Out, parent: t.parent, horizon: t.horizon}
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ ./arv/evaluator/ -run 'TestAttempt|TestRestart'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/concurrency.go arv/evaluator/restart_test.go arv/runtime/attempt_test.go arv/runtime/task.go && git commit -F - <<'MSG'
feat(evaluator): restart spawned tasks with attempt IDs

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
