# Phases 3-8 report

This is the final report for the Arv runtime Phases 3 to 8. It covers branch `feat/phases-3-8` at `40e7c95` (local, not pushed). The branch is 451 commits ahead of `origin/main`; its diff adds about 53,400 lines in 522 Go files. Part 2, [`18a-phases-3-8-fuzzing-and-limits.md`](18a-phases-3-8-fuzzing-and-limits.md), holds the fuzzing table and the known limits by phase. The notes, indexed in [`16o-phase-index.md`](16o-phase-index.md), are the record; this file sums them up.

## Summary

- All thirteen work items are built and merged: Phases 3, 8.3, 4, 5, 8.1, 8.2, 7, 6, 5E, 6x7, 8.4, 8.5 and the final review fixes.
- The Phase 5 gate passed: criteria 1 to 5 hold on numbers, and the owner said yes to criterion 6.
- Every phase with a mutation round ends with no uncaught behaviour-changing mutant, except two known equivalent mutants and one Phase 3 pass whose totals were not written down.
- The final verification run passed: the whole suite five times under `-race`, and the e2e suite on a race-built binary. One known darwin race-detector fork hang was seen and passed on rerun. No real failure.
- The Phases 0-2 e2e cases all pass. Each expectation that changed has a written reason.

## What was built, phase by phase

In the order the phases were built.

**Phase 3: budgets** ([10](10-phase3-budgets.md), [16c](16c-phase3-notes.md), rounds [16e](16e-phase3-round7.md) to [16i](16i-phase3-round11.md), [16p summary](16p-phase3-summary.md)). Scope options `timeout_ms`, `tasks`, `steps` and `alloc_mb`, and the matching CLI flags. Limits only shrink as they are inherited (I6). `for ... in` and `await [..]` make fan-out possible without recursion. A live-task cap (default 1000) and a shared evaluator-depth pool. No builtin or spawn starts after a deadline or after a spent `steps` or `alloc_mb` budget, even one a sibling spent. `alloc_mb` charges environments, closures, readers' chunks, concatenation before it is built, capability values, spawn requests and path canonicalisation. A denied fs request always costs exactly `prepaidCost(path)`, so `alloc_mb` is not an oracle on files outside the grants. Error text is bounded. Eleven review rounds.

**Phase 8.3: network hardening** ([16-implementation-notes](16-implementation-notes.md) §8.3, [16b](16b-phase83-notes.md)). `read_file` and `write_file` open through an `os.Root` at the outermost fs grant of the task's lineage, then step down one checked component at a time. Port grants (`net=host:443`) and CIDR grants (`net=10.0.0.0/8`). Every request names its port. A dial-time check on every connected address refuses SSRF through a granted name and DNS rebinding. `*` covers no internal address (I2). `go.mod` moved to `go 1.24.0`.

**Phase 4: supervision** ([11](11-phase4-supervision.md), [16j](16j-phase4-notes.md), [16k](16k-phase4-hardening.md)). Six error kinds (capability, budget, cancelled, effect, runtime, user), kept across task boundaries. `error(msg)`, string `==`, and `match ... { ok(v) => ..., err(e) => ... }`. `scope (supervise: "fail_fast" | "collect" | "restart", retries, backoff_ms, retry_on)`. Restart backoff doubles, is capped at 30 s, and has jitter derived from the task ID, so it replays. I8 (environments frozen at spawn) holds transitively. `collect` never drops an error.

**Phase 5: the gate** ([12](12-phase5-gate.md), [16m](16m-phase5-notes.md), [16n](16n-phase5-round1.md), [gate report](gate-report.md)). Builtins `lines`, `split`, `json` and `clock_ms`. `bench/farm` (a target farm), `examples/uptime.arv` (28 code lines), the Go baseline `bench/uptime-go` (115 code lines), `bench/run.sh` and the escape probe. Verdict below.

**Phase 8.1: `arv check`** ([16p81](16p81-check-notes.md), [16p81b](16p81b-check-round1.md), [16p81c](16p81c-check-round2.md), [16p81d](16p81d-check-hardening.md)). Package `arv/check` and `arv check FILE`: it prints the least set of `--allow` grants a program needs, and a note for every effect it cannot fold, without running the program. `// arv:allow` headers and `arv run --manifest`, which refuses to start if a need falls outside the header. Every syntax form and every builtin is classified, and tests fail when a new one is not.

**Phase 8.2: host effects** ([16-phase82](16-phase82.md), [hardening](16-phase82-hardening.md)). `arv.Options{Caps, Kinds, Effects}`: a Go host declares its own capability kinds and effects, for example `k8s_apply` under `k8s("staging")`. Host kinds attenuate like built-in ones (I2). Results must be data; they are normalised and charged in full by a memoising, cancellable walk. `examples/host` and the test host `e2e/p82host`.

**Phase 7: LLM, tools, agents** ([14](14-phase7-agents.md), [16o-phase7](16o-phase7-notes.md), reviews [16p](16p-phase7-round1.md) and [16q](16q-phase7-round2.md), [hardening](16r-phase7-hardening.md)). Capability kinds `llm` and `tool`. Effects `ask(model, prompt)` and `agent(model, goal, tools)`. A tool the task does not hold goes back to the model as an error result. Every prompt, goal, tool description and tool result is redacted: no grant list, task ID, host path or internal address reaches a model. `tokens`, `usd_cents` and `max_turns` budgets, reserved before each call and settled after it. The `arv/llm` package with a fake provider and the Anthropic Messages API. The API key is removed from the environment, procfs reads of it are refused, and replies that echo it are refused.

**Phase 6: durable replay and resume** ([13](13-phase6-durable-replay.md), [16o-phase6](16o-phase6-notes.md), rounds [16q](16q-phase6-round1.md), [16r](16r-phase6-round2.md), [16u](16u-phase6-round3.md) to [16y](16y-phase6-round7.md), [e2e](16z-phase6-e2e.md)). Ordered hashes and a value codec. A JSONL effect log keyed by task ID and sequence, with a SHA-256 hash chain. Every effect passes one gate, including `now`, `random`, `clock_ms`, `spawn`, model calls and host effects. Timing outcomes are logged as numbered checks, and deadlines are in the log. `--record`, `--replay`, `--resume` and `--fsync`. A replay is offline and runs no timers. A resume after SIGKILL continues the run. Seven review rounds.

**Phase 5E: ergonomics** ([16pe](16pe-ergonomics.md), [hardening](16pe-hardening.md)). `r.field`, `<=` and `>=`, `&&` and `||`, and `"${expr}"`. The parser lowers each form to existing nodes, at the same cost as the hand-written form. Added in the same wave as Phases 6 and 7, by the owner's criterion-6 decision. `examples/uptime-ergonomic.arv`.

**Phase 6x7: replayable agents** ([16zb](16zb-p67-notes.md), rounds [16zc](16zc-p67-round1.md) and [16zd](16zd-p67-round2.md), [e2e](16ze-p67-e2e.md)). A replay of an agent run is byte-identical, with no provider call and no tool effect. No log holds the API key. A resume after SIGKILL mid-agent sends only the call in flight and later ones. Model budget outcomes are logged (`llm_reserve`, `llm_settle`), so concurrent agents replay alike. Host effects record, replay and resume. Log format version 3.

**Phase 8.4: speed** ([16p84](16p84-speed.md), rounds [16p84b](16p84b-speed-round1.md) and [16p84c](16p84c-speed-round2.md), [e2e](16p84d-speed-e2e.md)). A closure compiler is now the default engine; the tree-walker stays as the reference (`-tags arv_treewalk`). Both engines give the same values, steps, bytes and logs. Speed-up 1.24x to 1.69x on the seven CPU benchmarks, and about a third of the allocations on `calls`. The design hoped for 2-5x; the bytecode VM was not built.

**Phase 8.5: distribution** ([16p85](16p85-distribution.md), [e2e](16p85b-distribution-e2e.md)). The command moved to `cmd/arv` (`go install github.com/delavalom/arvlang/cmd/arv@latest`). `arv version` prints the version and commit. `make build` and `make install` stamp the commit. `.goreleaser.yaml` builds darwin and linux on amd64 and arm64; it was never run. Nothing was released, pushed or tagged.

**Final review fixes** ([16zg](16zg-final-fixes.md)). The `TestP6PropRecordReplay` flake was a harness race (a request still in the kernel after the record exited); the suite now drains connections before it reads counters, and I7 holds. `ARV_ANTHROPIC_URL` is accepted only for https or loopback. `::a.b.c.d` addresses are classed by their IPv4 part. The two `t.Skip("BUG")` tests are fixed: a `return` that ends an `if` condition (and so `&&`, `||`) now leaves the function, and a negative `Policy.Backoff` counts as 0.

## The gate (Phase 5)

Full numbers, setup and method: [`gate-report.md`](gate-report.md). Measured at `9513540` on an M1 Pro, 10 alternating rounds per version on the default farm (200 targets, 50-400 ms).

| # | Criterion | Arv | Go | Arv / Go | Threshold | Holds |
|---|---|---|---|---|---|---|
| 1 | Code lines | 28 | 115 | 0.243 | ≤ 0.5 | yes |
| 2 | Median wall time | 3.59 s | 3.58 s | 1.003 | ≤ 1.2 | yes |
| 3a | Median peak RSS | 19832 KiB | 14888 KiB | 1.332 | ≤ 3 | yes |
| 3b | Median CPU | 0.15 s | 0.065 s | 2.31 | ≤ 5 | yes |
| 4 | Correct reports, peak in flight, exit code | 10/10, 20, 1 | 10/10, 20, 1 | | both correct | yes |
| 5 | Escape probe | denied, 0 bytes sent | 29 of 29 canary bytes sent | | Arv denied, Go succeeds | yes |
| 6 | Would you rather write the next ops script in Arv? | | | | owner | yes |

**The owner's decision (2026-11-11).** Criterion 6: yes. Phases 6 to 8 go ahead as a language. The syntax cost the report named is real, so the ergonomics items became Phase 5E, built in the same wave as Phases 6, 7, 8.1 and 8.2 instead of as a week before them. The gate was not re-run. The owner also chose to run Phase 8.4, although the gate did not call for it (CPU 2.31x Go against a limit of 5x, about 4% of wall time).

## Mutation testing per phase

"Round 1" is the first independent pass. Later rounds re-apply the survivors after new tests.

| Phase | Round 1: tried / caught | Final | Survivors and what happened | Notes |
|---|---|---|---|---|
| 3 | 81 / 77 | 81 / 81 | Round 2 caught 3 of 4. Round 3 split the last ("equivalent") into two mutants; both caught. | [16c](16c-phase3-notes.md) |
| 3 (rounds 7-11) | not recorded | not re-run | 16 survivors, each with a new test. The total and the commands were not written down; no second round. | [16zg](16zg-final-fixes.md) |
| 8.3 | totals not recorded (23-row table, then four rounds) | all caught but 1 | Round 1 found two test gaps. Rounds 2 to 4 showed that three mutants first called equivalent were test gaps. All five now have tests. One equivalent: keep-alives on with `CloseIdleConnections` kept. | [16b](16b-phase83-notes.md), [16d](16d-phase83-mutations.md) |
| 4 | 45 / 42 | 44 / 45, 1 equivalent | Jitter and the attempt context now caught. The backoff-clamp mutant was caught in round 4, and is equivalent again since the final fix (a negative backoff counts as 0). | [16l](16l-phase4-mutations.md), [16zg](16zg-final-fixes.md) |
| 5 | no round | | Three spot checks, all caught. | [16n](16n-phase5-round1.md) |
| 8.1 | 40 / 39 | 40 / 40 | B21a changed only the work done; a test added in round 2 catches it. Earlier review checks: 7 and 6 mutants, all caught. | [16p81e](16p81e-check-mutations.md) |
| 8.2 | 50 / 48 | 50 / 50 | M05b (equivalent through `Run`) and M07 (test gap) now caught. | [16-phase82-mutations](16-phase82-mutations.md) |
| 7 | 53 / 47 | 53 / 53 | 3 equivalent, 1 not observable on darwin, 2 gaps; a test (and a `procFS` seam) added for each. | [16s](16s-phase7-mutations.md), [16t](16t-phase7-mutations-round2.md) |
| 6 | 33 / 31 | 33 / 33 | M10 and M24 (test gaps) now caught. | [16za](16za-phase6-mutations.md), [16-implementation-notes](16-implementation-notes.md) §Phase 6 |
| 5E | 41 / 40 | 41 / 41 | M28 now caught. | [16pe-mutations](16pe-mutations.md) |
| 6x7 | 25 / 23 | 25 / 25 | M01 and M25 now caught. | [16zf](16zf-p67-mutations.md) |
| 8.4 | 38 / 36 | 38 / 38 | B6 and L1 now caught (plus a new L1b). | [16p84e](16p84e-speed-mutations.md) |
| 8.5 | 25 / 24 | 25 / 25 | M09 now caught. | [16p85c](16p85c-distribution-mutations.md) |
| Final fixes | 3 on the drain helper | 2 caught, 1 equivalent | | [16zg](16zg-final-fixes.md) |

Across the ten phases with counted rounds: 431 mutants tried in round 1, 407 caught at once. After the follow-up rounds every one is caught, except one Phase 4 mutant now equivalent. Many late catches are by unit tests only; the notes list them.

## Phases 0-2 e2e cases

`git diff origin/main feat/phases-3-8 -- e2e/` changes five pre-existing test files and one test script (`robust_await_mutual.arv`). The changed expectations are in `TestNetGrantValidation`, `TestNet`, `TestNetHostForms` (Phase 8.3: ports in grants and denials, `*` covers no internal IP, a loopback dial needs an IP grant) and `TestAwaitCycles` (Phase 4: environments frozen at spawn, I8). Each has a written reason, in [16-implementation-notes](16-implementation-notes.md) §8.3 and in [16j](16j-phase4-notes.md). No reason was missing. The table is in [part 2](18a-phases-3-8-fuzzing-and-limits.md#phases-0-2-e2e-changes). The 35 top-level Phases 0-2 tests all passed in the run below.

## Verification run (2026-11-15)

At `40e7c95`, on the M1 Pro (Go 1.26.2), load averages from about 75 at the start down to 11-15. Two orphaned `e2e.test` processes from the 16zg stress run (parent `launchd`, past their own 30-minute limit, at 70-94% CPU) were killed first. Free disk stayed at 22-24 GB and the build cache at 1.0-1.2 GB, so no `go clean -cache` was needed. Counts come from `go test -json`: a test is top-level if its name has no `/`.

| Run | Result | Wall time | Top-level tests | Subtests |
|---|---|---|---|---|
| `go test -race -count=5 -timeout 120m ./...` | **pass**, exit 0, 0 failures | 1016 s (16 min 56 s) | 1142 distinct, 5710 runs: 5700 pass, 10 skip | 2607 distinct, 12995 runs: 12980 pass, 15 skip |
| `ARV_E2E_RACE=1 go test -race -count=1 -timeout 60m ./e2e/`, first try | **fork hang** (known darwin race-detector limit), exit 1 | 685 s | 177: 176 pass, 1 fail | 779: 775 pass, 1 fail, 3 skip |
| the same, rerun once | **pass**, exit 0, 0 failures | 167 s (package 157.8 s) | 177: all pass | 779: 776 pass, 3 skip |

Package times in the first run (5 rounds, `-race`): `arv` 1014.8 s, `arv/evaluator` 312.0 s, `e2e` 135.7 s, `arv/runtime` 81.7 s, `cmd/arv` 48.7 s; the rest under 26 s each. `arv/token` and `e2e/p82host` have no tests.

The skips are by design: `TestLiveAnthropic` (needs `ARV_LIVE_ANTHROPIC=1`), `TestTimedSeed40019EveryCut` (needs `ARV_LONG=1`), and three subtests of `TestP85GitTestsSkipWithoutGit`, which check that the git tests skip when git is missing.

**The fork hang.** In the first race-binary e2e run, subtest `TestP84StepsExactOnBothEngines/loops_and_hashes` stopped making progress. The forked child of `e2e.test` never reached `exec`: it spun at 87% CPU for over 8 minutes. `sample` and `go tool addr2line` put it in `__tsan::TraceSwitchPartImpl` under `syscall.forkAndExecInChild`, called from `os/exec.(*Cmd).Start` in the harness's `run`. This is the darwin race-detector fork hang recorded in 16zg, not an Arv result. The child was killed, so the subtest failed with `signal: killed` after 9 min 6 s and an empty stdout. Every other test in that run passed. The single rerun passed in full. No real failure was seen.

**Phases 0-2 cases.** The 35 top-level e2e tests from Phases 0-2 (123 subtests) passed in all three runs: 175 top-level and 615 subtest runs in the five-round run, and 35 and 123 in each race-binary run.

## Open findings

None blocks the merge. Each is recorded in the note named.

1. **`TestResumedQuietChildSeesItsDeadline` timing flake** (package `arv`). Failed once under load in the final round; the spin calibration is per test binary. Not fixed ([16zg](16zg-final-fixes.md), [16zf](16zf-p67-mutations.md)).
2. **`return` inside a `let` value, a call argument or `${...}`** does not leave the function, on both engines. Base behaviour; a general fix touches every error-propagation site ([16zg](16zg-final-fixes.md)).
3. **Phase 3 rounds 7-11 mutation pass**: the totals were not written down and the 16 survivors were not re-applied after their tests landed ([16zg](16zg-final-fixes.md)).
4. **Phase 7 on Linux**: `TestProcFSIsRefused` has never run (it runs on Linux only), `/proc/self/fd/N` is unverified, and the 32-bit fix was only vet-checked ([16o-phase7](16o-phase7-notes.md), [16q-phase7](16q-phase7-round2.md)).
5. **Phase 8.1 acceptance amendment**: `arv check examples/uptime.arv` prints `fs_write=out/report.json` and notes, not the README's `fs_write=out` and `net=127.0.0.1:PORT`. The owner's sign-off on this change to §8.1 is still asked for ([16p81b](16p81b-check-round1.md)).
6. **Phase 8.3**: unreadable directories fail closed under `os.Root` (needs a search-only `openat` walk). `0177.0.0.1` is not in the e2e suite ([16b](16b-phase83-notes.md)).
7. **Phase 8.4**: compiling is not cancellable. The e2e suite catches neither B6 nor L1; unit tests do ([16p84](16p84-speed.md), [16p84e](16p84e-speed-mutations.md)).
8. **Phase 8.5**: goreleaser was never run ([16p85](16p85-distribution.md)).
9. **`FuzzMatchSyntax` harness**: accepts a name part holding a NUL byte and fails within a minute; the parser is right ([16pe](16pe-ergonomics.md)). The harness is unchanged in code (`isName` in `arv/parser/fuzz_p4_test.go` has no NUL check). A 60 s run on 2026-11-15 (4,585,285 execs) did not hit it.
10. **Phase 6**: seed 40019 was not reproduced in ten sweeps and is not marked closed ([16u](16u-phase6-round3.md)). Closing the window where a live effect in a resume runs before a logged deadline would deadlock ([16v](16v-phase6-round4.md)).
11. **18-handoff item 3** (leading-zero integers parsed as octal) is fixed in code (`e08dcf9`, `TestLeadingZeroIsDecimal`), but no note recorded it until this one.
12. **darwin `-race`**: a race-built test binary can crash or hang in fork. Run long suites with `-timeout` ([16zg](16zg-final-fixes.md)).
