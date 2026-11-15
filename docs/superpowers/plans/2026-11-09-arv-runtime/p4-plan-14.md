# Phase 4, task p4.14: README and Phase 4 notes

> **For agentic workers:** this file is one task, p4.14. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.13 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.14: README and Phase 4 notes

Documents what Phase 4 built:
- **README:** a feature bullet; a new "Errors and supervision" section with the kind table, a `match` example and the `supervise` table; and the restart idempotency warning, next to the option, as the design asks. `error`, `==` and `!=` are added to the Language paragraph.
- **`16j-phase4-notes.md`:** what was added, the changed test expectations with their reasons, and the known limits.
- **`16-implementation-notes.md`:** the Phase 4 follow-through line points to it.

No code changes, so there is no failing test; run the full checks before committing.

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`
- Create: `docs/superpowers/plans/2026-11-09-arv-runtime/16j-phase4-notes.md`

- [ ] **Step 1: Write the docs**

In `README.md`, replace:

````markdown
- **Structured concurrency.** `scope { ... }` doesn't finish until every task started in it has finished. The first failure cancels the rest.
- **Budgets.** `scope (timeout_ms: 5000, tasks: 20, steps: 1000000, alloc_mb: 64) { ... }` caps time, concurrent tasks, evaluation steps and allocation. A nested scope can only shrink what encloses it.
- **Planned:** token and dollar budgets, supervision strategies, durable replay, and LLM agents whose tools are capabilities. See `docs/superpowers/plans/2026-11-09-arv-runtime/`.

```arv
````

with:

````markdown
- **Structured concurrency.** `scope { ... }` doesn't finish until every task started in it has finished. The first failure cancels the rest.
- **Budgets.** `scope (timeout_ms: 5000, tasks: 20, steps: 1000000, alloc_mb: 64) { ... }` caps time, concurrent tasks, evaluation steps and allocation. A nested scope can only shrink what encloses it.
- **Supervision and errors as values.** `scope (supervise: "collect") { ... }` keeps going when a task fails, and `scope (supervise: "restart", retries: 3) { ... }` runs a failed task again. `match` looks at an error's kind instead of crashing.
- **Planned:** token and dollar budgets, durable replay, and LLM agents whose tools are capabilities. See `docs/superpowers/plans/2026-11-09-arv-runtime/`.

```arv
````

In `README.md`, replace:

```markdown
Option values are positive integers; an unknown or repeated option is an error. Limits nest: a scope inside `scope (steps: 1000)` can never spend more than what is left of those 1000, whatever it asks for.

## Development
```

with:

````markdown
Option values are positive integers; an unknown or repeated option is an error. Limits nest: a scope inside `scope (steps: 1000)` can never spend more than what is left of those 1000, whatever it asks for.

## Errors and supervision

Every error has a kind:

| Kind | Comes from |
| --- | --- |
| `capability` | a missing grant, in an effect or at `spawn ... with` |
| `budget` | a scope option or `--steps`, `--timeout`, ... running out |
| `cancelled` | the task's scope was cancelled |
| `effect` | the world: connection refused, file not found |
| `runtime` | the program: a type error, bad arguments, division by zero |
| `user` | `error("message")` |

`match` runs one arm or the other; both are required:

```arv
match await t {
  ok(v) => v,
  err(e) => if (e["kind"] == "capability") { "denied" } else { e["message"] }
}
```

`e` is a map with `kind`, `message`, `task` (the task it arose in, such as `root/0`) and `pos` (`line:col`).

`supervise` decides what a task's failure does to its scope:

| `supervise:` | A failed task |
| --- | --- |
| `"fail_fast"` (default) | cancels the scope, its other tasks and its body; the scope fails with it. `match` can't rescue it. |
| `"collect"` | cancels nothing; `await` returns its error, to `match`. A failure nobody awaits fails the scope when it ends: `N unawaited task failures: ...`. |
| `"restart"` | runs again, as task `root/0#2`, `root/0#3`, ..., up to `retries` times (default 3), waiting `backoff_ms` (default 100) doubled each time, at most 30 s. Only the kinds in `retry_on` restart: `["effect"]` by default, `["effect", "user"]` at most. Then it fails like `fail_fast`. |

**Restart is for idempotent work:** every attempt runs the task's effects again, so a task that writes a file writes it again. Attempts share the scope's budgets; restarting refunds nothing. A spawned task sees the variables it captured as they were at `spawn`, in every attempt: a later `let` in the parent never reaches it.

## Development
````

In `README.md`, replace:

```markdown
## Language

Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. `for x in xs { ... }` evaluates to the array of its body's values, so `await for url in urls { spawn check(url) }` starts one task per URL and collects their results; `await [t1, t2]` awaits each task in order. Builtins include `len`, `push`, `last`, `puts`, `http_get`, `read_file`, `write_file`, `getenv` and `sleep`.

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
```

with:

```markdown
## Language

Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. `for x in xs { ... }` evaluates to the array of its body's values, so `await for url in urls { spawn check(url) }` starts one task per URL and collects their results; `await [t1, t2]` awaits each task in order. Strings compare with `==` and `!=`. Builtins include `len`, `push`, `last`, `puts`, `error`, `http_get`, `read_file`, `write_file`, `getenv` and `sleep`.

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
```

In `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`, replace:

```markdown
  - Check `timeout_ms` for overflow, as `sleep` now does.
  - Keep per-scope state in a frame in `in.scopes`. Don't mutate exported `Task` fields.
- **Phase 4 (supervision):**
  - `Scope.Fail` stores a Go error, so the error `Kind` is lost. Carry the `*value.Error` instead.
  - The await-ownership rule constrains supervisors. `collect` can rely on owners to know which handles were awaited.
```

with:

```markdown
  - Check `timeout_ms` for overflow, as `sleep` now does.
  - Keep per-scope state in a frame in `in.scopes`. Don't mutate exported `Task` fields.
- **Phase 4 (supervision):** all three done in Phase 4; see [`16j-phase4-notes.md`](16j-phase4-notes.md).
  - `Scope.Fail` stores a Go error, so the error `Kind` is lost. Carry the `*value.Error` instead.
  - The await-ownership rule constrains supervisors. `collect` can rely on owners to know which handles were awaited.
```

Create `docs/superpowers/plans/2026-11-09-arv-runtime/16j-phase4-notes.md`:

```markdown
# Phase 4: supervision (as built)

Built from `p4-plan-1.md` to `p4-plan-14.md`. Their deviations from `11-phase4-supervision.md` are listed in `p4-plan-1.md`. Review rounds, e2e cases and the mutation table for this phase go below, or in a continuation file once this one nears 18 KB.

**What Phase 4 adds:**
- Every error has a kind (`capability`, `budget`, `cancelled`, `effect`, `runtime`, `user`), set where it is made, and keeps it, with its position and the ID of the task it arose in, across task boundaries (`failure` carries the `*value.Error` through `runtime.Scope`). `arv.Error.Kind` gives it to the host.
- `error(msg)`; string `==` and `!=`; `match <subject> { ok(v) => ..., err(e) => ... }`.
- `scope (supervise: "fail_fast" | "collect" | "restart", retries, backoff_ms, retry_on)`, run by `runtime.Scope.Supervise` under a `runtime.Policy`.
- I8, freeze at spawn, transitively: `value.Environment` keeps old bindings, and a task reads every environment as of its horizon (`runtime.Task.Horizon`).

## Changed expectations (each with its reason)

| Test | Before | After | Why |
|---|---|---|---|
| unit `TestAwaitOnlyBySpawner` "self through a captured binding" | `await: task root/0 cannot await itself` | `identifier not found: h` | I8: `h` is bound after the spawn, so the child cannot see it. |
| unit `TestAwaitOnlyBySpawner` "siblings awaiting each other" | either sibling's error, matched by `or its descendants` | renamed "sibling through a captured binding": only `fb` awaits `a`, and the exact message is expected | I8 turned `fa`'s `await b` into an unknown name; with both racing, the expectation was no longer deterministic. |
| unit `TestAwaitOnlyBySpawner` "grandchild awaiting its parent" | `... was not spawned by task root/0/0 ...` | `identifier not found: p` | I8, as above. |
| e2e `TestAwaitCycles` self, siblings, child awaits parent; `robust_await_mutual.arv` rewritten (only `fb` awaits) | the `await:` errors | `identifier not found: h`, `... root/0 was not spawned by task root/1 ...`, `identifier not found: p` | The same three cases through the binary. |
| unit `TestStringOperatorOtherThanPlusCostsNothing` | `s == s` is `unknown operator` | `s == s` is `true`, `s != s` is `false`, both still free; `s - s` is still unknown | Strings compare now, so `match` can test `e["kind"]`. |
| unit `TestAllocSizes`, the spawn row | `2*160` for the handles | `2*(160+8)` | The child's horizon (8 bytes per generation) is charged at spawn. |

`checkAwait`'s self and ancestor cases can no longer be reached from Arv (I8); the check stays as a guard.

## Known limits

- **`match` cannot rescue a task's failure under `fail_fast`.** The scope is cancelled before the handle finishes, and a cancelled task's `match` passes errors through. Use `collect`.
- **A budget error cannot be swallowed inside the budget that ran out.** The err arm's hash and environment are charged, and a spent pool stays spent.
- **`collect` reports unawaited failures only when the scope would otherwise succeed.** If the body fails, the scope fails with the body's error; the unawaited failures are not listed, but the failure is not silent.
- **Old bindings are kept.** A name rebound after a spawn keeps its old value alive as long as its environment lives (a 32-byte binding, charged 48 like every binding). A fresh binding measured 102 bytes including map growth, against 104 before Phase 4.
- **The clock is process-wide.** Runs in one process share `runtime.clock`; only its order matters.
- **Per-attempt overhead.** Each restart attempt allocates a `Task`, an `Interpreter` and the task body's scope, uncharged apart from the call's environment; attempts are bounded by `retries` (at most 1000). A collect scope keeps one handle pointer per spawn until it closes.
- **`retry_on` takes only `effect` and `user`.** The other kinds fail the same way again or must stop the task.
- **Restart re-runs effects.** It is for idempotent work; Phase 6's log will not make `write_file` idempotent.
```

- [ ] **Step 2: Run the full checks**

Run: `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 3: Commit**

```bash
cd <worktree> && git add README.md docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md docs/superpowers/plans/2026-11-09-arv-runtime/16j-phase4-notes.md && git commit -F - <<'MSG'
docs: Phase 4 errors and supervision in the README and notes

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
