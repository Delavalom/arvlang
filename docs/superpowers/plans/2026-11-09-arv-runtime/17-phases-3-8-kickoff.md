# Kickoff prompt: Phases 3–8 in one multi-agent run

Paste everything below the line into a new Claude Code session opened in `~/delavalom-labs/arvlang`. Before you paste:
- Raise "Dynamic workflow size" in `/config`. This run needs more than 10 agents.
- Expect a long, expensive run.
- Check that disk has more than 10 GB free (`df -h /System/Volumes/Data`).

---

Use a workflow, with as many parallel subagents as the dependency graph allows, to implement Phases 3–8 of the Arv runtime plan. Verify everything mercilessly, and build e2e coverage of the whole implementation.

## Where things stand
- **Repo:** `~/delavalom-labs/arvlang`, a Go module (`github.com/delavalom/arvlang`).
- **Phases 0–2 are done** on branch `feat/runtime`, which has an open PR to `main`.
  - **Capabilities:** `net`, `fs_read`, `fs_write` and `env`, granted with `--allow`, narrowed per task with `spawn f() with [caps]`, and checked at use through `CapSet.Admit`.
  - **Structured concurrency:** `scope {}` gives fail-fast scopes and `await` is restricted to the spawning task or its descendants. Panics are recovered, and recursion depth is bounded.
  - **Tests:** a hermetic black-box suite in `e2e/` runs the real binary against a local httptest server, with gates such as `/wait`, `/arrive`, `/slow` and `/mark`. `ARV_E2E_RACE=1` builds the binary with `-race`.
- **Start from `main` if the PR has merged; otherwise branch from `feat/runtime`.** Create the integration branch `feat/phases-3-8` either way.
- **Read these before planning anything** (all under `docs/superpowers/plans/2026-11-09-arv-runtime/`):
  - `00-index.md` and `01-design.md`: the invariants I1–I8, the target syntax and the package map.
  - `16-implementation-notes.md`: what was actually built and how it differs from plans 02–09, plus follow-through items for each later phase. **It overrides the older plan text wherever they conflict.**
  - `10-phase3-budgets.md` through `15-phase8-tooling.md`: design plus a task outline for each phase. None of them is bite-sized yet.

## Dependency graph (verify it against the plan files, then use it)
```
Wave 1: Phase 3 budgets  ||  Phase 8.3 network hardening (+ go.mod bump to >=1.24, os.Root for fs, net ports per 16-notes)
Wave 2: Phase 4 supervision + errors as values + match   (needs 3)
        Move I8 "freeze environments at spawn" into Phase 4 (see 16-notes)
Wave 3: Phase 5 GATE   (needs 3+4)  -> STOP POINT, see below
Wave 4: Phase 6 replay  ||  Phase 7 agents (needs 3+4)  ||  8.1 `arv check`  ||  8.2 custom host effects
Wave 5: 6 x 7 integration (agent runs replayable), 8.4 speed ONLY if the gate said CPU matters, 8.5 distribution LAST
        (moving main.go to cmd/arv breaks the e2e build target, Makefile and README: fix them in the same change)
Skip 8.6 editor support and 8.7 Wasm unless everything else is green.
```

## The Phase 5 gate is a real stop point
- Measure the scenario in `12-phase5-gate.md` against a Go baseline, using the decision criteria written there. The criteria are fixed: ≤50% of the Go version's LoC, ≤1.2× its wall time, and the key-stealing probe blocked.
- Write the measurements and the verdict to `docs/.../gate-report.md`, with the raw numbers and the commands to reproduce them.
- **If any criterion fails, do not start Wave 4.** Stop and report. The plan says a failed gate means pivoting to a Go library (`arvrt`), not adding more syntax, and that decision is mine.
- Don't tune the criteria or the scenario after measuring.

## How to run each phase (one pipeline per phase, phases in parallel inside a wave)
1. **Expand the plan.** A planner agent turns the phase's design and outline into a bite-sized plan in the same style as files 02–09: exact files, test first, the expected failure, the implementation, the expected pass, and a commit per task.
   - Every code block must be compiled and tested in a scratch copy before it goes into the plan.
   - Each plan file must stay under 18 KB; split it if needed.
2. **Implement.** A fresh implementer works on each task, in its own git worktree off the integration branch, using TDD. Each implementer runs `gofmt -l .`, `go vet ./...` and `go test -race ./...` before committing.
3. **Spec review.** A separate agent compares the code with the plan line by line and reports anything missing or extra.
4. **Adversarial review.** A separate agent tries to break the phase's guarantees. It writes a probe for every claim and runs it under `-race`. Every finding must include a reproduction.
5. **Fix loop.**
   - The implementer fixes the findings, then a *new* reviewer re-checks. Repeat until there are zero Critical and zero Important findings.
   - Minor findings are either fixed or written into `16-implementation-notes.md` as known limits.
6. **E2E for the phase.** Add cases to `e2e/` (for example `e2e/phase3_test.go`) that drive the real binary and cover every acceptance test in the phase file, plus every invariant the phase touches.
7. **Mutation check.** For each guarantee, deliberately break the runtime (remove the check, return early, skip the cancellation), run the suite, confirm a test fails, and restore. Record each mutation and the test that caught it in the phase's section of `16-implementation-notes.md`. A mutation that no test catches is an Important finding.
8. **Merge** the phase into `feat/phases-3-8`, then rerun the whole suite.

## Merciless verification: required for every phase
- `go test -race -count=5 ./...` stays green, and `ARV_E2E_RACE=1 go test -race ./e2e/` stays green.
- **Fuzzing:** add `go test -fuzz` targets wherever input is untrusted, and run each for at least 2 minutes. That means the lexer and parser, `runtime.Canonical`/`Admit`, net scope parsing, the Phase 6 value codec and log reader, and Phase 7 tool-call decoding.
- **Property tests:**
  - Budgets only shrink (I6).
  - Attenuation never grants more (I2).
  - Replay of a recorded run produces byte-identical output and effect results (I7).
  - Supervision never drops an error silently (`collect`).
- **Differential test:** for Phase 6, record → replay → resume. A run killed partway with SIGKILL and then resumed must give the same final result as an uninterrupted run.
- **Phase 7:**
  - All tests use a fake provider.
  - If the model asks for a tool the task doesn't hold, the model gets a "capability denied" tool result and the program keeps running.
  - Exceeding the token or `usd_cents` budget is a clean error.
  - Secrets never enter Arv: grep the logs and errors for the key.
  - A single live test against the Anthropic Messages API is allowed only behind an env flag and is skipped by default. Load the `claude-api` skill before writing any Anthropic code.
- **Timing tests** must survive load averages above 300. Use server-side ordering and gates, and keep wall-clock bounds generous.
- **Leak checks:** no goroutine survives `arv.Run`. Assert this in tests, for example by comparing `runtime.NumGoroutine` before and after, with a settle loop.

## Final whole-branch verification (after the last wave)
- **Three independent reviewers**, each given the code but not the others' conclusions:
  1. Security: capabilities, SSRF, fs, secrets, replay tampering.
  2. Concurrency: races, leaks, deadlocks, cancellation, supervision.
  3. Spec and plan: everything in files 10–15 plus 16-notes was built or explicitly deferred.
- Every Critical and Important finding gets a fix, a regression test and a re-check by a fresh reviewer.
- **Rerun every e2e case from Phases 0–2 unchanged.** Any changed expectation needs a written reason in `16-implementation-notes.md`.
- **Report:**
  - what was built, phase by phase;
  - the gate verdict with numbers;
  - the mutation table;
  - fuzzing time per target and anything it found;
  - known limits;
  - test counts and runtimes.

## Rules (from earlier sessions; don't relearn them)
- **File size:** a global pre-commit hook blocks any staged file over about 20 KB (5k tokens). Keep Go files under 15 KB and split test files by moving code. Never use `--no-verify`.
- **Commits:** commit on branches as you go. End each message with the session's `Co-Authored-By` trailer.
- **Pushing:** don't push or open PRs until the final report. Then push `feat/phases-3-8` and open one PR into the base branch, with the gate report linked in the PR body.
- **Disk:** free space on this Mac changes over time. Check `df -h /System/Volumes/Data` before long runs. Below 1 GB, pause and report. The only thing you may delete is the Go build cache (`go clean -cache`).
- **Rate limits:** if the 5-hour limit stops an agent, resume that same agent with SendMessage after the reset. Don't start a duplicate.
- **Worktrees:** each implementer must work in its own worktree. Merges into the integration branch happen one at a time, and the full suite runs after each merge.
- **Before using APIs:** `go.mod` is 1.22.5 until Wave 1 bumps it. Check the Go version before using `os.Root`, `t.Chdir`, `testing/synctest`, or `sync.WaitGroup.Go`.
- **Bugs and failures:** don't weaken a test to make it pass. If a test shows a real bug outside the current task, mark it `t.Skip("BUG: …")` with a reproduction and report it.
- **Scope:** keep `runtime` stdlib-only. Secrets stay in the Go host; Arv code never sees an API key.
