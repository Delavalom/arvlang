# Phase 8.4: speed (as built)

Built from `p84-plan-1.md` to `p84-plan-9.md`; their decisions and deviations from section 8.4 of `15-phase8-tooling.md` are in `p84-plan-1.md`. Review round 1 (fixes and dispositions): [`16p84b-speed-round1.md`](16p84b-speed-round1.md).

## Baseline (before any change)

Commit: b516ca2 plus the speed programs (`arv/speed_bench_test.go`), `make bench-cpu` (`-count 5`), medians. Machine: Apple M1 Pro, Go 1.26.2. Load (`uptime`) before: `load averages: 17.51 16.72 43.67`; after: `load averages: 10.14 14.80 41.27`. Raw output kept in `/tmp/p84-base.txt` for p84.9.

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| calls | 10 791 411 | 8 326 539 | 186 199 |
| loops | 3 522 759 | 4 270 827 | 81 735 |
| hashes | 8 670 156 | 10 673 402 | 151 782 |
| ergonomic | 8 473 549 | 5 198 740 | 143 418 |
| closures | 5 495 614 | 7 515 446 | 102 475 |
| match | 11 322 282 | 12 677 398 | 191 784 |
| strings | 2 956 604 | 4 195 692 | 61 938 |

Bytes and allocs per op match the plan's scratch-copy numbers exactly; times are within 5% of them.

**Profile** (`go test -run '^$' -bench 'BenchmarkSpeed/(calls|loops)' -cpuprofile`, samples under the interpreter): about half the time is allocation (an `Environment` and its map per call and per loop iteration, a `*value.Integer` per arithmetic result, argument slices grown by `append`); the environments' read locks about 9%; the per-node checks in `Eval` about 17%; the rest is dispatch. On macOS most samples land in `runtime.kevent` and the scheduler (the profiler's signal delivery), so read the shares within `(*Interpreter).Eval`, not of the total.

Re-measured on this tree (`-benchtime 3s`, `pprof -focus '(*Interpreter).Eval$'`, 1.81 s of samples under `Eval` out of 10.2 s): `runtime.mallocgc` 0.73 s (40%), plus `growslice` 0.11 s; `Environment.Set` 0.38 s (of which `maps.growToSmall` 0.25 s, the first `Set` growing each new map); `NewEnclosedEnvironment` 0.22 s; `Environment.GetAs` 0.29 s; `tick` 0.11 s. The `RWMutex` lock and unlock calls showed about 0.10 s (about 5%) in this sample, less than the plan's 9%; the ranking (allocation first, then environment maps, then per-node checks) is the same.

## TestHostDeadlineIsNotLogged

Its program now loops 1000 times per call before it recurses, so the 20 ms host deadline ends the run long before the 10 000-call depth limit on any engine. Before, a quiet machine reached the depth limit first (the baseline flake in `16za-phase6-mutations.md`), and Phase 8.4's faster code made that certain. What the test checks did not change.

Reproduced on the base before the change (load about 17): `go test ./arv/ -run TestHostDeadlineIsNotLogged -count=5` failed with an `"end"` line holding `stack overflow: call depth exceeded 10000`. After: `-count=5` and `-race -count=3` both `ok`.

## After (p84.2 to p84.8)

Commit: 819d212, the head of `phase/p84` before this notes commit. Machine as for the baseline.

The baseline above and a lone `make bench-cpu` ran minutes apart under different loads on a shared machine, and the spread inside one 5-run set (hashes after: 7.7 to 12.0 ms) is larger than some of the gains. So the table compares the two engines **interleaved**: test binaries built from 97e3bff (the baseline) and 819d212, run one after the other (`-test.bench BenchmarkSpeed -test.benchmem -test.count 1`), 10 rounds each in two blocks of five (base first, then after first), lower median of the 10. Load (`uptime`) at the start of each block: `45.35 43.08 32.86` and `9.73 25.56 27.52`; at the end: `14.94 31.61 29.66` and `12.99 20.78 25.28`. Bytes and allocs per op are exact (one alloc of run-to-run variation).

| Program | ns/op before | ns/op after | speed-up | B/op before | B/op after | allocs/op before | allocs/op after |
|---|---|---|---|---|---|---|---|
| calls | 12 857 698 | 7 624 094 | 1.69 | 8 326 539 | 4 563 214 | 186 199 | 65 878 |
| loops | 3 679 978 | 2 215 307 | 1.66 | 4 270 827 | 2 524 765 | 81 735 | 34 788 |
| hashes | 9 194 930 | 7 230 199 | 1.27 | 10 673 402 | 8 903 341 | 151 782 | 101 753 |
| ergonomic | 9 079 883 | 7 096 462 | 1.28 | 5 198 740 | 3 598 349 | 143 418 | 108 353 |
| closures | 5 988 383 | 3 626 415 | 1.65 | 7 515 446 | 4 532 485 | 102 475 | 52 245 |
| match | 12 330 904 | 9 908 521 | 1.24 | 12 677 398 | 9 706 043 | 191 784 | 141 750 |
| strings | 3 188 886 | 2 241 905 | 1.42 | 4 195 692 | 2 744 798 | 61 938 | 41 880 |

1.24x to 1.69x, and about a third of the allocations of `calls` (186 199 to 65 878 per op). Allocations and bytes are the plan's scratch-copy numbers exactly.

The plan's protocol, `make bench-cpu` (`-count 5`, medians) compared with the baseline's `/tmp/p84-base.txt`, raw output in `/tmp/p84-after.txt`: at load `13.09 30.39 29.25` (after: `10.32 25.94 27.66`) it gave calls 7.44 ms (1.45x), loops 2.25 ms (1.57x), hashes 8.58 ms (1.01x), ergonomic 7.57 ms (1.12x), closures 3.82 ms (1.44x), match 10.69 ms (1.06x), strings 2.34 ms (1.27x). A first run at load 61 was slower than the baseline on four programs and is not used. Neither run is a fair comparison with a baseline taken at load 17; the interleaved one is.

In the scratch copy (load about 7-10, `make bench-cpu`): 1.3x to 1.7x. Of the gain, by the scratch copy's per-task runs: closures alone about 1.1x (p84.3 to p84.5), environments without maps about 1.2x (p84.7), shared integers and sized slices the rest (p84.8). Not re-measured per task here.

## What was built

- **Two engines, one meaning.** `RunLimited` compiles the program (`compile`, `compile_ops.go`, `compile_flow.go`) into one `code` per node, then runs it with `exec`. Every node, on either engine, goes through `enter` (the old body of `Eval`): depth check, run-pool reservation, `tick`, release, error stamp. An infix chain's spine is flattened once and ticked as `evalInfixChain` ticks it. `for`, `match` and `scope` share `forEach`, `matchArm` and `openScope` with the tree-walker. `spawn`, `await` and scope options run the tree-walker's case (`walked`).
- **Functions carry their code.** `value.Function.Code` is the compiled body when compiled code made the function, so spawned tasks, restarts, host callbacks, agent tools and later REPL lines run it compiled; a function made by the tree-walker has none and its calls walk.
- **Choosing the engine.** Compiled by default. `evaluator.SetTreeWalk` switches it for tests; the build tag `arv_treewalk` makes the tree-walker the default (`make test-walk`, and e2e with `ARV_E2E_TAGS=arv_treewalk`). No CLI flag or `Options` field.
- **Allocation.** `value.Environment` keeps four names in place before it makes a map; `value.Int` shares -128 to 1023; argument and element slices are made at their exact size. These serve both engines.

## Deviations from 15-phase8-tooling.md, 8.4

Listed in `p84-plan-1.md`: run although the gate said CPU does not matter (the owner's call); 1.2x to 1.7x rather than "usually 2-5x"; no bytecode VM; `spawn`, `await` and scope options not compiled; the tree-walker changed in representation (shared helpers, environments, integers), not in meaning.

## Known limits

- **The gain is modest.** Each node still pays for its depth check, its tick (two counters) and its error check, which S2 to S4 require, and most values are still allocated. What is left, by the scratch profile: the environments' read locks (about 9%), hash literals (a Go map each), `match`'s error hash, and the per-node checks (about 17%). Further steps, not taken: resolving names to slots at compile time (`let` in a block binds into the enclosing call's environment, and host bindings and builtins are looked up by name, so it needs care), lock-free reads of environments a task owns, and a bytecode VM. The gate measured CPU at about 4% of wall time on the work Arv targets.
- **`spawn`, `await` and scope options run on the tree-walker**, and so does what is written inside them: a function literal in a `spawn` argument is a tree-walker function, and its calls walk. The meaning is the same; only speed differs.
- **Compiled code is not charged to `alloc_mb`** (Minor under the alloc_mb rule): like the AST it comes from, it is bounded by the program's own size and is built once, before the run starts. Measured: a 158 000-byte program allocated 5.70 MB to parse and 3.59 MB to compile (0.63 of the parse; under `-race` 6.33 MB and 3.59 MB, 0.57). `TestCompileAllocatesLessThanParsing` keeps it under 1.5 times the parse.
- **Compiling is not cancellable** (Minor; review round 1, two findings). `RunLimited` compiles the whole program, the bodies of functions never called included, inside the root scope and before the first tick. `compile` looks at neither `t.Ctx` nor the timeout, so a run whose context is already cancelled, or whose timeout is shorter than the compile, returns only after parse plus compile; the tree-walker returns after the parse. The delay is linear in the source, like the parse, and the host chose the program, so a script cannot grow it. Measured by the reviewers: a 4.7 MB program, `arv run --timeout 1ms`, took 0.75-0.95 s compiled and 0.54-0.58 s on the tree-walker (peak RSS 310 MB and 200 MB). A 17 MB program of 200 000 uncalled functions, already-cancelled context, took 1.30-1.84 s compiled and 0.83-0.97 s on the tree-walker, and the same with `Limits{Timeout: 1ms}`. Not fixed: polling the context every N nodes would need a context threaded through every `compile` case, and compiling function bodies on first call would need a lock on each function literal's code, which spawned tasks share. Either is a further step if programs of megabytes appear.
- **Timing tests.** Two Phase 6 resume tests built on `spinSrc` (calibrated once per test binary) fail under load on the tree-walker and pass when re-run alone: `TestResumeReplaysAFailedAttemptToItsEnd` and `TestResumedTaskBehindTheLogDoesNotSeeTheDeadlineYet` (review round 1: both failed together in `go test -count=1 -tags arv_treewalk ./arv/...` at load 55 with `recording: <nil>, "budget exceeded: timeout (limit 1500 ms)\n"`; `-count=3` of the two alone passed). The first also failed once in the scratch copy when the load average jumped to 790 in the middle of `go test -race -tags arv_treewalk ./...`. They flaked before this phase; it is not an engine difference. In this run (load 10 to 60) all three suites passed first time. The final pre-commit `go test -race ./...` (load 67) failed once in e2e: `TestPEStepsMatchTheHandWrittenForm/&&` saw exit code 66 with empty output, next to `ThreadSanitizer: CHECK failed: tsan_rtl.cpp:94`. The `arv` binary is not race-built (no `ARV_E2E_RACE`) and has no exit code 66, while 66 is TSan's exit code: the race runtime of the test process failed in the forked child before `exec`. Re-running `go test -race -count=1 ./e2e/` and that test with `-count=3` gave `ok`. A race-runtime flake on darwin under load, not an engine difference.

## Tests added

| Guarantee | Tests |
|---|---|
| S1 same meaning | `TestEnginesAgreeOnCorpus` (57 programs, 7 limits each), `TestEnginesAgreeProperty` (200 random programs), `FuzzEnginesAgree`, `TestEnginesAgreeOnSpeedPrograms`; the whole suite on both engines (`make test`, `make test-walk`) |
| S2 exact budgets | the same tests, each program fitted for its exact steps and bytes on both engines (`fit`, `eFit`) |
| S3 cancellation, leaks | `TestEnginesObserveCancellationPromptly` (timeout and host cancel, root and four tasks, goroutines counted) |
| S4 depth | corpus programs that hit `MaxDepth` (10 000 calls) and `MaxEvalDepth` (100 000 levels), compared message, kind and position |
| S5 the log | `TestStringLiteralsAreNewEachTime`, `TestSharedIntegersAreIndistinguishable`, `TestIntSharesSmallIntegers` |
| what is compiled | `TestNodesAreCompiled`, `TestFunctionsCarryTheirEngine` |
| environments | `TestEnvironmentSlotsAndMapMeanTheSame`, `TestEnvironmentSlotsAreLocked`; the existing `environment_test.go` |
| memory | `TestCompileAllocatesLessThanParsing` |
| logs across engines (round 1) | `TestLogsCrossEngines`: record on one engine, replay and resume (three cuts) on the other, both ways |
| the default engine (round 1) | `TestDefaultEngine`, in both builds |
| children compiled (round 1) | `TestCompiledProgramsNeverWalk` (`Interpreter.walks`) |

Fuzz: `go test -run '^$' -fuzz FuzzEnginesAgree -fuzztime 60s ./arv/` on 819d212 plus the memory test: 16 172 execs in 61 s, 322 corpus entries (46 new), no failure.
