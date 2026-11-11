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
- **No error is caught inside a budget that ran out.** While a steps or bytes pool the task charges is spent (after charging the steps it owes), `match` passes every error through, as a cancelled task does; outside the scope that ran out, its error is matched as usual. (Round 1 fixed this for steps: before, a spent steps pool let each caught error buy another 256 nodes, so a loop of matches ran on.)
- **`collect` reports unawaited failures only when the scope would otherwise succeed.** If the body fails, the scope fails with the body's error; the unawaited failures are not listed, but the failure is not silent.
- **Old bindings are kept.** A name rebound after a spawn keeps its old value alive as long as its environment lives (a 32-byte binding, charged 48 like every binding). A fresh binding measured 102 bytes including map growth, against 104 before Phase 4.
- **The clock is process-wide.** Runs in one process share `runtime.clock`; only its order matters.
- **Per-attempt overhead.** Each restart attempt allocates a `Task`, an `Interpreter` and the task body's scope, uncharged apart from the call's environment; attempts are bounded by `retries` (at most 1000). A collect scope keeps one handle pointer per spawn until it closes.
- **`retry_on` takes only `effect` and `user`.** The other kinds fail the same way again or must stop the task.
- **Restart re-runs effects.** It is for idempotent work; Phase 6's log will not make `write_file` idempotent.
