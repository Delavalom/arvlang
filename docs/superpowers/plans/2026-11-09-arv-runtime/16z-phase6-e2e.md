# Phase 6: end-to-end, fuzz, property and leak tests

Continuation of [`16y-phase6-round7.md`](16y-phase6-round7.md). Indexed from the Phase 6 section of [`16o-phase-index.md`](16o-phase-index.md). This pass added tests only. It found one Minor bug, now fixed, one cosmetic behaviour and one harness flake (below).

## End-to-end, through the real binary (`e2e/`)

`p6_replay_test.go` (from the build) already covered: an offline replay of a fan-out that sleeps 6 s, an edited script that diverges, a replay of restarts, a SIGKILL during a request followed by `--resume`, and bad logs. This pass added:

- **`p6_accept_test.go`**
  - `TestP6ResumeAfterThreeOfFive`: the design's acceptance test. The run is killed while the fourth of five sequential requests is held at a `/wait` gate, and the log then holds 3 `http_get` records. The resume makes exactly 2 new requests: the held request again, and the fifth. Its output equals that of an uninterrupted run. A replay of the resumed log makes no request.
  - `TestP6RestartAttemptsInTheLog`: the attempts are logged as `root/0`, `root/0#2` and `root/0#3`, in that order, and a replay answers them in that order.
  - `TestP6ReplayAnswersFilesAndEnv`: `read_file`, `write_file`, `getenv`, `clock_ms` and a spawned reader. The replay runs with no `--allow` and a different environment, after the data directory has been deleted. It prints the same output, names the header's grants, and creates no file.
  - `TestP6LogFlagsRefused`: `--replay` with `--allow` or a limit flag, `--resume` with a limit flag or other grants, `--record` with `--replay`, `--fsync` with `--replay`, and a missing log. Each is a clean error, makes no request and leaves the log unchanged. A finished log given to `--resume` replays.
  - `TestP6ResumeAfterAPartialLine`: a log is cut in the middle of a line. A replay of it is refused ("did not finish"). A resume with other grants is refused and leaves the file unchanged byte for byte. A resume with the recorded grants runs the cut effect again, and a replay of the result matches the recording.
- **`p6_tamper_test.go`**
  - `TestP6EveryLineIsChained`: one bit is flipped in each line, at a key, in the middle and in the hash. Both `--replay` and `--resume` refuse every such log, and the file is not changed.
  - `TestP6StructurallyBadLogs`: each log here has its chain recomputed, so only the structure is wrong: swapped lines, a duplicated line, a version 1 header, a second header, and bytes after the end. A log with swapped lines and the old chain is also tested. Both modes refuse each one before anything runs.
  - `TestP6MismatchedLogs`: a log moved to another directory gives a warning that names the recorded directory, then replays. A log of a different script gives a warning, then diverges at effect 1 and names both requests.
- **`p6_prop_test.go`**: random programs. Each is a tree of spawned tasks that print, sleep, fetch, write and read files, read the environment, draw `random`/`now`, open scopes, and may fail once.
  - `TestP6PropRecordReplay` (40 programs): the recording is replayed with no `--allow`, a different environment and the file directory deleted. The replay prints the same stdout and the same error text, exits with the same status, makes no request and touches no file.
  - `TestP6PropKillAtGateResume` (20 programs, sequential): one request in each program waits at a gate, and the run is killed there. The resume matches an uninterrupted run in stdout, stderr, status and files written. Every request is made once, and the held one twice. A replay of the resumed log matches as well.
  - `TestP6PropKillAnywhereResume` (40 programs, sequential, may fail): SIGKILL comes after a random request count plus 0 to 3 ms. The resume matches the uninterrupted run (stdout, stderr, status, files), and at most one request is made again. About 12 of the 40 cases are killed before the run ends (the test logs the count and fails if none are). The others check that a finished log resumes as a replay.

All of these tests are parallel and use the server gates (`/wait`, `/arrive`), not sleeps. No wall-clock bound in them is shorter than a minute.

## Fuzzing

| Target | Package | Seconds | Execs | Found |
|---|---|---|---|---|
| `FuzzCodecRoundTrip` (new) | `arv/value` | 121 | 3,175,409 | nothing |
| `FuzzDecode` | `arv/value` | 122 | 3,105,611 | nothing |
| `FuzzReadLogTamper` (new) | `arv/runtime` | 121 | 4,411,147 | nothing |
| `FuzzReadLog` | `arv/runtime` | 122 | 10,257,200 | nothing |
| `FuzzForgedReplay` | `arv` | 124 | 2,369,184 | nothing |

`FuzzCodecRoundTrip` builds a value from the input: shared parts used in several places, strings that are not valid UTF-8, long strings (which are shared by reference), hash keys of every kind, errors, and chains of arrays and hashes around `MaxCodecDepth`. It checks four things. Encode then Decode gives the same value. Each shared part decodes to one part (a bijection). Re-encoding gives the same bytes. `EncodeRequest` of the value is `[` + Encode + `]`. A value nested past the limit is refused by Encode, never by Decode. `FuzzReadLogTamper` changes, inserts or deletes bytes in a recorded log, cuts it, or swaps two of its lines. `ReadLog` must then either refuse the log or read only whole lines that are a prefix of the original, and must never read an end line from a cut log.

## Leak checks (`arv/leak_p6_test.go`)

- `TestNoGoroutineOutlivesAnyResumeCut` uses five programs: a timeout with sleepers, `fail_fast` with sleepers, a restart, a wait for a slot at a deadline, and a collect scope. For each, it records, replays, resumes from a cut after every line of the log, and replays an edited script, which diverges. No goroutine survives any of these runs. No honest resume diverges, and none takes longer than a minute.
- `TestNoGoroutineOutlivesACancelledResume`: the host cancels a recording, and then a resume, while sleepers are live. No goroutine survives either.

## Findings

- **Minor: a refused `--resume` dropped a partial last line (fixed).** `logArgs.setup` used to truncate the file to its whole lines (`f.Truncate(l.Size)`) before `arv.Run` checked the grants or parsed the script. A resume refused for either reason therefore left the log one partial line shorter. That line could never be read (it has no hash), so nothing usable was lost, but a refused run should change nothing. The truncation now happens at the resume's first append (`resumeLog` in `main_log.go`). Run writes nothing to the log before its checks pass. Test first: `TestP6ResumeAfterAPartialLine` resumes with no grants, with fewer grants and with more grants, and requires the file to be unchanged byte for byte. Before the fix it failed (`log changed: true`). After the fix the accepted resume still drops the partial line and then replays.
- **Minor (cosmetic): `--resume` of a finished log with the wrong grants prints `replay: the run finished; grants: …` and then the error.** It exits with status 1 and runs nothing. `TestP6LogFlagsRefused` accepts the two lines in this order.
- **Harness flake, not Arv:** in one `go test -race ./...` run, `TestP6PropRecordReplay/17` failed with `signal: segmentation fault` and no output. The macOS crash report shows that `e2e.test` itself crashed, "on child side of fork pre-exec" (the race-instrumented test binary, between fork and exec). The `arv` binary never ran. The case passed in the next 10 runs of that test and in the full race runs after it. It is the known fragility of fork under the race runtime on darwin.

## Runs

`go test -race ./...` and `ARV_E2E_RACE=1 go test -race ./e2e/` (79 s) are green, and so is `go test -race -count=5 ./...` (see the commit that adds this file).
