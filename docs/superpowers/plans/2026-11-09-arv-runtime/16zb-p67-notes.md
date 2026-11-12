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
- **The design's 200-program property test is not extended to agents.** `TestResumeOfAnAgentRunAfterAnyLine` resumes from every line of one agent run that uses every kind of tool, a spawned agent and `max_turns`. That covers every record kind it writes at each position, but its replies fit their reservations, so it writes no `llm_settle` (review round 1: `TestResumeAroundSettlementsAfterAnyLine` in [`16zc`](16zc-p67-round1.md) covers that kind).

## Seams closed

- `16o-phase7-notes.md`, "Model calls are not replayable yet": done in `16u`, tested here.
- `16-phase82.md`, "The Phase 6 seam" and "recorded and replayed": done (`logHost` in `16u`; the example and `e2e/p82host` here).
- `16o-phase6-notes.md`, "Some outcomes are detected, not reproduced": the model pools are now reproduced. `steps` and `alloc_mb` shared under concurrency are still detected only.
- `16-implementation-notes.md`, the `TestHostDeadlineIsNotLogged` timing flaw: fixed.

## Known limits

Review round 1 ([`16zc`](16zc-p67-round1.md)) fixed two Critical findings about model budgets in a resume, and its known limits replace the first three bullets below.

- **A resume's live reservation can see a pool the replayed part has not finished acting out.** The replayed tasks take their logged reservations in this run's order. While the log still has records, a live task in the same budget can find less (or more) left than the crashed run had at that moment, and be refused (or granted) differently. The same window exists for live checks (16q, "quiet checks"). After the log has drained, every level holds what the crashed run left.
- **A forged log can change what a resume's live part may spend**, by dropping a call's `llm_reserve` and `llm` records. This is inside the forged-resume limit of `16q` (a forged header can lift `usd_cents` altogether); a replay of a forged log sends nothing.
- **Uncharged (Minor, a constant per call):** each limited model call encodes two integers for its request and writes one log line. Neither grows with input the script controls beyond the call count, which the budgets bound.
- **`e2e/p82host -resume` drops a partial last line when it opens the log,** before checking the grants (the CLI waits for its first append, `16z`). It is a test host.
- **A `-race` test binary can crash in a forked child before `exec` (not Arv).** In the final checks, `go test -race ./...` failed once: `TestP6PropRecordReplay/7` reported `running arv: signal: segmentation fault` after 13 ms, with no output. The macOS crash report names the forked `e2e.test` child, faulting in the race detector (`__tsan::TraceSwitchPartImpl`, called from `syscall.forkAndExecInChild`, symbolised against a rebuilt `e2e.test`), so the `arv` binary never started. Six further runs of the e2e package, one with `ARV_E2E_RACE=1`, passed. It is a Go race-detector and fork issue on a loaded darwin machine, not an Arv bug; a rerun clears it.
