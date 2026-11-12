# Phase 8.4, task p84.9: after numbers, compile memory, both suites, notes

> **For agentic workers:** this file is one task, p84.9. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p84.8 is committed on `phase/p84` in `/Users/delavalom/delavalom-labs/arvlang-wt/p84`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race` on the packages it touches. Overview, guarantees S1-S6, decisions and deviations: `p84-plan-1.md`.

## Task p84.9: after numbers, compile memory, both suites, notes

The last task measures what p84.2 to p84.8 bought, pins the one new uncharged allocation (compiled code) with a test, runs everything on both engines, and finishes the notes and the README.

**Files:**
- Create (test): `arv/evaluator/engine_memory_test.go`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16p84-speed.md`, `README.md`

- [ ] **Step 1: Pin what compiling allocates**

Compiled code is not charged to `alloc_mb`, like the AST: both are bounded by the program's size and built before it runs. Under the alloc_mb rule that is a Minor known limit, recorded with a number. This test passes as soon as it is written: it pins a bound p84.3 to p84.5 already meet, so a later change that makes compiled code allocate more than 1.5 times the parse fails it.

Create `arv/evaluator/engine_memory_test.go`:

```go
package evaluator

import (
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
)

// Compiled code is not charged to alloc_mb, like the AST it is made from:
// both are bounded by the program's size, and compile allocates less than
// the parser does (about 0.6 times in the scratch copy; a known limit in
// 16p84-speed.md). This keeps it that way: at most 1.5 times.
func TestCompileAllocatesLessThanParsing(t *testing.T) {
	src := strings.Repeat(`let f = fn(a, b) { if (a < b) { [a, b, {"k": a + b * 2}] } else { f(b, a) } }; `, 2000)
	var m0, m1, m2 goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&m0)
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	goruntime.ReadMemStats(&m1)
	c := compile(program)
	goruntime.ReadMemStats(&m2)
	if len(p.Errors()) > 0 || c.walks {
		t.Fatalf("parse errors %v, walks %v", p.Errors(), c.walks)
	}
	parse, comp := m1.TotalAlloc-m0.TotalAlloc, m2.TotalAlloc-m1.TotalAlloc
	t.Logf("source %d bytes: parse %d, compile %d bytes (%.2f of parse)", len(src), parse, comp, float64(comp)/float64(parse))
	if comp > parse*3/2 {
		t.Errorf("compile allocated %d bytes, parse %d: more than 1.5 times", comp, parse)
	}
}
```

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/evaluator/ -run TestCompileAllocatesLessThanParsing -v -count=1 2>&1 | grep -E 'source|ok|FAIL'`
Expected: a line like `source 158000 bytes: parse 5710096, compile 3585120 bytes (0.63 of parse)`, then `ok`. Keep the numbers for the notes.

- [ ] **Step 2: Measure after**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && uptime && make bench-cpu 2>&1 | tee /tmp/p84-after.txt && uptime`
Expected: 35 benchmark lines. In the scratch copy the medians were about: calls 6.8 ms, 3.5-4.6 MB, 65878 allocs; loops 2.0 ms, 34788 allocs; hashes 6.3 ms, 101753 allocs; ergonomic 6.6 ms, 108353 allocs; closures 3.3 ms, 52245 allocs; match 8.7 ms, 141749 allocs; strings 2.0 ms, 41880 allocs. Allocs per op are exact; compare times with `/tmp/p84-base.txt` from p84.1 (if that file is gone, run `make bench-cpu` on a checkout of the p84.1 commit, `git worktree add /tmp/p84-base <p84.1 commit>`, and remove that worktree afterwards).

- [ ] **Step 3: Run everything on both engines**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && df -h /System/Volumes/Data && gofmt -l . && go vet ./... && go test -race ./... && make test-walk`
Expected: more than 1 GB free, no gofmt output, vet clean, every package `ok` in all three suites (compiled, `-tags arv_treewalk`, and e2e on the tree-walker). A spin-calibrated Phase 6 timing test may fail when the load jumps mid-run (see p84.6); re-run it alone with `-count=3`.

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race -count=1 ./e2e/ && go test -run '^$' -fuzz FuzzEnginesAgree -fuzztime 60s ./arv/`
Expected: `ok`; the fuzzer reports no failure (record its exec count in the notes).

- [ ] **Step 4: Finish the notes**

Append to `docs/superpowers/plans/2026-11-09-arv-runtime/16p84-speed.md`, filling the table from `/tmp/p84-base.txt` and `/tmp/p84-after.txt` (medians; speed-up is before / after, two digits), the two `uptime` lines, and the commit:

```markdown

## After (p84.2 to p84.8)

Commit: the head of `phase/p84` before this notes commit, `make bench-cpu` (`-count 5`), medians. Load (`uptime`) before: ...; after: ...

| Program | ns/op before | ns/op after | speed-up | allocs/op before | allocs/op after |
|---|---|---|---|---|---|
| calls | | | | | |
| loops | | | | | |
| hashes | | | | | |
| ergonomic | | | | | |
| closures | | | | | |
| match | | | | | |
| strings | | | | | |

In the scratch copy (load about 7-10): 1.3x to 1.7x, and about a third of the allocations of `calls` (186199 to 65878 per op). Of the gain, closures alone gave about 1.1x (p84.3 to p84.5), environments without maps about 1.2x (p84.7), shared integers and sized slices the rest (p84.8).

## What was built

- **Two engines, one meaning.** `RunLimited` compiles the program (`compile`, `compile_ops.go`, `compile_flow.go`) into one `code` per node, then runs it with `exec`. Every node, on either engine, goes through `enter` (the old body of `Eval`): depth check, run-pool reservation, `tick`, release, error stamp. An infix chain's spine is flattened once and ticked as `evalInfixChain` ticks it. `for`, `match` and `scope` share `forEach`, `matchArm` and `openScope` with the tree-walker. `spawn`, `await` and scope options run the tree-walker's case (`walked`).
- **Functions carry their code.** `value.Function.Code` is the compiled body when compiled code made the function, so spawned tasks, restarts, host callbacks, agent tools and later REPL lines run it compiled; a function made by the tree-walker has none and its calls walk.
- **Choosing the engine.** Compiled by default. `evaluator.SetTreeWalk` switches it for tests; the build tag `arv_treewalk` makes the tree-walker the default (`make test-walk`, and e2e with `ARV_E2E_TAGS=arv_treewalk`). No CLI flag or `Options` field.
- **Allocation.** `value.Environment` keeps four names in place before it makes a map; `value.Int` shares -128 to 1023; argument and element slices are made at their exact size. These serve both engines.

## Deviations from 15-phase8-tooling.md, 8.4

Listed in `p84-plan-1.md`: run although the gate said CPU does not matter (the owner's call); 1.3x to 1.7x rather than "usually 2-5x"; no bytecode VM; `spawn`, `await` and scope options not compiled; the tree-walker changed in representation (shared helpers, environments, integers), not in meaning.

## Known limits

- **The gain is modest.** Each node still pays for its depth check, its tick (two counters) and its error check, which S2 to S4 require, and most values are still allocated. What is left, by the scratch profile: the environments' read locks (about 9%), hash literals (a Go map each), `match`'s error hash, and the per-node checks (about 17%). Further steps, not taken: resolving names to slots at compile time (`let` in a block binds into the enclosing call's environment, and host bindings and builtins are looked up by name, so it needs care), lock-free reads of environments a task owns, and a bytecode VM. The gate measured CPU at about 4% of wall time on the work Arv targets.
- **`spawn`, `await` and scope options run on the tree-walker**, and so does what is written inside them: a function literal in a `spawn` argument is a tree-walker function, and its calls walk. The meaning is the same; only speed differs.
- **Compiled code is not charged to `alloc_mb`** (Minor under the alloc_mb rule): like the AST it comes from, it is bounded by the program's own size and is built once, before the run starts. Measured: a 158 000-byte program allocated 5.7 MB to parse and 3.6 MB to compile (0.63 of the parse; 0.57 under `-race`). `TestCompileAllocatesLessThanParsing` keeps it under 1.5 times the parse.
- **Timing tests.** `TestResumeReplaysAFailedAttemptToItsEnd` (Phase 6, spin-calibrated) failed once in the scratch copy, on the tree-walker, when the load average jumped to 790 in the middle of `go test -race -tags arv_treewalk ./...`; it passed when re-run alone. It is a pre-existing load-sensitive test, not an engine difference.

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
```

Add a last line under "Tests added": the fuzz time and exec count from step 3. If the memory numbers of step 1 differ from those above, use yours.

- [ ] **Step 5: README**

In `README.md`, replace:

```bash
make e2e     # black-box suite that drives the real binary
```

with:

```bash
make e2e     # black-box suite that drives the real binary
make test-walk  # the same suites on the tree-walker, the reference engine
make bench-cpu  # CPU-bound speed programs (Phase 8.4)
```

and after that code block (its closing fence), add the paragraph:

```markdown

Programs run as Go closures, compiled from the syntax tree once per run. The tree-walking interpreter they are compiled from stays as the reference: the test suite runs on both (`make test-walk`), and they must agree on every value, error, position, output, step and byte.
```

- [ ] **Step 6: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./... && go test -race ./arv/evaluator/ && wc -c docs/superpowers/plans/2026-11-09-arv-runtime/16p84-speed.md`
Expected: no gofmt output, vet clean, `ok`, and the notes under 18 000 bytes (if not, move "Tests added" to a new `16p84b-speed-tests.md` and link it).

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/evaluator/engine_memory_test.go README.md docs/superpowers/plans/2026-11-09-arv-runtime/16p84-speed.md && git commit -m "docs(notes): Phase 8.4 numbers before and after, known limits; README

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
