# Phase 8.4, task p84.1: speed programs, the baseline, and a timing flake

> **For agentic workers:** this file is one task, p84.1. Do it exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your worktree `/Users/delavalom/delavalom-labs/arvlang-wt/p84` is on branch `phase/p84`, branched from `feat/phases-3-8` at b516ca2, with only the `p84-plan-*.md` files added. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block in `p84-plan-1.md` to `p84-plan-9.md` was applied, in task order, to a scratch copy of the tree and passed `gofmt -l`, `go vet ./...` and `go test -race` on the packages it touches after each task, and the whole suite (`go test -race ./...`, and the same with `-tags arv_treewalk`, e2e included) after the last. Design: section 8.4 of `15-phase8-tooling.md`, and the owner's decision under criterion 6 in `gate-report.md` ("Phase 8.4 (speed): run it").

## Phase 8.4 overview

**Goal:** run CPU-bound Arv faster with no change in meaning. The gate does not say CPU matters (2.31x Go's CPU on the I/O-bound gate job, limit 5x); the owner chose to do the work anyway, so the plan profiles CPU-bound programs first and targets what the profile shows.

**What the profile showed** (base b516ca2, the speed programs of this task, darwin/arm64, Go 1.26.2; `-cpuprofile`, samples under `(*Interpreter).Eval`): about half of the interpreter's time is allocation. Every call and every loop iteration made an `Environment` with its own Go map (`maps.growToSmall` on the first `Set` alone was about a third of `fib`'s time), every arithmetic result a new `*value.Integer`, and every argument list grew by `append`. The read lock of each environment visited (`sync.RWMutex`) was about 9%. The per-node bookkeeping in `Eval` (depth checks, `tick`, the error check) was about 17%, and the type switch in `eval` the rest of the dispatch. Base numbers: `calls` (fib 20) 10.7 ms/op, 8.3 MB/op, 186k allocs/op; `loops` (100 x 100) 3.4 ms/op.

**How:**
1. Compile the AST to Go closures once per run (design 8.4): one `code` per node whose `run` does what `eval`'s case for that node does, with the children already compiled. The tree-walker stays as the reference, and every test can run on both engines.
2. Cut the allocations the profile names, in code both engines share: environments keep their first four names in place instead of a map, small integers are shared, argument slices are made at their exact size.

**Measured in the scratch copy** (same machine, load average about 7-10, medians of 3): `calls` 10.7 to 6.8 ms, `loops` 3.4 to 2.0, `hashes` 8.3 to 6.3, `ergonomic` 8.5 to 6.6, `closures` 5.5 to 3.3, `match` 11.1 to 8.7, `strings` 2.9 to 2.0: 1.3x to 1.7x. Closures alone gave about 1.1x; most of the gain is allocation. Your own before and after numbers go in the notes (p84.1 and p84.9).

**Guarantees (each task's tests name the ones they cover):**
- S1. Same meaning: every program gives the same value or error (message, kind, position, task) and the same output on both engines. The whole suite passes on both: `make test` (compiled) and `make test-walk` (tree-walker, e2e included).
- S2. Budgets stay exact: each program is charged the same steps and bytes on both engines. Every node is ticked once, in the same order (an infix chain's spine nodes too), so `runtime.Task.Tick`'s count, and with it every logged check of a recorded run, falls on the same step.
- S3. Cancellation is observed as promptly: compiled code ticks per node through the same `Task.Tick`; a timeout and a host's cancel end an endless program on both engines, and no goroutine outlives `Run`.
- S4. Depth limits are the same: every compiled node goes through `enter`, the function `Eval` uses, so `MaxEvalDepth`, the run's depth pool and `MaxDepth` trip at the same place.
- S5. The effect log is the same: a string literal is a new value each time it is evaluated (the codec numbers long strings by identity); only integers are shared, and the codec never numbers integers.
- S6. `arv/runtime` is not touched (it stays stdlib-only); no secret moves.

| Task | File | What |
|---|---|---|
| p84.1 | `p84-plan-1.md` | speed programs and `make bench-cpu`; baseline in the notes; `TestHostDeadlineIsNotLogged` made independent of speed |
| p84.2 | `p84-plan-2.md` | the engine seam: `enter`, `code`, `SetTreeWalk`, the `arv_treewalk` tag, the differential corpus |
| p84.3 | `p84-plan-3.md` | compile statements, literals, names, `if`, `let`, `return`, prefix |
| p84.4 | `p84-plan-4.md` | compile infix chains, functions (`value.Function.Code`), calls, arrays, index, hashes |
| p84.5 | `p84-plan-5.md` | compile `for`, `match`, `scope` (shared helpers `forEach`, `matchArm`, `openScope`) |
| p84.6 | `p84-plan-6.md` | random-program property and fuzz test across engines; cancellation and leak test |
| p84.7 | `p84-plan-7.md` | environments keep four names in place, no map |
| p84.8 | `p84-plan-8.md` | shared small integers; exact-size argument slices |
| p84.9 | `p84-plan-9.md` | after numbers, compile memory, `make test-walk`, notes, README |

### Decisions, and why

1. **Compiled once per run, no global cache.** `RunLimited` compiles the program before it runs. A cache keyed by AST node would keep every program a long-lived host ever ran alive. A function made by compiled code carries its compiled body in a new field, `value.Function.Code` (type `any`: `value` cannot import `evaluator`), so a spawned task, a restart, a host callback, an agent tool and a later REPL line all run it compiled, with no table to pass around.
2. **Every compiled node goes through `enter`**, the body `Eval` had, split out: the `MaxEvalDepth` check, the run-pool reservation, `tick`, the release, and the error stamp at the node's position. Steps, depth errors, cancellation checks and positions cannot drift between engines because there is one copy of that code.
3. **Shared helpers where the meaning is more than evaluating children:** the loop of `for` (`forEach`), the choice of a `match` arm (`matchArm`) and opening a scope (`openScope`) are split out of the tree-walker and called by both engines. Only how a child is evaluated differs.
4. **`spawn`, `await` and scope options stay on the tree-walker** (`walked`): they run once per task or per wait, not per step of a loop. Their children run on the tree-walker too, which means the same; a function literal written inside a `spawn` argument is a tree-walker function.
5. **Choosing the engine:** `evaluator.SetTreeWalk` (an atomic, for tests that compare engines in one process) and the build tag `arv_treewalk` (the default flips, so the whole suite runs on the reference: `make test-walk`; e2e builds its binaries with `ARV_E2E_TAGS`). No CLI flag and no `Options` field: the engines mean the same, so a host has nothing to choose.
6. **The allocation work is in shared code** (`value.Environment`, `value.Int`, `evalExpressions`), so the tree-walker gets it too. It changes representation, not meaning, and each change has its own tests.
7. **The notes go in a new file, `16p84-speed.md`**, with a pointer in `16o-phase-index.md`. `16-implementation-notes.md` (16 KB) is not edited.
8. **`TestHostDeadlineIsNotLogged` is rewritten here, first.** It recursed until a 20 ms host deadline, and on a quiet machine the 10 000-call depth limit came first (recorded as a baseline flake in `16za-phase6-mutations.md`). Faster code makes that failure certain without `-race`. Its program now loops 1000 times per call, so the deadline comes first on any engine. What it checks is unchanged.

### Deviations from the design (15-phase8-tooling.md, 8.4)

- "Only if the gate said CPU matters": it did not; the owner chose to run it (gate-report.md).
- "Usually 2-5x": measured 1.3x to 1.7x. The closures remove the type switch, but each node still pays for its depth check, its tick and its allocations, which the guarantees require. The design's next step, a bytecode VM "only if closures aren't enough", is not taken: the gate shows CPU at about 4% of wall time on the work Arv targets. The notes (p84.9) record what a further step would need.
- Not every node type is compiled (decision 4).
- The tree-walker changes too (decisions 3 and 6), in representation, not meaning.

## Task p84.1: speed programs, the baseline, and a timing flake

**Files:**
- Create (test): `arv/speed_bench_test.go`
- Modify (test): `arv/replay_hostdeadline_test.go`
- Modify: `Makefile`
- Create: `docs/superpowers/plans/2026-11-09-arv-runtime/16p84-speed.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase-index.md`

- [ ] **Step 1: Write the speed programs**

Create `arv/speed_bench_test.go` with the Write tool (it holds `${`):

```go
package arv

import (
	"strconv"
	"strings"
	"testing"
)

// hundred is the Arv array literal [0, 1, ..., 99].
var hundred = func() string {
	parts := make([]string, 100)
	for i := range parts {
		parts[i] = strconv.Itoa(i)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}()

// speedPrograms are the CPU-bound programs Phase 8.4 measures, one per
// kind of work the interpreter does: calls, loops, hashes, closures,
// match, string building, and the Phase 5E forms (.field, interpolation,
// &&, ||, <=, >=) in "ergonomic". Each takes 2 to 12 ms on the base
// (Apple M1 Pro) and needs no grant.
var speedPrograms = []struct{ name, src string }{
	{"calls", `let fib = fn(n) { if (n < 2) { n } else { fib(n - 1) + fib(n - 2) } }; fib(20)`},
	{"loops", `let a = ` + hundred + `; let s = for x in a { for y in a { x * y + 1 } }; len(s)`},
	{"hashes", `let a = ` + hundred + `; let h = {"a": 1, "b": 2}; let r = for x in a { for y in a { h["a"] + h["b"] + {"k": y}["k"] } }; len(r)`},
	{"ergonomic", `let a = ` + hundred + `; let h = {"a": 1, "b": 2}; let r = for x in a { for y in a { if (x <= y && y >= 50 || x == 3) { "${h.a}:${y}" } else { h.b } } }; len(r)`},
	{"closures", `let a = ` + hundred + `; let add = fn(n) { fn(m) { n + m } }; let r = for x in a { let f = add(x); for y in a { f(y) } }; len(r)`},
	{"match", `let a = ` + hundred + `; let r = for x in a { for y in a { match (if (y < 50) { error("e") } else { y }) { ok(v) => v, err(e) => e.kind } } }; len(r)`},
	{"strings", `let a = ` + hundred + `; let r = for x in a { let s = ""; for y in a { s + "ab" } }; len(r)`},
}

// BenchmarkSpeed runs each speed program through Run, parse included.
func BenchmarkSpeed(b *testing.B) {
	for _, p := range speedPrograms {
		b.Run(p.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Run(p.src, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestSpeedProgramsRun keeps the benchmark programs valid: each one runs
// without an error, so a benchmark never measures an early failure.
func TestSpeedProgramsRun(t *testing.T) {
	for _, p := range speedPrograms {
		if _, err := Run(p.src, Options{}); err != nil {
			t.Errorf("%s: %v", p.name, err)
		}
	}
}
```

In `Makefile`, replace:

```make
.PHONY: build test e2e bench-test bench escape
```

with:

```make
.PHONY: build test e2e bench-test bench escape bench-cpu
```

and append at the end of the file:

```make

# Phase 8.4: the CPU-bound speed programs (arv/speed_bench_test.go).
bench-cpu:
	go test -run '^$$' -bench BenchmarkSpeed -benchmem -count 5 ./arv/
```

- [ ] **Step 2: Check them and record the baseline**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/ -run TestSpeedProgramsRun -count=1`
Expected: `ok`.

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && uptime && make bench-cpu 2>&1 | tee /tmp/p84-base.txt && uptime`
Expected: 35 benchmark lines (7 programs, 5 runs each). In the scratch copy the medians were about: calls 10.7 ms, 8.3 MB, 186199 allocs; loops 3.4 ms, 4.3 MB, 81735; hashes 8.3 ms, 10.7 MB, 151782; ergonomic 8.5 ms, 5.2 MB, 143418; closures 5.5 ms, 7.5 MB, 102475; match 11.1 ms, 12.7 MB, 191784; strings 2.9 ms, 4.2 MB, 61938. Bytes and allocs per op are exact and should match; times depend on the load. Keep `/tmp/p84-base.txt` for p84.9.

- [ ] **Step 3: Show the timing flake**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/ -run TestHostDeadlineIsNotLogged -count=5`
Expected on a lightly loaded machine: FAIL, `the host's deadline is in the log` with an `"end"` line holding `stack overflow: call depth exceeded 10000` (the flake in `16za-phase6-mutations.md`). Under heavy load it may pass; go on either way.

- [ ] **Step 4: Make the test independent of speed**

In `arv/replay_hostdeadline_test.go`, replace:

```go
// of it, which replays its records, does not wait on anything.
func TestHostDeadlineIsNotLogged(t *testing.T) {
	src := `let f = fn(i) { len("x"); f(i + 1) }; f(0)`
```

with:

```go
// of it, which replays its records, does not wait on anything.
//
// Each call loops over 1000 elements before it recurses, so the 10 000
// calls of the depth limit take seconds on any engine, and the 20 ms
// deadline ends the run first. (Before Phase 8.4 each call only
// recursed: on a quiet machine the depth limit ended the run first, and
// its logged end failed the test; see 16za-phase6-mutations.md.)
func TestHostDeadlineIsNotLogged(t *testing.T) {
	src := "let a = [" + strings.TrimSuffix(strings.Repeat("0, ", 1000), ", ") + "];\n" +
		`let f = fn(i) { for x in a { len("x") }; f(i + 1) }; f(0)`
```

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/ -run TestHostDeadlineIsNotLogged -count=5 && go test -race ./arv/ -run TestHostDeadlineIsNotLogged -count=3`
Expected: `ok` twice.

- [ ] **Step 5: Start the notes**

Create `docs/superpowers/plans/2026-11-09-arv-runtime/16p84-speed.md` with this content, the table filled with the medians (of the 5 runs) from `/tmp/p84-base.txt`, and the two `uptime` lines:

```markdown
# Phase 8.4: speed (as built)

Built from `p84-plan-1.md` to `p84-plan-9.md`; their decisions and deviations from section 8.4 of `15-phase8-tooling.md` are in `p84-plan-1.md`.

## Baseline (before any change)

Commit: b516ca2 plus the speed programs (`arv/speed_bench_test.go`), `make bench-cpu` (`-count 5`), medians. Machine: Apple M1 Pro, Go 1.26.2. Load (`uptime`) before: ...; after: ...

| Program | ns/op | B/op | allocs/op |
|---|---|---|---|
| calls | | | |
| loops | | | |
| hashes | | | |
| ergonomic | | | |
| closures | | | |
| match | | | |
| strings | | | |

**Profile** (`go test -run '^$' -bench 'BenchmarkSpeed/(calls|loops)' -cpuprofile`, samples under the interpreter): about half the time is allocation (an `Environment` and its map per call and per loop iteration, a `*value.Integer` per arithmetic result, argument slices grown by `append`); the environments' read locks about 9%; the per-node checks in `Eval` about 17%; the rest is dispatch. On macOS most samples land in `runtime.kevent` and the scheduler (the profiler's signal delivery), so read the shares within `(*Interpreter).Eval`, not of the total.

## TestHostDeadlineIsNotLogged

Its program now loops 1000 times per call before it recurses, so the 20 ms host deadline ends the run long before the 10 000-call depth limit on any engine. Before, a quiet machine reached the depth limit first (the baseline flake in `16za-phase6-mutations.md`), and Phase 8.4's faster code made that certain. What the test checks did not change.
```

In `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase-index.md`, append at the end:

```markdown

## Phase 8.4: speed

Notes, numbers before and after, deviations and known limits: [`16p84-speed.md`](16p84-speed.md). Plans: `p84-plan-1.md` to `p84-plan-9.md`.
```

- [ ] **Step 6: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./... && go test -race ./...`
Expected: no gofmt output, vet clean, every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/speed_bench_test.go arv/replay_hostdeadline_test.go Makefile docs/superpowers/plans/2026-11-09-arv-runtime/16p84-speed.md docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase-index.md && git commit -m "bench(arv): Phase 8.4 speed programs and baseline; deadline test independent of speed

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
