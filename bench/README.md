# Benchmarks

The method and the plan are in [`03-benchmarks.md`](../docs/superpowers/plans/2026-11-15-arv-next/03-benchmarks.md). This file holds the rules, and what each script does today.

## The rules (03-benchmarks.md §B0)

1. **Load.** Don't start a run when the 1-minute load average is above `MAX_LOAD`: 1.0 on Linux, 3.0 on macOS. A run that ends above it is marked INVALID and exits 3. `bench/lib.sh` enforces this for every script. Close other work first (the Phase 8.4 numbers were taken at loads of 10 to 61).
2. **Hardware.**
   - Machine A is the Apple M1 Pro the gate used.
   - Machine B is a dedicated bare-metal Linux x86-64 host, with its CPU frequency pinned and turbo off (`cpupower frequency-set -g performance`, or [perflock](https://go.dev/wiki/Benchmarks)).
   - Name the machine with `BENCH_HOST`, so result directories say where they came from.
3. **Repetitions.** At least 10 runs per configuration, interleaved: A then B, then B then A. Never all of A, then all of B. Test binaries are built once, before the timed loop.
4. **Statistics.**
   - Report benchstat's median and 95% confidence interval. Never report the best of N.
   - A difference counts only when benchstat gives p < 0.05. Claims in docs and release notes need p < 0.01.
   - benchstat cannot reach p < 0.05 with fewer than about 6 samples a side.
5. **Warmup.** `cpu.sh` throws away `WARMUP` calls of each test binary (default 1), then runs one benchmark iteration per call. Check the raw files for a trend before trusting a result. CLI timings discard 3 warmup runs (`hyperfine --warmup 3`).
6. **Records.**
   - Every run writes `bench/results/<date>-<host>-<commit>-<suite>/`, holding:
     - `env.txt`: date, OS, CPU, core count, Go version, commit, a dirty flag, the load limit, and the loads before and after;
     - the raw output;
     - the comparison.
   - Commit result directories that back a claim. A number with no directory behind it is not a result.

## Scripts

| Script | Measures | Run |
|---|---|---|
| `cpu.sh` | `BenchmarkSpeed` (7 CPU-bound programs, `arv/speed_bench_test.go`), compiled engine against the tree-walker, or this tree against `BASE` | `bench/cpu.sh`; `BASE=origin/main bench/cpu.sh`; `COUNT=20 BENCH_FILTER='calls$' bench/cpu.sh` |
| `startup.sh` | `arv version`, `arv run` of an empty and a one-line program, `arv check examples/uptime.arv`, with hyperfine | `bench/startup.sh` (needs `hyperfine`) |
| `run.sh` | The Phase 5 gate: `examples/uptime.arv` against the Go baseline on the local farm | `make bench` (results in `bench/out/`, kept as the gate defined them) |
| `escape/probe.sh` | The Phase 5 escape probe | `make escape` |

`harness_test.go` checks the load rule and the result layout of `cpu.sh`.

## Not built yet

These are in 03-benchmarks.md, in order:
- B1: Are We Fast Yet ports;
- B3: memory;
- B4: recording, replay and resume overhead;
- B5: budget and capability overhead;
- B6: agent workloads against other stacks;
- B7: the CI regression gate (a duet run of base against head on each PR, plus a nightly run on machine B).
