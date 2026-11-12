# Phase 6: review round 3

Review round 3 reported 2 Critical, 6 Important (two of them the same race as a Critical) and 10 Minor findings. Every Critical and Important finding was fixed test-first: the regression test was written and seen to fail for the right reason before the fix. Minors were fixed the same way, or are recorded below as known limits with a measured number.

## What changed

### `feat/phases-3-8` merged; Phase 7 and 8.2 effects go through the gate

`phase/p6` was 99 commits behind the integration branch (Phase 7, 8.1, 8.2). It is merged (`b41de07`), not rebased: one conflict resolution instead of 27 replayed commits. The per-phase notes moved to [`16o-phase-index.md`](16o-phase-index.md) on the integration branch; the Phase 6 section is there now.

- **`ask` and `agent`** make two effects each (`arv/evaluator/llmgate.go`). `llm_model` asks the host for a model's provider, ID and price (request: the builtin's name and the model; result: the ID and price, or the error). `llm` is one provider call (request: the model and the whole `llm.Request` as JSON; result: the reply, or the error with its text, kind and the `*llm.Error` fields that `CostNothing` reads). A replay needs no `llm.Host`, so the CLI replays without `ANTHROPIC_API_KEY` and sends nothing. The reservation from `tokens` and `usd_cents`, the `alloc_mb` charge for the reply and the tool calls are the program's own work and are done again. A recording goes on with the reply as the log has it (its raw JSON compacted), which is what a replay reads back, so the input estimate is the same in both.
- **Host effects** (Phase 8.2): `hostGate`, the `SEAM(p6)`, is now `logHost`. A call is logged as `host:<name>`: a host's name cannot hold `:`, so it never clashes with a built-in effect or with the `done` record kind. The capability check and the `t.Err()` check stay before the gate. Recording, the effect logs what the call came to as an Arv value: the error as `readError` makes it (its look at cancellation is live, so it happens inside the effect), or the result as data (`hostValue`). `hostResult` then checks, normalises and charges it in both runs. A replay must bind the same `Effects` and `Kinds`, but never calls `Fn`.
- **Classification.** `TestEveryBuiltinIsPureOrAnEffect` has a third class, `ask` and `agent` (no effect themselves, they make effects inside), and `llm` and `tool` are pure constructors. `TestEveryHostBuiltinIsGatedOrPure` covers the builtins a host binds per run: each effect logs one `host:<name>` record and each kind constructor logs nothing. `arv check` knows `now` and `random` (pure: no grant).
- **The header** records the `tokens`, `usd_cents` and `max_turns` limits. `--replay` refuses `--manifest` (its grants come from the log), and the limit flags list includes the new ones.

### The first failure of a scope is in the log (Critical)

`Task.Fail` (`arv/runtime/fail.go`) replaces `Scope.Fail` for a task's failure (`SuperviseWith`'s new `fail` argument) and for a scope body's.

- **Recording.** The task whose failure is first logs the check `failed`, inside `Scope.fail`'s `once`, before the scope is cancelled. So every cancellation the failure causes is logged after it. If the scope's deadline had passed first (the recorded failure is then the timeout), the record's `by` names the scope.
- **Replay.** The failure the log marks first is recorded (after firing the named deadline, if any), and only then is its record used. Any other failure waits until the scope has a failure (`awaitFailure`).
- **Resume.** A live failure waits likewise until the log is used up: every logged failure came before the crash.
- A scope that never started a task has nothing to race, and logs nothing (`supervised`), so most logs do not change.

### A host's passed deadline is never logged (Critical)

`liveErr` reports `context.DeadlineExceeded` once `Options.Ctx`'s deadline has passed, before its timer fires. A busy run's check then logged a cancellation that no deadline in the log names, and the run finished its log. A replay of that honest log waited forever; through the CLI the Go runtime aborted with "all goroutines are asleep". `EffectLog.aborted` now counts a passed host deadline as the host cancelling the run: from then on nothing is logged, and the log is unfinished.

### A replay that stalls diverges (Important)

A replay runs nothing live, so every wait in it is for another task: a record being used, a cancellation or first failure the log says happened, a task's end, or a slot. If every task waits, nothing can wake any of them: the log does not fit the run. `arv/runtime/stall.go` counts a replay's running tasks (`Enter` at a spawn, `Exit` at its end, `Park` around every replay wait, the evaluator's `Scope.Wait` included). Once none runs, and that lasts 50 checks 20 ms apart with no task parking or waking, the replay diverges: "every task waits for something the log says happened, which nothing in this run will do". A woken task is counted only once it runs, so a wake is seen within the window. The watcher is joined in `Finish`. `replayForged` and `FuzzForgedReplay` now run with no host deadline.

### Shared values encode once (Important)

`value.Encode`, `EncodeRequest` and `Decode` (`arv/value/codec.go`) number every array, hash, error and string longer than `RefMinLen` (16) bytes in the order the encoding meets it, and write a repeat as `{"ref":n}`. A value with n levels of `[a, a]` now encodes in O(n), not O(2^n). `Decode` shares the values again, so a value's shape is the same in a recording and in its replay. That matters because `hostWalk` charges a shared node once. A reference must name a value already read in full.

The review's repro (`dag2.arv`, n = 22, `--record`) went from 2.83 s, 1.56 GB RSS and a 298 MB log to 0.00 s, 11.7 MB RSS and a 1111-byte log.

### Seed 40019 (Important)

`TestTimedProgramsReplayAndResume` now runs `checkTimedSeed`. Any divergence saves its log (the cut log for a resume) to `ARV_DIVERGENCE_DIR` or the system's temporary directory, and names the path and seed. `TestTimedSeed40019EveryCut` (with `ARV_LONG=1`) sweeps every cut of seed 40019 `ARV_SWEEP_REPS` times; `ARV_SWEEP_SEEDS=first-last` sweeps other seeds. Ten sweeps of seed 40019 at every cut under `-race` passed (83 s, load average about 5). The 16r divergence did not recur. A debug sweep from round 2, before its deadline fixes, did catch it once: "RESUME case 40019 cut 38/51: ... the log has puts("flaky"), the program asked for puts("task root/0/0/1 cancelled while awaiting root/0/0/1/2: budget exceeded: timeout (limit 60 ms)")". The recording's first failure was the task's own; the resume's was the scope's timeout. That is the race the `failed` check (with its `by`) now logs. No log was kept then; the harness now keeps one if it recurs.

## Dispositions

| Finding | Fix | Regression test | Mutant caught |
|---|---|---|---|
| Critical: two simultaneous `fail_fast` failures diverge (and the Important duplicate) | `Task.Fail`, `failed` check | `TestReplayedFirstFailure`; `TestTimedProgramsReplayAndResume` without `failureRace` | no logging: yes; a loser that does not wait: yes |
| Critical: host deadline makes an unreplayable log | `aborted` counts a passed deadline | `TestHostDeadlineIsNotLogged` (failed 3 of 3 before) | the test itself |
| Important: shared values encode exponentially | `{"ref":n}` | `TestCodecSharesRepeatedValues`, `TestDecodeRejectsBadRefs`, `TestSharedValuesLogSmall` | the tests hang without refs |
| Important: Phase 7/8.2 effects bypass the gate | merge; `llm_model`, `llm`, `host:<name>` | `TestReplayOfModelCalls`, `TestReplayOfAChangedPromptDiverges`, `TestReplayOfHostEffects`, `TestEveryHostBuiltinIsGatedOrPure`, `TestCLIReplayOfModelCallsNeedsNoKey` | live `hostGate`, ungated `llm_model`, ungated `llm`: all caught |
| Important: seed 40019 open with no harness | saved logs, long sweep | `TestTimedSeed40019EveryCut` | n/a |
| Important: forged log kills the CLI | stall watcher | `TestForgedCancellationDivergesInsteadOfHanging` (with leak check), `TestForgedDeadlineAtTheWrongPlaceDiverges`, `TestCLIForgedCancellationIsACleanError` | watcher off: hangs |
| Minor: finished-log resume untested, no grants line | CLI prints "replay: the run finished; grants: ..." | `TestResumeOfAFinishedLogReplaysIt`, `TestResumeOfAFinishedLog` | |
| Minor: host-cancelled replay reports a divergence | `diverge` reports the host's cause once it cancelled | `TestHostCancelledReplayIsNotADivergence` | yes |
| Minor: `--fsync` untested | `syncOf` seam | `TestFsyncRunsAfterEveryLine` | record and resume lines: yes |
| Minor: 16q says the header's timeout bounds a hang | 16q, 16o-phase-index corrected | | |
| Minor: stray fields on header and end lines | `bare`; a check may not be charged or cancelled | `TestReadLogChecksStructure` | yes |
| Minor: logged `puts` prints nothing when a later argument fails | prints the lines before the failure | `TestLoggedPutsPrintsTheLinesBeforeAFailure` | yes |
| Minor: `--record` empties its file before parsing | `lazyLog` opens it at the header | `TestRecordOfABadScriptKeepsTheOldLog` | yes |
| Minor: copies made while recording a result; 45x for control bytes | known limit, measured (below) | | |
| Minor: the working directory is unlogged, `arv.Run` does not warn | known limit (below) | | |

## Known limits (round 3)

- **What recording a result costs (Minor, measured).** A recorded effect's result is copied about three times without a charge: `value.Encode`, `json.Encoder`'s compaction of `Res` into its buffer, and the line. `puts` copies its text into `Req` the same way. Control bytes expand 6x (`\u00XX`) and invalid UTF-8 4/3 (base64). Measured with the release binary: `read_file` of 8 MiB, then `puts` of it, under `--alloc-mb 20`, has 29.3 MB maximum RSS without a log. With `--record` it is 88.6 MB for plain text (a 16.8 MB log) and 378 MB for 0x01 bytes (a 100.7 MB log). It is bounded by the charged result, a constant factor of it, so it is Minor under the `alloc_mb` rule. Streaming the line to the file would remove the two buffer copies.
- **Model requests are logged whole, every turn.** An agent's `llm` record holds the whole conversation, so the log grows with the square of the turns, and the JSON copy of the request is not charged. Both are bounded by the conversation, which is charged.
- **A host effect's arguments compare loosely.** A function or capability argument is logged as its type only, so a replay that passes another one does not diverge there.
- **A live failure in a resume waits for the log to be used up** before it can be its scope's first. A live task's failure that the crashed run would have made before an unused logged one cannot happen: the logged one was before the crash.
- **Stall detection is a replay's only, and takes about a second.** (Corrected in round 4, [`16v`](16v-phase6-round4.md): through the CLI, a forged log given to `--resume` did not hang but crashed with "all goroutines are asleep". A resume now watches for stalls too.) A forged log given to `--resume` can still hang its replayed part until the host cancels it, as in round 2. An honest replay never stalls; the watcher's 50 checks also need the Go scheduler to run a woken task within a second, which it does with an idle P to steal it.
- **The working directory is read, not logged** (16o). Capability constructors resolve relative paths against `os.Getwd`. Only the CLI compares `Header.Cwd` with the current directory and warns. An embedder that replays from another directory gets a divergence at the first spawn whose request names a relative path; it should compare `Log.Header.Cwd` itself before `arv.Run`.
