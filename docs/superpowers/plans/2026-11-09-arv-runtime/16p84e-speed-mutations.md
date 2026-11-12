# Phase 8.4: speed, mutation check round 1

Continuation of [`16p84-speed.md`](16p84-speed.md) (e2e and hardening: [`16p84d`](16p84d-speed-e2e.md)). Run on 2026-11-12 in `arvlang-wt/p84` on `phase/p84` at 4bd811a. Each mutant was applied alone. The run covered `go test -count=1 -timeout 420s ./arv/... ./e2e/ .`: the unit tests, the e2e suite (both binaries, compiled and `arv_treewalk`) and the CLI package. After each mutant, `git checkout -- arv` restored the tree, and `git status` was clean before the next one. Nothing was committed. Load average was 3 to 22. The unmutated tree was green before the run (`./arv/...` 122 s, `./e2e/ .` 9 s).

Guarantees: **S1** same meaning as the tree-walker, **S2** budgets (steps, bytes) exact, **S3** cancellation observed promptly, **S4** depth limits (from `p84-plan-1.md`). Invariants this phase touches: I1 (through the compiled call path), I5 and I6 (compiled scopes), I7 (the log's checks and effect records), I8 (environments). Phase 5E syntax is desugared by the parser into `if`, `!`, index and `$str` calls, so M4, M5, M10, B3, B5 and B7 cover it. "e2e" in the last column means the e2e suite caught the mutant as well as unit tests.

38 mutants. **36 caught, 2 not caught (B6, L1).**

| # | Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|---|
| M1 | S1 | `compileProgram`: a top-level `return` yields the `ReturnValue` wrapper, not its value | yes | `TestReturnStatements` (unit only) |
| M2 | S1 | `compileBlockRun` stops only on errors, not on `return` | yes, e2e | `TestReturnStatements`, `TestMatch`, `TestEnginesAgreeOnStatementsProperty`; e2e `TestP84EnginesAgreeOnValues/return_from_nested_loops` |
| M3 | S1 | `compileInfixChain` pairs operators with operands outermost first | yes, e2e | `TestEnginesAgreeOnCorpus`, `TestLessOrEqualAndGreaterOrEqual`, `TestLogsCrossEngines`; e2e `TestP84REPL`, `TestPEInterpolation` |
| M4 | S1, 5E `&&` `\|\|` | `compileIf` takes any value except `false` as true | yes, e2e | `TestAndOr`, `TestEnginesAgreeOnCorpus`, `TestEnginesAgreeProperty`; e2e `TestPEAndOr`, `TestP84EnginesAgreeOnValues` |
| M5 | S1, 5E `.field` | `compileIndex` does not return the error from its left side | yes, e2e | `TestFieldErrorsMatchIndexErrors`, `TestEnginesAgreeOnCorpus`; e2e `TestP82Accept`, `TestP83SpawnNetAttenuation` |
| M6 | S1 | `compileHash` evaluates each value before its key | yes, e2e | `TestHashLiteralEvaluatesInSourceOrder`, `TestEnginesAgreeOnCorpus`; e2e `TestP84StepsExactOnBothEngines` |
| M7 | S1 | `compileCall` evaluates the arguments before the function | yes, e2e | `TestEnginesAgreeOnCorpus`, `FuzzEnginesAgree` seeds; e2e `TestP84StepsExactOnBothEngines` |
| M8 | S1 | `compileMatch` runs the other arm | yes, e2e | `TestMatch`, `TestCompiledProgramsNeverWalk`, `TestLogsCrossEngines`; e2e `TestP84EnginesAgreeOnValues` |
| M9 | S1 | `compileFor` runs the body in the enclosing environment, not the iteration's | yes, e2e | `TestEnginesAgreeOnCorpus`, `TestEnginesObserveCancellationPromptly`; e2e `TestForIn`, `TestP84TimeoutStopsCompiledCode` |
| M10 | S1, 5E `<=` `>=` `!=` | `compilePrefix`: `!x` returns `x` | yes, e2e | `TestBangOperator`, `TestAndOr`, `TestEnginesAgreeProperty`; e2e `TestPEAndOr` |
| M11 | S1, I7 (S5) | a string literal's compiled code returns one shared value | yes | `TestStringLiteralsAreNewEachTime` (unit only) |
| M12 | S1 | `value.Int(1023)` returns the integer 0 (the last shared integer not initialised) | yes, e2e | `TestIntSharesSmallIntegers`, `TestAllocLimit`; e2e `TestP84EnginesAgreeOnValues`, `TestBudgetAlloc` |
| M14 | S1 | `Environment.latest` ignores names past the first four (the map) | yes, e2e | `TestEnvironmentSlotsAndMapMeanTheSame`; e2e `TestLanguageBasics`, `TestP84EnginesAgreeOnValues` |
| M15 | I8 | `Environment.Set` on an inline slot drops the previous binding (`prev`) | yes, e2e | `TestEnvironmentFrozenAtSpawn`, `TestChildNeverSeesALaterRebinding`, `TestEnvironmentSlotsAndMapMeanTheSame`; e2e `TestPhase4Invariants`, `TestSupervision` |
| M16 | I8 | `Environment.Set` past four names drops the previous binding | yes | `TestEnvironmentSlotsAndMapMeanTheSame`, `TestEnvironmentSlotsAreLocked` (unit only) |
| M17 | I8 | a compiled identifier is looked up with no task (no spawn horizon) | yes, e2e | `TestChildNeverSeesALaterRebinding`, `TestChildCannotSeeANameBoundAfterSpawn`; e2e `TestPhase4Invariants` |
| M18 | what is compiled | `compileFunction` leaves `Code` nil, so every call walks | yes | `TestFunctionsCarryTheirEngine`, `TestCompiledProgramsNeverWalk`, `TestDefaultEngine` (unit only) |
| B1 | S2, S3 | `enter` does not tick compiled nodes | yes, e2e | `TestEnginesAgreeOnCorpus`, `TestRunLimits`; e2e `TestBudgetSteps`, `TestP84StepsExactOnBothEngines`; endless tests hang to the timeout |
| B2 | S2 | an infix chain's spine ticks one node fewer (`len(spine) - 2`) | yes, e2e | `TestEnginesAgreeOnCorpus`, `TestEnginesAgreeProperty`; e2e `TestP84StepsExactOnBothEngines` |
| B3 | S2 | compiled `let` does not charge its binding | yes | `TestAllocCountsClosuresEnvsAndBindings`, `TestInterpolationCostsWhatConcatenationCosts`, `TestEnginesAgreeOnCorpus` (unit only) |
| B4 | S2 | a compiled function literal is not charged | yes | `TestEnginesAgreeOnCorpus`, `TestEnginesAgreeProperty` (unit only) |
| B5 | S2 | a compiled array literal is charged one element fewer | yes | `TestEnginesAgreeOnCorpus`, `TestInterpolationErrorPositions` (unit only) |
| B6 | S2 | a compiled hash literal is charged per key written (`len(keys)`), not per pair kept (`len(h.Pairs)`) | **no** | none: no test has a hash literal with a repeated key. A probe on a scratch copy, `sameOnBothEngines` on `{1: 1, 1: 2}`, failed with `bytes: tree-walker 96, compiled 144`, and passes unmutated |
| B7 | S2, 5E interpolation | compiled concatenation is not charged before it is built | yes, e2e | `TestConcatOverLimitBuildsNothing`, `TestInterpolationCostsWhatConcatenationCosts`; e2e `TestPEInterpolationOutOfBytes` |
| B8 | S2 | `enter` ticks a compiled node after running it, not before | yes, e2e | `TestEnginesAgreeOnCorpus`, `TestLogsCrossEngines`; e2e `TestP84StepsExactOnBothEngines`, `TestP84StepsCutReplaysOnTheOtherEngine` |
| B9 | S2, S4 | a compiled function's body runs `c.run`, skipping `enter` (no tick, no depth) | yes, e2e | `TestEnginesAgreeOnCorpus`, `TestRunDepthBoundsDeepTasksTogether`; e2e `TestP84StepsExactOnBothEngines`, `TestBudgetFlags` |
| B10 | S2 (both engines) | `forEach` does not charge each iteration's environment | yes | `TestAllocSizes`, `TestAllocCountsClosuresEnvsAndBindings`, `TestForDoesNotPreallocate` (unit only) |
| B11 | S2 | a compiled `for` body runs `body.run`, skipping `enter` | yes, e2e | `TestEnginesAgreeOnCorpus`, `TestLogsCrossEngines`; e2e `TestP84StepsExactOnBothEngines` |
| C1 | S3, I5 | compiled nodes ignore a cancelled `tick` (steps still counted) | yes, e2e | `TestLogsCrossEngines`; `TestP84NoGoroutineOutlivesRun` hangs to the timeout; e2e `TestP84TimeoutStopsCompiledCode`, `TestP84ScopeTimeoutStopsCompiledCode`, `TestFailFastCPUSibling` |
| C2 | S3 (both engines) | `Task.Tick` checks the context every 1 048 576 steps, not 256 | yes | `TestTickSeesCancellation`; `TestResumedLiveFailureWaitsForALoggedDeadline` hangs to the timeout. The e2e timeout tests passed |
| C3 | I6, I5 | compiled `scope` runs its body without `openScope` (no options, no supervision) | yes, e2e | `TestNestedLimitsOnlyShrinkProperty`, `TestEnginesAgreeProperty`; e2e `TestBudgetSteps`, `TestFailedScopeIsAnError`, `TestSupervision` |
| D1 | S4 (both engines) | `MaxEvalDepth` check `>=` becomes `>` | yes, e2e only | e2e `TestP84EnginesAgreeOnErrors/eval_depth` (message position moved) |
| D2 | S4 | compiled nodes never reserve depth from the run's pool | yes | `TestRunDepthBoundsDeepTasksTogether` (unit only) |
| L1 | I7, S2 (logged checks) | compiled `if` calls `in.Task.Tick()` once more after its condition (the steps budget is unchanged; only the task's check count moves) | **no** | none, in the full run and in 6 more runs of `TestLogsCrossEngines`: its programs whose checks are logged (**fail-fast**, **fail-fast dense**, **timed**) have no `if`, `&&` or `\|\|` in their loops |
| L2 | I7 | a compiled call to an effect builtin calls `Fn` directly, not through the gate | yes, e2e | `TestEffectsAreReplayed`, `TestLogsCrossEngines`, `TestEnginesAgreeOnCorpus`; e2e `TestP84ReplayOnTheOtherEngine`, `TestP6PropRecordReplay` |
| A1 | I1 | `Task.Require` allows everything | yes, e2e | `TestChildIDsAndAuthority`, `TestSpawnedAgentHoldsOnlyItsGrants`; e2e `TestP84EnginesAgreeOnErrors/I1_no_ambient_authority`, `TestNetPorts` |
| K1 | I8 (memory safety) | `Environment.GetAs` reads without `mu.RLock` | yes (`-race`) | `go test -race ./arv/value/ ./arv/evaluator/`: `TestEnvironmentSlotsAreLocked`, `TestTasksRunInParallel`, 16 `DATA RACE` reports. Without `-race` it is not observable; e2e was not run with a race-built binary |

(M13 was dropped before the run: it was a duplicate of M15.)

## The two mutants not caught

**B6: bytes of a hash literal with a repeated key.** `evalHashLiteral` and `compileHash` charge `hashPairSize` per pair kept. The parser accepts `{1: 1, 1: 2}`, which keeps one pair. Charging per key written passes the whole suite. Exact-bytes fitting (`fit`, `eFit`, `sameOnBothEngines`) would catch it if a corpus program, a `sgen` program or an e2e program had a repeated key. None has one.

**L1: a task check that only one engine adds.** S2 says that `runtime.Task.Tick`'s count, and so every logged check, falls on the same step on both engines. The steps budget is counted in `Interpreter.tick`. So a mutant that calls `Task.Tick` without counting a step changes no budget, no output and no corpus result. Only a log recorded on one engine and read on the other can catch it. `TestLogsCrossEngines` catches drifts in calls and `for` (round 2). Its cancelled loops have no `if`, so a drift in `IfExpression`, which is also how `&&` and `||` run, goes unseen. The same holds for any node type not in those loops (prefix, index, hash, match).

Both are test gaps. The runtime has no bug: on the real code the engines agree on these cases (the B6 probe passes unmutated). This round added no tests.

## Round 2 (2026-11-12, at 3940a9f)

Re-check of B6 and L1 after the fixer added `TestEnginesChargeHashPairsKept`, `TestEnginesTickAlike` (`arv/evaluator/engine_tick_test.go`) and two repeated-key corpus programs. Each mutant was run against `./arv/evaluator/`, `./arv/` (L1) and `./e2e/`, then restored with `git checkout -- <file>`; `git status` was clean before the next. **Both caught.**

| ID | Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|---|
| B6 | S2 | `compileHash` returns `in.alloc(h, hashPairSize*int64(len(keys)))` | **yes** (unit) | `TestEnginesChargeHashPairsKept`: `{1: 1, 1: 2}`, tree-walker 48 bytes, compiled 96. `TestEnginesAgreeOnCorpus` and `FuzzEnginesAgreeSource` seeds 57, 58: 144 vs 288 and 200 vs 296. e2e: green (none) |
| L1 | I7, S2 | `compileIf`, after the condition: `if e := in.Task.Tick(); e != nil { return newError(...) }` | **yes** (unit) | `TestEnginesTickAlike`: cancelled run stops at 41:7 on the tree-walker, 37:5 compiled. `./arv/` (incl. `TestLogsCrossEngines`) and e2e: green (none) |
| L1b | I7, S2 | as L1, but the result is dropped (`_ = in.Task.Tick()`): a pure count drift | **yes** (unit) | `TestEnginesTickAlike`, same positions. e2e: green (none) |

The e2e suite still sees neither: no e2e program has a repeated hash key or compares cancellation positions across engines. The unit tests close both round 1 gaps.
