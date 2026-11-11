# Phase 4, task p4.1: Error kinds in the runtime

> **For agentic workers:** this file is one task, p4.1. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your tree is `feat/phases-3-8` with Phase 3 merged and no Phase 4 work yet. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

## Phase 4 overview

**Goal:** each scope chooses what a task's failure means: `fail_fast` (the default, unchanged), `collect` or `restart`. `match` lets Arv code look at an error's kind instead of crashing. Every error has a kind, and keeps its kind, position and task across task boundaries. A task sees its environments as they were at `spawn` (I8).

**Guarantees, and the tasks that test them:**
- `fail_fast` cancels siblings and propagates the first error with its kind (p4.2, p4.10, p4.13).
- `collect` never drops an error silently and returns every child's outcome (p4.13).
- `restart` re-runs at most `retries` times, against the environment frozen at spawn, and never resets or escapes a budget (p4.10, p4.11, p4.12).
- `match` dispatches on error kinds (p4.7, p4.13).
- Errors keep kind, position and task across task boundaries (p4.2, p4.3).
- I8 holds, transitively (p4.8, p4.9).
- No goroutine outlives `arv.Run`: settle-loop leak tests for restart and collect (p4.11, p4.13).

**Invariants touched:** I5 (fail fast), I6 (budgets only shrink), I8 (no shared mutable state: freeze at spawn).

| Task | File | What |
|---|---|---|
| p4.1 | `p4-plan-1.md` | `runtime.Kind*`, `Kinded`, `ErrorKind` |
| p4.2 | `p4-plan-2.md` | `value.Error.Kind`; `newErrorKind`, `errorOf`; budget and cancelled kinds |
| p4.3 | `p4-plan-3.md` | `value.Error.Task`; `arv.Error.Kind` |
| p4.4 | `p4-plan-4.md` | kinds in `http_get`, `read_file`, `write_file`, `getenv`, `sleep`, `puts`, `push` |
| p4.5 | `p4-plan-5.md` | `error` builtin (`user`); string `==`, `!=` |
| p4.6 | `p4-plan-6.md` | `MATCH`, `=>`, `ast.MatchExpression`, missing-arm parse errors |
| p4.7 | `p4-plan-7.md` | `evalMatch`; the error hash |
| p4.8 | `p4-plan-8.md` | spawn clock, `Task.Horizon`, versioned `Environment` |
| p4.9 | `p4-plan-9.md` | identifiers read as of the task's horizon; changed await-cycle expectations |
| p4.10 | `p4-plan-10.md` | `Strategy`, `Policy`, the retry loop and backoff |
| p4.11 | `p4-plan-11.md` | `Task.Attempt`; `spawn` under `Supervise`; Go-level acceptance tests |
| p4.12 | `p4-plan-12.md` | option parsing and validation; `evalScope` sets the policy |
| p4.13 | `p4-plan-13.md` | unawaited-failure rule; escalation; `fail_fast` and `match` |
| p4.14 | `p4-plan-14.md` | README section; `16j-phase4-notes.md`; 16-notes pointer |

**16-implementation-notes follow-through:**
- `Scope.Fail` loses the kind: `failure` carries the `*value.Error` and implements `runtime.Kinded`, so the runtime reads the kind from it (p4.2, p4.10).
- Supervisors rely on the await-ownership rule: a restart attempt keeps the first attempt's parent, so its spawner still owns the handle (p4.11). `collect` uses `await` to learn which handles were looked at (p4.13).
- I8 lands here, transitively (p4.8, p4.9).

### Deviations from `11-phase4-supervision.md`, and why

1. **I8 by versioned environments, not copies.**
   - Built: a spawn clock, a per-task horizon of spawn times, and bindings that keep the value they replaced; a task reads every environment as of its horizon (p4.8).
   - Why: copying every reachable environment at each spawn costs time and memory proportional to everything in scope. This is O(1) per spawn and transitive.
   - Cost: three Phase 0–2 await-cycle expectations change, because a child no longer sees a handle bound after its spawn (p4.9). Old bindings live as long as their environment.
2. **`Scope.Supervise(id, attempt func(n int) error, done func())`, not `Scope.Go(fn func(attempt int) (kind string, err error))`.**
   - The kind comes from the error itself (`runtime.Kinded`), so a task cannot report one kind and return another error.
   - `done` runs once, after `Fail`, which makes "a failed fail_fast scope is cancelled before the handle finishes" deterministic (p4.13). `Go(fn)` stays, as a single attempt.
3. **Backoff and jitter.** The delay is `backoff_ms` doubled per attempt, plus up to 10% from an FNV hash of `id#attempt`, capped at 30 s overall. The design asks for jitter from the task ID but gives no formula.
4. **Option bounds.**
   - `retries` is 1 to 1000 and `backoff_ms` 1 to 30000, positive like the Phase 3 options.
   - `retry_on` accepts only `"effect"` and `"user"`, and defaults to `["effect"]`, following the design's retryable column.
   - `retries`, `backoff_ms` and `retry_on` without `supervise: "restart"` are errors, not silently ignored.
5. **`match` in a cancelled task does not catch.** The error passes through, so a cancelled task never goes on (I5). Under `fail_fast` a task's failure therefore cannot be rescued by `match`; use `collect` (p4.7, p4.13). The err arm's hash is charged to `alloc_mb`.
6. **Errors record their task.** The design's hash has `"task"`; it comes from a new `value.Error.Task`, set once like `Pos` (p4.3). `arv.Error` also gets `Kind`, for the host.
7. **String `==` and `!=`** (p4.5). The design's own example compares `e["kind"] == "capability"`, which was an unknown operator.
8. **The unawaited-failure rule applies when the scope would otherwise succeed.** If the body fails, the body's error wins. The message is clipped, and the error has the first failure's kind, position and task.
9. **`spawn` charges 8 bytes per generation** of the child's horizon to `alloc_mb` (p4.9).
10. **An OS error while canonicalising a `spawn ... with` grant is `effect`.**
11. **Order and split.**
    - The design's seven steps become 14 tasks, so each file stays under 18 KB.
    - Attempt suffixes (step 7) are part of the restart wiring (p4.11): a restart needs them.
    - Options (step 6) come after the restart loop and before `collect`.
    - The restart acceptance rows run at Go level in p4.11, through `runPolicy`, and through the syntax in p4.12.
    - Docs are a separate task (p4.14).

Seen while validating, not caused by Phase 4: `arv/ast`'s `TestLongInfixChainPrintsInLinearTime` (a 2 s bound) failed once at 2.69 s under heavy load, and passed when rerun.

### Task p4.1: Error kinds in the runtime

The six error kinds of the design, as constants in `arv/runtime`, and one way to read an error's kind: `ErrorKind(err, fallback)`.
- An error that knows its kind implements `Kinded` (`ErrorKind() string`). `*DeniedError` is `capability` and `*ExceededError` is `budget`.
- `ErrorKind` looks along the error chain with `errors.As`, so a wrapped `DeniedError` still reads as `capability`. A context's cancellation or deadline reads as `cancelled`. Anything else gets the caller's fallback, because only the caller knows whether a plain OS error is the world failing (`effect`) or the program being wrong (`runtime`).

Nothing uses it until p4.2. `arv/runtime` stays standard library only.

**Files:**
- Create: `arv/runtime/errkind.go`
- Create (test): `arv/runtime/errkind_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/errkind_test.go`:

```go
package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestErrorKind(t *testing.T) {
	denied := &DeniedError{Want: Capability{Kind: Env, Scope: "X"}}
	exceeded := &ExceededError{Kind: "steps", Limit: 10}
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"denied", denied, KindCapability},
		{"wrapped denied", fmt.Errorf("spawn: %w", denied), KindCapability},
		{"exceeded", exceeded, KindBudget},
		{"wrapped exceeded", fmt.Errorf("read: %w", exceeded), KindBudget},
		{"canceled", context.Canceled, KindCancelled},
		{"deadline", fmt.Errorf("get: %w", context.DeadlineExceeded), KindCancelled},
		{"plain", errors.New("connection refused"), KindEffect},
	} {
		if got := ErrorKind(tt.err, KindEffect); got != tt.want {
			t.Errorf("%s: ErrorKind = %q, want %q", tt.name, got, tt.want)
		}
	}
	if got := ErrorKind(errors.New("x"), KindRuntime); got != KindRuntime {
		t.Errorf("fallback: got %q, want %q", got, KindRuntime)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestErrorKind'`

Expected: FAIL. The build fails: (the first lines; ports and times vary)

```text
arv/runtime/errkind_test.go:18:22: undefined: KindCapability
arv/runtime/errkind_test.go:19:55: undefined: KindCapability
arv/runtime/errkind_test.go:20:26: undefined: KindBudget
arv/runtime/errkind_test.go:21:58: undefined: KindBudget
```

- [ ] **Step 3: Implement**

Create `arv/runtime/errkind.go`:

```go
package runtime

import (
	"context"
	"errors"
)

// Error kinds: what an Arv error value's "kind" says about its cause.
// match shows it to the program, and a restart supervisor retries only the
// kinds its policy names.
const (
	KindCapability = "capability" // a grant was missing (*DeniedError)
	KindBudget     = "budget"     // a budget ran out (*ExceededError)
	KindCancelled  = "cancelled"  // the task's scope was cancelled
	KindEffect     = "effect"     // an effect failed in the world: refused, not found, ...
	KindRuntime    = "runtime"    // the program is wrong: types, arguments, overflow
	KindUser       = "user"       // the program's own error(msg)
)

// Kinded is an error that knows its kind.
type Kinded interface {
	ErrorKind() string
}

func (*DeniedError) ErrorKind() string   { return KindCapability }
func (*ExceededError) ErrorKind() string { return KindBudget }

// ErrorKind is the kind of the first error in err's chain that knows its
// kind (Kinded); else KindCancelled for a context's cancellation or
// deadline; else fallback.
func ErrorKind(err error, fallback string) string {
	var k Kinded
	if errors.As(err, &k) {
		return k.ErrorKind()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return KindCancelled
	}
	return fallback
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestErrorKind'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/runtime/errkind.go arv/runtime/errkind_test.go && git commit -F - <<'MSG'
feat(runtime): error kinds and ErrorKind

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
