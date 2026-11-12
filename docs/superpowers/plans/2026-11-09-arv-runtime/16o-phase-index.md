# Implementation notes: index of Phases 4 onwards

Continuation of [`16-implementation-notes.md`](16-implementation-notes.md), which was near the 18 KB limit. Each phase adds its pointer section here.

## Phase 4: supervision

Notes: [`16j`](16j-phase4-notes.md), [`16k`](16k-phase4-hardening.md). Mutation tables (round 1: 45 mutations, 42 caught, one test gap; round 2: the gap and the clock jitter now caught, one equivalent): [`16l-phase4-mutations.md`](16l-phase4-mutations.md).

| Guarantee | Mutation | Caught | Test |
|---|---|---|---|
| 30 s backoff cap (round 3) | drop `d = min(d, MaxBackoff)` | no (equivalent, see 16l) | none possible |
| 30 s backoff cap (round 4) | drop `d = min(d, MaxBackoff)` | yes, unit only (not equivalent) | `TestDelayClampsBeforeJitter` |

## Phase 5: gate

Notes: [`16m`](16m-phase5-notes.md). Review round 1: [`16n`](16n-phase5-round1.md).

## Phase 6: replay

Notes: [`16o`](16o-phase6-notes.md). Review round 1 (fixes, dispositions, mutation checks): [`16q`](16q-phase6-round1.md). Review round 2 (deadlines in the log: design, dispositions, mutation checks, and the known limits on time): [`16r`](16r-phase6-round2.md).

**Known limits added in review round 1** (each is Minor, or a bound on a fix):
- **Quiet checks (resume).** While any task still has records, a resumed task past its own records does not see a failure it has not seen before. Since round 2 it does see a deadline, unless it is still behind the log (a spawn it has still to make has records). It can go on to print or write after a sibling's failure that a live run would have stopped it at, only in that window. A resumed run whose script changed can leave records nobody uses. It then stays quiet, and a loop that ends only by a failure's cancellation runs until the host cancels the run.
- **Resumed deadlines are bounds.** Round 2 replaced the round-1 rule (`TimeoutLeft`): see [`16r`](16r-phase6-round2.md). A resumed deadline is never sooner than the recorded one, and can be later by the unlogged time between a scope opening and the record that bounds it.
- **Output order across a resume.** `puts` takes its output index before its record is written. If task A takes index k, B takes k+1 and is logged, and the run is killed before A's record, then the resumed A prints after B. Replays of the resumed log keep that order.
- **A forged log in `--resume`.** The header's limits are taken as they are: the CLI and `arv.Run` refuse limit flags on a resume, so a forged header can set `steps`, `alloc_bytes`, `timeout_ms`, `usd_cents`, `tokens` or `max_turns` to 0 (no limit), or to any value, for the live part. A forged `usd_cents` lifts the money budget for every live model call of the resume. The calls still go only to models the grant covers, at true prices (review round 6, [`16x`](16x-phase6-round6.md)). A spawn record's grants are checked against the parent's grants (I2), not against what the spawn asked for. So a forged record can give a resumed child more than the program's own narrowing, but never more than its parent holds. Requests are not canonical, and checking them strictly would need an lstat.
- **A forged `cancelled` record hung a replay** until the host cancelled it (rounds 1 and 2). Since round 3 a replay in which every task waits for the log diverges after about a second ([`16u`](16u-phase6-round3.md)).

Review round 3: [`16u`](16u-phase6-round3.md). Review round 4 (host results that alias program values, a resume inside a model call, stall detection in a resume, a live failure ahead of a logged deadline): [`16v`](16v-phase6-round4.md). Review round 5 (a forged `llm_model` in a resume, `liveErr` and honest logs that stalled, forged logs that drain a resume): [`16w`](16w-phase6-round5.md). Review round 6 (an honest resume that hung on a slot, so a task's end is now logged before its slot is freed; live awaits and slot waits parked; the forged-resume wait bounded by the header): [`16x`](16x-phase6-round6.md). Review round 7 (log format version 2, so a log from before round 6 is refused; why a forged resume that waits for its deadline reports the timeout, not a divergence): [`16y`](16y-phase6-round7.md). End-to-end, fuzz, property and leak tests (with fuzz seconds and execs; one Minor fix): [`16z`](16z-phase6-e2e.md). Mutation check, round 1 (33 mutations, 31 caught; two test gaps): [`16za`](16za-phase6-mutations.md).

## Phase 7: agents

Notes and limits: [`16o`](16o-phase7-notes.md); reviews [`16p`](16p-phase7-round1.md), [`16q`](16q-phase7-round2.md); hardening [`16r`](16r-phase7-hardening.md); mutations: [`16s`](16s-phase7-mutations.md), round 2 [`16t`](16t-phase7-mutations-round2.md) (6 of 6 caught).

## Phase 8.1: arv check

Notes: [`16p81`](16p81-check-notes.md). Round 1: [`16p81b`](16p81b-check-round1.md). Mutation table: [`16p81e`](16p81e-check-mutations.md).

## Phase 8.2: host effects

[Notes](16-phase82.md) (the `hostGate` seam), [hardening](16-phase82-hardening.md), [mutations](16-phase82-mutations.md).

## Phase 5E: ergonomics

Notes, known limits and review round 1: [`16pe`](16pe-ergonomics.md). E2E, fuzz, property and leak tests: [`16pe-hardening`](16pe-hardening.md). Mutation check, rounds 1-2 (41 mutations; 40 caught in round 1, the last, M28, caught in round 2): [`16pe-mutations`](16pe-mutations.md).

**Merge into `feat/phases-3-8`.** Phase 5E added the builtins `str` and `$str` (`ast.StrBuiltin`, what `"${x}"` calls) after Phases 6 and 8.1 had made every builtin be classified. On merge both were added to the pure builtins of `arv check` (`arv/check/builtins.go`) and of the Phase 6 gate test (`arv/evaluator/gate_test.go`): each renders its argument as `puts` prints it and needs no grant. Without that, `arv check` gave a "builtin arv check does not know" note at every interpolation. `arv/check/ergonomics_test.go` checks that the new syntax needs nothing of its own and that effects inside it are found, and `TestP81ExamplesCheck` has the expected report for `examples/uptime-ergonomic.arv` (that of `uptime.arv`, one line higher).

## Phase 8.4: speed

Notes, numbers before and after, deviations and known limits: [`16p84-speed.md`](16p84-speed.md). Plans: `p84-plan-1.md` to `p84-plan-9.md`. Review round 1: [`16p84b-speed-round1.md`](16p84b-speed-round1.md). E2E, fuzz, property and leak tests: [`16p84d-speed-e2e.md`](16p84d-speed-e2e.md).
