# Phase 3: Budgets (plus the ergonomics they need)

**Status:** design and task outline. Write the bite-sized plan (same format as files 02–09) once the Phase 0–2 PR is merged.

**Goal:** `scope (timeout_ms: 2000, tasks: 20, steps: 1000000, alloc_mb: 64) { ... }` enforces each limit, and limits only shrink as they're inherited (I6). Errors carry `line:col`. Fan-out no longer needs recursion.

## Design

### Scope options syntax

`scope (<ident>: <expr>, ...) <block>`. After `scope`, the parser checks for `(`. A block starts with `{`, so there's no ambiguity with hash literals. The node becomes `ScopeExpression{Options []ScopeOption{Name string; Value Expression}, Body}`. The evaluator rejects unknown names (`unknown scope option "timout_ms"`) and non-integer values. Phase 4 reuses the same list for `supervise`, Phase 7 for `tokens` and `usd_cents`.

### `runtime.Budget`

```go
// arv/runtime/budget.go
type Limits struct {
	Timeout    time.Duration // 0 = no new limit
	Tasks      int           // max concurrently running *direct* children; 0 = unlimited
	Steps      int64         // nodes evaluated by every task under the scope; 0 = no new limit
	AllocBytes int64         // bytes of Arv values allocated under the scope; 0 = no new limit
}

type Budget struct {
	parent *Budget
	steps  *pool // nil = unlimited at this level
	alloc  *pool
	slots  chan struct{} // nil = unlimited
}

func Root(l Limits) *Budget
func (b *Budget) Child(l Limits) *Budget          // new pools at this level; ancestors still apply (I6)
func (b *Budget) Take(kind string, n int64) error // draws from this level and every ancestor; *ExceededError
func (b *Budget) Acquire(ctx context.Context) error
func (b *Budget) Release()

type ExceededError struct{ Kind string; Limit int64 } // "budget exceeded: steps (limit 1000000)"
```

- **Timeout** isn't a pool. `withScope` derives the scope context with `context.WithTimeoutCause(parent, d, &ExceededError{Kind: "timeout"})`. Nested deadlines take the earlier one automatically, which satisfies I6 without extra code.
- **Steps:** `Task.Tick` already counts. Instead of an atomic per node, a task takes fuel from its scope's budget 256 steps at a time and spends it locally. Overshoot is at most 255 steps per task.
- **Tasks** counts *direct* children only. If it counted descendants, a scope with `tasks: 2` whose two children each spawn a grandchild would deadlock: the children hold both slots while waiting on grandchildren that can't get one. `evalSpawn` calls `Acquire` before `scope.Go`, so a full scope makes `spawn` *block* (backpressure), and the block respects cancellation. `Release` runs when the child finishes.
- **AllocBytes** charges where values are created: strings `len`, arrays `16×n`, hashes `48×n` (rough Go sizes). Nothing is refunded, which is why it's called `alloc_mb` (see the design doc, §6).

### Positions in errors

`token.Token` gains `Line, Col int`; the lexer tracks them. `value.Error` gains `Pos string`. The evaluator stamps the position of the call or spawn node that produced an error. Messages become `fanout.arv:12:7: capability denied: …`. Without this, budget errors in a 200-line script are unusable, so it comes first.

### `for … in` and `await` on arrays

- `for x in xs { body }` evaluates to an **array of body values**. It's a comprehension, so fan-out is one expression. Each iteration gets a fresh enclosed environment. Only arrays are iterable for now.
- `await [t1, t2]` awaits each in order and returns an array. The first error wins.

The fan-out example becomes:

```arv
let statuses = scope (tasks: 20, timeout_ms: 5000) {
  await for url in urls { spawn check(url) with [net("api.github.com")] }
};
```

### CLI

`arv run --timeout 30s --steps 50000000 --tasks 100 --alloc-mb 512 …` sets the root budget. The defaults are unlimited except `--timeout`, which defaults to 10 minutes so a forgotten script can't run forever.

## Task outline (each becomes test → fail → implement → pass → commit)

1. **Positions:** `Line`/`Col` in tokens (lexer test), `Pos` on errors, stamped at call/spawn nodes, `arv run` prefixes the file name.
2. **`for … in`:** tokens `FOR`, `IN`; `ast.ForExpression`; parser and evaluator tests.
3. **`await` on arrays.**
4. **Scope options:** parser (`ScopeOption`), evaluator validation (unknown names, non-integer values).
5. **`runtime.Budget`:** unit tests for `Take` across three levels, `Acquire` blocking and unblocking on cancellation, `ExceededError` text.
6. **Timeout:** wire into `withScope`.
7. **Steps:** fuel chunks in `Task.Tick`; the task holds a `*Budget`.
8. **Tasks:** `Acquire`/`Release` around `scope.Go` in `evalSpawn`.
9. **Allocation:** charge in the evaluator at value construction.
10. **CLI flags** and root budget in `arv.Options{Limits: runtime.Limits{…}}`.

## Acceptance tests (Arv programs the evaluator tests must run)

| Test | Program | Expect |
|---|---|---|
| timeout | `scope (timeout_ms: 100) { sleep(5000) }` | error `budget exceeded: timeout`, returns in < 300 ms |
| I6, nested timeout | `scope (timeout_ms: 100) { scope (timeout_ms: 10000) { sleep(5000) } }` | fails at ≈100 ms, not 10 s |
| steps | `let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } }; scope (steps: 1000) { f(5000) }` | `budget exceeded: steps (limit 1000)` |
| steps shared by tasks | 4 tasks each needing ≈400 steps under `steps: 1000` | fails; same 4 tasks under `steps: 5000` pass |
| tasks backpressure | 50 × `spawn work()` (each `sleep(100)`) under `tasks: 5`, against an `httptest` server that records peak in-flight requests | peak in-flight ≤ 5; wall time ≥ 1 s |
| no deadlock | `tasks: 2`, two children that each spawn and await a grandchild | completes |
| alloc | `let grow = fn(n, acc) { if (n == 0) { acc } else { grow(n - 1, push(acc, "x")) } };` then `scope (alloc_mb: 1) { len(grow(5000, [])) }`. `push` copies, so this allocates ≈200 MB | `budget exceeded: alloc`; the same program under `alloc_mb: 1024` returns `5000` |
| positions | a denied `getenv` on line 3 of a script run through `arv run` | stderr starts with `<file>:3:` |

**Phase done when:** all of the above pass under `go test -race -count=5`, and `examples/fanout.arv` uses `for` and `scope (tasks: …)` instead of recursion.
