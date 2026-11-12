# Phase 6: review round 6 (fixes, dispositions, known limits)

Continuation of [`16w-phase6-round5.md`](16w-phase6-round5.md). Indexed from the Phase 6 section of [`16o-phase-index.md`](16o-phase-index.md).

## Critical: an honest `--resume` could hang on a slot

**Cause.** The evaluator released a finished task's slot (`tasks:`) and its live-task place, and only then logged its end (`KindDone`). The task waiting for that slot could start and log records in between. A crash in that window left an honest log with the next task's records but no end for the task that held the slot. The resume then ran the holder live, because it had no end. The spawn of the next task had records, so it waited for its slot in `Task.Acquire`'s `hasRecords` branch. That wait watched only the run's abort and was not parked. If the live holder then failed (in the sweep, a deadline it reached live under load), its failure waited in `Task.Fail` for the log to drain. The drain needed the records of the task waiting for the slot. Nothing broke the cycle, and the stall watcher never started, because the spawner counted as running.

**Fix.** `Task.Finished(release)` now takes the release. Recording, it logs the end and then releases the slot. Replaying, it uses the end, releases, and only then counts the end used (`doneUsed`). A resumed spawn that waits for logged ends (`waitDone`) therefore finds their slots free when it wakes. It runs the release on every path: a divergence, a panic, no log. An attempt that a restart replaces passes `nil`, because the slot stays the task's. So a log with any record of the next task in a slot also has the holder's end, and a resume replays the holder to that end instead of running it live. As before, a handle finishes, and a scope's `Wait` returns, only after the end is logged.

**Tests.** `TestTaskEndIsLoggedBeforeItsSlotIsFreed` (package `arv`) records `scope (tasks: 1) { a; b }`. Its log writer holds root/0's end for up to 2 s, waiting for root/1 to print. Before the fix, root/1 printed while the end waited, every run. `TestFinishedOrdersItsEndAroundRelease` (package `runtime`) checks the order in a recording, in a replay, and on a divergence (release runs once). A throwaway probe recorded the reviewer's program (`spinSrc`, a 600 ms scope around `scope (tasks: 1)`), cut the log after every line, and resumed each cut while 8 × GOMAXPROCS busy goroutines ran. All 7 cuts ended cleanly. Cuts 1 to 5 timed out live under the load, which an uninterrupted run under that load can also do. Cuts 6 and 7 printed the recorded output.

The reviewer's crafted log ([header, root/0/0 done, root/1 puts y]) is no longer one the recorder can write. As a forged log, it now diverges cleanly under the same load (3 of 3 runs, about 3 to 4 s), and without load it still resumes to the recorded output. `TestForgedResumeWhoseSlotHolderFailsLiveDiverges` is a deterministic form: the slot holder fails live because `getenv` reads a value the recording did not see. Before the fix, it hung for 60 s.

## Critical: a forged log still crashed `--resume` (live await, live slot wait)

**Cause.** A live `await` (`Task.WaitFor`'s live branch) and a live wait for a slot (`Task.Acquire`, both branches) were not parked. A live task waiting on a replayed task that a forged log holds forever (a `cancelled` check with no `by`) therefore counted as running. The watcher never started. With no deadline timer (`--timeout 0`, or a library host with no `Timeout`), Go aborted with "all goroutines are asleep" (exit 2). With the CLI's default timeout, the resume hung until that deadline fired.

**Fix.** Both waits are now parked (`EffectLog.Park`, a no-op without a log and while recording), as the replayed forms already were. A wait on another task counts as running only through that task: if the task is running or in a live effect, it counts as running itself, and a pending deadline timer still holds the watcher. The `stall.go` comment now lists which waits are parked.

**Tests.** `TestForgedResumeWithALiveWaitOnAHeldTaskDiverges` (subtests `await` and `slot`) uses the reviewer's library-level forgery: the first two lines of the recording plus `root/0` cancelled at check 1. Before the fix, each subtest hung for 60 s. Now each diverges in about 1.2 s with no goroutine left. The CLI repros (`--timeout 0 --record`, the forged line, rechain, `--resume`) both now print `a`, then `replay diverged at its end: every task waits ...`, and exit with status 1 in about 1 s.

## Minor: the forged-resume wait is bounded by the log, not by the CLI

Recorded as a known limit (below). It replaces round 5's wording, which gave the CLI's 10-minute default as the bound.

## Minor: the forged-header limit names the money budgets

The round-1 known limit in [`16o-phase-index.md`](16o-phase-index.md) now names `usd_cents`, `tokens` and `max_turns` as well as `steps`, `alloc_bytes` and `timeout_ms`. `arv/logged.go` builds the whole of `opts.Limits` from the header, and `main_log.go` refuses limit flags with `--resume`. So a forged header with `usd_cents: 0` (no limit) lifts the money budget for every live call of the resume. The calls still go only to models the grant covers, at true prices, and the replayed `llm` records can understate what the crashed run spent. This is the documented trust model: the log has no key, so the owner's acceptance of a forged header's limits covers the money budgets too.

## Minor (outside Phase 6): `TestConcurrentConcatStopsAtAllocLimit` was timing-dependent

The first failed charge leaves the alloc pool spent (`tripped`). When the 500-spawn loop outlasts the tasks' `sleep(300)`, the next spawn's charge can fail, and fail the scope, before the task whose concatenation failed does. The error is then the alloc error at `spawn f() with []`, which is correct behaviour. The test now accepts the alloc error at either position. It still requires the bare alloc error and the 32 MiB bound. It was not red on demand (it needs load), so this fix has no red run.

## Known limits (round 6)

These supersede round 5's first known limit ("A forged resume can last until a live deadline fires").

- **A forged resume can last until a live deadline fires, and the log sets that deadline.** While a live deadline timer is set, the watcher waits for it rather than diverge. A live task waiting on its scope, on another task, or on a slot keeps its scope's deadline armed, and the root scope's deadline is the header's `timeout_ms`. A resume takes its limits from the log; the operator cannot override them. So a forged header bounds the wait however it likes. A large `timeout_ms` makes the wait arbitrarily long, and `timeout_ms: 0` removes the timer, so the run diverges in about 1 s. An honest CLI header has the 10-minute default unless `--timeout` was given. Nothing crashes, and nothing live runs beyond the program's own effects. A live wait for time (a `sleep`, a request in flight) still counts as running, as in round 4.
- **Logs recorded before this round can still fail a resume** if they were cut between a slot holder's release and its end. If the resumed holder then fails live, the resume no longer hangs. It diverges once every task waits (the slot wait is parked), after any live deadline has fired, as above. If the holder succeeds, the resume is unaffected. Re-record to get the new order.
