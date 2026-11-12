# Phase 8.4: speed (as built)

Built from `p84-plan-1.md` to `p84-plan-9.md`; their decisions and deviations from section 8.4 of `15-phase8-tooling.md` are in `p84-plan-1.md`.

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
