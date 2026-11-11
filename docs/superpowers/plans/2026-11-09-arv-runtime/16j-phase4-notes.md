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
- **Per-attempt overhead.** Each restart attempt allocates a `Task`, an `Interpreter`, the task body's scope and the backoff timer, uncharged apart from the call's environment. Measured (round 1, `TotalAlloc` over 25 against 1 attempts of `fn() { error("down") }`): about 1.35 KB per attempt, three runs 1328 to 1350 bytes. It is transient: once an attempt ends, only the last attempt's error stays reachable. Attempts are bounded by `retries` (at most 1000), so at most about 1.35 MB allocated per restarted spawn. A collect scope keeps one handle pointer per spawn until it closes.
- **`await` of an array under `collect` stops at its first failure.** The handles after it are not awaited by that `await`, so a later failure among them fails the scope as unawaited when it closes (`TestCollectAwaitArrayLeavesLaterHandlesUnawaited`). Nothing is dropped; to see every outcome, await each handle (`for t in ts { match await t { ... } }`).
- **`retry_on` takes only `effect` and `user`.** The other kinds fail the same way again or must stop the task.
- **Restart re-runs effects.** It is for idempotent work; Phase 6's log will not make `write_file` idempotent.

## Review round 1

| Finding | Severity | Outcome |
|---|---|---|
| `match` swallowed a spent steps budget: each caught error bought the arm and its loop up to 256 more nodes, so work grew with loop nesting (the repro: 2.3 s CPU, about 32M nodes, for `steps: 1000`) | Critical | Fixed. While a steps or bytes pool the task charges is spent, `match` passes every error through, after charging the steps it owes (`settleSteps`). The repro now stops at 1000 steps. `match_budget_test.go`: in the scope, in a task, any error under a spent pool, and the same error still matched outside the scope. |
| An HTTP client timeout while reading the body was kind `cancelled` (net/http's timeout error Is `DeadlineExceeded`), so restart never retried it | Important | Fixed. `readError` takes the task: `effect` unless the task was cancelled, as the `Do()` path already decides. `errkind_body_test.go`: the kind against a live and a cancelled task, a server that stalls mid-body (with `clientTimeout`, a test knob, at 2 s), and http_get's size cap. |
| A user error's message was copied, uncharged, into every cancellation error it caused (CLI: 416 MB peak RSS for 300 tasks and a 1 MiB message under `--alloc-mb 8`) | Important | Fixed. `failure.Error()`, the scope's cancellation cause, clips the message; the value read back through `errorValue` keeps it whole. Same run: 18.9 MB. `TestCancellationDoesNotCopyAUserMessage`. |
| A collect scope whose opening steps charge failed stayed in `Interpreter.collects` | Minor | Fixed: `withScope`'s early return forgets it. `TestCollectScopeForgottenWhenItCannotOpen`. |
| A child's horizon allocated about twice the 8 bytes per entry spawn charges | Minor | Fixed: `Task.Child` allocates it exactly. `TestChildHorizonIsExact`. |
| Deviation 10 (OS error canonicalising inside a grant is `effect`) untested | Minor | `TestOSErrorInsideGrantKinds`: a symlink loop and an unsearchable directory, through `spawn ... with` and `read_file`, against `capability` outside the grant. Changing either branch to another kind fails it. |
| Per-attempt overhead had no number | Minor | Measured above (Known limits). |
| `await` of an array under collect | Minor | Documented above, with a test. |
| `supervisor_leak_test.go` is not in p4-plan-10 | Minor | Kept as an accepted deviation: it is the leak rule's settle-loop check over every strategy, including a restart cancelled mid-backoff, which the kickoff rules require of new concurrency. |
| No e2e case | Minor | `e2e/p4_supervise_test.go` `TestSupervision`: error(), match on kinds, fail_fast, collect (every outcome, and an unawaited failure on stderr), restart (retries; a capability error runs once) and I8, all through `arv run`. |
| `TestLongInfixChainPrintsInLinearTime` flaked under load | Minor | Its bound is now 60 s: a quadratic `String()` would copy about 3 TB for 1M terms, and the 16 MB stack still catches recursion. |
| The design's `-race -count=5` bar was not recorded | Minor | Run after round 1 (load averages between 26 and 60): the Phase 4 tests in `arv/evaluator` (`Restart|Collect|Escalation|FailFast|Match|Freeze|Child|Supervise|Kind|UserError|Cancellation|ReadError|StalledBody|OSErrorInsideGrant`), `arv/runtime` (`Supervise|Restart|Delay|Cancel|Deadline|Horizon|Attempt|ErrorKind`) and e2e `TestSupervision|TestAwaitCycles` all pass with `-race -count=5`. |
