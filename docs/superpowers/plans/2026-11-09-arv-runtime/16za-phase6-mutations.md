# Phase 6: mutation check, round 1

Continuation of the Phase 6 notes ([`16o`](16o-phase6-notes.md), indexed in [`16o-phase-index.md`](16o-phase-index.md)). Run on 2026-11-12 on `phase/p6` at `e78dd39`.

**Method.** Each mutation was one edit to one runtime file, applied with a `perl` substitution, built, and then tested with `go test -count=1 -timeout 600s . ./arv/... ./e2e/` (the CLI package, every `arv` package, and the e2e suite through the binary). The file was then restored with `git checkout --`, and `git status` was checked to be clean before the next mutation. No mutation was committed. Load average was between 5 and 40.

**Result: 33 mutations, 31 caught, 2 not caught** (M10 and M24, both test gaps; see below). 16 of the caught mutations were caught by the e2e suite as well as by unit tests. 2 more (M01, M33) also hung a package until the 10-minute timeout.

**A baseline failure, not caused by any mutation.** `TestHostDeadlineIsNotLogged` (`arv/replay_hostdeadline_test.go`) fails on the unmutated tree, without `-race`, whenever the machine is lightly loaded. With `-race` it passes, and `go test -race ./...` was green before the commit. Its program recurses until a 20 ms host deadline cancels it. On a quiet machine, the 10 000-call depth limit is reached first, so the run ends with `stack overflow` and logs an `end` line, and the test reads that as the host's deadline being logged. This is a timing flaw in the test, not a runtime bug. The test was left as it is (this role adds no tests), and it was left out when deciding whether a mutation was caught.

## The table

Unit tests are in `arv/...` or the CLI package. Tests named `TestP6...` are in `e2e/`.

| # | Guarantee | Mutation | Caught | Catching tests |
|---|---|---|---|---|
| M01 | I7: a replay performs no live effect | `Task.Effect` replay branch also runs `do()` (every kind but `puts`) | yes | `TestReplayAnswersFromTheLog`, `TestReplayedSpawnGrants`, `TestRecordThenReplay`; e2e `TestP6ResumeAfterSIGKILL` ("resume repeated work: 4 /ok"), `TestP6ReplayOfRestarts`, `TestP6MismatchedLogs` (+ the `arv` package hung in `TestNoGoroutineOutlivesAnyResumeCut`) |
| M02 | I7/I3: identical effect results (alloc charges replayed) | drop `in.chargeBytes(o.Charged)` from the evaluator gate | yes, unit only | `TestReplayChargesTheLoggedBytes` |
| M03 | I7: byte-identical output | `output.emit` prints replayed lines as they arrive, not in logged order | yes | `TestReplayPrintsInRecordedOrder`, `TestReplayProperty`, `TestResumeProperty`; e2e `TestP6ReplayIsOffline`, `TestP6PropRecordReplay` |
| M04 | I7: a mismatched request diverges | `Task.Effect` compares `rec.Req` with itself, not with `req` | yes | `TestReplayDiverges`, `TestReplayOfAnEditedScriptDiverges`, `TestReplayOfAChangedPromptDiverges`; e2e `TestP6EditedScriptDiverges` |
| M05 | I7: codec round trip | encoder writes invalid UTF-8 as a plain string, not `{"bytes":...}` | yes, unit only | `TestCodecRoundTrip`, `TestCodecFormat`, `FuzzCodecRoundTrip` seeds, `FuzzDecode` seed |
| M06 | I7/I5: timing checks are logged | `observe` never logs what a check saw | yes | `TestReplayedCancellationIsSeenAtTheSameCheck`, `TestReplayFiresADeadlineInLogOrder`, `TestResumeKeepsTheDeadlines`, 17 more; e2e `TestP6PropRecordReplay` |
| M07 | I7: spawn-time lstat goes through the gate | `attenuate` always canonicalises live, never as the `spawn` effect | yes | `TestReplayRunsNoEffectLive`, `TestReplayedSpawnGrants`, `TestReplayProperty`; e2e `TestP6PropRecordReplay` |
| M08 | I2: a forged spawn record cannot widen a child | `loggedCaps` drops `Caps.Allows(c)` | yes, unit only | `TestReplayedSpawnGrants` |
| M09 | I1/I7: freeze at spawn is transitive (Phase 4) | a child's horizon holds only its own spawn time, not its parent's | yes, unit only | `TestChildNeverSeesALaterRebinding`, `TestChildHorizonIsExact`, `TestHorizon`, `TestPhase4Invariants` |
| M10 | resume: same result as an uninterrupted run (time) | `NewResumer` starts the run's clock now, not back-dated by the log's last `At` | **no** | none (see below) |
| M11 | resume: the appended log continues the chain | `NewResumer` starts the writer's chain at `""`, not `log.last` | yes | `TestResumeAfterACrash`, `TestResumeCutsAPartialLine`, `TestResumeProperty`, 20 more; e2e `TestP6ResumeAfterSIGKILL`, `TestP6ResumeAfterThreeOfFive` |
| M12 | resume: a partial last line is cut | `resumeLog.Write` never truncates or seeks | yes | `TestResumeCutsAPartialLine`; e2e `TestP6ResumeAfterAPartialLine` |
| M13 | resume: a task's end is logged before its slot is freed (round 6) | `Finished` frees the slot before appending `done` | yes, unit only | `TestTaskEndIsLoggedBeforeItsSlotIsFreed`, `TestFinishedOrdersItsEndAroundRelease` |
| M14 | resume: a task past its records goes live (deadlines armed) | drop `t.goLive()` from `Task.next` | yes, unit only | `TestResumeKeepsTheDeadlines`, `TestResumeOfAScopeWithoutTheOpenersRecords`, `TestNoGoroutineOutlivesAnyResumeCut` |
| M15 | tampering detected: hash chain | `verify` skips the chain comparison | yes | `TestReadLogRefusesTampering`; e2e `TestP6BadLogs`, `TestP6StructurallyBadLogs`, `TestP6EveryLineIsChained` |
| M16 | truncation detected: a replay needs a finished log | `NewReplayer` accepts a log with no `end` | yes, unit only | `TestResumeReplaysThenRecords` (the CLI checks again, so e2e passes) |
| M17 | truncation detected: a partial last line is reported | `ReadLog` sets `Truncated = false` | yes, unit only | `TestReadLogTruncated`, `TestReadLogRefusesBytesAfterTheEnd`, `FuzzReadLogTamper` |
| M18 | tampering detected: per-task `seq` | `add` skips the `seq` check | yes | `TestReadLogChecksStructure`; e2e `TestP6StructurallyBadLogs` |
| M19 | tampering detected: an output index used twice | `add` skips the duplicate-`out` check | yes, unit only | `TestReadLogChecksStructure` |
| M20 | mismatch detected: the replay's end differs | `Finish` skips the `end` comparison | yes, unit only | `TestReplayDiverges` |
| M21 | mismatch detected: unused records | `unconsumed` returns nil at once | yes, unit only | `TestReplayDiverges` |
| M22 | mismatched format refused (version 1) | `ReadLog` accepts a version 1 header | yes | `TestReadLogRefusesAnEarlierVersion`; e2e `TestP6StructurallyBadLogs` |
| M23 | never panic on any log | `verify` drops the minimum-length check (short lines slice out of range) | yes | `TestReadLogRefusesTampering`; e2e `TestP6BadLogs` |
| M24 | --replay installs only canonical header grants | `LogGrantsKinds` drops `c.Scope != g.Scope` | **no** | none (see below) |
| M25 | --replay installs the header's grants | `arv.Run` gives a replay's root no grants (`caps = nil`) | yes | `TestReplayOfFileEffects`, `TestReplayOfHostEffects`, `TestCLIReplayOfModelCallsNeedsNoKey`, 14 more; e2e `TestP6ReplayOfRestarts`, `TestP6ReplayAnswersFilesAndEnv` |
| M26 | --replay prints the grants it installs | drop the `replay: grants:` line | yes | `TestRecordThenReplay`, `TestCLIReplayOfModelCallsNeedsNoKey`; e2e `TestP6ReplayAnswersFilesAndEnv`, `TestP6ReplayIsOffline`, `TestP6PropRecordReplay` |
| M27 | --replay refuses --allow | `splitLogFlags` lets `--replay` take `--allow` | yes | `TestLogFlagErrors`; e2e `TestP6LogFlagsRefused` |
| M28 | replay and resume use the header's limits | `logSetup` drops the header's `timeout_ms` | yes, unit only | `TestReplayUsesTheHeaderLimits`, `TestForgedLogsAreRefused`, `TestResumeKeepsTheDeadlines` |
| M29 | no goroutine leaks | a quiet check's watcher goroutine blocks forever (`select {}`) once released | yes, unit only | `TestNoGoroutineOutlivesAnyResumeCut` ("5 goroutines outlived the run"), `TestResumeAndReplayOfDeadlinesLeaveNoGoroutines`, `TestForgedResumeWhoseSlotHolderFailsLiveDiverges` |
| M30 | mismatched resume grants refused | `arv.Run` skips `sameGrants` for a resume | yes | `TestResumeAfterACrash`, `TestResumeCutsAPartialLine`; e2e `TestP6ResumeAfterAPartialLine`, `TestP6LogFlagsRefused` |
| M31 | I7: a logged deadline fires where the log has it | `fireLogged` skips `waitFront` | yes, unit only | `TestForgedDeadlineAtTheWrongPlaceDiverges` |
| M32 | I5/I7: the scope's first failure is logged | `Task.Fail` never appends the `failed` check | yes | `TestReplayedFirstFailure`, `TestReplayOfFailFast`, 18 more; e2e `TestP6PropRecordReplay`, `TestP6PropKillAnywhereResume` |
| M33 | a forged log ends with a clean error, never hangs | stall detection never fires (`stallRounds = 1 << 40`) | yes, unit only | `TestForgedCancellationDivergesInsteadOfHanging`, `TestCLIForgedCancellationIsACleanError`, 5 more (+ the `arv` package hit its timeout) |

## Not caught

- **M10 (a test gap; a real defect if it ever regressed).** In a resume, each task's clock (`Task.now`) runs from the log's `At` values (`vbase`, `rbase`), so deadlines and record times do not depend on `EffectLog.start`. The one reader of `start` in a resume is `clock_ms` (`EffectLog.Elapsed`, through `ClockOffset`). With the mutation, a live `clock_ms()` reading after a resume is `offset + time since the resume`, not `offset + time since the crashed run started`, so it can be smaller than a reading the crashed run logged. For example, a program that sleeps 500 ms, reads `t0 = clock_ms()`, is killed, and is resumed reads a later `clock_ms() - t0` about 500 ms short, and it can be negative. No test reads `clock_ms` both before and after a resume cut late in the run. Suggested test: record `sleep(500); let t0 = clock_ms(); sleep(50); puts(clock_ms() - t0 >= 0)`, cut the log after the `clock_ms` record, resume, and assert `true` and a difference of at least 0.
- **M24 (a test gap, low impact).** `forge_test.go`'s "uncanonical" case forges `127.0.0.1:0`, which `runtime.Literal` rejects outright, so the scope comparison is never the check that refuses it. A header grant that `Literal` accepts but normalises (for example `net("LOCALHOST")`, or a port with a leading zero) passes with the mutation, and the replay installs the normalised grant instead of refusing the header. It is the same authority the forged text names, so the I2 bound holds, but a forged non-canonical header is no longer detected. Suggested test: forge the header scope to an accepted but non-canonical form and expect "not a canonical capability".
