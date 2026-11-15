# Phase 6: review round 2 (deadlines in the log)

Review round 2 of Phase 6 reported 3 Critical and 5 Important findings, all about time. Each was fixed test-first: a regression test was written and seen to fail for the right reason before the fix. Every fix comes from one root cause: in a replay and in a resume, scope deadlines and the run timeout were wall-clock timers, so their outcome came from this run's timing and not from the log. That breaks I7. The fix makes a deadline firing part of the log, like every other outcome that depends on timing.

## The design

The code is in `arv/runtime/deadline.go`, with small parts in `gate.go`, `checks.go`, `quiet.go` and `effectlog.go`.

- **Recording.** A timed scope's deadline is a wall-clock timer, as in an unlogged run. Each check or effect that sees the cancellation it causes logs `by`: the scope's ID. The ID is the opener's task ID and how many timed scopes that task has opened, for example `root:1` or `root/0/2:3` (`Task.OpenTimed`). The opener's `expired` check logs `by` too, and so does a restart backoff that the deadline cut short. A deadline that passed while its timer was late is seen at the next check as the scope's timeout, not as `context.DeadlineExceeded` (`expireDue`), so the log can name it.
- **Replay.** No timer runs. A deadline fires when a replayed record names it (`awaitCancel`), once every record logged before the first record that names it has been used (`Log.sighted`, `EffectLog.waitFront`). Those records happened first, and none of them depends on the deadline: a task that saw it logged that. A replay as slow as you like reaches the same deadlines, and a fast one does not wait for them. A `by` that names a scope the task is not inside is a divergence.
- **Resume.** A deadline that the log names fires the replay's way. Any other deadline runs on the recorded run's clock: it passes `vdl` ms after that run started (`vdl` is when the scope opened, plus the timeout). Each task reads that clock for itself (`Task.now`):
  - A replaying task is at its last record's `at`.
  - A live task goes on from there in real time. So an effect that was in flight at the crash, which the task runs again from the start, is counted once (F7).
  - A child starts at its parent's time.
  - The deadline fires when the first live task inside the scope reaches `vdl` (`deadline.arm`: one timer per scope, set to the earliest arm). A task stops counting when it ends (`retire`), and while it waits for finished tasks (`idle`).
- **When a resumed scope opened.** A replaying opener opened it before its next record began (or was logged, for a check). Any opener opened it before the first record under any child it spawns after opening it (`EffectLog.first`). The bound is the earliest of these, and never sooner than the opener's own clock. A live opener with no such record opened the scope at its own time now. Each bound is late, so a resumed deadline is never sooner than the recorded one (F3, F5). The child bound matters for a live opener whose clock lags. Its spawn skipped a wait for a slot, which the log does not time (property test, seed 20020).
- **A task's end is a record.** The last record of a spawned task is `done` (`Task.Finished`). Every restart attempt has one; an earlier attempt's is logged when the next one starts. A resume replays a finished task to its end, including a pure tail, so it does not run that tail live while the replay races ahead (F1). A replay checks that each task ends where its `done` record is.
- **Live spawns in a resume** (`Task.Acquire`). If the log has records of the child, or of a task under it, the spawn takes its slot without the cap: the recorded spawn got one. Otherwise the spawn first waits for every task whose `done` record is before the spawner's last record, with the spawner's clock stopped, and then takes the cap as usual (F1).
- **Quiet checks see deadlines** (F2, F8). A deadline never fires early for a task at the log's frontier, so a quiet check sees it as the recorded run would have. A failure that the task has not seen stays hidden, as in round 1. The exception is a task that is still behind the log (`Task.behind`): a spawn it has still to make, the next one or a later one, has records under it. That task does not see deadlines yet. The recorded task made that spawn, so it had not seen one.
- **Removed:** `Task.TimeoutLeft`. The resumed clock offset (`EffectLog.start`) is kept only for `clock_ms`.

**Log format.** There is a new record field, `by`, and a new kind, `done`. `ReadLog` refuses a `done` record with anything but its time, a `by` on anything but a cancellation or an `expired` check, and any record after a task's `done`.

## Dispositions

| # | Finding | Fix | Regression tests | CLI repro after the fix |
|---|---|---|---|---|
| F1 Critical | `--resume` re-ran a finished task's pure tail live while the replay raced ahead, so the live-task cap failed the run | `done` records; a live spawn waits for finished tasks before the cap | `TestResumeWaitsForFinishedTasksBeforeTheCap`, `TestResumeWaitForFinishedTasksTakesNoTime`, `TestResumeDoesNotWaitForALaterSibling`, `TestResumeSpawnsATaskWhoseDescendantsHaveRecords`, `TestResumeReplaysAFailedAttemptToItsEnd`, `TestFinishedIsARecord` | prints 1 and 2, exit 0 |
| F2 Important | Quiet checks hid a scope's deadline: the resumed run wrote a file and printed after the timeout | Quiet checks see deadlines | `TestResumedQuietChecksSeeDeadlines` | no `late.txt`; same timeout at `q.arv:2:98` (3 of 3 runs) |
| F3 Important | A resumed scope deadline restarted when the opener had no record inside the scope | Open time bound; virtual clocks | `TestResumeOfAScopeWithoutTheOpenersRecords`, `TestResumeOfAScopeWhoseOpenerSkippedAWait`, `TestResumeOpensAScopeAtTheOpenersNextRecord` | first, then the 2000 ms timeout, exit 1, as uninterrupted |
| F4 Important | Replay applying the header's `live_tasks` was untested | Tested | `TestReplayUsesTheHeaderLimits` ("tasks") | mutation caught (below) |
| F5 Critical | A resume after a timed scope that closed early diverged | No live timer in a replayed scope; the open time bound | `TestResumeOfAScopeClosedBeforeItsDeadline` | in scope / ok / end, exit 0 |
| F6 Critical | A slow replay diverged: scope timers and the run timer fired live | No timers in a replay; deadlines fire from the log | `TestSlowReplayKeepsItsDeadlines` (scope, run), `TestReplayFiresALoggedDeadline` | `GOMAXPROCS=1` replay prints `[c, c, c]`, exit 0 |
| F7 Important | An in-flight effect was counted twice, so a resume timed out | Per-task clocks go on from the last record | `TestResumeCountsAnInFlightEffectOnce` (run, scope) | d done / e done / all done, exit 0 |
| F8 Important | Quiet checks let a live child write a file after its scope's deadline | Quiet checks see deadlines | `TestResumedQuietChildSeesItsDeadline` | no `late.txt` (3 of 3 runs) |

**Found while fixing** (by `TestTimedProgramsReplayAndResume` and longer sweeps of its generator), each with a test that failed first:

- A deadline fired in a replay ahead of causally earlier records, so a scope's first failure became the timeout: `TestReplayFiresADeadlineInLogOrder`.
- A spawn waiting for a slot and cut short by a deadline was logged as the cap: `TestReplayOfASpawnCutShortByADeadline`.
- A forged `by` was waited on: `TestReplayRefusesADeadlineOutsideTheTask`.
- A failed restart attempt's pure tail ran live: `TestResumeReplaysAFailedAttemptToItsEnd`.
- A live task behind the log saw a deadline, the next spawn first (`TestResumedTaskBehindTheLogDoesNotSeeTheDeadlineYet`), then a later spawn (seed 20020: `TestBehindUntilTheLastSpawnWithRecords`, `TestResumedTaskBehindALaterSpawnDoesNotSeeTheDeadlineYet`).
- A live opener's lagging clock made its deadline early: `TestResumeOfAScopeWhoseOpenerSkippedAWait`.
- A backoff cut short by a deadline did not name it, and a replay hung (seed 40051): `TestReplayOfABackoffCutShortByADeadline`.

Leaks: `TestResumeAndReplayOfDeadlinesLeaveNoGoroutines` covers timers, waits for the frontier and quiet watchers. Timing: the tests that time `spin()` against deadlines calibrate it once per test binary to about 300 ms (`arv/spin_test.go`), so they pass under `-race`.

## Property test and sweeps

`TestTimedProgramsReplayAndResume` (`arv/replay_timed_prop_test.go`) writes random programs with timed scopes, `tasks: 1`, restart, collect, sleeps and pure work. Each recording must replay identically. Each cut of its log must resume without diverging, and the resumed log must replay identically to the resumed run. It runs 30 programs with 8 cuts each (6 and 4 under `-short`). Before the fix, 208 cuts of 60 programs diverged.

Final sweeps of every cut (seeds 20000-20059, 30000-30059, 40000-40059 and 50000-50059) found two failures that are not deadline bugs:

- **Seed 40000 (1 run in 6).** Two tasks of one `fail_fast` scope fail at once. The replay of the resumed log reaches the other first failure. This is the known limit in [`16o`](16o-phase6-notes.md) ("some outcomes are detected, not reproduced"): the replay reports it as a divergence and prints nothing wrong.
- **Seed 40019 (1 run, not reproduced).** One resume diverged: "replay diverged at task root/0/0/1, effect 3: the log has puts("flaky"), the program asked for puts("task root/0/0/1 cancelled while awaiting root/0/0/1/2: budget exceeded: timeout (limit 60 ms)")". The same seed then passed 65 more times, at every cut. No crashed log was saved, and the code paths do not explain it: a task with records left replays an `await` that has no record as uncancelled. It is reported as open.

## Mutation check

Each mutant was applied alone to a copy of the tree. The listed tests were run.

| Mutant | Caught by |
|---|---|
| F4: replay drops the header's `LiveTasks` | `TestReplayUsesTheHeaderLimits` ("budget exceeded: tasks (limit 1000 ...)") |
| `behind` is never true | `TestBehindUntilTheLastSpawnWithRecords` |
| `behind` looks at the next spawn only (before seed 20020) | `TestBehindUntilTheLastSpawnWithRecords` |
| a backoff cut short names no deadline | `TestReplayOfABackoffCutShortByADeadline` (the replay hangs) |
| a live opener opens its scope on its own clock | `TestResumeOfAScopeWhoseOpenerSkippedAWait` |
| a replaying opener opens at its last record, not its next | `TestResumeOpensAScopeAtTheOpenersNextRecord` |
| a logged deadline fires without waiting for the frontier | `TestReplayFiresADeadlineInLogOrder` |
| a deadline that ends a wait for a slot is logged as the cap | `TestReplayOfASpawnCutShortByADeadline` |
| a late timer is `context.DeadlineExceeded` | `TestLateDeadlineIsTheScopesTimeout` |
| `idle` keeps the clock running | `TestResumeWaitForFinishedTasksTakesNoTime` |
| quiet checks never see a deadline | `TestResumedQuietChecksSeeDeadlines` |
| no wait for finished tasks before the cap | `TestResumeWaitsForFinishedTasksBeforeTheCap` |
| an effect's `began` is the run's own time (counted twice) | `TestResumeCountsAnInFlightEffectOnce` |
| a replay does not check where a task ends | `TestFinishedIsARecord` |
| a forged `by` is waited on | `TestReplayRefusesADeadlineOutsideTheTask` |
| a child's clock starts at 0, not at its parent's time | not caught: equivalent in practice. A parent with records left replays them at once and goes live at its next check (the spawn's Acquire is one), which arms it on its own clock. The child's start matters only in that window. |
| earlier restart attempts get no `done` record | not caught. Since log-order firing, `TestResumeReplaysAFailedAttemptToItsEnd` passes without it too: a logged deadline cannot fire before the next attempt's records, which follow the failed attempt's end. It is kept so that a failed attempt's pure tail is replayed, not run live, when an unnamed deadline fires on a clock. |
| a resumed spawn with records under it waits for the cap | not caught: equivalent in practice. The wait for finished tasks frees every slot the recorded spawn found free, except in the race under "A slot can stay held" below. |

## Known limits

- **A deadline that the log does not name is not ordered by the log.** In a resume, it fires when the first live task reaches it on its own clock. A task behind the log does not see it, and a scope opens no sooner than its children's first records. But a check of another live task can still see it a little earlier, or later, than the uninterrupted run would have. Only the live part's outcome changes; no logged record is skipped.
- **A slot can stay held across a resume.** This happens when an effect was in flight at the crash, or when a task's `done` record comes after the spawner's last record (the cap was released, but its end was not logged yet). A resumed spawn with no records can then wait for that task, or fail at the run's live-task cap.
- **Open times are bounds.** A resumed deadline is never sooner than the recorded one. It can be later by the work between the scope opening and the record that bounds it.
- **Clocks are not merged when a task wakes.** A task that waited for another goes on from its own last record, not from the time of the task it waited for.
- **A failure racing a deadline changes only the text.** If a task fails while a deadline fires, the replay can name the other cause in a message. The structure of the run (which records exist) is the same.
- **A forged `by` that names an enclosing scope, but at the wrong place, can hang a replay** until the host cancels it, as a forged `cancelled` record can. The header's timeout no longer ends such a replay: no timer runs in a replay.
