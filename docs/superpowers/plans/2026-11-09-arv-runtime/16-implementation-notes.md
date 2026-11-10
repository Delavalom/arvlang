# Implementation notes: Phases 0–2 as built

**Status (2026-11-10):** Phases 0–2 are implemented on `feat/runtime`. Execution was subagent-driven: one implementer per task, plus spec and quality reviews. The branch is not pushed or merged. Read this file before expanding Phases 3–8: the code differs from plan files 02–09 in the ways listed below. The commit log is the source of truth.

## Added beyond the plan

| What | Where | Why |
|---|---|---|
| Black-box e2e suite (Tasks 1.8, 2.10) | `e2e/` | The owner asked for e2e coverage of the whole implementation. It builds the real binary and uses a local `httptest` server and temp dirs, with no internet. Server gates (`/wait`, `/arrive`, `/slow`, `/mark`) order events, so tests don't depend on wall-clock timing. `ARV_E2E_RACE=1` builds the binary with `-race`. |
| `Makefile` | root | `make build`, `make test`, `make e2e`. |
| Split parser tests | `arv/parser/parser_*_test.go` | The 20 KB per-file commit hook. |

## Security and robustness fixes found during execution

1. **Dangling symlink escape.** The e2e suite found it. `write_file` through a dangling link inside a grant wrote outside the grant. `Canonical` now walks path components with `Lstat`/`Readlink` and allows at most 40 hops. It returns `(string, error)`. Files are opened with `O_NOFOLLOW`.
2. **Scope validation.** An empty fs scope used to cover `/`, and `--allow fs_read=` used to grant the working directory. Both are now rejected. `arv.Run` validates and canonicalises the host's grants.
3. **One `..` rule.** `runtime.Resolve` joins paths without cleaning them, and `Canonical` resolves `..` the way the kernel does, after following symlinks. This applies everywhere: grants, constructors and requests.
4. **Limits.** Bodies over 10 MiB are an error instead of being truncated. `read_file` reads regular files only, so a FIFO fails fast. `sleep` has an upper bound. The parser has a nesting limit.
5. **Net scopes.** Ports, schemes, paths, `@` and whitespace are rejected. A trailing dot is stripped. Bracketed IPv6 addresses are normalised. Wildcards are allowed only as a leading `*.` on DNS names, never on IP addresses.
6. **Concurrency.**
   - A task panic now cancels and waits for the scope (I4), and the handle always finishes.
   - The first error wins, and the body's error goes through `Scope.Fail`.
   - `arv.Run` never panics into the host.
   - Division by zero is an Arv error. Empty blocks return `NIL`.
7. **Await ownership.** A task may await only handles spawned by itself or its descendants. Without this, a self-await, mutual awaits or awaiting a parent deadlocked the process.
8. **No authority, no OS access.**
   - Capability constructors don't touch the filesystem.
   - `CapSet.Admit` is the single admission point, used by `Attenuate`, `read_file` and `write_file`.
   - A denial outside every grant names the path as the script wrote it, never the resolved target or the OS error.
   - When resolution fails inside a grant, the script sees the real OS error.
   - `..` after a missing path component fails, as it does in the kernel.
   - Remaining limit: allow and deny can still reveal, one bit at a time, whether a path outside every grant exists. Canonicalising has to `lstat` it.
9. **Robustness.**
   - Total evaluator recursion is bounded at 100000 levels, so no program can overflow the Go stack. Before this, call depth multiplied by nesting depth was a fatal crash.
   - The parser counts only real nesting (`MaxNesting` 10000).
   - Infix chains are evaluated and printed iteratively.
   - `write_file` opens with `O_NONBLOCK`, so a FIFO can't hang a scope.
   - The REPL keeps one root task per session.
   - Await is allowed on handles spawned by the awaiting task or any of its descendants.

## Follow-through for later phases

- **Phase 3 (budgets):**
  - Release `tasks:` slots in a `defer`, so the panic path releases them too.
  - Scopes are created in `evalScope` and `Run`, not in `withScope` alone.
  - Spawn recursion is unbounded today, because each task starts at depth 0. Don't make the default task budget unlimited.
  - Check `timeout_ms` for overflow, as `sleep` now does.
  - Keep per-scope state in a frame in `in.scopes`. Don't mutate exported `Task` fields.
- **Phase 4 (supervision):**
  - `Scope.Fail` stores a Go error, so the error `Kind` is lost. Carry the `*value.Error` instead.
  - The await-ownership rule constrains supervisors. `collect` can rely on owners to know which handles were awaited.
  - `restart` re-runs against a live environment, so I8 (freeze at spawn) should land in Phase 4, not Phase 6. Today a child can see a parent rebind a name after `spawn`.
- **Phase 6 (replay):**
  - Freezing the environment at spawn must be transitive.
  - Spawn-time canonicalisation calls `lstat`. That is OS input, so log it or route it through the effect gate (I7).
  - `--replay` without `--allow` must install the grants from the log header. Otherwise every `spawn … with` is denied.
- **Phase 7 (agents):** denial messages still list the held grants. Redact them before errors reach a model.
- **Phase 8 (tooling and hardening):**
  - `net("host:443")` conflicts with the current rule that rejects ports. You would have to parse the port and compare it with each scheme's default port, in `http_get` and in `CheckRedirect`.
  - CIDR grants are rejected by the `/` check.
  - `os.Root` closes the directory-swap race between check and use, but it needs `go 1.24` or later. `go.mod` says 1.22.5. It also refuses escaping symlinks instead of following them to check where they lead, so some hardening e2e expectations will flip.
  - Moving `main.go` to `cmd/arv` breaks the e2e build target, the `Makefile` and the README.
  - DNS rebinding and SSRF are still open.

## Phase 3: budgets

Built from `p3-plan-1.md` to `p3-plan-13.md`; their deviations from `10-phase3-budgets.md` are listed in `p3-plan-1.md`. Review round 1 changed the following.

**Fixes:**
- **Every budget error has a position.** The root scope has no enclosing node, so `RunLimited` places its own errors (the last partial steps charge, a timeout after the body returned) at the program's last statement, or `1:1` if the program is empty.
- **A scope that closes after its deadline has timed out.** The context is checked only every 256 nodes and in cancellable builtins, so late work used to return success. `Scope.Wait` now checks the deadline itself, because the timer can fire late. `Scope.Fail` treats a failure that arrives after the deadline but before the timer as a timeout. `withScope` checks `Expired` before it checks for success.
- **A run's tasks share one pool of evaluator depth.** The pool is `runtime.MaxRunEvalDepth`, which is 4 × `MaxEvalDepth`. Each task reserves depth in chunks of 64 and returns it as its stack unwinds and when it ends. The live-task cap limited only how many tasks ran, not how much stack each one used. Before this fix, 20 tasks at a call depth of 9900 used 778 MB RSS, and 1000 tasks could have used tens of GB. With the pool, the run stops with `stack overflow: the run's tasks together nested deeper than 400000 levels` at about 340 MB.
- **`alloc_mb` charges more allocations.** It now charges environments (320 bytes for each call and each `for` iteration), bindings (48 bytes for each parameter, loop variable and `let`), closures (128 bytes) and task handles (160 bytes). These sizes were measured on darwin/arm64 with Go 1.26. The exact sizes in `TestAllocSizes` changed to match.
- **Timeouts under 1 ms are rounded up** to the next whole millisecond in the error message. A 999 µs timeout used to report `limit 0 ms`.
- **`--tasks` help text** now says that the flag caps live tasks and that spawning one more task is an error (p3-plan-12).
- **Tests added.** The backpressure test now uses an `httptest` server, as designed. Tests that cover the largest accepted option values, leak checks (`settled`) on the tasks and timeout tests, and a `startTask` test seam that makes `runTask` itself panic, so the deferred `Release` is exercised.

**Recorded deviations:**
- **Timeout bounds in the acceptance tests are 3 s, not 300 ms.** The tests must pass at load averages above 300. A 3 s bound still separates the 100 ms limit from the inner 10 s timeout and the 5 s sleep, so I6 is still tested.
- **Commit 323d76e has a README edit that is not in p3-plan-13.** It adds ` (at examples/fanout.arv:17:27)` to the fanout denial line. This matches what the binary prints, so it was kept. The getenv example was updated to match the same format.

Review round 2 changed the following.

**Fixes:**
- **No builtin runs after its scope has ended.** `applyFunction` calls `runtime.Task.Err` before every builtin. `Err` returns the context's cause, or `context.DeadlineExceeded` once a deadline on the context has passed but its timer has not fired yet. A `write_file` or `puts` that came after slow work used to run after the deadline, and only then was the timeout reported. Tests whose spawned builtin could start after its scope was cancelled (`TestPanicInNestedScope` and others) now wait until that builtin is running, so they still check that the scope waits for a running task (I4).
- **`puts` builds its text within the budgets.** An array that holds the same sub-array twice prints as about 5 × 2^n bytes for 32 charged bytes per level. `value.InspectChecked` calls a check every 64 KiB of text. `puts` charges that text to `alloc_mb` and stops once the task's scope has ended or its deadline has passed. Under `--alloc-mb 1 --timeout 50ms`, `puts(dup(1, 25))` used to take 2 s and 759 MB. It now fails with the alloc error at 15 MB RSS. A string argument prints without a copy and without a charge.
- **A task frees its live-task place before it wakes its awaiter.** `Release` used to run after `handle.Finish`. A loop that spawned and awaited one task at a time under `--tasks 1` could then spawn again while the finished task still counted, and failed in about half of the runs.
- **`Scope.Wait` returns the timeout for a failure after the deadline.** `Fail` records the timeout, not the late failure, once the scope's own deadline has passed. Before, only `withScope` got this right, because it checks `Expired` first.
- **The depth pool grows with the live-task cap.** The pool is the larger of `MaxRunEvalDepth` and `runtime.LiveTaskDepth` (256 levels) for every live task plus the root. The fixed pool capped live tasks that run Arv code at about 6250, whatever `--tasks` said. 7000 shallow tasks under `--tasks 8000` now run. At the default cap of 1000 tasks, the pool is still 400000 levels. The error is now `stack overflow: the run's live tasks together nested deeper than N levels`, where N is the pool's actual size.

**Known limits:**
- **A timeout is reported late, not enforced early, for work that ignores cancellation.** A host builtin that ignores its context runs to the end, because the scope waits for it (I4). The scope then reports the timeout. Pure Arv work between context checks (every 256 nodes) can also run past the deadline, but no builtin, and so no effect, starts after it.
- **`alloc_mb` still bounds churn, not live memory.** Integers (8 bytes each) are not charged, because each one is kept alive only by a charged array element or binding. Rebinding a name is charged again. Live memory stays within a small constant factor of the limit. The text `puts` builds is charged in 64 KiB steps, so a run can go up to 64 KiB past the limit before `puts` stops. The REPL prints each result with plain `Inspect`. It runs without budgets, so nothing bounds that text, just as nothing bounds the REPL's other allocations.
- **The depth pool bounds the total depth of the live tasks, not the number of deep tasks.** At the default cap, any mix whose depths add up to more than 400000 levels fails. For example, 900 tasks each 80 Arv calls (about 560 levels) deep fail, and 900 tasks each 60 calls deep pass. A raised `--tasks` raises the pool, and with it the worst-case stack memory: about 640 bytes per level (twice that under `-race`), so about 1.3 GB at `--tasks 8000`. `MaxRunEvalDepth` and `LiveTaskDepth` are constants, not `Limits` fields.
- **A root-level panic is placed at the program's last statement.** That is where the run ended, not where the panic happened.
- **The `httptest` backpressure test does not check for leaked goroutines.** The HTTP client keeps idle keep-alive connections open.

Review round 3 changed the following.

**Fixes:**
- **No effect starts after the steps limit is spent.** Steps are charged every 256 nodes and when a scope closes. Before this fix, a scope whose steps budget was spent still ran `write_file`, `http_get` or `puts`, and reported the error afterward. Under `steps: 10`, 4 to 8 of 20 spawned `write_file` tasks wrote their files. Now a task charges the steps it owes before every builtin and every spawn. If it owes none, it calls `runtime.Budget.StepsSpent`, because a sibling may have spent the shared limit before its error cancels the scope. The cost is one atomic operation per limited level for each builtin call and each spawn. The error is now placed at the builtin call or the spawn, not at the scope. The 255-step overshoot per task still applies to pure Arv work between builtins.
- **`TestTimeoutWaitsForTasks` survives a starved scheduler.** A task that starts after its deadline never runs `stubborn`, so that attempt tells nothing about I4. The test now records whether `stubborn` started, and if it did not, it tries again with timeouts of 4 s and then 16 s. `TestTimeoutWaitsForLateStartingTask` uses the `startTask` seam to hold the first task until the scope has timed out, which shows that the retry works.

Review round 4 changed the following.

**Fixes:**
- **No effect starts after a sibling has spent the shared `alloc_mb`.** A failed `Alloc` leaves the shared pool below zero. Before this fix, a task that allocated nothing before a builtin, such as `write_file(path, s)` on existing strings, saw that failure only when the scope was cancelled. `runtime.Budget.AllocSpent` reads the pools without charging, as `StepsSpent` does. `settleSteps` now calls it before every builtin and every spawn. The cost is one more atomic load per limited level.
- **The sibling-spent checks have an evaluator test.** `TestNoEffectAfterSiblingSpent` holds open the window between a sibling spending the shared budget and its error cancelling the scope. The sibling spends inside an inner scope that waits for a blocked host builtin, so the sibling does not return. A builtin with nothing left to charge then runs, both called directly and spawned. The test fails if `settleSteps` loses its `StepsSpent` or `AllocSpent` check. Before this, only `runtime.TestStepsSpent` covered the check. `TestNoSpawnedEffectAfterStepsSpent` never reached it, because its spawner spends the limit first.
- **Parse errors name the script file.** `arv.Run` returns a `*arv.ParseError`, whose `Error()` text is unchanged. `arv run` prints `ParseError.In(file)`, which rewrites each trailing `(at line:col)` as `(at file:line:col)`, as runtime errors do. A parse error without a position, such as the nesting limit, is printed as before.

Mutation check, round 1: 83 mutations, each run against `go test ./...` (unit and e2e), then reverted. 79 were caught.

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

**Survivors:**
- **Cancelled `Acquire`.** Its test has one level. A scope that holds a leaked slot is failing too, so Arv code cannot see the leak until Phase 4.
- **Depth.** With the mutation, 10000 sequential `await (spawn f())` calls overflow, because each task keeps 64 levels. No test runs that many.
- **Empty program.** `arv run --timeout 1ns` on an empty file loses `(at f:1:1)`. No test.
- **`await [..]`.** Equivalent under fail-fast; Phase 4 makes it observable.
