# Phase 8.4: speed, e2e and hardening

Continuation of [`16p84-speed.md`](16p84-speed.md) (rounds 1 and 2: [`16p84b`](16p84b-speed-round1.md), [`16p84c`](16p84c-speed-round2.md)). End-to-end, fuzz, property and leak tests for the compiled engine. No bug was found in this phase's scope; nothing in `arv/` outside tests changed.

## E2E through the binary

The e2e suite builds one `arv` binary per engine (`e2e/p84_helpers_test.go`): the default build (compiled) and one with `-tags arv_treewalk` (the reference), each `-race` under `ARV_E2E_RACE`. The binary `ARV_E2E_TAGS` already made is reused. `spec.bin` (one new field in `harness_test.go`) picks the binary. Every case runs the same file on both binaries and requires the same stdout, stderr and exit code, and also the expected output, so the engines cannot agree on a wrong answer.

| Test | Guarantee | What |
|---|---|---|
| `TestP84BinariesAreTheTwoEngines` | setup | `go version -m` shows `arv_treewalk` in the walk binary's build settings only |
| `TestP84EnginesAgreeOnValues` (13 programs) | S1 | environments past four names, closures over loop variables, shadowing, shared integers (1023, 1024, -128, -129, hash keys), hashes and `.field`, recursion, `return` out of nested loops, `match`, builtins, `&&` `||` `<=` `>=` and interpolation, `return` inside `&&`, spawn/await of compiled functions, `collect`, scope `steps`, top-level `return` |
| `TestP84EnginesAgreeOnErrors` (17 programs) | S1, S4, I1, I2 | each error a compiled node raises with its message and position: call depth (10 000) and evaluation depth (100 000), type errors, arguments, `.field` on an integer and through nil, unknown names, non-functions, `<=`/`>=` mismatches, division, `for` over an integer, hash keys, a task's error, `restart` giving up, `getenv` without a grant (I1), a spawn asking for more than its parent holds (I2) |
| `TestP84REPL` | S1 | a function made on one line is called, compiled, on later lines and in a spawned task; errors print the same |
| `TestP84StepsExactOnBothEngines` (5 programs) | S2 | bisection of `--steps` through the binary: the same smallest limit on both engines, and with one step fewer the same error at the same node (for tasks sharing the budget, only the total) |
| `TestP84AllocCutOnBothEngines` | S2 | `--alloc-mb 1` and `2` stop a growing program after the same printed line with the same error and position |
| `TestP84TimeoutStopsCompiledCode` | S3 | `--timeout 300ms` stops nine nested loops in the root and four tasks with the same message; bound 30 s |
| `TestP84ScopeTimeoutStopsCompiledCode` | S3, I6 | `scope (timeout_ms: 200)` and `scope (steps: 5000)` around compiled loops; the program goes on after |
| `TestP84FailFastStopsCompiledCode` | S3, I5 | three spinning tasks arrive at a server gate; a fourth waits for them and fails; the run ends with its error; bound 60 s |
| `TestP84ReplayOnTheOtherEngine` | S5, I7 | net, env, time, randomness and sleep in the root and in tasks, long strings shared between values: recorded on one engine, replayed offline on the other, both ways |
| `TestP84StepsCutReplaysOnTheOtherEngine` | S2, S5 | a run cut by `--steps` (200, 437, 650) between effects replays on the other engine to the same output and error |
| `TestP84ResumeOnTheOtherEngine` | S5, I7 | a log cut at half its lines resumes on the other engine; a replay of the resumed log on the first engine prints what the resume printed |

A shared budget spent by tasks running at once is left out of the replay tests on purpose. Which task runs out first is a race. A replay of such a run diverges on either engine (`replay diverged at task root/N, check K: the log has failed`). Checked here with 16 parallel loops of record and replay (`--steps` 200 and 400, 96 runs per engine pair): 11 to 15 replays diverged for each pair, compiled to compiled and walk to walk included. The base binary (b516ca2) diverged in 26 of 160. This is the Phase 6 known limit "some outcomes are detected, not reproduced" (`16o-phase6-notes.md`), not an engine difference. The sequential program used in the tests had 0 divergences in 576 record and replay runs under the same load.

## Fuzz targets

Each ran in the foreground with `-fuzztime=2m`, after commit 6b4020e. Load average 12 to 50. No failure, so no corpus entry was committed.

| Target | Package | Seconds | Execs | New interesting (total) |
|---|---|---|---|---|
| `FuzzEnginesAgreeSource` (new) | `arv/evaluator` | 121.4 | 2 583 929 | 486 (757) |
| `FuzzEnginesAgreeStatements` (new) | `arv` | 121.5 | 108 898 | 35 (45) |
| `FuzzEnginesAgree` (p84.6) | `arv` | 121.5 | 55 031 | 109 (431) |
| `FuzzDesugarEquivalence` (Phase 5E, now on compiled code) | `arv` | 120.7 | 180 722 | 177 (585) |

- **`FuzzEnginesAgreeSource`** mutates raw source, seeded with the 57 programs of the differential corpus. A program that parses must give the same outcome (value or error, kind, position, task, output) on both engines under a cap (20 000 steps, 1 MiB) and under four tight limits. If the cap does not stop it, it must also be charged the same steps and bytes (bisection below the cap). Sources naming `spawn`, `random`, `now`, `sleep` or `timeout_ms` are skipped: their outcome can change between two runs on one engine. Most mutations do not parse, hence the high exec rate.
- **`FuzzEnginesAgreeStatements`** decodes bytes into whole programs (`arv/engine_stmt_test.go`, `sgen`). It writes top-level functions that call only earlier ones (so every run ends) and values. Their bodies use `let` in blocks, `if ... { return ... }`, `puts`, `for` over arrays, `match` with `e.kind`/`e.message`/`e.pos`, hash literals with string, integer and boolean keys, index, `.field`, an immediately called closure, `scope`, `await spawn`, interpolation of locals, and every infix operator including `&&` `||` `<=` `>=`. The Phase 5E expression generator supplies the leaves. Names can be out of scope and values of the wrong type, so the errors are compared too. Same checks as above, with a cap of 200 000 steps and 8 MiB. Of 300 sampled programs, 108 ran to the end and 192 stopped with a runtime error. None failed to parse.

## Property tests

- `TestEnginesAgreeOnStatementsProperty`: 150 fixed seeds of `sgen` (20 under `-short`), same checks as the fuzz target. About 1.2 s.
- The existing `TestEnginesAgreeProperty` (200 seeds), `TestEnginesAgreeOnCorpus` and `TestDesugaringEquivalenceProperty` (300 seeds, now on compiled code by default) are unchanged.

## Leak checks

`arv/leak_p84_test.go`. On each engine, it counts goroutines before and after (with `settle`) eight runs: compiled closures in tasks, fail-fast cancelling two spinning tasks, `restart` (3 retries), `collect`, steps running out in three tasks, a scope's `timeout_ms` over a task and the scope's own loop, the run's timeout, and a host cancel of the root and two tasks. A second test runs eight programs with tasks at once on each engine. No goroutine outlived `Run`. `TestEnginesObserveCancellationPromptly` (p84.6) already covers an endless program on both engines.

## Full runs

At commit 6b4020e:
- `go test -race ./...`: green, except one e2e flake in `TestP6PropKillAnywhereResume/14`. The non-race `arv` child died with `signal: segmentation fault` after 18 ms, with empty stdout and stderr, in the uninterrupted reference run. A Go fault prints a trace, and this child printed nothing, so it most likely died in the `-race` test process's fork, before `exec`. That is the same kind of TSan-on-darwin flake as the exit code 66 recorded in `16p84-speed.md`. Re-runs: `go test -race -count=1 ./e2e/` was green, and `-count=15` of the test both with and without `-race` was green.
- `ARV_E2E_RACE=1 go test -race -count=1 ./e2e/`: green (128.5 s, load 50).
- `ARV_E2E_TAGS=arv_treewalk go test -race -count=1 ./e2e/`: green.
- `go test -race -count=5 ./...` (first try, default 10 min timeout): `arv` hit the timeout (one `-race` pass of `arv` takes about 3 min), and `TestForgedLogsAreRefused` failed twice: `times reversed: err = <nil>, want "out of order"`. Every other package was green.
- `go test -race -count=5 -timeout 90m ./...`, after the fix below: every package green, `arv` (869 s) and `arv/evaluator` (280 s) included, except `e2e` (see the next section).
- `go test -count=5` and `-count=10 ./e2e/` (test process without `-race`): green.

## Children that crash before exec under a `-race` test process (environment, not Phase 8.4)

Under `go test -race ./e2e/`, an `arv` child sometimes dies with `signal: segmentation fault` after 5 to 18 ms, with empty stdout and stderr. In this phase's runs: 1 to 3 per `-count=5`. It hit new tests (`TestP84StepsExactOnBothEngines`) and old ones (`TestP6LogFlagsRefused`, `TestP6PropRecordReplay`, `TestP6PropKillAnywhereResume`). One crash was a `--replay` run that the CLI refuses before any Arv code runs. The evidence that the crash is in the forked child, before `exec`:

- A Go program that faults after the runtime starts prints a trace. These children printed nothing.
- Two forked children of `e2e.test` were found spinning for 17 and over 70 minutes. Each had about 0.5 KB RSS, the name and arguments of `e2e.test` rather than `arv`, and its working directory already set to the test's temp dir: forked, not yet exec'd. The 17-minute one was this phase's (killed); the other was an orphan of another run on the shared machine.
- With a test process built without `-race`: 0 crashes in 15 `-count` passes of `./e2e/`. Run straight from a shell, 6 000 runs of the binary gave 0 crashes. A small `-race` Go program forking 4 800 runs also gave 0.
- It happens on the base too. The main checkout, b516ca2, with no Phase 8.4 code: 1 crash in 3 `-race -count=5` runs (`TestP6PropRecordReplay/7`). With `ARV_E2E_TAGS=arv_treewalk -skip TestP84` here, the tree-walker binary only: 1 crash in the first `-count=5` run, and a stuck child in the second.

It looks like the race runtime's fork handling on this darwin (25.6, Go 1.26.2); compare the TSan exit code 66 in `16p84-speed.md`. No test was changed for it. `ARV_E2E_RACE=1 go test -race ./e2e/` (race-built binary) was green both times it ran in this phase.

## A Phase 6 test that depended on the machine being slow

`TestForgedLogsAreRefused/times reversed` forged a record that ends before it begins. It did this by inserting `"began":99999999` before the first `"at":` in the log. `at` is milliseconds since the run started, and it is left out when it is 0. When the run took under 1 ms, no line had an `"at":`. The edit then changed nothing, and the honest log replayed without an error. On a quiet machine (load about 4) it failed 199 of 200 runs here (`-count=200`) and 198 of 200 on the base, b516ca2. It is not an engine difference. Under load, the recordings were slower and the test passed.

Fixed in `arv/forge_test.go`, without weakening it. The edit now appends `"began":99999999` to the first record. A record's `at` is a few milliseconds, or 0, so `at < began` always holds, and the log is refused with "out of order" as before. After the fix: 200 of 200 runs passed, and 20 of 20 under `-race`. The product code did not change.
