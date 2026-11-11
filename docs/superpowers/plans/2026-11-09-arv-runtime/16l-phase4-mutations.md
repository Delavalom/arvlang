# Phase 4 notes: independent mutation check

This file holds the mutation table for the "Phase 4: supervision" section of `16-implementation-notes.md` (the phase's own notes are `16j-phase4-notes.md` and `16k-phase4-hardening.md`). It is a separate file because `16` is close to the 18 KB limit. Like `16`, it overrides older plan text.

## Mutation check, round 1 (independent)

Each mutation was applied alone to the runtime (never committed), the whole suite was run (`go test -count=1 -timeout 4m ./...`: every unit package and the e2e suite through the binary), and the file was restored with `git checkout --` and `git status` checked clean before the next one. Load averages were between 35 and 390. "Caught" means at least one test failed; a mutation that made tests hang hit the 4-minute timeout as well as failing tests. Test names are top-level; e2e names are marked (e2e).

Of 45 mutations, 42 were caught in the suite run. Of the three survivors, one is equivalent (a stronger variant of it is caught), one is caught in only about half of the runs, and one is a **test gap** (the code is correct; nothing catches the mutant).

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| fail_fast cancels siblings (I5) | `Scope.Fail` records the error but does not cancel | yes | `TestScopeFailFast`, `TestFailingTaskCancelsGrandchild`, `TestSupervisePanic`; (e2e) `TestFailFastCPUSibling`, `TestFailFastHTTPSibling`, `TestFailFastSleepingSiblings`, `TestPhase4Invariants` (I5 case), `TestSupervision`. Two packages also hung to the timeout. |
| fail_fast propagates the error (I5) | `Supervise` treats fail_fast like collect (an error does not fail the scope) | yes | `TestFailFastIsTheDefault`, `TestScopeFailFast`, `TestTimedScopeFailureFirst`; (e2e) `TestFailFastScopeBody`, `TestFailedScopeIsAnError`, `TestPhase4Invariants`, `TestSupervision` |
| fail_fast: the first error wins (I5) | `Scope.Fail` without `once`: the last failure overwrites the first | yes | `TestFirstErrorWinsOverBodyError`, `TestChildErrorKeepsKindPosAndTask`, `TestCollectAccountsForEveryChild`; (e2e) `TestPhase4Invariants`, `TestPhase4Acceptance`, `TestAwaitArray` and 13 more |
| fail_fast cancels the scope body (I5) | `withScope` leaves the body on the outer context | yes | (e2e) `TestFailFastScopeBody`, `TestBudgetTimeout`, `TestTimeoutFlag`, `TestSupervision` |
| kind across task boundaries | `errorValue` rebuilds the value without its `Kind` | yes | `TestChildErrorKeepsKindPosAndTask`, `TestEvaluatorErrorKinds`, `TestEffectErrorKinds`, `TestCollectAccountsForEveryChild`; (e2e) `TestPhase4Invariants`, `TestPhase4Acceptance`, `TestSupervision` |
| position across task boundaries | `errorValue` rebuilds the value without `Pos` and `Task` | yes | `TestChildErrorKeepsKindPosAndTask`, `TestTaskErrorPositions`, `TestMatchAwaitedFailure`; (e2e) `TestPhase4Invariants`, `TestPhase4Acceptance`, `TestAwaitArray` |
| kind reaches the supervisor | `failure.ErrorKind` always says `runtime` | yes | `TestRestartRecovers`, `TestRestartThroughOptions`, `TestEscalation`, `TestHostCancelDuringRestartBackoffIsCancelled`; (e2e) `TestPhase4Acceptance` (restart rows), `TestPhase4Invariants` |
| position of a task's unplaced error | `evalSpawn` no longer places an error at the spawn | yes, unit only | `TestTaskErrorPositions`, `TestConcurrentReadsStopAtAllocLimit`. No e2e case spawns a builtin directly. |
| error kinds: capability | `DeniedError` has kind `effect` (so restart would retry a denial) | yes | `TestErrorKind`, `TestRestartSkipsCapabilityErrors`, `TestRestartOnlyRetryableKinds`, `TestOSErrorInsideGrantKinds`; (e2e) `TestPhase4Acceptance` (no retry on capability), `TestSupervision` |
| error kinds: cancelled | `ErrorKind` drops the context-cancellation case | yes, unit only | `TestErrorKind` (runtime). Every Arv-level cancellation path makes its own kind. |
| error kinds: user | `error(msg)` has kind `runtime` | yes | `TestUserError`, `TestMatch`, `TestRestartRecovers`, `TestCollectAccountsForEveryChild`; (e2e) `TestPhase4Acceptance`, `TestPhase4Invariants`, `TestSupervision` |
| collect never drops an error | `unawaited` always returns nil | yes | `TestCollectNeverDrops`, `TestCollectNeverDropsProperty`, `TestCollectAccountsForEveryChild`, `TestCollectTreeShapes`; (e2e) `TestPhase4Acceptance` (collect never drops), `TestSupervision` |
| collect never drops an error | collect handles are not tracked (`in.track` skipped) | yes | the same tests |
| collect: awaited failures are not reported again | `unawaited` counts awaited failures too | yes | `TestCollect`, `TestCollectNeverDrops`, `TestCollectAccountsForEveryChild`, `TestEscalation`; (e2e) `TestPhase4Acceptance`, `TestSupervision` |
| collect: the first unawaited failure, in spawn order | `unawaited` reports the last one | yes | `TestCollectNeverDrops`, `TestCollectAccountsForEveryChild`; (e2e) `TestPhase4Acceptance` (first unawaited failure in spawn order) |
| collect: the count of unawaited failures | the count is always 1 | yes | `TestCollectNeverDrops`, `TestCollectNeverDropsProperty`, `TestCollectLeaksNothing`, `TestCollectAccountsForEveryChild`; (e2e) `TestPhase4Acceptance` |
| collect: the unawaited error keeps its position | `unawaited` drops `Pos` | yes | `TestCollectNeverDrops`, `TestCollectTreeShapes`; (e2e) `TestPhase4Acceptance`, `TestSupervision` |
| collect returns every child's outcome; siblings not cancelled | `Supervise` treats collect like fail_fast | yes | `TestCollect`, `TestCollectDoesNotFailTheScope`, `TestCollectAccountsForEveryChild`, `TestEscalation`; (e2e) `TestPhase4Acceptance` (collect: siblings reach the server after the failure), `TestSupervision` |
| supervisors rely on await ownership | `checkAwait` lets any task await any handle | yes | `TestAwaitOnlyBySpawner`, `TestAwaitArrayErrors`; (e2e) `TestAwaitCycles` |
| restart runs at most `retries` more times | `attempt <= Retries+1` | yes | `TestRestartGivesUp`, `TestRestartThroughOptions`, `TestEscalation`; (e2e) `TestPhase4Acceptance` (at most retries+1 times, escalation), `TestPhase4Invariants` (slot reuse) |
| restart runs at most `retries` more times | `attempt < Retries` | yes | the same tests |
| restart retries only the kinds in `retry_on` | `restarts` ignores `RetryOn` | yes | `TestRestartOnlyRetryableKinds`, `TestRestartSkipsCapabilityErrors`; (e2e) `TestPhase4Acceptance` (no retry on capability; user errors not retried by default), `TestPhase4Invariants` (I6 rows) |
| restart obeys its options | `scopeOptions` returns the default policy, ignoring `retries`, `backoff_ms`, `retry_on` | yes | `TestRestartThroughOptions`, `TestEscalation`; (e2e) `TestPhase4Acceptance`, `TestPhase4Invariants` |
| retry_on admits only effect and user | `retryOn` accepts any string | yes, unit only | `TestSuperviseOptionErrors` |
| the default is 3 retries | `DefaultRetries = 2` | yes, e2e only | (e2e) `TestPhase4Acceptance` (an effect error retried by default, with attempt IDs) |
| backoff doubles | `d *= 1` | yes | `TestDelayBounds`, `TestRestartBackoff`, `TestRestartWaits`; (e2e) `TestPhase4Acceptance` (backoff) |
| backoff is capped at 30 s | drop `d = min(d, MaxBackoff)` after the doubling loop | no (equivalent) | The loop doubles at most once past the cap, and the final `min(..., MaxBackoff)` caps the result anyway. Dropping that final `min` instead is caught by `TestDelayBounds`. |
| jitter is derived from the task ID (replay) | jitter from `time.Now().UnixNano()` | partly | `TestDelayBounds` compares two back-to-back calls; they often fall in the same clock tick. It passed in the suite run, and failed in 165 of 300 runs of `-run TestDelayBounds -count=300`. |
| a backoff ends when the scope is cancelled | `backoff` waits out its timer after the cancel | yes | `TestCancelDuringBackoff`, `TestDeadlineDuringBackoff`; (e2e) `TestPhase4Invariants` (a timeout cuts a restart backoff short) |
| a cancel during a backoff is kind cancelled | `Supervise` reports the last attempt's error instead | yes, unit only | `TestCancelDuringBackoff`, `TestHostCancelDuringRestartBackoffIsCancelled` (package `arv`) |
| attempt IDs `root/0#2`, ... | `Attempt` names attempt n `#n-1` | yes | `TestAttempt`, `TestRestartThroughOptions`, `TestEscalation`; (e2e) `TestPhase4Acceptance`, `TestPhase4Invariants` |
| restart re-runs against the environment frozen at spawn (I8) | `Attempt` gives each new attempt no horizon (it sees the live environment) | yes | `TestAttempt`, `TestRestartSeesTheSpawnTimeEnvironment`; (e2e) `TestPhase4Invariants` (every restart attempt sees the bindings as of the spawn) |
| I8: a child never sees a later rebinding | `GetAs` ignores the task's horizon | yes | `TestEnvironmentFrozenAtSpawn`, `TestChildNeverSeesALaterRebinding`, `TestFreezeUnderConcurrentRebinding`, `TestAwaitOnlyBySpawner`; (e2e) `TestPhase4Invariants`, `TestAwaitCycles`, `TestSupervision` |
| I8 | `Set` always overwrites in place (the old binding is lost) | yes | `TestEnvironmentFrozenAtSpawn`, `TestChildNeverSeesALaterRebinding`, `TestFreezeUnderConcurrentRebinding`; (e2e) `TestPhase4Invariants`, `TestSupervision` |
| I8, transitively | `Horizon` uses the task's own spawn time, not the first one after the environment was created | yes | `TestHorizon`, `TestChildNeverSeesALaterRebinding` (the child's own environment, frozen for the grandchild); (e2e) `TestPhase4Invariants` (a grandchild does not see its parent rebind a name) |
| I8 | the child's horizon is one tick late (`clock.Add(1) + 1`) | yes | `TestHorizon`, `TestChildCannotSeeANameBoundAfterSpawn`, `TestEnvironmentFrozenAtSpawn`; (e2e) `TestPhase4Invariants`, `TestStructure`, `TestAttenuation` |
| restart never resets budgets (I6) | attempts after the first run under a fresh unlimited root budget | yes | `TestRestartNeverResetsBudgets`; (e2e) `TestPhase4Invariants` (steps, alloc, an inner restart scope inside the parent's steps) |
| restart never escapes the scope's timeout or cancellation (I5, I6) | `Attempt` gives attempts after the first `context.Background()` | **gap** | Not caught. Every restart test cancels the scope during a backoff or before the first attempt ends; none cancels it while a second or later attempt runs. A probe (`timeout_ms: 1000`, `retries: 1`, the first attempt fails at once, the second sleeps 15 s) ends in about 2 s on the real binary and in 15 s under the mutant, with the timeout reported late. A test should time out or fail a sibling during attempt 2 and check that it stops. |
| I2 under restart | `Attempt` gives attempts the parent's capabilities | yes, unit only | `TestAttempt` |
| match dispatches on kinds | the err arm's `kind` is always `runtime` | yes | `TestMatch`, `TestMatchChargesTheErrorHash`, `TestCollect`, `TestCollectAccountsForEveryChild`; (e2e) `TestPhase4Acceptance` (collect: `[1, capability, 3]`), `TestPhase4Invariants`, `TestSupervision` |
| match dispatches on error vs value | an error runs the ok arm | yes | `TestMatch`, `TestMatchAwaitedFailure`, `TestMatchArmErrorsPropagate`; (e2e) `TestPhase4Acceptance`, `TestPhase4Invariants`, `TestSupervision` |
| match does not rescue a cancelled task (I5) | drop the `in.Task.Err()` check | yes, unit only | `TestMatchDoesNotCatchInACancelledTask` |
| match does not swallow a spent budget (I6) | drop the `settleSteps` check | yes, unit only | `TestMatchCatchesNoErrorOnceStepsAreSpent`, `TestMatchDoesNotSwallowASpentStepsBudget`, `TestMatchInATaskDoesNotSwallowASpentStepsBudget` |
| no goroutine outlives its scope (I4, leak rule) | `Scope.Wait` does not wait | yes | `TestScopeWaitsForAllTasks`, `TestRestartLeaksNothing`, `TestCollectLeaksNothing`, `TestStructure`; (e2e) `TestPhase4Invariants`, `TestPhase4Acceptance` and 11 more |
| no goroutine outlives its scope (I4, leak rule) | `Supervise` calls `wg.Done` at the start, not on return | yes | the same tests, and `TestTaskPanicWaitsForGrandchildren`, `TestTimeoutWaitsForTasks` |

**Not touched by this phase:** I1, I3 and I7 (no Phase 4 code checks a grant or makes an effect; the error kinds of effects are covered above).

**Gaps to close (round 2):**
- Restart attempts after the first escaping the scope's context (the gap above).
- Jitter derived from the task ID is checked only probabilistically: compare `Delay` for one ID and attempt against a fixed value, or against another ID, rather than two back-to-back calls.

## Mutation check, round 2 (round 1's survivors)

The fixer's commit `3274568` added `TestAttemptSharesTheScopeContext`, `TestDelayIsReplayable` and three evaluator tests in `restart_scope_test.go`. Each survivor was reapplied alone and `go test -count=1 ./arv/runtime/ ./arv/evaluator/ ./e2e/` was run, then the file was restored with `git checkout --` and `git status` checked clean. Load averages were between 7 and 33.

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| backoff is capped at 30 s | drop `d = min(d, MaxBackoff)` after the doubling loop | no (equivalent) | Confirmed equivalent: a throwaway probe compared the mutant with the original over 2,808 inputs (backoffs 0 to 2^62 ns, attempts -1 to 70, three IDs) and every result matched. The line only changes `d` when `d > MaxBackoff`, and then the final `min` returns `MaxBackoff` either way (`d < 2 x MaxBackoff`, so `d + jitter` cannot overflow). Dropping the final `min` is caught by `TestDelayBounds`. |
| jitter is derived from the task ID (replay) | jitter from `time.Now().UnixNano()` | yes | `TestDelayIsReplayable` (failed in 100 of 100 runs of the compiled test binary), `TestDelayBounds` (58 of 100). Both failed in the suite run. |
| restart never escapes the scope's timeout or cancellation (I5, I6) | `Attempt` gives attempts after the first `context.Background()` | yes, unit only | `TestAttemptSharesTheScopeContext` (runtime), `TestRestartedAttemptStopsWithItsScope`, `TestRestartedAttemptStopsWhenTheHostCancels`, `TestRestartedAttemptStopsAtTheTimeout` (evaluator; each took 30 s under the mutant and reported the attempt outliving its scope). The e2e suite still passes under this mutant. |

All three round-1 survivors are now resolved: two caught, one equivalent. Remaining limit: no e2e test runs a second attempt past a timeout through the binary; the unit tests above cover it.

## Mutation check, round 3 (round 2's survivor)

No test was added for this mutant after round 2 (the branch head was `0c46552`). The mutant was reapplied alone, `go test -race -count=1 ./arv/runtime/` and then `./arv/evaluator/ ./e2e/` were run (all green), and the file was restored with `git checkout --` and `git status` checked clean. Load averages were between 9 and 24.

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| backoff is capped at 30 s | drop `d = min(d, MaxBackoff)` after the doubling loop (`arv/runtime/supervisor.go:52`) | no (equivalent) | None can exist. A second throwaway probe compared both formulas over 43,416 inputs (negative, zero, near every whole second up to 40 s, and a geometric sweep to 2^62 ns; attempts -1 to 70; three IDs) and none differed. For `d <= MaxBackoff` the line is a no-op; for `d > MaxBackoff` (only `d < 2 x MaxBackoff` is reachable) the final `min` returns `MaxBackoff` either way. `TestDelayBounds` covers the final `min`. |

Result: Phase 4 has no surviving non-equivalent mutant. The line is redundant; removing it would not change behaviour, but keeping it makes the jitter range (a tenth of the capped delay) explicit.

## Mutation check, round 4: the clamp is not equivalent

Rounds 1 to 3 were wrong to call this mutant equivalent. Their probes never tried a backoff below -2^62 ns. For such a backoff, the first doubling wraps around to a positive `d` near 2^63, which is far over `2 x MaxBackoff`. The clamp brings it back to the cap. Without the clamp, `d + jitter` overflows and gives a negative wait. For example, `Policy{Backoff: -(1<<62) - 1}.Delay("root/0", 2)` is 30 s on the original and -8319207133421237636 ns under the mutant. Arv code cannot set this backoff, because `backoff_ms` must be positive. A Go host can set it on `Policy`.

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| backoff is capped at 30 s | drop `d = min(d, MaxBackoff)` after the doubling loop | yes, unit only | `TestDelayClampsBeforeJitter` checks that a backoff whose doubling wraps gives a wait in `[0, MaxBackoff]` on attempts 2 to 70 |

Known limit, skipped as `TestDelayNegativeBackoff` (`BUG:`): any negative `Policy.Backoff` already gives meaningless waits on the original code. `Policy{Backoff: -100}.Delay("root/0", 1)` is about -1.86e18 ns, because `uint64(d/10+1)` is huge when `d` is negative. A negative wait fires at once, so the effect is a retry with no backoff. Arv code cannot reach this case. The fix is to reject or zero a negative `Backoff` in `Delay` or `SetPolicy`. The fix must keep `TestDelayClampsBeforeJitter` green; the clamp itself is still needed.

Re-check by the round 4 mutation checker (head `525d905`): the mutant was applied alone and `go test -race -count=1` was run on `./arv/runtime/`, `./arv/evaluator/` and `./e2e/`. `TestDelayClampsBeforeJitter` failed (for example, `Delay("root/0", 2)` with `Backoff -(1<<62) - 1<<40` gave -8319205594104958751 ns). The evaluator and e2e suites passed, as expected for a Go-host-only input. The file was restored with `git checkout --`, `git status` was clean, and the test passed again. Phase 4 has no surviving mutant.
