# Arv: the next roadmap (2026-11-15)

This plan follows the Phases 3-8 report ([`18-phases-3-8-report.md`](../2026-11-09-arv-runtime/18-phases-3-8-report.md)) and its known limits ([`18a`](../2026-11-09-arv-runtime/18a-phases-3-8-fuzzing-and-limits.md)). The invariants I1 to I8 in [`01-design.md`](../2026-11-09-arv-runtime/01-design.md) §2 still apply to every phase below. Each file stays under the 5,000-token commit hook.

| File | What it holds |
|---|---|
| `00-roadmap.md` (this file) | State today, the ranked next actions, the phase overview and the timeline |
| [`01-phases.md`](01-phases.md) | Phases 9 to 15: goal, scope, exit criteria and size of each |
| [`02-verification.md`](02-verification.md) | How to move the invariants from tested to proven |
| [`03-benchmarks.md`](03-benchmarks.md) | A reproducible benchmark suite, the method, and the comparisons |
| [`04-proof-of-value.md`](04-proof-of-value.md) | Demos, the safety story, a comparison matrix, docs, the launch and adoption metrics |

**Sizes.** XS is under a day. S is up to 1 engineer-week, M is 1-3 weeks, L is 3-6 weeks and XL is over 6 weeks. Phases 3-8 were built by agent waves with review rounds, so calendar time can be shorter. The review, mutation and fuzz work is not shorter, and it is included in each size.

## 0. State today (checked against the code on 2026-11-15)

- **PR #2 is merged.** `gh pr view 2` says MERGED at 14:40 UTC today, as merge commit `c7b67a8` on `main`. `feat/phases-3-8` (`7561eff`) is in `origin/main`. "Finish and merge PR #2" is therefore done. What is left is the work after the merge (§1).
- **One check on PR #2 failed.** GitGuardian found a "Generic High Entropy Secret" in `e2e/p81_accept_test.go`, commit `36df8d0`, line 123. That line is the test canary `const secret = "s3cr3t-p81-value"` in `TestP81CheckNeverReadsTheEnvironment`. It is a false positive: no real key is in the repo. The other checks, CodeRabbit and Macroscope, were skipped.
- **The repo has no CI of its own.** There is no `.github/` directory. Every number in the Phases 3-8 report comes from one M1 Pro running darwin/arm64 (18 §Verification run).
- **It cross-compiles, but the tests have never run on another platform.** Today, `GOOS/GOARCH=… go build ./...` succeeds for linux/amd64, linux/arm64, linux/386, linux/arm, windows/amd64 and freebsd/amd64, and `go vet ./arv/...` is clean for windows/amd64. No test has run on any of these.
- **Size.** About 16,900 lines of non-test Go and 44,700 lines of tests. Tests tag invariants (`// In`) in these counts: I1 13, I2 31, I3 16, I4 16, I5 14, I6 31, I7 31, I8 9.
- **The `return` bug is real, on the default (compiled) engine.** `return` is a statement (`arv/parser`), so it reaches a value position only through a block expression. A probe gave:
  - `let f = fn() { let x = if (true) { return 1 } else { 0 }; 2 }` returns `2`.
  - `fn() { puts(if (true) { return 3 } else { 0 }); 4 }` **prints 3** and returns `4`. The `return` value leaked into an effect.
  - `"${if (true) { return 5 } else { 0 }}"` in a `let` behaves the same way.
- **The language surface is small.** The keywords are in `arv/token/token.go`, and the effects and pure builtins are listed in `arv/check/builtins.go:36-69`. What is missing:
  - JSON parsing: `json` only encodes (`arv/evaluator/json.go`).
  - Any HTTP method but GET: there is only `http_get`, with no headers.
  - Floats: integers only (`value.Integer`).
  - Loops other than `for … in` over arrays, and `break`.
  - Modules and `import`.
  - String literals inside `${}`.
  - A full reference: `docs/language.md` is 13 lines.
- **The REPL has no budgets and no grants** (`arv/repl/repl.go`; 18a Phase 3).
- **Only one LLM provider.** Anthropic plus a fake (`arv/llm/anthropic.go`, `arv/llm/fake.go`). Tools take strings only, `max_tokens` is fixed at 4096, and prices were copied by hand on 2026-11-11 (`arv/llm/pricing.go`).
- **Distribution.** `.goreleaser.yaml` builds darwin/linux on amd64/arm64 only (no Windows). goreleaser is not installed on this machine and has never run.
- **The README is out of date.** Its feature list still says "**Planned:** durable replay" (`README.md` line 10), although the README's own "Record, replay and resume" section documents it.
- **The benchmarks are not yet reproducible.** The Phase 8.4 numbers were taken at load averages of 10 to 61, with raw output in `/tmp` (`16p84-speed.md` line 49). The Phase 5 gate method (`bench/run.sh`, alternating rounds, `gate-report.md`) is the one to build on.

## 1. Next action items, ranked

Severity: **S1** means wrong results or a security gap. **S2** means a gap in trust or verification. **S3** means polish or availability. Within one severity, cheaper items come first. "Checked" says what was confirmed today.

| # | Item | Sev | Effort | Checked | Action | Done when |
|---|---|---|---|---|---|---|
| A1 | `return` inside a `let` value, a call argument or `${…}` does not leave the function | S1 | M | Reproduced above, compiled engine. 18a says both engines agree | Propagate the return signal through every value position: `let`, call arguments, array and hash literals, index, prefix and infix, interpolation, `match` subjects, scope options. Write the failing tests first, on both engines. Then extend `FuzzEnginesAgreeSource` with a generator that puts `return` in nested blocks | The three probes give 1, (nothing printed) 3, 5. Engines agree under fuzz. A mutation round passes |
| A2 | No CI | S1 | S | No `.github/` | GitHub Actions matrix (02 §V6): linux/amd64 with `-race` and race e2e, linux/arm64, linux/386, macOS arm64 (unit `-race` with `-timeout`, no race e2e), and windows/amd64 build and vet. Add `staticcheck` and `govulncheck` | `main` is green, and it is a required check on PRs |
| A3 | GitGuardian is red on the merged PR | S2 | XS | Check output read | Mark incident 40102116 as a false positive in the GitGuardian dashboard, and add a repo ignore for that match, or rename the canary to something low-entropy. No history rewrite: nothing secret leaked | The next PR's GitGuardian check is green |
| A4 | Phase 7 untested on Linux and on 32-bit | S2 | S | 32-bit fix only vet-checked (16q-phase7); `TestProcFSIsRefused` is Linux-only | Comes free with A2: Linux runners run the procfs tests and the `/proc/self/fd/N` case. `GOARCH=386 go test ./...` runs natively on an amd64 runner | Both jobs pass, and 18 open finding 4 is closed |
| A5 | goreleaser never run | S2 | S | Not installed; config has no Windows | A CI job runs `goreleaser check` and `goreleaser build --snapshot --clean`, and runs each binary for its own OS/arch with `arv version`. Decide on Windows (§Phase 15). Then tag `v0.1.0` as a draft release | Snapshot artifacts are built in CI, and `go install …@v0.1.0` prints `v0.1.0` |
| A6 | Owner sign-off on the 8.1 acceptance change for `uptime.arv` | S2 | XS | 16p81b line 23; `arv check` output re-run today matches it | **Recommend: accept.** `fs_write=out/report.json` is narrower than the README's `fs_write=out`, which is least privilege working. `net=127.0.0.1:PORT` cannot be static, because the port is chosen at run time. Reword §8.1: "the printed set is the least set; every non-static need is a `#` note" | Owner says yes in writing, and `15-phase8-tooling.md` §8.1 is amended |
| A7 | Compiling is not cancellable, and compiled code is not charged | S2 | S | `compile(node ast.Node)` takes no context (`arv/evaluator/compile.go:13`, called at `engine.go:57`) | Pass the task to `compile`, check cancellation every N nodes, and charge the closures to `alloc_mb` | A 17 MB program cancels within 50 ms, and its compiled size is charged |
| A8 | `TestResumedQuietChildSeesItsDeadline` timing flake | S2 | S | `arv/resume_deadline_test.go:155`; spin calibration per test binary (`arv/spin_test.go`) | Replace the CPU-spin calibration with a deterministic seam: a hook the test waits on, or a `testing/synctest` bubble ([Go 1.25](https://tip.golang.org/doc/go1.25)) | 1,000 runs under `stress` at load 2× the core count, with no failure |
| A9 | README says replay is planned | S3 | XS | `README.md` line 10 | Replace the line with a pointer to "Record, replay and resume" | Done |
| A10 | `FuzzMatchSyntax` harness accepts a NUL | S3 | XS | `isName`, `arv/parser/fuzz_p4_test.go:123`, has no NUL check | Reject `\x00` in `isName` | A 10-minute fuzz run is clean |
| A11 | Seed 40019 not closed | S2 | S | `TestTimedSeed40019EveryCut` skips without `ARV_LONG=1` (`arv/replay_timed_prop_test.go:96`) | A nightly Linux job sets `ARV_LONG=1`. Later, the DST harness (02 §V3) replaces the seed sweep | 30 clean nights: mark it closed in a note. A failure: reproduce it under DST |
| A12 | Phase 3 rounds 7-11 mutation pass: no totals | S2 | S | 16zg | Re-run the 16 survivors and record the totals. Then fold every recorded mutant into the mutant regression suite (02 §V5) | A table with totals is in a note |
| A13 | Directories without the read bit fail closed under `os.Root` | S3 | M | `arv/evaluator/fsopen.go`, `nofollow_unix.go`; 18a Phase 8.3 | Search-only walk: `openat` with `O_PATH` on Linux and `O_SEARCH` where the OS has it. Keep failing closed where neither exists | `0711` and `0100` parents read within a grant. The swap-race tests still pass |
| A14 | darwin race-built binaries hang or crash in fork | S3 | XS | 18 finding 12 | Run race e2e only on Linux in CI. Add `-timeout` to every `go test` in the `Makefile` (none has one today) | No local or CI run waits past its timeout |
| A15 | `0177.0.0.1` is not in the e2e suite | S3 | XS | 18a Phase 8.3 | Add the case to `TestNetHostForms` | Done |
| A16 | Model prices are hand-copied with no date check | S3 | XS | `arv/llm/pricing.go` | Store the source URL and date with the table. A test fails when the table is more than 90 days old, which prompts a review | The test exists |

**Order of work.** A1, A2, A3 and A9 first, in parallel, because A2 makes A4, A5, A11 and A14 nearly free. Then A6 (an owner decision), A7, A8, A5, then the rest. Together these items are Phase 9.

## 2. Phases (details in [`01-phases.md`](01-phases.md))

| Phase | Goal | Size | Depends on |
|---|---|---|---|
| **9. Ship v0.1** | A tagged release, built and tested in CI on every target, with §1 closed | M | — |
| **10. Language core v0.2** | Write the next ten real scripts with no workaround: return semantics, JSON parsing, HTTP with method, headers and body, secrets, strings and collections, `else if`, loops with `break` | L | 9 |
| **11. Tooling** | `arv fmt`, an LSP, error messages with a snippet and one primary error, a REPL with grants and budgets, `arv test` with replay fixtures, `arv log`, a docs site and a playground | L | 10 (syntax settled) |
| **12. Modules, stdlib, packages** | Share code without sharing authority: `import`, imported code holds nothing, a lockfile with hashes, `arv check` across modules | L | 10, decision D2 |
| **13. Runtime** | Signed logs, log compaction, a live-memory limit, connection reuse, a durable worker (`arv serve`) with timers and signals; speed only if the benchmarks call for it | XL | 9 |
| **14. Host and agent story** | The safest way to let a model act: code mode, MCP in and out, more providers, typed tools, `approve()`, hosts in other languages, the Wasm effect | XL | 10, 13 (part) |
| **15. Distribution and v1.0** | Signed releases, Homebrew, a Windows decision, Go API and log-format compatibility promises | M | all, plus the 02 exit criteria |

Two tracks run beside the phases from Phase 9 on: **verification** ([02](02-verification.md)) and **benchmarks** ([03](03-benchmarks.md)). The **proof-of-value** work ([04](04-proof-of-value.md)) starts in Phase 10, when the demos become writable.

**Every phase keeps the Phases 3-8 discipline.** Tests are written first and name the invariant they protect. Every new syntax form and builtin is classified in `arv/check` and in the gate test, and the tests already fail when one is missing. Both engines must agree on each change. Each phase ends with review rounds, a mutation round with recorded totals, fuzzing with recorded seconds and execs, and a report.

## 3. Decisions the owner needs to make

| # | Decision | Recommendation | Needed by |
|---|---|---|---|
| D1 | Accept the 8.1 acceptance change (A6) | Yes | Phase 9 |
| D2 | Move to real object capabilities, where the value is the authority (`01-design.md` §6, "Capability values aren't authority") | Yes, for imported modules (Phase 12). Keep `--allow` at the root | Phase 12 start |
| D3 | Floats | Add IEEE-754 `float` with one canonical text form and NaN encoding in the codec, so replay stays byte-identical. Money stays in integer cents | Phase 10 |
| D4 | A subprocess capability (`proc("git")`) | Yes, off by default, argv only and no shell. `arv check` flags it as authority-amplifying, as Deno says `--allow-run` amounts to allow-all ([Deno security](https://docs.deno.com/runtime/fundamentals/security/)) | Phase 10 |
| D5 | Windows as a supported platform | Tier 2 in v0.x (builds and unit tests in CI), tier 1 at 1.0 only if the `os.Root`, nofollow and procfs paths are proven there | Phase 15 |
| D6 | The bytecode VM | No, unless 03's agent workloads show interpreter CPU above 20% of wall time (the gate measured CPU at 2.31× Go, about 4% of wall time) | Phase 13 |
| D7 | Adoption gate at launch + 90 days | Adopt the criteria in 04 §7 before launch, and do not tune them afterwards (as with the Phase 5 gate) | Before launch |

## 4. Timeline (one owner plus agent waves; adjust after Phase 9)

| When | Phases | Tracks |
|---|---|---|
| 2026-11 to 2026-12 | 9 | V0, V6 (CI), B0 (method, harness) |
| 2027 Q1 | 10, 11 | V1 (core semantics), V2 (reference interpreter), V3 (DST), B1-B5 |
| 2027 Q2 | 12, 14 (code mode, MCP) | V4, V5 continuous; V7 external audit; B6 (agent workloads); private alpha, then public v0.x launch (04) |
| 2027 Q3 | 13, 14 (rest), 15 | Adoption gate (D7); v1.0 if 02's exit criteria and the gate hold |

## 5. Top 10 next actions

1. **A1:** fix `return` propagation, test-first on both engines, plus fuzz.
2. **A2:** add the CI matrix (Linux race and e2e, linux/386, macOS, Windows build).
3. **A3:** clear the GitGuardian false positive.
4. **A9:** remove the stale "Planned: durable replay" line.
5. **A6:** the owner signs off the 8.1 change (recommend yes).
6. **A7:** make compiling cancellable and charged.
7. **A8:** remove the timing flake with a deterministic seam.
8. **A5:** run goreleaser in CI, then tag `v0.1.0` as a draft.
9. **02 §V0:** write the invariant traceability matrix and the threat model, which are the inputs to the formal work and to the audit.
10. **03 §B0:** set up the benchmark harness and its hardware rules before any new speed claim, and start Phase 10 with JSON parsing and HTTP requests.
