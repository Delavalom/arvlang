# Phase 6x7, task p67.11: README, notes, seams closed, final checks

> **For agentic workers:** this file is one task, p67.11. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.10 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.11: README, notes, seams closed, final checks

Record what Phase 6x7 built, its deviations, the seams it closed and its known limits, in a notes file of its own (`16zb-p67-notes.md`) indexed from `16o-phase-index.md`; mark the seams resolved where they were written; add the agent replay paragraph to the README (it stays under 18 KB: about 17.7 KB). Then run the whole suite, the e2e suite with a race-built binary, and the new tests three times. If a test name or a number below differs from what you see, correct the notes to what you see (for example the line count in p67.6's log).

**Files:**
- Modify: `README.md`
- Create: `docs/superpowers/plans/2026-11-09-arv-runtime/16zb-p67-notes.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase-index.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase7-notes.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16-phase82.md`

- [ ] **Step 1: README**

In `README.md` (edit 1 of 2), replace:

````markdown
created with mode 0600.

## Host effects (embedding)
````

with:

````markdown
created with mode 0600.

An agent run replays the same way. The log holds each model call (`llm_model`, `llm`), each tool's own effects, and, under a `tokens` or `usd_cents` limit, whether each call's reservation was granted (`llm_reserve`) and whether a call that used more than it reserved fit (`llm_settle`). So a replay sends nothing, needs no key, runs no tool effect, and refuses exactly the calls the recording refused, even when agents ran at once. A resume after a crash in the middle of an agent sends the call that was in flight, byte for byte, and the calls after it. The API key is never in the log.

## Host effects (embedding)
````

In `README.md` (edit 2 of 2), replace:

````markdown
./examples/host -allow staging`. With `Options.Record`,
````

with:

````markdown
./examples/host -allow staging`, and with `-record run.jsonl`, then `-replay run.jsonl` (the replay leaves the cluster empty). With `Options.Record`,
````

- [ ] **Step 2: The notes**

Create `docs/superpowers/plans/2026-11-09-arv-runtime/16zb-p67-notes.md`:

````markdown
# Phase 6x7: replayable agents (as built)

Built from `p67-plan-1.md` to `p67-plan-11.md` on `phase/p67`. Phase 6 review round 3 (`16u`) had already routed `ask`, `agent` and host effects through the gate (`llm_model`, `llm`, `host:<name>`). This phase tests the guarantees end to end, fixes the two gaps that testing found, and closes the seams Phases 6, 7 and 8.2 left.

## What changed

- **A replayed spawn keeps the run's host kinds** (`loggedCaps`, `arv/evaluator/spawncaps.go`). It checked a logged grant with `runtime.Literal`, which knows only built-in kinds, and gave the child a set without the host's kinds. Every replay or resume of a run that spawned with `k8s("staging")` diverged ("the log's grants for a spawn include k8s(\"staging\"), which the task does not hold"); `examples/host` itself does that spawn. It now uses `Caps.Kinds().Literal` and `NewCapSetKinds`. I2 still holds: a forged wider grant diverges (`TestReplayOfASpawnWithAHostGrant`).
- **Model budgets are in the log** (`arv/evaluator/llmbudget.go`, `arv/runtime/budget_llm_log.go`). When a `tokens` or `usd_cents` level limits a call and the run keeps a log, its reservation is the effect `llm_reserve` (request: tokens and micro-dollars; result: nil or the budget error), and a settlement past the reservation is `llm_settle`. A replay acts the outcome out: a granted reservation is taken whatever the pool holds (`Budget.ReserveLogged`), a refused one takes nothing, a settlement is made and the logged outcome returned. Before, two agents racing for one budget replayed in the replay's own order and diverged 5 times in 5 (`TestReplayOfConcurrentModelBudgets`, `TestReplayOfConcurrentModelSettlements`). A resume's live calls see the pools the replayed part spent.
- **`TestHostDeadlineIsNotLogged` no longer depends on machine speed.** Its unbounded recursion hit the 10000-call depth limit before the 20 ms host deadline on an idle machine without `-race` (16za); it now loops over a bounded recursion (10^9 builtin calls, at most 1000 deep).
- **Hosts record, replay and resume.** `examples/host` takes `-record` and `-replay`; `e2e/p82host` takes `-record`, `-replay` and `-resume`, and has `k8s_gate` (a host effect that waits at a server gate) for SIGKILL tests.

## Guarantees and the tests that hold them

| Guarantee | Tests |
|---|---|
| A replay of an agent run is byte-identical, with no provider call and no tool effect (request, file, environment, host call) | `TestReplayOfAnAgentRunsNoToolEffect`, `TestP67ReplayOfAnAgentRunIsOffline`, `TestReplayOfModelCalls` (16u) |
| No log holds the API key, whatever the provider or the model sends | `TestP67LogsNeverHoldTheKey` (6 cases, each also replayed without a key), `noKeyIn` in every p67 e2e test, `TestCLIReplayOfModelCallsNeedsNoKey` |
| A resume after SIGKILL mid-agent equals an uninterrupted run, and sends only the call in flight (byte for byte) and those after it | `TestP67ResumeAfterSIGKILLInAModelCall`, `TestP67ResumeAfterSIGKILLInATool`, `TestResumeOfAnAgentRunAfterAnyLine` (a resume after each of the log's 38 lines, with leak checks), `TestResumeInsideAModelCall` (16v) |
| Budgets on replay match the original, also under concurrency; a resume's live calls see them | `TestReplayOfConcurrentModelBudgets`, `TestReplayOfConcurrentModelSettlements`, `TestReserveLogged` |
| Host effects record, replay with no `Fn` call, and resume | `TestP67HostRecordAndReplay`, `TestP67HostResumeAfterSIGKILL`, `TestHostExampleRecordsAndReplays`, `TestReplayOfASpawnWithAHostGrant` |

## Deviations from `13-phase6-durable-replay.md` and `14-phase7-agents.md`

- **Design 14, task 8 ("ask and agent turns go through effect()") was done in Phase 6 review round 3** (`16u`), as two effects per call, not one. This phase adds the tests the design asks for and the budget records.
- **Model budget outcomes are effects, not checks.** Phase 6 logs other scheduling-dependent outcomes (a cancellation, the live-task cap) as checks, which hold only a kind. A refused reservation must replay its exact error (which level, which limit), and the codec already carries errors in effect results. An effect is logged for every limited call, not only when it fails: one line of about 144 bytes per call (`{"task":"root","seq":2,"kind":"llm_reserve","req":[4626,2101],"res":null,"h":"…"}`).
- **The design's 200-program property test is not extended to agents.** `TestResumeOfAnAgentRunAfterAnyLine` resumes from every line of one agent run that uses every kind of tool, a spawned agent and `max_turns`, which covers each record kind at each position.

## Seams closed

- `16o-phase7-notes.md`, "Model calls are not replayable yet": done in `16u`, tested here.
- `16-phase82.md`, "The Phase 6 seam" and "recorded and replayed": done (`logHost` in `16u`; the example and `e2e/p82host` here).
- `16o-phase6-notes.md`, "Some outcomes are detected, not reproduced": the model pools are now reproduced. `steps` and `alloc_mb` shared under concurrency are still detected only.
- `16-implementation-notes.md`, the `TestHostDeadlineIsNotLogged` timing flaw: fixed.

## Known limits

- **A resume's live reservation can see a pool the replayed part has not finished acting out.** The replayed tasks take their logged reservations in this run's order. While the log still has records, a live task in the same budget can find less (or more) left than the crashed run had at that moment, and be refused (or granted) differently. The same window exists for live checks (16q, "Quiet checks"). After the log has drained, every level holds what the crashed run left.
- **A forged log can change what a resume's live part may spend**, by dropping a call's `llm_reserve` and `llm` records. This is inside the forged-resume limit of `16q` (a forged header can lift `usd_cents` altogether); a replay of a forged log sends nothing.
- **Uncharged (Minor, a constant per call):** each limited model call encodes two integers for its request and writes one log line. Neither grows with input the script controls beyond the call count, which the budgets bound.
- **`e2e/p82host -resume` drops a partial last line when it opens the log,** before checking the grants (the CLI waits for its first append, `16z`). It is a test host.
````

In `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase-index.md`, replace:

````markdown
`uptime.arv`, one line higher).
````

with:

````markdown
`uptime.arv`, one line higher).

## Phase 6x7: replayable agents

Notes, guarantees and their tests, deviations, the seams closed and known limits: [`16zb`](16zb-p67-notes.md). Model budget outcomes (`llm_reserve`, `llm_settle`) are logged, so concurrent agents replay alike; a replayed spawn keeps the run's host kinds.
````

In `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase7-notes.md`, replace:

````markdown
- **Model calls are not replayable yet.** They are nondeterministic input (I7). Phase 6 is not on this base; routing `ask` and `agent` turns through `effect()` and testing that a replay makes zero provider calls is follow-up work for p67.
- **`max_tokens` is
````

with:

````markdown
- **Model calls are replayable** (resolved: Phase 6 review round 3, [`16u`](16u-phase6-round3.md), routed them through the gate; Phase 6x7, [`16zb`](16zb-p67-notes.md), tested replay, resume and the key, and logs the model budgets).
- **`max_tokens` is
````

In `docs/superpowers/plans/2026-11-09-arv-runtime/16-phase82.md`, replace:

````markdown
the 6x7 integration)

Phase 6 was not merged
````

with:

````markdown
the 6x7 integration)

**Resolved.** `hostGate` is `logHost` (Phase 6 review round 3, [`16u`](16u-phase6-round3.md)); `examples/host` and `e2e/p82host` record, replay and resume, and a replayed spawn keeps the host kinds (Phase 6x7, [`16zb`](16zb-p67-notes.md)). The plan below is kept as it was written.

Phase 6 was not merged
````

- [ ] **Step 3: Measure the record size quoted in the notes**

Optional: record `scope (tokens: 100000) { ask("anthropic:claude-haiku-5-5", "hi") }` with a `llm.Fake` (as `TestReplayOfModelCalls` does) and print the `llm_reserve` line's length; it was 144 bytes. Do not commit the probe.

- [ ] **Step 4: Run everything**

```bash
df -h /System/Volumes/Data
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race -count=1 ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && ARV_E2E_RACE=1 go test -race -count=1 ./e2e/
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test -race -count=3 ./arv -run 'TestReplayOfASpawnWithAHostGrant|TestReplayOfConcurrentModel|TestReplayOfAnAgentRunsNoToolEffect|TestResumeOfAnAgentRunAfterAnyLine|TestHostDeadlineIsNotLogged'
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test -count=3 ./e2e -run 'TestP67'
wc -c README.md docs/superpowers/plans/2026-11-09-arv-runtime/16zb-p67-notes.md docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase-index.md
```

Expected: all green; each markdown file under 18000 bytes.

- [ ] **Step 5: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "docs: Phase 6x7 notes, README; seams from Phases 6, 7 and 8.2 closed

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
