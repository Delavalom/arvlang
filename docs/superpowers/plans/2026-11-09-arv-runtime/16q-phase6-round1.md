# Phase 6: review round 1 (fixes and dispositions)

Review round 1 of the Phase 6 implementation reported 1 Critical, 8 Important and 12 Minor findings. Each was fixed test-first: a regression test was written and seen to fail for the right reason before the fix. The known limits that remain are listed in the "Phase 6: replay" section of [`16-implementation-notes.md`](16-implementation-notes.md).

## Critical

- **`--resume` of a valid crash log failed with "replay diverged" and lost the lines printed before the crash.** In a resumed run, a task went live as soon as its own records ran out. Its checks then saw what this run's replay had done early. For example, a sibling's logged failure is replayed at once, so the task stopped short of a spawn whose child had records. It could also stop short of a result that an ancestor's logged `puts` prints.
  - **Fix: quiet checks** (`arv/runtime/quiet.go`). While any task still has records, a live check in a resumed run sees nothing new. It still sees the run's abort, and a cancellation cause the task has already seen. A logged or replayed cancellation, or a live effect that returned one, counts as seen: cancellation does not undo itself. A blocking check (await, the live-task cap) that began quiet ends once the log is used up. A restart backoff does not wait when the next attempt has records.
  - **Why it is sound:** every logged record happened before the crash, and a check that saw something before the crash was logged. So a check that sees nothing new until the log is used up sees what the recorded run saw.
  - **Tests:** `TestResumedChecksWaitForTheLog` (the reviewer's repro, ordered by a gated server instead of timing), `TestResumedChecksKeepALoggedResult`, `TestResumedBackoffWithALoggedNextAttempt`, `TestQuietChecks` and `TestQuietSeesASeenCause`.
  - **Property:** `TestResumeAtEveryCutProperty` cuts programs that fail at every line boundary. Without the fix it failed at seed 7051, cut 7. With only the first version of the fix (quiet, without seen causes) it failed at seed 7137, cut 20: a cancelled task printed after a cancellation it had already seen, and the race for its scope's first failure then made the replay of the resumed log diverge. The reviewer's seed 7018 passes with or without the fix in this tree.

## Important

| Finding | Disposition | Test |
|---|---|---|
| `--resume` restarted the wall-clock timeout | Fixed. Each record logs `at` (and an effect `began`). A resumed run's clock starts as far back as the last record's `at`. The root deadline counts from the run's start; a scope the crashed run had opened counts from no later than its opener's next record (`Task.TimeoutLeft`). | `TestResumeKeepsTheDeadlines` (run and scope) |
| Header limits on replay untested | Tested | `TestReplayUsesTheHeaderLimits` (steps, alloc, timeout) |
| Re-charging logged bytes untested | Tested | `TestReplayChargesTheLoggedBytes` |
| Evaluator's scope `Expired` check untested end to end | Tested. Output slower than the deadline makes the recorded scope time out at close; the replay is instant and must still time out. | `TestReplayOfAScopeThatTimedOutAtClose` |
| `--resume` cutting a partial last line untested | Tested through the CLI. The resumed append is shorter than the partial line, so a missing truncate leaves bytes behind. | `TestResumeCutsAPartialLine` |
| Forged logs with a valid chain barely tested | Tested. `forge` recomputes the chain. Covered: a result that does not decode, a bogus or non-canonical header grant, a negative timeout, times out of order, and Replay with Resume, Replay with Record, Resume without Record. `FuzzForgedReplay` (90 s, 700k execs, no failure) checks for no panic, no request and no file. | `TestForgedLogsAreRefused`, `FuzzForgedReplay` |
| Positive fs replay, fsync failure, cwd warning untested | Tested. `--resume` without `--allow` is now tested through the CLI. | `TestReplayOfFileEffects`, `TestLogSyncFailureStopsTheRun`, `TestReplayWarnsOfAnotherDirectory`, `TestResumeCutsAPartialLine` |
| `ReadLog` accepted bytes after the end line | Fixed: an error | `TestReadLogRefusesBytesAfterTheEnd` |

## Minor

| Finding | Disposition |
|---|---|
| `clock_ms()` across a resume could be negative | Fixed. Live readings in a resumed run continue from the largest `res - at` of the logged readings (`EffectLog.ClockOffset`). Test: `TestClockMSIsMonotonicAcrossAResume`. |
| Output order after a resume | Known limit (16 notes) |
| Forged log sets a resumed run's limits | Known limit (16 notes) |
| Forged log widens a resumed child within its parent | Known limit (16 notes). 16o now says that "cannot widen grants" holds for a replay, and is bounded by I2 in a resume. |
| Forged log hangs a replay | Known limit (16 notes) in round 1. Superseded: since round 2 no timer runs in a replay, so the header's timeout never ended it, and through the CLI (no host context) it hung, or aborted with Go's deadlock error. Since round 3 a replay that stalls diverges ([`16u`](16u-phase6-round3.md)). |
| `--record` kept an old file's mode (reported twice) | Fixed: `Chmod(0o600)` after opening. Test: `TestRecordMakesAnOldFilePrivate`. |
| `replay: grants` printed before they were checked | Fixed: `arv.LogGrants` checks them first. Test: `TestReplayChecksTheGrantsBeforeNamingThem`. |
| "effect N" counted checks | Fixed: effects are counted separately (`Task.effects`). Test: `TestDivergenceCountsEffectsOnly`. |
| `TestReplayOfRestarts` caught the wait by timing only | Fixed: 1 s and 2 s backoffs against the same 900 ms bound. With `wait()` in place of `Backoff`, the replay took 3.2 s. |
| `16-implementation-notes.md` over 18,000 bytes | Fixed. The Phase 3 deviations and limits moved, unchanged, to [`16p-phase3-summary.md`](16p-phase3-summary.md). |

## Mutation checks (round 1)

| Mutation | Caught by |
|---|---|
| `opts.Limits = runtime.Limits{...header...}` made a no-op | `TestReplayUsesTheHeaderLimits`, `TestReplayOfAScopeThatTimedOutAtClose` |
| `in.chargeBytes(o.Charged)` replaced by `_ = o.Charged` | `TestReplayChargesTheLoggedBytes` |
| `expired := in.Task.Expired(s)` replaced by `s.Expired()` | `TestReplayOfAScopeThatTimedOutAtClose` |
| `err = f.Truncate(l.Size)` replaced by `err = nil` | `TestResumeCutsAPartialLine` |
| `return last.Backoff(...)` replaced by `return wait()` | `TestReplayOfRestarts` (3.2 s against 900 ms) |
| quiet checks removed (the round-1 runtime files reverted) | `TestResumedChecksWaitForTheLog`, `TestResumedChecksKeepALoggedResult`, `TestResumeAtEveryCutProperty` (seed 7051) |
| quiet backoff's `hasRecords(next)` branch disabled | `TestResumedBackoffWithALoggedNextAttempt` |
