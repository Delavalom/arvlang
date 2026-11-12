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

## Phase 8.1: arv check

Notes: [`16p81`](16p81-check-notes.md). Round 1: [`16p81b`](16p81b-check-round1.md). Mutation table: [`16p81e`](16p81e-check-mutations.md).

## Phase 8.2: host effects

[Notes](16-phase82.md) (the `hostGate` seam), [hardening](16-phase82-hardening.md), [mutations](16-phase82-mutations.md).
