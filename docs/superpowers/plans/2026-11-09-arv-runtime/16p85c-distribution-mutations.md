# Phase 8.5: distribution, mutation check

## Round 1 (2026-11-13, at `e29094f`, independent checker)

25 mutations, at least one per guarantee (D1 to D5, I1 through the
distributed binary). 24 were caught. The checker's table was returned to
the orchestrator but not committed, because the disk filled before it
could write it; the orchestrator recorded it here and re-ran M25 by hand.

| Guarantee | Mutation | Caught | Catching test |
|---|---|---|---|
| D1 builds from cmd/arv only | M01: untracked `doc.go` (package arvlang) at the module root | yes | e2e TestP85CommandInCmdArv |
| D1/D5 the binary is named arv | M02: `.goreleaser.yaml` binary arv -> arvlang | yes | e2e TestP85ReleaseBuild, TestP85ReleaseTargets |
| D2 make build builds cmd/arv | M03: Makefile build recipe `./cmd/arv` -> `.` | yes | e2e TestP85MakeBuild, TestP85MakeRecipes, TestP85MakeBuildInWorktree |
| D2 only the command line moves make build output | M04: `BIN = bin` -> `BIN ?= bin` | yes | e2e TestP85MakeRecipes |
| D2 make test runs under -race | M05: test recipe drops `-race` | yes | e2e TestP85MakeRecipes, TestP85ReadmeMakeTargets |
| D2 make e2e never serves cached results | M06: e2e recipe drops `-count=1` | yes | e2e TestP85MakeRecipes |
| D2/D4 make build marks a changed tree | M07: COMMIT drops the `(modified)` suffix | yes | e2e TestP85MakeBuildInWorktree |
| D2/D4 make build names the commit | M08: `-X main.commit` -> `-X main.Commit` | yes | e2e TestP85MakeBuild, TestP85MakeBuildInWorktree |
| D3 the suite binary's stamp names this checkout | M09: TestMain drops `build.Env = cleanEnv()` | **no** | none (see round 2) |
| D3 secrets stay in the host | M10: hostLLM no longer unsets ANTHROPIC_API_KEY | yes | unit TestCLIKeyIsNotInTheEnvironment; e2e TestPhase7Secrets, TestP67LogsNeverHoldTheKey |
| I1 no ambient authority | M11: runCLI grants env=* with no --allow | yes | unit TestRunCLI and 8 more; e2e TestP85MakeBuild and many P6/P67 tests |
| D3 bench scripts build cmd/arv | M12: bench/run.sh builds `.` | yes | bench TestRunScript |
| D3 the suite builds the real binary | M13: cmdArv -> e2e/p82host | yes | e2e (152 top-level failures) |
| D3 the suite binary is the compiled engine | M14: buildFlags always adds `-tags arv_treewalk` | yes | e2e TestP84BinariesAreTheTwoEngines |
| D4 link-time commit is printed | M15: versionText ignores ldCommit | yes | unit TestVersionText; e2e TestP85MakeBuild, TestP85ReleaseBuild |
| D4 link-time version is printed | M16: versionText ignores ldVersion | yes | unit TestVersionText, FuzzVersionText; e2e TestP85ReleaseBuild |
| D4 (modified) only for a changed tree | M17: `vcsModified = s.Value != ""` | yes | unit TestVersionText; e2e TestP85VersionFromGit |
| D4 pseudo-version with +dirty | M18: pseudoRevision drops `strings.Cut(v, "+")` | yes | unit FuzzPseudoRevision seed (no e2e) |
| D4 pseudo-version parsing | M19: timestamp length 14 -> 13 | yes | unit TestVersionText, FuzzPseudoRevision; e2e TestP85GoInstallLatest |
| D4 link-time commit wins over the stamp | M20: vcs.revision taken whenever present | yes | unit TestVersionText (no e2e in a worktree) |
| D4 arv version takes no arguments | M21: `len(args) != 1` -> `< 1` | yes | unit TestVersionCommand; e2e TestP85VersionCommand |
| D5 release builds name their version | M22: `-X main.version` -> `-X main.Version` | yes | e2e TestP85ReleaseBuild, TestP85ReleaseTargets |
| D5 macOS and Linux targets | M23: goos `[darwin, linux]` -> `[darwin]` | yes | e2e TestP85ReleaseBuild, TestP85ReleaseTargets |
| D5 release builds are CGO-off | M24: `CGO_ENABLED=0` -> `1` | yes | e2e TestP85ReleaseBuild, TestP85ReleaseTargets |
| D1/D2 README install line | M25: README `go install .../cmd/arv@latest` -> `.../arvlang@latest` | yes | e2e TestP85ReadmeInstallLine (re-run by the orchestrator) |

Some runs saw load-induced timeouts in unrelated tests (TestNet*, TestTimeoutFlag) at a load average near 180; they passed on rerun.

## Round 2 (2026-11-14, at `8904b32`)

M09 was not caught because no test set GIT_DIR around TestMain's build,
and in a worktree the go command stamps no vcs.revision, so even a
polluted build looked right. TestMain's build moved into `suiteBuild`, and
`TestP85SuiteBuildDropsGitEnv` sets GIT_DIR and GIT_WORK_TREE and checks
that the command passes no GIT_* variable to the go command. With M09
applied it fails ("the suite build inherits the whole environment"), and
without it it passes. All 25 are caught.

## A Phase 6 flake fixed on the way (`bf5710b`)

`TestForgedLogsAreRefused/times reversed` failed in 5 of 300 runs: log
records write `began` after `kind`, so when the request started 1 ms or
more into the run the record's own `began` followed the forged one, and
decoding keeps the last key. The test now removes the record's own
`began` first; 1000 of 1000 runs pass. `TestP6PropRecordReplay` (see
16p85b) is still open.
