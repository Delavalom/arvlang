# Phase 4: Supervision and errors as values

**Status:** design and task outline. Write the bite-sized plan after Phase 3 merges.

**Goal:** Each scope chooses what a failure means: `fail_fast` (the default and today's behaviour), `collect`, or `restart`. Arv code can inspect errors with `match` instead of only crashing. A failed scope is a failure in its parent task, whose scope applies *its* strategy. Nested scopes are a supervision tree.

## Design

### Error kinds

`value.Error` gains `Kind`:

| Kind | Produced by | Retryable by `restart`? |
|---|---|---|
| `capability` | `DeniedError` (spawn or effect) | **No.** It would fail the same way every time. |
| `budget` | `ExceededError` | No |
| `cancelled` | context cancellation | No |
| `effect` | an effect that failed in the world (connection refused, file not found, HTTP client timeout) | **Yes** |
| `runtime` | type errors, bad arguments, stack overflow | No |
| `user` | the new `error(msg)` builtin | Only with `retry_on: ["effect", "user"]` |

Effects decide the kind where the error is created. `newError` stays for `runtime`; add `newErrorKind(kind, …)`.

### `match`

```arv
match await t {
  ok(v) => v,
  err(e) => if (e["kind"] == "capability") { "denied" } else { e["message"] }
}
```

- New tokens `MATCH` and `ARROW` (`=>`, which the lexer forms from `=` followed by `>`). `ok` and `err` are pattern names inside `match`, not reserved words.
- The subject is evaluated **without** propagating an error. That's the one place an error value doesn't abort the enclosing block.
- `e` is a hash: `{"kind", "message", "task", "pos"}`.
- Both arms are required. A `match` with a missing arm is a parse error, so error handling can't be half-written.
- The `ok`/`err` arm syntax follows the `match OK: … ERROR: …` idea from the original README.

### Strategies (`scope (supervise: …)`)

- **`fail_fast`** (default): unchanged. The first failure cancels siblings and the body.
- **`collect`:** failures don't cancel anything. Each failure is available through `await` (so `match` it). Rule: **errors are never dropped silently.** If a failed task is never awaited, the scope fails when it ends, with `N unawaited task failures: <first message>`.
- **`restart`:** options `retries` (default 3) and `backoff_ms` (default 100), with exponential backoff capped at 30 s. Jitter is derived from the task ID, not a random source, so Phase 6 replay stays deterministic. Each attempt is a fresh interpreter running `f(args)` again with ID `root/0#2`, `root/0#3`, …. Only retryable kinds restart. When retries run out, the last error is handled like `fail_fast`.

Restarting re-runs effects. Document this next to the option: restart is for idempotent work. Phase 6 adds a log, but it doesn't make `write_file` idempotent.

### Runtime shape

```go
// arv/runtime/supervisor.go
type Strategy int // FailFast, Collect, Restart
type Policy struct {
	Strategy Strategy
	Retries  int
	Backoff  time.Duration
	RetryOn  map[string]bool // error kinds
}
// Scope gets a Policy. Scope.Go(fn func(attempt int) (kind string, err error))
// runs the retry loop and the backoff sleeps (which honour ctx).
```

## Task outline

1. `Kind` on `value.Error`; set kinds in every effect and in `DeniedError`/`ExceededError` conversion. Table-driven test over all effects.
2. `error(msg)` builtin (`user` kind).
3. Lexer `=>`; parser `MatchExpression` (tests for the missing-arm parse error); evaluator, where the subject doesn't propagate.
4. `runtime.Policy` + `Scope.Go` retry loop (unit tests with fake failing funcs, attempt counting, backoff bounds, cancellation during backoff).
5. `collect` semantics, including the unawaited-failure rule.
6. Scope options `supervise`, `retries`, `backoff_ms`, `retry_on` (validation tests).
7. Attempt suffixes in task IDs.

## Acceptance tests

| Test | Setup | Expect |
|---|---|---|
| restart recovers | `httptest` server returns 500 for the first 2 calls (the Arv check turns that into `error(...)`), `retry_on: ["effect","user"]`, `retries: 3` | success; server hits = 3 |
| no retry on capability | `restart`, the task calls `getenv` without a grant | fails after 1 attempt (count attempts via `puts`) |
| backoff | `backoff_ms: 100`, 2 failures then success | elapsed ≥ 300 ms (100 + 200) |
| collect | 3 tasks, the middle one fails; `for t in ts { match await t { ok(v) => v, err(e) => e["kind"] } }` | `[1, "capability", 3]`; siblings not cancelled |
| collect never drops | `collect`, a failing task never awaited | the scope fails with `1 unawaited task failures` |
| escalation | inner `restart` scope exhausts retries inside an outer `collect` scope | outer sees one failed task; others finish |
| match passthrough | `match 5 { ok(v) => v + 1, err(e) => 0 }` | `6` |

**Phase done when:** these pass under `-race -count=5`, and every effect's error kind is covered by the table-driven test.
