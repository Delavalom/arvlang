# Phase 6x7: review round 2

This file continues [`16zc-p67-round1.md`](16zc-p67-round1.md). Review round 2 had one Important finding and three Minor ones. The Important finding has a regression test that failed before its fix, for the reason the finding gives.

## Fixes

**An honest log could not be resumed after a sibling's settlement went past the limit (Important).** Round 1 checks a replayed trailing grant (a granted `llm_reserve` that is its task's last record, so its call is sent live) once every logged reservation has been acted out: if a `tokens` or `usd_cents` level is below zero, the resume diverges. A settlement past its reservation that fails the budget also takes a level below zero, and it can come after the grant in the recorded run, while the granted call is in flight. The reviewer's program runs a and b under `tokens: 10000`. b reserves 4625 while a's call is in flight. a's reply uses 9000 tokens against its reservation of 4625, so a's settlement takes the level to -3625 (a budget error, logged), and b is answered. A crash while b's call was in flight left a log that failed every resume with "the log grants a reservation of 4625 tokens … which the budget cannot cover". The uninterrupted run prints `"budget: budget exceeded: tokens (limit 10000)\nb answered\n"`.

16zc described a second case as "a window of microseconds". It was not. When the crash falls between a's `llm` record and its `llm_settle` record, a's settlement runs live just after its replayed `llm`, with no model call in between. The reviewer measured 18 divergences in 20 resumes for that cut, and 3 in 3 for each later cut up to b's reply.

Now each pool also counts the excess of the settlements that failed (`pool.overshot`, `arv/runtime/budget_llm_order.go`). A live settlement counts as failed by its own outcome. A replayed one counts by the log's outcome (`Reservation.SettleLogged`), whatever it does in this run's order. `Budget.Overdrawn` adds that excess back before it checks a level. The reasoning:

- A granted reservation leaves every level at zero or above.
- A settlement past its reservation that succeeds also leaves every level at zero or above.
- A settlement within its reservation only gives back.
- A failed settlement lowers the level by its excess and raises the count by the same amount, so the sum does not change.

So in an honest run, a level plus that count never goes below zero, in any order of its steps. A grant the recorded run could not have made still shows up, because no failed settlement covers it. `Overdrawn` takes the run's ledger lock (`models.ledger`, a `sync.RWMutex`) for writing. Reservations and settlements take it for reading. So the check never sees a step half made: a refused reservation, which takes its amount and gives it back, or a failed settlement, whose excess is charged before it is counted. Before this, the check could read a refused live reservation's brief dip below zero and diverge on an honest log.

Tests:

- `TestResumeAfterASiblingSettlementOvershoots` is the reviewer's probe. It resumes three times and checks for leaked goroutines.
- `TestResumeAfterASiblingOvershootAfterAnyLine` cuts after every line and resumes each cut three times. Each resume must print the uninterrupted output and send only the missing calls. Its fake provider keeps the recorded order of what runs live: a's reply waits for b's call when b's call is live, and b's reply waits for a when a's `k8s_done` is live. Before the fix, every cut from a's `llm` record to b's reply diverged with "cannot cover".
- `TestOverdrawnLeavesOutFailedSettlements` (runtime) checks four things. A live failed settlement and a replayed one logged as failed are left out. One logged as succeeded is not. A grant beyond what was free before the excess is still caught. The count saturates.
- `TestResumeRefusesAForgedTrailingGrant` still diverges with no provider call.

**Minor findings fixed.**

- *The failed-settlement coverage claim was wrong.* With `tokens: 10000`, a's overage fits and leaves 3000, so b and c are refused at their reservations, and no `llm_settle` fails. `TestResumeAroundSettlementsAfterAnyLine` now also runs `tokens: 12000` and `usd_cents: 12`. In those runs b's reservation fits and its settlement fails. The test now asserts which cases log a failed `llm_settle`. `TestResumeOfConcurrentModelSettlementsAfterAnyLine` adopts the reviewer's probe. It resumes the concurrent failed-settlement run of `TestReplayOfConcurrentModelSettlements` after each of its lines, for both options. The reviewer had found that behaviour correct; only the test was missing. The `16zc` bullet is corrected.
- *`16zb` and `16zc` disagreed about which bullets round 1 replaces.* `16zc` supplies replacements for all three of `16zb`'s first three bullets. Its sentence now says so, and `16zb` points to this file too.

## Known limits (round 2)

This list replaces `16zc`'s second Known limits bullet (the trailing-grant check) and extends its third bullet (forged logs).

- **A replayed trailing grant is checked against the level plus the excess of the failed settlements, not against what was free at the moment it was granted.** That bounds what an honest run could have left free, and it no longer refuses an honest log. A forged grant that fits within the level as it stood before the failed settlements charged their excess is not caught. In the probe above, that is up to 750 tokens (-3625 plus a's excess of 4375), although the level is spent when the check runs. That is the most the recorded run could have had free for a grant made before a's settlement, so the check cannot tell such a grant from an honest one.
- **A forged log can still raise what a resume's live part may spend.** It can do so in four ways. The hash chain is not a signature, so a forger can recompute it after any edit.
  - Drop a call's `llm_reserve` and `llm` records (`16zb`).
  - Lift a run-level limit in the header (`16q`, `16y`).
  - **Lower the usage in a replayed `llm` record** (review round 2, Minor). The replayed settlement then gives back more than the crashed run really used. The reviewer's probe: under `scope (tokens: 10000)`, three calls of 3000+1000 tokens, cut after b's `llm` record, with a's `InputTokens` changed from 3000 to 1. The resume answers c live, so 12000 real tokens are spent under a 10000-token scope, and the run ends well.
  - **Mark a successful `llm_settle` as failed** (new in round 2). Its excess is then left out of the trailing-grant check, and a forged grant up to that excess passes.

  All four are the forged-log risk class the owner accepted (round 1's forged grant was Critical only because it needed no other record to be forged). Usage is the provider's answer. For the replayed part, only the log holds that answer, so a resume cannot check it without a signed log. A replay of a forged log sends nothing.
- **Uncharged (Minor, a constant per run and per level).** The ledger is one `sync.RWMutex` (24 bytes) per run, allocated with the root level. Each pool grows by one `atomic.Int64` (8 bytes) for the failed excess. Neither grows with input beyond the number of scope levels, which `steps` bounds. Reservations and settlements take the ledger's read lock, which allocates nothing.
