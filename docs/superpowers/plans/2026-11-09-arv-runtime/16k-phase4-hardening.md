# Phase 4: e2e and hardening

Continues `16j-phase4-notes.md`: the end-to-end cases, fuzz targets, the collect property and the leak checks added after review round 2, and the bug they found.

## Bug found and fixed

| Finding | Severity | Outcome |
|---|---|---|
| A block the input ends inside parsed without error: the parser closed it silently at EOF. A program missing a closing brace ran with another meaning: `scope (supervise: "collect") { spawn f(); 1` ran, and `let f = fn() { 3` followed by `puts(f())` put the `puts` inside `f` and printed nothing. Not caused by Phase 4 (every block since Phase 0), but it reaches the supervise syntax. | Important | Fixed in `parseBlockStatement`: `expected next token to be }, got EOF instead (at L:C)`, at the end of the input, once for blocks nested in each other. `unterminated_block_test.go` (fn, if, else, for, scope, scope with options, the error position, one error for three nested blocks). Found by `FuzzScopeOptionsSyntax`; its input is kept in `arv/parser/testdata/fuzz`. |

## Fuzz targets

| Target | Package | What it checks | Run |
|---|---|---|---|
| `FuzzMatchSyntax` | `arv/parser` | `match <subject> { ok(a) => B, err(b) => C }` composed from fuzzed parts, one per line, arms in either order, with or without a trailing comma. No panic, deterministic errors, each with a position inside the input. When every part parses alone (closed: no open string, balanced brackets, no top-level `;`), the match parses to exactly those parts; a name that is not one identifier is an error. | 2m0s, 2,285,807 execs, nothing found (after a harness fix: a name with spaces around it is fine) |
| `FuzzScopeOptionsSyntax` | `arv/parser` | `scope (n1: v1, ...) { body }` (or `scope { body }`) from NUL-separated names and values and a fuzzed body; the same checks, plus the options in order and the body. | found the bug above in 2 s; then, at 1m47s (984,694 execs), a harness false positive (an open string in a value swallows the next name), fixed and kept as a corpus entry; final run 2m0s, 3,601,196 execs, nothing found |
| `FuzzRunSupervise` | `arv` | Any program, seeded with Phase 4 programs (every strategy and option, valid and not, nested, under each budget), under a 200 ms timeout or a host cancel after 20 ms, with one directory granted: ends in time, no panic into the host, every `*Error` has a position and one of the six kinds, no goroutine outlives `Run`. | 2m0s, 1,199,900 execs, nothing found |
| `FuzzCollectTree` | `arv` | The collect property below, over trees decoded from fuzzed bytes. | 2m0s, 381,527 execs, nothing found |

Commands: `go test -run='^$' -fuzz='^FuzzName$' -fuzztime=2m ./arv/parser` (or `./arv`). Load averages during the runs were between 10 and 700.

## The collect property

`arv/collect_prop_test.go`, `TestCollectAccountsForEveryChild`: 200 random trees (40 with `-short`) of up to 80 nodes, three levels deep. A leaf succeeds or fails with each kind (`user` by `error`, `capability` by `getenv`, `effect` by `read_file` of a missing file in a granted directory, `runtime` by `len(5)`, `budget` by a `steps: 50` scope). An inner node is a `collect` scope that awaits every child with `match`, a `collect` scope that awaits only some, or a `fail_fast` scope with at most one failing leaf. Every node is one top-level function on its own line, so its position is known.

It checks, against outcomes computed in Go, that every awaited child's outcome appears exactly once, in spawn order, with its kind, message, task ID (`root/1/0`, ...) and position; that a `collect` scope with unawaited failures fails with `N unawaited task failures: ` and the first one's (in spawn order) kind, task and position; that a `fail_fast` scope passes its one failure on unchanged; and that no goroutine outlives the run (the settle loop `noLeak`). `TestCollectTreeShapes` pins five trees (every leaf kind, an unawaited failure, a `fail_fast` failure, nesting). Changing the expected unawaited count makes it fail at the fifth tree.

## End-to-end cases

`e2e/p4_accept_test.go` covers every acceptance row of `11-phase4-supervision.md` through the binary; `/flaky?k=K&fail=N` (`e2e/p4_routes_test.go`) answers 500 to the first N requests and records arrival times, so attempts are counted and backoffs timed on the server.

| Row | Case | Checked |
|---|---|---|
| restart recovers | `restart recovers` | `up`; 3 requests |
| (bound) | `restart runs at most retries+1 times`; `... does not retry user errors by default` | 4 requests for `retries: 3`; 1 without `retry_on: ["user"]` |
| no retry on capability | `no retry on capability` | the denial, at its position; 1 attempt |
| backoff | `backoff` | waits of at least 100 and 200 ms between the server's arrivals |
| (effect default) | `restart retries an effect error by default, with attempt IDs` | `[effect, root/0#4]`; 4 attempts |
| collect | `collect` | `[1, capability, 3]`; both siblings reach the server after the failure |
| collect never drops | `collect never drops`; `... first unawaited failure in spawn order` | `1 unawaited task failures: x`; with two, the first spawned (which fails last) is reported, with its kind, task and position |
| escalation | `escalation` | `[1, [user, in, root/1/0#3], 3]`; the siblings finish after the inner scope's 3 attempts |
| match passthrough | `match passthrough` | `6` |

`e2e/p4_invariants_test.go`:
- **I5:** `fail_fast` cancels a sibling's request (the server sees it cancelled) and passes on `[user, first, root/1, 4:3]`.
- **Kind, task and position across task boundaries:** an effect error from a grandchild under `fail_fast` (`[effect, root/0/0, 1:16]`), an uncaught grandchild denial on stderr at its position, and a user error collected across two task boundaries.
- **I6, restart never resets or escapes a budget:** steps (`root/0#2` stops at the second attempt), steps inside an inner scope that asks for more (the outer `limit 3000` is reported), alloc, a timeout that cuts a 30 s backoff short (one attempt, kind `budget`), and `tasks: 1` (attempts reuse the task's slot: 3 attempts).
- **I8:** each of three restart attempts prints the value bound at the spawn, though the scope rebound it before the attempts read it (a server gate orders it); a grandchild does not see its grandparent or its parent rebind a name; a task spawned after the rebinding sees it.

## Leak checks

New concurrency paths run through `arv.Run` with the settle-loop leak check: every tree of the collect property and every `FuzzCollectTree` and `FuzzRunSupervise` input (restart backoffs cut by a timeout or a host cancel, unawaited failures, nested strategies). The existing `supervisor_leak_test.go`, `collect_leak_test.go` and `TestHostCancelDuringRestartBackoffIsCancelled` stay.

## Runs

- `go test -race -count=5 ./...`: green (load average 730 at the start; evaluator 300 s).
- `ARV_E2E_RACE=1 go test -race ./e2e/`: green; the Phase 4 e2e tests (`TestPhase4|TestSupervision|TestAwaitCycles`) also with `-count=5`.
