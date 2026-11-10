# Phase 3 notes: review rounds and mutation checks

This file extends the "Phase 3: budgets" section of `16-implementation-notes.md`, which keeps the recorded deviations and the known limits. The review-round fixes and the mutation tables moved here when Phase 3 and Phase 8.3 were merged, because the pre-commit hook blocks files over about 20 KB. Like `16`, this file overrides older plan text.

Built from `p3-plan-1.md` to `p3-plan-13.md`; their deviations from `10-phase3-budgets.md` are listed in `p3-plan-1.md`. Review round 1 changed the following.

**Fixes:**
- **Every budget error has a position.** The root scope has no enclosing node, so `RunLimited` places its own errors (the last partial steps charge, a timeout after the body returned) at the program's last statement, or `1:1` if the program is empty.
- **A scope that closes after its deadline has timed out.** The context is checked only every 256 nodes and in cancellable builtins, so late work used to return success. `Scope.Wait` now checks the deadline itself, because the timer can fire late. `Scope.Fail` treats a failure that arrives after the deadline but before the timer as a timeout. `withScope` checks `Expired` before it checks for success.
- **A run's tasks share one pool of evaluator depth.** The pool is `runtime.MaxRunEvalDepth`, which is 4 × `MaxEvalDepth`. Each task reserves depth in chunks of 64 and returns it as its stack unwinds and when it ends. The live-task cap limited only how many tasks ran, not how much stack each one used. Before this fix, 20 tasks at a call depth of 9900 used 778 MB RSS, and 1000 tasks could have used tens of GB. With the pool, the run stops with `stack overflow: the run's tasks together nested deeper than 400000 levels` at about 340 MB.
- **`alloc_mb` charges more allocations.** It now charges environments (320 bytes for each call and each `for` iteration), bindings (48 bytes for each parameter, loop variable and `let`), closures (128 bytes) and task handles (160 bytes). These sizes were measured on darwin/arm64 with Go 1.26. The exact sizes in `TestAllocSizes` changed to match.
- **Timeouts under 1 ms are rounded up** to the next whole millisecond in the error message. A 999 µs timeout used to report `limit 0 ms`.
- **`--tasks` help text** now says that the flag caps live tasks and that spawning one more task is an error (p3-plan-12).
- **Tests added.** The backpressure test now uses an `httptest` server, as designed. Tests that cover the largest accepted option values, leak checks (`settled`) on the tasks and timeout tests, and a `startTask` test seam that makes `runTask` itself panic, so the deferred `Release` is exercised.

Review round 2 changed the following.

**Fixes:**
- **No builtin runs after its scope has ended.** `applyFunction` calls `runtime.Task.Err` before every builtin. `Err` returns the context's cause, or `context.DeadlineExceeded` once a deadline on the context has passed but its timer has not fired yet. A `write_file` or `puts` that came after slow work used to run after the deadline, and only then was the timeout reported. Tests whose spawned builtin could start after its scope was cancelled (`TestPanicInNestedScope` and others) now wait until that builtin is running, so they still check that the scope waits for a running task (I4).
- **`puts` builds its text within the budgets.** An array that holds the same sub-array twice prints as about 5 × 2^n bytes for 32 charged bytes per level. `value.InspectChecked` calls a check every 64 KiB of text. `puts` charges that text to `alloc_mb` and stops once the task's scope has ended or its deadline has passed. Under `--alloc-mb 1 --timeout 50ms`, `puts(dup(1, 25))` used to take 2 s and 759 MB. It now fails with the alloc error at 15 MB RSS. A string argument prints without a copy and without a charge.
- **A task frees its live-task place before it wakes its awaiter.** `Release` used to run after `handle.Finish`. A loop that spawned and awaited one task at a time under `--tasks 1` could then spawn again while the finished task still counted, and failed in about half of the runs.
- **`Scope.Wait` returns the timeout for a failure after the deadline.** `Fail` records the timeout, not the late failure, once the scope's own deadline has passed. Before, only `withScope` got this right, because it checks `Expired` first.
- **The depth pool grows with the live-task cap.** The pool is the larger of `MaxRunEvalDepth` and `runtime.LiveTaskDepth` (256 levels) for every live task plus the root. The fixed pool capped live tasks that run Arv code at about 6250, whatever `--tasks` said. 7000 shallow tasks under `--tasks 8000` now run. At the default cap of 1000 tasks, the pool is still 400000 levels. The error is now `stack overflow: the run's live tasks together nested deeper than N levels`, where N is the pool's actual size.

Review round 3 changed the following.

**Fixes:**
- **No effect starts after the steps limit is spent.** Steps are charged every 256 nodes and when a scope closes. Before this fix, a scope whose steps budget was spent still ran `write_file`, `http_get` or `puts`, and reported the error afterward. Under `steps: 10`, 4 to 8 of 20 spawned `write_file` tasks wrote their files. Now a task charges the steps it owes before every builtin and every spawn. If it owes none, it calls `runtime.Budget.StepsSpent`, because a sibling may have spent the shared limit before its error cancels the scope. The cost is one atomic operation per limited level for each builtin call and each spawn. The error is now placed at the builtin call or the spawn, not at the scope. The 255-step overshoot per task still applies to pure Arv work between builtins.
- **`TestTimeoutWaitsForTasks` survives a starved scheduler.** A task that starts after its deadline never runs `stubborn`, so that attempt tells nothing about I4. The test now records whether `stubborn` started, and if it did not, it tries again with timeouts of 4 s and then 16 s. `TestTimeoutWaitsForLateStartingTask` uses the `startTask` seam to hold the first task until the scope has timed out, which shows that the retry works.

Review round 4 changed the following.

**Fixes:**
- **No effect starts after a sibling has spent the shared `alloc_mb`.** A failed `Alloc` leaves the shared pool below zero. Before this fix, a task that allocated nothing before a builtin, such as `write_file(path, s)` on existing strings, saw that failure only when the scope was cancelled. `runtime.Budget.AllocSpent` reads the pools without charging, as `StepsSpent` does. `settleSteps` now calls it before every builtin and every spawn. The cost is one more atomic load per limited level.
- **The sibling-spent checks have an evaluator test.** `TestNoEffectAfterSiblingSpent` holds open the window between a sibling spending the shared budget and its error cancelling the scope. The sibling spends inside an inner scope that waits for a blocked host builtin, so the sibling does not return. A builtin with nothing left to charge then runs, both called directly and spawned. The test fails if `settleSteps` loses its `StepsSpent` or `AllocSpent` check. Before this, only `runtime.TestStepsSpent` covered the check. `TestNoSpawnedEffectAfterStepsSpent` never reached it, because its spawner spends the limit first.
- **Parse errors name the script file.** `arv.Run` returns a `*arv.ParseError`, whose `Error()` text is unchanged. `arv run` prints `ParseError.In(file)`, which rewrites each trailing `(at line:col)` as `(at file:line:col)`, as runtime errors do. A parse error without a position, such as the nesting limit, is printed as before.

Review round 5 changed the following.

**Fixes:**
- **`alloc_mb` bounds what reading builtins hold.** `read_file` and `http_get` used to read up to 10 MiB (`maxBody`) and copy it to a string before the result was charged. `AllocSpent` stops a builtin only after a sibling's charge has failed, so readers running at once all got past it. Under `alloc_mb: 1`, 100 spawned `read_file` calls on a 10 MB file reached 0.65 to 1.0 GB RSS, and one call reached 45 MB. Both are now `value.ChargingFunction` builtins: `applyFunction` passes them the scope's charge, and `readCapped` charges each 32 KiB chunk before keeping it, then builds the string in place without a second copy. `http_get` then charges its record's two pairs. A result still costs exactly what it did (a file's bytes; a body plus 96 bytes), and a spent budget is the bare budget error at the call. The same 100-task run now fails at 14 to 15 MB RSS, and one call at 14 MB, against 10 MB for a trivial run. `TestReadFileStopsAtAllocLimit`, `TestConcurrentReadsStopAtAllocLimit` and `TestHTTPGetStopsAtAllocLimit` bound the heap allocated, and `TestReadingBuiltinsCostTheirResult` pins the exact cost.

**Not changed:**
- **The notes are already split.** The finding that `16-implementation-notes.md` was 18416 bytes, 16 bytes under 18 KiB, was true of `phase/p3` at 620538b. The Phase 3 / Phase 8.3 merge (95b2261, d0edc29) had already moved the review rounds and mutation tables here, to `16c-phase3-notes.md`. At d0edc29, `16` is 13959 bytes and this file is 10812 bytes. `phase/p3` was fast-forwarded to d0edc29 before this round, so the split is on this branch too.

Mutation check, round 1: 81 mutations, each run against `go test ./...` (unit and e2e), then reverted. 77 were caught.

| Guarantee | Mutation | Caught | Caught by |
|---|---|---|---|
| Limits | no deadline; `2N` ms | yes | TestBudgetTimeout |
| Limits | no `tasks:` slots; `N+1` | yes | TestTasksBackpressure |
| Limits | live cap off, `+1`, or unlimited by default | yes | TestDefaultLiveTaskCap |
| Limits | `Steps` never fails; `N+1` (option, flag); no charge in `tick` or at close | yes | TestBudgetSteps, TestStepsExactAtScopeExit |
| Limits | `Alloc` never fails; +1 byte or MiB; envs, calls, `puts`, `await [..]` free | yes | TestAllocLimit, TestAllocSizes |
| Limits | pools trip at `-1`; wrong unit | yes | TestExceededStaysExceeded |
| Options | no overflow max; 0, unknown, duplicate or non-INTEGER accepted | yes | TestScopeOptions |
| I6 | `Child` unchained; `Task` drops levels; `scope` on a new root; timer off the parent; worst level inverted; slots innermost only or past the task | yes | TestFlagsOnlyShrink, TestTaskBoundaryStopsSlots |
| I4 | no join once cancelled; panic path skips `Wait`; `scope` reuses its parent's `Scope` | yes | TestRootPanicWaitsForTasks |
| I5 | `Fail` does not cancel; body or task error does not fail; `tick` ignores cancel | yes | TestFailFastCPUSibling |
| I4, I5 | `Expired` nil; `expireIfDue` off; `Task.Err` skips deadlines; no `settleSteps` or its checks | yes | TestTaskErr, TestNoEffectAfterSiblingSpent |
| Slots | `Release` not deferred, or after `Finish`; live task or slot kept; live-cap failure keeps slots | yes | TestSlotsReleasedWhenRunTaskPanics, TestLiveCapSequentialSpawnAwait |
| Slots | cancelled `Acquire` keeps lower slots | **no** | none |
| Leaks | `runTask` keeps its depth | **no** | none |
| Positions | col+1 in `at` or lexer; infix at operator; option, spawn, root or timeout position dropped | yes | TestErrorPositions, TestRootBudgetErrorsHaveAPosition |
| Positions | empty program loses `1:1` | **no** | none |
| `for`, `await` | shared env; return or error ignored; non-array; `await [..]` reversed, unchecked | yes | TestForIn, TestAwaitArray |
| `for`, `await` | `await [..]` past an error | **no** | equivalent |
| CLI | flags ignored; MiB as KiB; no default timeout; `0` accepted | yes | TestBudgetFlags |

Mutation check, round 2: the four round-1 survivors, after the tests in 70c0000. 3 are now caught.

| Guarantee | Mutation | Caught | Caught by |
|---|---|---|---|
| Slots | cancelled `Acquire` keeps lower slots | yes | TestAcquireCancelFreesLowerSlots |
| Leaks | `runTask` keeps its depth | yes | TestRunDepthReturnedByFinishedTasks |
| Positions | empty program loses `1:1` | yes | TestEmptyProgramErrorsAtOneOne |
| `for`, `await` | `await [..]` past an error | **no** | equivalent |

Mutation check, round 3: the survivor as two mutants, after 79d1770. Both are caught. e2e misses both: in one run, fail-fast hides them.

| Guarantee | Mutation | Caught | Caught by |
|---|---|---|---|
| `for`, `await` | error stored, waiting goes on | yes | TestAwaitArrayStopsAtFirstError |
| `for`, `await` | waits for all, then first error | yes | TestAwaitArrayDoesNotWaitPastAnError |
