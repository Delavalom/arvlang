# A7 and A8: compiling is cancellable and charged; the quiet-deadline flake

Branch `fix/compile-cancel-and-flake`. Roadmap items A7 and A8 ([00-roadmap.md](../00-roadmap.md) §1). Machine: Apple M1 Pro (8 cores), Go 1.26.2. The machine was shared with other agents throughout: load averages of 60 to 650 during these runs.

## A7, part 1: compiling is cancellable

**Before.** `compile(node)` took no context. A run cancelled, or past its deadline, while its program compiled returned only after the whole compile (16p84, known limits). Measured here with a 4 MB program: 373 ms after the cancel, and 415 ms after the start for a 20 ms timeout.

**What changed.**

- `compile` is now a method of a small `compiler` (`arv/evaluator/compile.go`). It counts the nodes it compiles. Every `pollEvery` (1024) nodes it looks at its context. Once the context is done, it panics with `abandoned{}`, and `compileCtx` recovers that panic and reports `ok == false`. Any other panic goes on up, as before.
- `start` (`engine.go`) compiles with `in.Task.Ctx`, the root scope's context, so it holds both the host's cancel and the run's timeout. **When the compile is abandoned, the run goes on on the tree-walker.** The meaning is the same (Phase 8.4, S1). Its first check (`runtime.Task.Tick`, at step 256) sees the cancellation and ends the run with the error a cancelled run gives today.
- Why the tree-walker, and not an error returned at once: a logged run numbers its checks (`Task.observe`). A check made during compile would add a check that the other engine never makes, and a replay or resume on that engine would diverge. A resume can also start with a context whose deadline has already passed on the wall clock while the log has not seen it yet. In that case, falling back changes only the speed. An error returned at once would end a run that the log says went on.
- The other compile call sites only gained the `cc.` receiver. No compile case changed what it builds.

**After.** The 4 MB program returns 6 to 18 ms after the cancel, and 23 to 27 ms after the start with a 20 ms timeout (`TestCompileIsCancellable`, load about 380). The roadmap's case, measured with a one-off probe at load 385 (17.8 MB, 5 156 583 nodes, 200 000 uncalled functions):

| Run | Time |
|---|---|
| Not cancelled (compile and run) | 5.91 s |
| Cancelled 100 ms after the start | returned 8.5 ms after the cancel |
| Context already cancelled | 0.14 ms |
| `Limits{Timeout: 1ms}` | 5.7 ms |

## A7, part 2: the program's code is charged

**Design.** Both engines charge the program's code to `alloc_mb` as the run starts: `codeNodeSize` (64) bytes per AST node. They charge the same amount at the same point (`RunLimited`, inside the root scope, before `start`, before any step), so they agree on bytes (S2). This holds although only one engine compiles.

- **Per node, not per byte compiled.** What `compile` allocates depends on the engine and on Go. A node count depends only on the program. Measured compile cost: 58 to 82 bytes per node, and the AST takes 91 to 110 bytes per node (darwin/arm64, Go 1.26). So 64 covers the closures of a typical program and leaves the AST uncharged, as before: the host parsed the AST before the run's budget existed.
- **Per node, not per token.** A first try charged per token. It broke `TestDesugaringEquivalence*`, `TestAndOrCostWhatIfCosts` and `TestFieldCostsWhatIndexCosts`: sugar must cost exactly what its expansion costs, and the expansion has more tokens. The parser builds sugar as the nodes of its expansion (`a && b` is an `IfExpression`; `"x${y}"` is `"x" + $str(y)`; `h.k` is `h["k"]`), so the two forms have the same node count. `TestSugarHasTheSizeOfItsExpansion` holds this.
- **Counted once, by the parser.** `ast.Size` (new, `arv/ast/size.go`) counts the nodes that `arv/check`'s walk visits, with an explicit stack. `ParseProgram` stores the count in `ast.Program.Nodes`. A run costs no extra walk, and counting needs no cancellation: it is part of the parse. `check` imports `evaluator`, so the evaluator cannot reuse `check`'s walk. `TestSizeCountsWhatWalkVisits` keeps the two equal over every node type, the examples and every form of syntax.
- A program not made by the parser (`Nodes` 0) is not charged, and nor is a node that is not a program. A program whose code does not fit fails before its first step with `budget exceeded: alloc (limit N bytes)` (kind `budget`), placed like any root-scope error at the last statement.
- The 17.8 MB program above is charged 330 MB (18.5 bytes per source byte).

**Tests that pin runtime bytes.** The evaluator's unit tests (`fit`, `sameCost`, through `runLimitedEnv`) state what evaluating a program allocates. That helper now sets `Nodes = 0`, so these tests keep their meaning. Before this, seven of them failed by exactly the charge. `runEngine`, behind `TestEnginesAgreeOnCorpus`, the property tests and `FuzzEnginesAgree*`, keeps the charge and adds it to any byte limit it is given. Tight limits (40, 400 and 600 bytes) still stop a program partway through its run, and a charge made on only one engine shows as a difference there (checked: mutant M6 fails `TestEnginesAgreeOnCorpus`).

## A8: TestResumedQuietChildSeesItsDeadline

**Cause.** The failure in 16zg was `recording: <nil>, "q\nok\n"`: w's four calibrated `spin()`s finished inside the 200 ms scope. `spin()` is calibrated once per test binary, so a calibration made while the machine was busier gives a shorter spin later.

**Fix.** A hook in the source, `hold(ms)` (`arv/spin_test.go`). It does pure work, ten `unit()`s at a time, so the task makes the checks `Task.Tick` makes. It stops once `clock_ms()` has moved `ms` past its first reading. w now runs `hold(1000)` instead of four spins. When recording, w cannot reach `write_file` before the deadline: `write_file`'s gate (`liveErr`, `expireDue`) sees a passed deadline even if its timer has not fired. The recording's premise therefore holds in every run, however fast or loaded the machine is. x's tail (eight spins) is unchanged, so the bug the test guards stays reachable. The sibling `TestResumedQuietChecksSeeDeadlines` raced the same way (four spins in the scope's body) and got the same change.

**Rejected alternatives.**

- `testing/synctest`: `synctest.Test` is Go 1.25, while `go.mod` says `go 1.24.0`, and `vet`'s stdversion check would flag it. In a bubble CPU work takes no fake time, so a spin could never outlast a deadline. The test's gated HTTP server is also outside the bubble.
- A timer seam in `runtime`: it would change runtime code, which this item did not need.

**Two more `spinSrc` tests fixed.** The first required `go test -race ./arv/... ./cmd/...` failed (load about 300) on two calibrated-spin tests, both premise misses: `TestResumeWaitForFinishedTasksTakesNoTime` ("c did not finish before the root's sleep ended": four spins took longer than 4 s) and `TestResumeReplaysAFailedAttemptToItsEnd` (`recording: <nil>, "budget exceeded: timeout (limit 1500 ms)\n"`: three attempts of one spin each took longer than 1500 ms). Both were already known flakes (16p84).

- In the first, c now runs `hold(1200)`, so its tail stays about 1.2 s and ends before the 4 s sleep. The premise finds c's `done` record at any seq, since `hold` adds `clock_ms` records.
- In the second, the attempt's last stretch must stay pure work, so `hold`, which ends on an effect, does not fit. The stretch is now a fixed `unit(); unit()`: about 20 000 steps and 78 checks, with no calibration. The calibrated `spin()` it replaces is about 300 ms, often 1000 units or more.
- Both passed `-count=3`, with and without `-race`, at load 341.

**Other `spinSrc` users, not changed.** `hold` would add `clock_ms` records to the task that spins, and these tests cut logs at, count, or rely on that task's records. Or their spin has no deadline to race.

| Test | File | What the spin must do |
|---|---|---|
| `TestResumedTaskBehindALaterSpawnDoesNotSeeTheDeadlineYet` | `resume_behind_test.go` | q, behind the log, must stay in its spin and under a 2000 ms timeout; already retries a missed premise 3 times |
| `TestResumeOpensAScopeAtTheOpenersNextRecord` | `resume_open_test.go` | the root spins between a record and the scope, which opens at the next record |
| `TestHostCancelledReplayIsNotADivergence` | `replay_round3_test.go` | f spins before failing; no timeout |
| `orderedPairSrc` users | `replay_budget_test.go` | `first` spins before its ask; the order is gated |
| `engineLogPrograms` | `engine_log_test.go` | loops that a timed scope or a failure ends; retries 3 times |

## Test commands and results

All runs were on the branch, with load averages of 300 to 520 from other agents on the machine.

| Command | Result |
|---|---|
| `go vet ./...` | clean |
| `go test -race -count=1 -timeout 40m ./arv/... ./cmd/...` | first run: `arv` failed on the two spin tests fixed above, and every other package passed. Re-run after the fix: all `ok` (`arv` 588 s) |
| `go test -count=1 -timeout 30m ./e2e/` | `ok` (97 s) |
| `go test -count=1 -tags arv_treewalk -timeout 40m ./...` | all `ok`; re-run after the fix: all `ok` |
| `ARV_E2E_TAGS=arv_treewalk go test -count=1 -timeout 30m ./e2e/` | `ok` (66 s) |

The e2e runs came before the spin-test fix, which changed only `arv` package tests.

**The flake run.** The test binary was built once (`go test -c -o arv.test ./arv/`). It ran under 16 CPU burners (`for i in $(seq 16); do (yes > /dev/null &); done`, twice the 8 cores), on a machine already loaded by other agents:

```
arv.test -test.run '^TestResumedQuietChildSeesItsDeadline$' -test.count=500 -test.timeout=2h -test.v
```

Result: **500 passed, 0 failed**, from 13:58 to 15:09, with the load average rising from 102 to 651. Each run took 7 to 20 s. At that pace the 2 h timeout looked tight, so a second process ran concurrently with `-test.count=250` from 14:13. It was stopped after 184 passes and 0 failures, once the first process had finished. In total: 684 runs, no failure. The burners were then killed (`pkill yes`).

## Fuzz

`go test -run '^$' -fuzz '^FuzzEnginesAgreeSource$' -fuzztime 120s -timeout 10m ./arv/evaluator/` (load 348): 121 s, 392 915 execs, 126 new interesting (553 in the corpus), no failure.

## Mutation check

Each mutant was applied by hand, one at a time, run against the tests named, and restored with `git checkout`. The first round left M2 and M4 alive. Two tests were added (`TestBigProgramsRunCompiled`, `TestCompileCtxRepanicsOtherPanics`), and then both were caught. Totals: 14 tried, 14 caught.

| # | Mutant | Caught by |
|---|---|---|
| M1 | compile never polls | `TestCompileIsCancellable` (walk=false, cancel and deadline) |
| M2 | poll inverted: abandon a live compile | `TestBigProgramsRunCompiled` (added; survived before) |
| M3 | an abandoned compile returns the cancel error at once, not the tree-walker | `TestCompileCancelledRunLogsAndReplays` (no check 1 in the log) |
| M4 | `compileCtx` swallows panics that are not its own | `TestCompileCtxRepanicsOtherPanics` (added; survived before) |
| M5 | charge 0 bytes per node | `TestCodeIsChargedPerNodeOnBothEngines`, `TestCodeThatDoesNotFitFailsFirst` |
| M6 | charge only on the compiled engine | the two above, and `TestEnginesAgreeOnCorpus` |
| M7 | charge after the run, not before | `TestCodeThatDoesNotFitFailsFirst` (puts printed) |
| M8 | `ast.Size` skips hash values | `TestSizeCountsNodes`, `TestSizeCountsWhatWalkVisits` |
| M9 | `ast.Size` counts a missing child | the two above |
| M10 | the parser does not set `Nodes` | `TestCodeIsCharged…`, `TestCodeThatDoesNotFit…`, `TestHandBuiltProgramsAreNotCharged`, `TestSizeCountsNodes` |
| M11 | `pollEvery` = 2^30 | `TestCompileIsCancellable` |
| M12 | `hold` ends at once (`>` for `<`) | `TestResumedQuietChildSeesItsDeadline` |
| M13 | compile polls `context.Background()` | `TestCompileIsCancellable` |
| M14 | `codeNodeSize` 63 | `TestRunChargesTheProgramsCode` |

Also tried on runtime code this branch did not change: make quiet checks never see deadlines (`visible` returns `slices.Contains(seen, cause)` only). `TestResumedQuietChecksSeeDeadlines` catches it ("b\nlate\n"). `TestResumedQuietChildSeesItsDeadline` does not catch it, before this branch (2 runs) or after (1 run). Why not was not investigated; it is not a change this branch made.

## Known limits

- **Parsing is still not cancellable.** The 17.8 MB program takes seconds to parse at this load. The host chose the source, and `arv run --timeout` starts its clock after the parse, as before.
- After an abandoned compile, the tree-walker runs up to 255 nodes before its first check, which is what a compiled run did after its compile. An effect among them still checks the context at its gate.
- The charge is an estimate per node, not the bytes the closures take. The AST itself is still not charged.
- Bytes now include the code. A log recorded by an older binary, whose run ended at an `alloc_mb` limit, can end at a different point when replayed by this one. Runs without `alloc_mb` are unaffected. No log-format promise exists yet (Phase 15).
- `TestCompileIsCancellable` has a 50 ms bound and three tries per case, because a machine under heavy load can delay any goroutine. Before A7, every try took the whole compile (373 ms and more).
- When resuming, the quiet-deadline tests rely on the resumed deadline's timer firing less than 800 ms late (`hold(1000)` against 200 ms). A quiet check sees a deadline only once its timer has fired.
- The five `spinSrc` users in the last table still use the calibrated spin.
