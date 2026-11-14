# Phase 8.5: e2e, fuzz and hardening

The hardening pass after the review loop converged (`16p85-distribution.md`, commit 0bf2fa3). Nothing was released, pushed or tagged. Every test drives a binary built from `cmd/arv` the way a user or goreleaser builds it.

## New e2e cases

| Guarantee | Test (file) | What it checks |
|---|---|---|
| D1, design 8.5 bullet 1: `go install .../cmd/arv@latest` produces `arv` | `TestP85GoInstallLatest` (`e2e/p85_install_test.go`) | Runs the real `go install cmdArv@latest` outside any module, from a **local file proxy** (`GOPROXY=file://...`, the go command's proxy protocol; `GOSUMDB=off`, its own `GOMODCACHE` and `GOBIN`, `GOTOOLCHAIN=local`), whose module zip holds the checkout's current sources (`sourceFiles`). Three cases: `@latest` with a tag `v0.8.5` (prints `arv v0.8.5`, `commit unknown`), `@latest` of a module without tags (the proxy's `@latest` answers a pseudo-version; prints it and `commit c1b8296f0c55`), and `@<pseudo-version>`. `GOBIN` then holds one file, `arv`; `go version -m` names `path cmdArv` and `mod <module> <version>`; `checkDistBinary` passes. Nothing is downloaded. |
| README install line | `TestP85ReadmeInstallLine` (`p85_install_test.go`) | README has one `go install` line and it is `go install cmdArv@latest`. |
| D2 README and Makefile agree | `TestP85ReadmeMakeTargets` (`e2e/p85_cli_test.go`) | Every README line `make T   # go ...` names words that appear, in order, in `make --dry-run T`; `build`, `install` and `test` must be documented. Mutation `# go build -o bin/arv .` in README: caught. |
| D3 the distributed binaries behave as the suite's | `checkDistBinary` (`p85_cli_test.go`), used by `TestP85GoInstallLatest` and `checkMakeBuild` (so also `TestP85MakeBuild` and the decoy run in `TestP85GitFixturesIgnoreGitEnv`) | `basics.arv` prints what the suite's binary prints; `getenv` without a grant is denied with `holds: []` and neither `ARV_E2E_TOKEN` nor `ANTHROPIC_API_KEY`'s value appears (I1, secrets stay in the host); `version extra` exits 2 with the usage. |
| D4 version information | `TestP85VersionCommand` (`p85_cli_test.go`) | `arv version` prints the same three lines with an empty environment and with `ANTHROPIC_API_KEY`, `HOME`, `ARV_VERSION`, `ARV_COMMIT` set (no value echoed); exit 2 and usage for `version --allow env=HOME`, `version extra`, `version --help`; an unknown command's usage lists `arv version`; it runs in a working directory it cannot read (mode 0711). |
| D5 every goreleaser target | `TestP85ReleaseTargets` (`e2e/p85_cross_test.go`) | Builds darwin and linux, amd64 and arm64, with `.goreleaser.yaml`'s main, flags and ldflags (shared helper `releaseBuild`, `e2e/p85_release_test.go`). Each binary: right format and CPU (`debug/elf`, `debug/macho`), an executable; holds the link-time `v9.8.7` and the 40-hex commit; no symbol of arv's own (`main.`, the module path) and no DWARF (`-s -w`); never the checkout's absolute path (`-trimpath`); build info `path cmdArv`, `GOOS`, `GOARCH`, `CGO_ENABLED=0`, `-trimpath=true`; Linux: no `PT_INTERP`/`PT_DYNAMIC`, no needed libraries (static). Mutation (config without `-s -w`, `-trimpath` replaced by `-v`): all four subtests fail, each on the expected checks. |

Mutation of the code under test: `pseudoRevision` always `""` fails `TestP85GoInstallLatest/latest_without_tags` and `/pseudo-version` (the proxy zip copies the working tree, so the installed binary carries the mutant).

## Fuzzing

`arv version` reads no untrusted input: its sources are the link-time variables and the go command's build information, both fixed when the binary is built. The phase adds no other input. Two targets were still added for its only parser, in `cmd/arv/version_fuzz_test.go`:

- `FuzzPseudoRevision`: differential against a regexp of the pseudo-version shape, `(?s)^.*[-.][0-9]{14}-([0-9a-f]{12})$`, on the string before its first `+`.
- `FuzzVersionText`: any link-time version and commit, `Main.Version`, `vcs.revision`, `vcs.modified` and missing build info; the output is three lines, the first version and commit known in order, `(modified)` only after the stamped commit.

| Target | Time | Execs | Result |
|---|---|---|---|
| `FuzzPseudoRevision` (first run) | 28 s | (stopped at the failure) | failure in the **oracle**: its `.` did not match a leading newline (`"\n.00000000000000-000000000000"`); `pseudoRevision` was right. Fixed with `(?s)`; the input is committed as `testdata/fuzz/FuzzPseudoRevision/7a788ddf67c17f7c` (commit 9197442) |
| `FuzzPseudoRevision` | 121.4 s (`-fuzztime=2m`) | 6 627 129 | pass, 153 corpus entries |
| `FuzzVersionText` | 120.5 s (`-fuzztime=2m`) | 6 819 125 | pass, 77 corpus entries |

## Leak checks

Phase 8.5 adds no concurrency and no path through `arv.Run`: `arv version` returns before any run starts, and the rest of the phase is build configuration. There is no new goroutine to assert on; the existing leak tests run unchanged in the suites below.

## Runs

All on 2026-11-13 in the p85 worktree, load average 5 to 40.

- `go test -race ./...`: all `ok` (`arv` 201 s, `e2e` 27 s, `cmd/arv` 10.7 s). `gofmt -l .` empty, `go vet ./...` clean.
- `go test -count=1 ./e2e/`: ok, 48.3 s. `ARV_E2E_RACE=1 go test -race -count=1 ./e2e/`: ok, 180 s. The P85 tests under a race-built binary take at most 9.8 s each (`TestP85GitFixturesIgnoreGitEnv`); `TestP85GoInstallLatest` 8 s per case, `TestP85ReleaseTargets` under 13 s for all four.
- `go test -race -count=5 ./...`: **first run** failed twice, neither in p85 code: `arv` hit go test's default 10-minute limit (five rounds at about 200 s; rerun with `-timeout 90m`), and `TestP84StepsExactOnBothEngines/loops_and_hashes` saw the binary die by **SIGSEGV after 11 ms with empty stderr** (the same signature review round 1 recorded for `TestP83SwapRaceCannotEscape/write`; a Go runtime fault prints a trace, so a fault at exec is likelier). **Second run** (`-timeout 90m`): every package `ok` (`arv` 929 s, `arv/evaluator` 281 s, `cmd/arv` 47 s) except `e2e`, where `TestP6PropRecordReplay/39` failed with `the replay made 1 requests` (see below). **Third run of e2e alone** (`go test -race -count=5 ./e2e/`): ok, 279 s.
- `TestResumedQuietChecksSeeDeadlines` and `TestResumedQuietChildSeesItsDeadline` did not fail in any of these runs.

## Flake outside this phase: `TestP6PropRecordReplay` (Phase 6)

`go test -race -count=10 -run 'TestP6PropRecordReplay$' ./e2e/` failed in about 1 of 10 invocations (cases 9, 10, 39 seen), always `the replay made 1 requests` with the replay's stdout, stderr and exit code identical to the record's. In the captured instance (seed 9) the record ended by fail-fast (`boom in t2`) while other tasks had requests to `/ok`; the extra hit was still there 1 s later. Two explanations fit and were not separated: a request the record's process had sent before it exited, which the test server counts after the test reads `gets` (a harness race), or a live request in the replay (an I7 bug). With a 500 ms settle after the record (and a print if the count changed in it), 60 rounds of all 40 cases passed and no late hit was printed, so the evidence is inconclusive. Not fixed here (Phase 6 scope, no p85 code is involved: the runtime is unchanged by the move to `cmd/arv`). Suggested next step for Phase 6: count hits server-side with the process's lifetime (for example, a `/ok` request that carries an `rec`/`replay` marker from the program's environment), so the test can tell which run made the request.


## Known limits (new)

- `TestP85GoInstallLatest` exercises the go command's proxy path, not GitHub: the real `@latest` resolves through `proxy.golang.org` and the checksum database, which the suite cannot reach. A module path or major-version mistake would show here as well (the proxy layout uses `modulePath`, derived from `cmdArv`).
- `TestP85ReleaseTargets` checks a Mach-O binary's format, not that it runs on the other architecture; the host binary runs in `TestP85ReleaseBuild`. A darwin binary links `libSystem` dynamically by design, so only the Linux targets are checked for static linking.
- The release builds in the suite pass `CGO_ENABLED=0` themselves; the config's `env` line is checked as text only (as before).
