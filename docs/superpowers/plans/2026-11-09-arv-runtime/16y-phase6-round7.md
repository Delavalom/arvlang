# Phase 6: review round 7 (dispositions, known limits)

Continuation of [`16x-phase6-round6.md`](16x-phase6-round6.md). Indexed from the Phase 6 section of [`16o-phase-index.md`](16o-phase-index.md). The review found two Minor findings and nothing Critical or Important.

## Minor: a version 1 log looked the same as a new one (fixed)

**Finding.** Round 6 changed the order in which the recorder logs a task's end (before its slot is freed, `Task.Finished`), but the log still said `"v":1`. A log written by an earlier build of this branch, cut between a slot holder's release and its end, therefore resumed as if it were new. If the resumed holder then failed live, the resume diverged once every task waited, after any live deadline had fired, instead of resuming.

**Fix.** `LogVersion` is now 2, and `ReadLog` refuses a version 1 header with its own error: `effect log line 1: an effect log of version 1, from an earlier build; this build reads version 2: record the run again`. `--replay` and `--resume` both report it and exit with status 1 before anything runs. Version 1 logs exist only from earlier builds of this unmerged branch, so refusing all of them, including logs that would replay correctly, costs nothing outside development. Every other header version still gets the general `want a version 2 header first`.

**Tests.** `TestReadLogRefusesAnEarlierVersion` (package `runtime`) writes a chained log with a version 1 header and a record. Before the fix, `ReadLog` accepted it (`err = <nil>`). The structure tests now name version 2. The fixture `arv/testdata/resume/deadline-cut.log` was rewritten with `"v":2` and its chain recomputed (no other byte changed). `TestResumedLiveFailureWaitsForALoggedDeadline` passes on it. By hand, with the CLI built from this commit: a version 1 copy of a fresh recording (header changed, chain recomputed) gives the error above for both `--replay` and `--resume`, with status 1.

This supersedes round 6's second known limit ("Logs recorded before this round can still fail a resume").

## Minor: a forged `--resume` waits for the header's deadline, then reports the timeout (known limit, extended)

**Finding.** The finding confirms round 6's first known limit: a forged log (the first two lines of a recording, plus `root/0` `cancelled` at check 1 with no `by`) makes a resume wait until a live deadline fires. The header's `timeout_ms` sets that deadline. The finding adds a new detail. When the deadline fires, `awaitCancel("")` takes the deadline's cancellation as the record's cause. The run ends with `budget exceeded: timeout`, not with a divergence. In an honest log, a `cancelled` record with no `by` means "cancelled by a failure". A root deadline would have logged `by: "root:1"`.

**Disposition.** We kept the known limit and did not apply the tightening the review suggested ("in a resume, diverge when a record with no `by` is satisfied only by a deadline"). An honest resume can reach the same state:

- Recording, the failing task logs `failed` before its failure cancels the scope (`Task.Fail`). So in an honest log, a `cancelled` record with no `by` comes after the `failed` record of its cause.
- Resuming, the record with no `by` is counted as used as soon as it is consumed (`consume` holds only a record with `by`). Its task then waits on its context (`awaitCancel`). The cause is applied when the failing task replays its `failed` record.
- A live task inside the same timed scope fires that scope's deadline when its own clock passes `vdl` (`deadline.arm`). Its clock is the recorded run's clock, continued from its last record. On a loaded machine, a live sibling can reach `vdl` before the failing task has replayed up to its `failed` record. The deadline then cancels the waiting task first. This is the same live timeout that an uninterrupted run under that load can hit, and that round 6 accepted for honest cuts (cuts 1 to 5 of the 600 ms probe timed out live).

With the tightening, that honest resume would report `replay diverged`, which tells the operator the log was tampered with when it was not. Reporting the timeout is correct for the honest case. For the forged case it is wrong but harmless: the run still ends with an error, not with a result, and nothing live runs beyond the program's own effects. The tightening would also not shorten the forged wait: it would change only the message at the end. A forger who wants a timeout can write `by` anyway, and the log is then accepted as the record of a run that timed out, as the trust model allows (round 1's forged-header limit). A static check ("no `failed` record before a `cancelled` record with no `by`") can be evaded by forging a `failed` record as well, so it was not added either.

Separating the two cases would need the resume to know whether the replayed failure "would have come first". That is a timing fact the log does not hold for the live part. Round 2 ([`16r`](16r-phase6-round2.md)) chose to treat resumed deadlines as bounds, and this follows from that choice.

## Known limits (round 7)

These supersede both of round 6's known limits.

- **A forged resume can last until a live deadline fires, and the log sets that deadline. It then reports the timeout, not a divergence.** While a live deadline timer is set, the stall watcher waits for it instead of reporting a divergence. A live task waiting on its scope, on another task or on a slot keeps its scope's deadline armed, and the root scope's deadline is the header's `timeout_ms`. A resume takes its limits from the log, and the operator cannot override them. So a forged header can make the wait as long as it likes. `timeout_ms: 0` removes the timer, and the run then diverges in about 1 s. An honest CLI header has the 10-minute default unless `--timeout` was given. When the deadline fires, a forged `cancelled` record with no `by` is taken as matched by it, and the run ends with `budget exceeded: timeout (limit … ms)`. For example: record with `--timeout 5s`, forge, `--resume`, and the run ends with status 1 after 5 s. Nothing crashes, and nothing live runs beyond the program's own effects. The reasons for not diverging there are above.
- **Logs of version 1 are refused** (see above). Record again with this build.
