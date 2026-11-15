# Phase 6x7, task p67.1: `TestHostDeadlineIsNotLogged` independent of machine speed

> **For agentic workers:** this file is one task, p67.1. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your worktree is `phase/p67`, branched from `feat/phases-3-8` (b516ca2), with nothing of Phase 6x7 in it yet. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

## Phase 6x7 overview

**Goal:** agent runs are replayable. `ask`, `agent`, the effects of an agent's tools and a Go host's own effects (Phase 8.2) go through the effect gate and are logged; `--replay` of an agent run makes no provider call and reproduces its output byte for byte; `--resume` continues an interrupted agent run. Every test uses the fake provider (`llm.Fake`) or a local stand-in for the Messages API.

**Where the base already is.** Phase 6 review round 3 (`16u-phase6-round3.md`) merged Phases 7 and 8.2 into Phase 6 and routed them through the gate: each `ask` or `agent` logs `llm_model` (the host's model ID and price) and one `llm` record per call (the whole request; the reply or the error); a host effect logs `host:<name>` (`logHost`). Rounds 4 and 5 (`16v`, `16w`) made a resume inside a model call look its provider up, and refuse a forged `llm_model`. So this phase is mostly tests of the guarantees end to end, and they found two real gaps, fixed here test first:

1. **A replayed spawn loses the host's kinds** (p67.2). `loggedCaps` checks a logged grant with `runtime.Literal`, which knows only the built-in kinds, so every replay or resume of a run that does `spawn ... with [k8s("staging")]` diverges. `examples/host` does exactly that spawn.
2. **Model budgets do not replay under concurrency** (p67.3, p67.4). Whether a call's reservation fits a shared `tokens` or `usd_cents` pool, or its settlement past the reservation trips the limit, depends on which task got there first. A replay runs tasks in its own order: two agents racing for one budget diverged 5 times in 5.

**Guarantees, and the tasks that test them:**
- A replay of an agent run is byte-identical and makes zero provider calls and zero tool effects (p67.5 in process, p67.7 through the binary).
- Recorded logs never contain the API key (p67.7: every Phase 7 way of getting the key out, recorded and replayed; p67.8 checks the logs of killed and resumed runs).
- Resume after SIGKILL mid-agent-run equals an uninterrupted run (p67.6: a resume after every line of an agent run's log; p67.8: SIGKILL in a model call and in a tool, through the binary).
- Budgets on replay match the original (p67.3, p67.4), and a resume's live calls see what the replayed part spent (p67.3).
- Host effects record, replay without calling `Fn`, and resume (p67.2, p67.9, p67.10).

**Invariants touched:** I2 (a replayed spawn's host grants are still checked against the parent: p67.2), I6 (budgets replay exactly), I7 (model-budget outcomes become logged effects), and the leak rule (p67.6 checks goroutines after every resume).

| Task | File | What |
|---|---|---|
| p67.1 | `p67-plan-1.md` | `TestHostDeadlineIsNotLogged` independent of machine speed |
| p67.2 | `p67-plan-2.md` | a replayed spawn keeps the run's host kinds |
| p67.3 | `p67-plan-3.md` | model budget reservations are logged (`llm_reserve`) |
| p67.4 | `p67-plan-4.md` | settlements past a reservation are logged (`llm_settle`) |
| p67.5 | `p67-plan-5.md` | a replay of an agent run runs no tool effect |
| p67.6 | `p67-plan-6.md` | an agent run resumed after any line of its log |
| p67.7 | `p67-plan-7.md` | e2e: offline replay of an agent run; no log holds the key |
| p67.8 | `p67-plan-8.md` | e2e: resume after SIGKILL in a model call or a tool |
| p67.9 | `p67-plan-9.md` | e2e: `p82host` records, replays and resumes |
| p67.10 | `p67-plan-10.md` | `examples/host` records and replays |
| p67.11 | `p67-plan-11.md` | README, `16zb-p67-notes.md`, seams closed, final checks |

### Deviations from the design files, and why

1. **Design 14, task 8 ("`ask` and `agent` turns go through `effect()`") was already done** by Phase 6 review round 3, as two effects per call (`llm_model`, `llm`), not one. This phase adds the tests the design asks for.
2. **Model-budget outcomes are effects, not checks.** Phase 6 logs other scheduling-dependent outcomes (a cancellation, the live-task cap) as checks, which hold only a kind. A refused reservation must replay its exact error (which limit), which an effect's result carries through the value codec. So under a `tokens` or `usd_cents` limit, with a log, every call adds an `llm_reserve` record (about 144 bytes), and a call that used more than it reserved adds `llm_settle`. A replay acts the logged outcome out and leaves every pool where the recording left it (`Budget.ReserveLogged`). Runs without those limits log nothing new.
3. **The design's 200-program property test is not extended to agents.** p67.6 resumes one agent run (every kind of tool, a denied tool, a failing tool, a spawned agent, `max_turns`) from every line of its log, which puts a crash before and after every record kind.
4. **The replay time bound ("< 50 ms") is not asserted**, as in Phase 6: process start alone exceeds it at a load average of 300.
5. **Seams closed** (p67.11 records them): `16o-phase7-notes.md` "Model calls are not replayable yet"; `16-phase82.md` "The Phase 6 seam" and "recorded and replayed"; `16o-phase6-notes.md` "Some outcomes are detected, not reproduced" (for the model pools; `steps` and `alloc_mb` shared under concurrency stay detected only); the `TestHostDeadlineIsNotLogged` timing flaw that `16-implementation-notes.md` records (p67.1).

### Task p67.1: `TestHostDeadlineIsNotLogged` independent of machine speed

`TestHostDeadlineIsNotLogged` (`arv/replay_hostdeadline_test.go`) runs `let f = fn(i) { len("x"); f(i + 1) }; f(0)` under a 20 ms host deadline. On an idle machine without `-race` the recursion reaches the 10000-call depth limit first, and the test fails with `stack overflow: call depth exceeded 10000` (Phase 6 mutation rounds 1 and 2, `16za`, saw it 3 times in 3). It is a flaw in the test, not the runtime, but every later task runs `go test ./arv`, so fix it first. The new program does 10^9 builtin calls, each a check, never more than 1000 deep; each `g(1000)` leaves one `0` in the loops' arrays, so memory stays small even if the deadline fires late.

**Files:**
- Modify (test): `arv/replay_hostdeadline_test.go`

- [ ] **Step 1: See the flaw (optional)**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv -run TestHostDeadlineIsNotLogged -count=3`

Expected on an idle machine: FAIL with `stack overflow: call depth exceeded 10000` in the log it prints. Under load it may pass; go on either way.

- [ ] **Step 2: Change the program**

In `arv/replay_hostdeadline_test.go`, replace:

```go
func TestHostDeadlineIsNotLogged(t *testing.T) {
	src := `let f = fn(i) { len("x"); f(i + 1) }; f(0)`
	for i := range 3 {
```

with:

```go
func TestHostDeadlineIsNotLogged(t *testing.T) {
	// Busy work that outlasts the deadline on any machine: 10^9 calls of
	// a builtin, each a check, at most 1000 deep. (An unbounded recursion
	// here hit the 10000-call depth limit before 20 ms on an idle machine
	// without -race.) Each g(1000) leaves one 0 in the loops' arrays.
	src := `let a = [` + ones(1000) + `];
let g = fn(n) { if (n == 0) { 0 } else { len("x"); g(n - 1) } };
for i in a { for j in a { g(1000) } }`
	for i := range 3 {
```

- [ ] **Step 3: Run it**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv -run TestHostDeadlineIsNotLogged -count=3 && go test -race ./arv -run TestHostDeadlineIsNotLogged -count=2`

Expected: PASS both times (each run about 0.1 s).

- [ ] **Step 4: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "test(arv): the host-deadline test outlasts its deadline on any machine

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
