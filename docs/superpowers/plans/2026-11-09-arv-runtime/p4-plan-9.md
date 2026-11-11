# Phase 4, task p4.9: I8 in the evaluator

> **For agentic workers:** this file is one task, p4.9. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.8 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.9: I8 in the evaluator

Identifiers are read with `env.GetAs(name, in.Task)`, so a child never sees a name its parent binds or rebinds after the `spawn`, and neither does anything the child spawns.

`spawn` also charges the child's horizon to `alloc_mb`: 8 bytes per generation (`horizonEntrySize`), which grows with spawn depth.

**Changed expectations** (also recorded in `16j-phase4-notes.md`, p4.14): three await cases relied on a child seeing a handle bound after it was spawned.
- "self through a captured binding" and "grandchild awaiting its parent" now fail with `identifier not found`.
- "siblings awaiting each other" raced: each sibling could see the other. It becomes "sibling through a captured binding", where only `fb` awaits `a`, with an exact message.
- The same three cases change in `e2e/robustness_test.go` and `robust_await_mutual.arv`, and the alloc table's spawn row becomes `2*(160+8)`.

**Files:**
- Modify: `arv/evaluator/alloc.go`
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/eval_functions.go`
- Modify: `arv/evaluator/evaluator.go`
- Modify (test): `arv/evaluator/alloc_test.go`
- Modify (test): `arv/evaluator/await_test.go`
- Create (test): `arv/evaluator/freeze_test.go`
- Modify (test): `e2e/robustness_test.go`
- Modify (test): `e2e/testdata/robust_await_mutual.arv`

- [ ] **Step 1: Write the failing tests**

In `arv/evaluator/alloc_test.go`, replace:

```go
		{`"a" + "b" + "c"`, 5},                     // "ab", then "abc"
		{`for x in [1, 2] { x }`, 64 + 2*(320+48)}, // arrays, an env and binding per iteration
		{`let f = fn() { 1 }; await [spawn f(), spawn f()]`, 64 + 48 + 128 + 2*160 + 2*320}, // + binding, closure, handles, call envs
		{`push([1], 2)`, 48},
		{`let a = [[1, 2, 3, 4]]; last(a)`, 80 + 48}, // last shares, so costs nothing
```

with:

```go
		{`"a" + "b" + "c"`, 5},                     // "ab", then "abc"
		{`for x in [1, 2] { x }`, 64 + 2*(320+48)}, // arrays, an env and binding per iteration
		{`let f = fn() { 1 }; await [spawn f(), spawn f()]`, 64 + 48 + 128 + 2*(160+8) + 2*320}, // + binding, closure, handles and horizons, call envs
		{`push([1], 2)`, 48},
		{`let a = [[1, 2, 3, 4]]; last(a)`, 80 + 48}, // last shares, so costs nothing
```

In `arv/evaluator/await_test.go`, replace:

```go
// await it. Anything else (itself, a sibling, an ancestor) can form a
// cycle of tasks waiting on each other that no scope would ever end.
func TestAwaitOnlyBySpawner(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{"self through a captured binding",
			`let f = fn() { wait(); await h }; let h = spawn f(); open()`,
			"await: task root/0 cannot await itself"},
		{"siblings awaiting each other",
			`let fa = fn() { wait(); await b }; let fb = fn() { wait(); await a };
			 let a = spawn fa(); let b = spawn fb(); open()`,
			"or its descendants"},
		{"grandchild awaiting its parent",
			`let g = fn() { wait(); await p }; let parent = fn() { spawn g(); 1 };
			 let p = spawn parent(); open()`,
			"await: task root/0 was not spawned by task root/0/0 or its descendants"},
		{"handle passed down to a child",
			`let f = fn(h) { await h }; let s = fn() { wait(); 1 };
```

with:

```go
// await it. Anything else (itself, a sibling, an ancestor) can form a
// cycle of tasks waiting on each other that no scope would ever end.
//
// Since Phase 4 freezes environments at spawn (I8), a task can no longer
// reach its own handle or its parent's: both are bound after it was
// spawned. Those cases now fail as unknown names, which pins I8 too;
// checkAwait still guards siblings.
func TestAwaitOnlyBySpawner(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{"self through a captured binding",
			`let f = fn() { wait(); await h }; let h = spawn f(); open()`,
			"identifier not found: h"},
		{"sibling through a captured binding",
			`let fa = fn() { wait(); 1 }; let fb = fn() { await a };
			 let a = spawn fa(); let b = spawn fb(); open()`,
			"await: task root/0 was not spawned by task root/1 or its descendants"},
		{"grandchild awaiting its parent",
			`let g = fn() { wait(); await p }; let parent = fn() { spawn g(); 1 };
			 let p = spawn parent(); open()`,
			"identifier not found: p"},
		{"handle passed down to a child",
			`let f = fn(h) { await h }; let s = fn() { wait(); 1 };
```

Create `arv/evaluator/freeze_test.go`:

```go
package evaluator

import (
	goruntime "runtime"
	"testing"
	"time"
)

// I8: a spawned task never sees a name its parent binds after the spawn,
// however it reaches the environment: directly, through a closure defined
// earlier, through a closure passed as an argument, or from a task it
// spawns later. The gate (wait/open) makes the child read only after the
// parent has rebound.
func TestChildNeverSeesALaterRebinding(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{"directly",
			`let x = 1; let f = fn() { wait(); x };
			 let t = spawn f(); let x = 2; open(); [await t, x]`, "[1, 2]"},
		{"through a closure defined before",
			`let x = 1; let g = fn() { x }; let f = fn() { wait(); g() };
			 let t = spawn f(); let x = 2; open(); await t`, "1"},
		{"through a closure in an argument",
			`let x = 1; let fs = [fn() { x }]; let f = fn(a) { wait(); a[0]() };
			 let t = spawn f(fs); let x = 2; open(); await t`, "1"},
		{"from a grandchild spawned after the rebinding",
			`let x = 1; let inner = fn() { x }; let f = fn() { wait(); await spawn inner() };
			 let t = spawn f(); let x = 2; open(); await t`, "1"},
		{"the child's own environment, frozen for the grandchild",
			`let f = fn() { let y = 1; let h = fn() { wait(); y }; let t = spawn h(); let y = 2; open(); [y, await t] };
			 await spawn f()`, "[2, 1]"},
		{"the child rebinds its own names",
			`let f = fn() { let y = 1; let y = y + 1; y }; await spawn f()`, "2"},
		{"recursion bound before the spawn",
			`let fib = fn(n) { if (n < 2) { n } else { fib(n - 1) + fib(n - 2) } }; await spawn fib(10)`, "55"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := runBounded(t, 10*time.Second, tt.src, gate()); got.Inspect() != tt.want {
				t.Fatalf("got %s, want %s", got.Inspect(), tt.want)
			}
		})
	}
}

// A name bound only after the spawn does not exist for the child.
func TestChildCannotSeeANameBoundAfterSpawn(t *testing.T) {
	got := runBounded(t, 10*time.Second,
		`let f = fn() { wait(); later }; let t = spawn f(); let later = 1; open(); await t`, gate())
	wantErrorHas(t, got, "identifier not found: later")
}

// Freezing keeps no goroutine and needs no lock held across tasks: a
// parent rebinding while many children read finishes and leaks nothing.
func TestFreezeUnderConcurrentRebinding(t *testing.T) {
	before := goruntime.NumGoroutine()
	got := runBounded(t, 30*time.Second, `
		let x = 0;
		let f = fn(n) { wait(); x + n };
		let ts = for n in [1, 2, 3, 4, 5, 6, 7, 8] { spawn f(n) };
		let x = 100; let x = 200;
		open();
		await ts`, gate())
	if got.Inspect() != "[1, 2, 3, 4, 5, 6, 7, 8]" {
		t.Fatalf("got %s", got.Inspect())
	}
	settled(t, before)
}
```

In `e2e/robustness_test.go`, replace:

```go
			code: 1, stderrHas: []string{msg}, stderrNot: goCrash}
	}
	runAll(t, []spec{
		await("self", "robust_await_self.arv", "error: await: task root/0 cannot await itself"),
		await("siblings", "robust_await_mutual.arv", "error: await: task root/"),
		await("child awaits parent", "robust_await_parent.arv", "error: await: task root/0 was not spawned by task root/0/0 or its descendants"),
	})
}
```

with:

```go
			code: 1, stderrHas: []string{msg}, stderrNot: goCrash}
	}
	// Since Phase 4 freezes environments at spawn (I8), a task cannot see
	// its own handle or its parent's: both are bound after its spawn.
	runAll(t, []spec{
		await("self", "robust_await_self.arv", "error: identifier not found: h"),
		await("siblings", "robust_await_mutual.arv", "error: await: task root/0 was not spawned by task root/1 or its descendants"),
		await("child awaits parent", "robust_await_parent.arv", "error: identifier not found: p"),
	})
}
```

In `e2e/testdata/robust_await_mutual.arv`, replace:

```arv
// Two siblings await each other.
let fa = fn() { http_get("{{SERVER}}/wait?id={{ID}}&k=bound&n=1"); await b };
let fb = fn() { http_get("{{SERVER}}/wait?id={{ID}}&k=bound&n=1"); await a };
let a = spawn fa() with [net("127.0.0.1")];
let b = spawn fb() with [net("127.0.0.1")];
```

with:

```arv
// fb awaits its sibling a. (fa could not await b: b is bound after fa was
// spawned, and a task never sees that (I8).)
let fa = fn() { http_get("{{SERVER}}/wait?id={{ID}}&k=bound&n=1"); 1 };
let fb = fn() { await a };
let a = spawn fa() with [net("127.0.0.1")];
let b = spawn fb() with [net("127.0.0.1")];
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race -count=1 ./arv/evaluator/ ./e2e/ -run 'TestChildNeverSeesALaterRebinding|TestChildCannotSeeANameBoundAfterSpawn|TestFreezeUnderConcurrentRebinding|TestAwaitOnlyBySpawner|TestAllocSizes|TestAwaitCycles'`

Expected: FAIL. The tests fail: (the first lines; ports and times vary)

```text
--- FAIL: TestAllocSizes (0.00s)
    alloc_test.go:61: let f = fn() { 1 }; await [spawn f(), spawn f()] under 1215 bytes: got [1, 1], want alloc exceeded
--- FAIL: TestAwaitOnlyBySpawner (0.00s)
        await_test.go:79: got ERROR ERROR: await: task root/0 cannot await itself, want an error mentioning "identifier not found: h"
--- FAIL: TestChildNeverSeesALaterRebinding (0.00s)
        freeze_test.go:38: got [2, 2], want [1, 2]
--- FAIL: TestChildCannotSeeANameBoundAfterSpawn (0.00s)
    freeze_test.go:48: got INTEGER 1, want an error mentioning "identifier not found: later"
```

- [ ] **Step 3: Implement**

In `arv/evaluator/alloc.go`, replace:

```go
// only by a charged array element or binding, at least twice its size.
const (
	arrayElemSize  = 16  // one interface value
	hashPairSize   = 48  // key and value interfaces plus map overhead
	envSize        = 320 // a new environment, its map and first slot group
	bindingSize    = 48  // one name bound in an environment
	functionSize   = 128 // a closure
	handleSize     = 160 // a task handle and its channel
	capabilitySize = 32  // a capability value: its kind and scope headers
)
```

with:

```go
// only by a charged array element or binding, at least twice its size.
const (
	arrayElemSize    = 16  // one interface value
	hashPairSize     = 48  // key and value interfaces plus map overhead
	envSize          = 320 // a new environment, its map and first slot group
	bindingSize      = 48  // one name bound in an environment
	functionSize     = 128 // a closure
	handleSize       = 160 // a task handle and its channel
	capabilitySize   = 32  // a capability value: its kind and scope headers
	horizonEntrySize = 8   // one spawn time in a task's horizon (runtime.Task.Horizon)
)
```

In `arv/evaluator/concurrency.go`, replace:

```go
	}

	if e := in.charge(handleSize); e != nil {
		return e
	}
```

with:

```go
	}

	// The handle, and the child's horizon: one spawn time per generation.
	if e := in.charge(handleSize + horizonEntrySize*int64(in.Task.HorizonLen()+1)); e != nil {
		return e
	}
```

In `arv/evaluator/eval_functions.go`, replace:

```go
import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)
```

with:

```go
import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)
```

In `arv/evaluator/eval_functions.go`, replace:

```go
}

// evalIdentifier evaluates an identifier value from the value system
// this functions compares the identifier and returns the value of the identifier
// it takes as input an identifier and an environment
func evalIdentifier(
	node *ast.Identifier, env *value.Environment,
) value.Object {
	if val, ok := env.Get(node.Value); ok {
		return val
	}
```

with:

```go
}

// evalIdentifier evaluates an identifier as task t sees env: a spawned
// task never sees a name its parent bound after the spawn (I8, see
// value.Environment.GetAs), then falls back to the builtins.
func evalIdentifier(
	node *ast.Identifier, env *value.Environment, t *runtime.Task,
) value.Object {
	if val, ok := env.GetAs(node.Value, t); ok {
		return val
	}
```

In `arv/evaluator/evaluator.go`, replace:

```go
		env.Set(node.Name.Value, val)
	case *ast.Identifier:
		return evalIdentifier(node, env)
	case *ast.FunctionLiteral:
		return in.alloc(&value.Function{Parameters: node.Parameters, Env: env, Body: node.Body}, functionSize)
```

with:

```go
		env.Set(node.Name.Value, val)
	case *ast.Identifier:
		return evalIdentifier(node, env, in.Task)
	case *ast.FunctionLiteral:
		return in.alloc(&value.Function{Parameters: node.Parameters, Env: env, Body: node.Body}, functionSize)
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race -count=1 ./arv/evaluator/ ./e2e/ -run 'TestChildNeverSeesALaterRebinding|TestChildCannotSeeANameBoundAfterSpawn|TestFreezeUnderConcurrentRebinding|TestAwaitOnlyBySpawner|TestAllocSizes|TestAwaitCycles'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/alloc.go arv/evaluator/alloc_test.go arv/evaluator/await_test.go arv/evaluator/concurrency.go arv/evaluator/eval_functions.go arv/evaluator/evaluator.go arv/evaluator/freeze_test.go e2e/robustness_test.go e2e/testdata/robust_await_mutual.arv && git commit -F - <<'MSG'
feat(evaluator): a task reads its environments as of its spawn (I8)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
