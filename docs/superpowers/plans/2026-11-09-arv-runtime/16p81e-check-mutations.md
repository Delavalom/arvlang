# Phase 8.1: `arv check`, mutation check round 1

Continues [`16p81d-check-hardening.md`](16p81d-check-hardening.md). Each mutation was applied to the worktree, then `go test -count=1 -timeout 300s ./arv/check/ . ./e2e/` was run (unit tests, CLI tests and the e2e suite against a freshly built binary). After each run the file was restored with `git checkout --` and `git status` was confirmed clean before the next mutation. No mutation was committed and no test was added. Load averages during the run were 7 to 32.

"Caught" names the tests that failed. *Unit* means `arv/check`, *CLI* means the root package, and *e2e* means `e2e/`.

## Guarantee 1: `arv check` never runs effects (I1, I7, leak rule)

| Id | Mutation | Caught | Catching tests |
|---|---|---|---|
| A1 | `check_cli.go`: `checkCLI` runs the file with `arv.Run`, granting its own reported needs, before it prints them | yes | CLI `TestCheckDoesNotRunTheProgram` (and the package then hit its 10-minute limit in `TestManifestRunsLeaveNoGoroutine`). e2e `TestP81CheckNeverRunsTheProgram`, `TestP81CheckIsQuickOnProgramsThatNeverEnd`, `TestP81CheckNeverReadsTheEnvironment`, `TestP81CheckSearchesEverySyntaxForm`, `TestP81ExamplesCheck`, and the `TestP81Manifest*` tests |
| A2 | `check.go`: `Source` evaluates the program after checking it, with its reported needs and a 2 s timeout (the library, so both `check` and `run --manifest` are affected) | yes | CLI `TestCheckDoesNotRunTheProgram`, `TestRunManifestRefusesBeforeRunning`. e2e `TestP81CheckNeverRunsTheProgram`, `TestP81RunManifest`, `TestP81ManifestRefusesBeforeEvaluating`, `TestP81ManifestComparesCanonicalPaths`, `TestP81ManifestDialNeed`. The unit package was killed (`signal: killed`, 47 s) while its fuzz seeds were running programs |
| A3 | `main.go`: a refusal from `applyManifest` (exit 1) no longer stops the run: `code != 0` becomes `code == 2` | yes | CLI `TestRunManifestRefusesBeforeRunning`, `TestRunManifestErrors`, `TestManifestRefusalListsAtMostTenNeeds`, `TestNotesAndRefusalsNeutraliseBidiAndC1`. e2e `TestP81ManifestRefusesBeforeEvaluating`, `TestP81ManifestWithoutAHeader`, `TestP81ManifestMalformedHeader`, `TestP81ManifestComparesCanonicalPaths` |
| A4 | `check.go`: `Source` leaks a goroutine (`go func() { select {} }()`) | yes | unit `FuzzCheckNoEffects` (seed corpus). CLI `TestManifestRunsLeaveNoGoroutine`. e2e: none (it cannot see goroutines in the child process) |

## Guarantee 2: the reported grants over-approximate what the program can request (I1, I2, I3)

| Id | Mutation | Caught | Catching tests |
|---|---|---|---|
| B1 | `call`: an argument that is not known gives no note | yes | unit `TestUnknownArgumentsAreNoted`, `TestReboundNamesAreNotConstants`, `TestOnlyStringPlusFolds`, `TestFoldingIsBounded`, `TestNameChainsAreBounded`, `TestAssertSoundRunsGeneratedPrograms`. CLI `TestCheckPrintsGrantsAndNotes`. e2e `TestP81CheckNotes`, `TestP81ExamplesCheck`, `TestP81ManifestNotesAreDeniedAtRunTime` |
| B2 | `reference`: an effect used as a value gives no note | yes | unit `TestBuiltinUsedAsAValueIsNoted`. e2e `TestP81CheckNotes` |
| B3 | `call`: a call with the wrong number of arguments gives no note | yes | unit `TestCallsTheRuntimeRefusesAreNoted`. e2e `TestP81CheckNotes` |
| B4 | `dialNeed` always returns nothing, so a request to `0.0.0.0:P` loses its `127.0.0.1:P` dial need | yes | unit: the four `dial_test.go` tests, `TestAssertSoundRunsGeneratedPrograms`, `TestReportedGrantsSufficeAndAreNeeded`, `FuzzCheckSound` (seeds). CLI `TestNotesAndRefusalsNeutraliseBidiAndC1`. e2e `TestP81ManifestDialNeed` |
| B5 | `urlScope`: a URL without a port always reaches 443, so `http://x/` reports `x:443` | yes | unit `TestURLScope`, `TestUnspecifiedAddressAlsoNeedsLoopback`, `TestWildcardDoesNotCoverLoopback`, `TestNeedsMatchTheRuntimesRequests`. e2e: none |
| B6 | `newFolder`: function parameters are not counted as bindings, so a name a parameter shadows folds as a constant | yes | unit `TestReboundNamesAreNotConstants`. e2e: none |
| B7 | `newFolder`: a `for` variable is not counted | yes | unit `TestReboundNamesAreNotConstants`. e2e: none |
| B8 | `newFolder`: `match`'s `ok` and `err` names are not counted | yes | unit `TestReboundNamesAreNotConstants`. e2e: none |
| B8b | as B8, the `err` name only | yes | unit `TestReboundNamesAreNotConstants` |
| B9 | `newFolder`: a name bound twice still folds (`count != 1` becomes `count > 2`) | yes | unit `TestReboundNamesAreNotConstants`. e2e: none |
| B10 | `least`: covering is tested the wrong way round (`covers(nc, mc)`), so a broad need is dropped in favour of a narrow one | yes | unit `TestLeastSetDropsCoveredNetAndEnvNeeds`, `TestWildcardDoesNotCoverLoopback`, `TestLeastMatchesEveryPairComparison`, `TestLeastIsNotQuadratic` |
| B11 | `Outside`: sites are de-duplicated by kind instead of by grant, so only the first site of each kind is compared with the header | yes | unit `TestOutside`, `TestOutsideMatchesAdmit`, `TestOutsideSeesTheDialNeed`, `TestOutsideTellsUnresolvedFromUncovered`, `TestOutsideIsNotQuadratic`. CLI `TestManifestRefusalListsAtMostTenNeeds`. e2e `TestP81ManifestDialNeed` |
| B12 | `grantIndex.allows`: any candidate scope found in the index is accepted without `Covers`, so `net=*` admits `127.0.0.1` | yes | unit `TestOutsideMatchesAdmit`. e2e: none |
| B13 | `grantIndex.admit`: the request is not canonicalised (`runtime.New` skipped) (I3) | yes | unit `TestOutside`, `TestOutsideMatchesAdmit`, `TestOutsideTellsUnresolvedFromUncovered`, `TestOutsideIsNotQuadratic`, `FuzzCheckNoEffects`. CLI `TestManifestRefusalSaysWhenAPathCannotBeResolved`, `TestNotesAndRefusalsNeutraliseBidiAndC1`, `TestManifestRunsLeaveNoGoroutine`. e2e `TestP81ManifestComparesCanonicalPaths` |
| B14 | off by one on the limit: a relative path exactly `MaxScopeLen` long once resolved becomes a "never allowed" note (`>` becomes `>=`) | yes | unit `TestRelativePathTooLongOnceResolvedIsANote`. e2e: none |
| B15 | off by one in `least`: the last need is never printed | yes | 19 unit tests (including `TestReportedGrantsSufficeAndAreNeeded` and all three fuzz seed sets), 4 CLI tests, 11 e2e tests |
| B16 | `visit` marks an effect call as handled but never records it (`a.call` dropped) | yes | 27 unit tests, 10 CLI tests, including `TestManifestRunsLeaveNoGoroutine` |
| B17 | `literalScope`: a net scope is returned as written, not normalised as the runtime compares it (I3: hosts lowercased) | yes | unit `TestURLScope`, `TestLiteralScopeKeepsPathsAsWritten`, `TestUnspecifiedAddressAlsoNeedsLoopback`, `TestLeastMatchesEveryPairComparison`, `TestNeedsMatchTheRuntimesRequests`. e2e: none |
| B18 | `checkCLI` never compares the needs with an `arv:allow` header | yes | CLI `TestCheckWithAManifest`, `TestManifestRefusalListsAtMostTenNeeds`, `TestManifestRefusalSaysWhenAPathCannotBeResolved`, `TestNotesAndRefusalsNeutraliseBidiAndC1`. e2e `TestP81RunManifest`, `TestP81ManifestMalformedHeader`, `TestP81ManifestComparesCanonicalPaths` |
| B19 | `applyManifest` accepts `--allow` together with `--manifest`, so the run holds more than the header (I1) | yes | CLI `TestRunManifestErrors`. e2e `TestP81RunManifest` |
| B20 | `applyManifest` grants the header plus `env=*`, `fs_read=/` and `fs_write=/` (I1: exactly the header) | yes | CLI `TestRunManifestIsTheWholeGrantSet` (the package then hit the 300 s limit). e2e `TestP81ManifestNotesAreDeniedAtRunTime` |
| B21 | `least`: the `Covers` test is removed, so any candidate scope drops a need (`net=*` drops `127.0.0.1:P`) | yes | unit `TestWildcardDoesNotCoverLoopback`, `TestLeastMatchesEveryPairComparison`. e2e: none |
| B21a | `least` env `coverers` also lists `PATH` | yes (round 2). The answers do not change (`Covers` still decides), but every env need costs an extra `Covers` call against `env=PATH`, the start of the quadratic comparison `coverers` avoids | unit `TestEnvCoverersAreOnlyTheNameAndStar` (records every `Covers` call from `least` and `Outside`: an env need is asked about only `*` and its own name) |
| B22 | `concat` folds any infix operator, not only `+` | yes | unit `TestOnlyStringPlusFolds`. e2e: none (the evaluator refuses `-` on strings before admitting anything) |
| E1 | `getenv` removed from the `effects` table | yes | unit `TestEveryBuiltinIsClassified` and 14 more. CLI 4 tests. e2e 6 tests |
| E2 | `evaluator.BuiltinNames` leaves out its first name | yes | unit `TestEveryBuiltinIsClassified` (and `arv/evaluator`'s `TestBuiltinNamesListsEveryBuiltin` checks the count) |

## Guarantee 3: every syntax form is searched (scope options, `match`, `supervise`)

All in `walk.go`'s `children`. `supervise` is a scope option, so C1 covers it.

| Id | Mutation | Caught | Catching tests |
|---|---|---|---|
| C1 | scope option values are not walked (`supervise`, `timeout_ms`, ...) | yes | unit `TestEverySyntaxFormIsSearched`. **e2e: none** (see gaps) |
| C2 | the `err` arm of `match` is not walked | yes | unit `TestEverySyntaxFormIsSearched`, `TestReboundNamesAreNotConstants`. e2e `TestP81CheckSearchesEverySyntaxForm` |
| C3 | the `ok` arm of `match` is not walked | yes | the same three tests as C2 |
| C4 | `spawn ... with`'s capability list is not walked (I2) | yes | 7 unit tests including `TestEverySyntaxFormIsSearched`, `TestNeedsMatchTheRuntimesRequests`. CLI `TestCheckPrintsGrantsAndNotes`. e2e `TestP81CheckSearchesEverySyntaxForm`, `TestP81CheckNotes`, `TestP81ExamplesCheck` |
| C5 | `else` branches are not walked | yes | unit `TestEverySyntaxFormIsSearched`, `FuzzCheckSound` (seeds). e2e: none |
| C6 | hash keys are not walked | yes | unit `TestEverySyntaxFormIsSearched`. e2e: none |
| C7 | a `for`'s iterable is not walked | yes | unit `TestEverySyntaxFormIsSearched`. e2e: none |
| C8 | a node type `children` does not know is skipped silently, with no error | yes | unit `TestUnknownNodeStopsTheWalk`, `TestProgramFailsOnUnknownSyntax` |
| C9 | a `return`'s value is not walked | yes | unit `TestEverySyntaxFormIsSearched`. e2e: none |
| C10 | call arguments are not walked | yes | 6 unit tests including both generator properties. CLI `TestCheckPrintsGrantsAndNotes`. 7 e2e tests |

## Result

All 39 mutations that change behaviour were caught. One mutation (B21a) changes no answer, only the work done; round 2 added a test that catches it, so all 40 are caught. Every guarantee and every invariant this phase touches (I1, I2, I3, I7, and the leak rule) is caught by at least one test.

**Gaps (recorded here; this role adds no tests):** the e2e suite alone misses C1 (scope option values), C5, C6, C7 and C9. `e2e/testdata/p81_forms.arv` places its needs in scope *bodies*, hash *values*, the *then* branch and a `for` *body*, never in an option value, a hash key, an `else` branch, an iterable or a `return`. The unit test `TestEverySyntaxFormIsSearched` catches all five. The e2e suite also misses the folding and binding-count mutations (B6 to B9, B22), B5, B12, B14, B17 and B21, which unit tests catch. A follow-up could add an option-value need (`scope (timeout_ms: len(read_file("in/opt.txt")))`) and a hash-key need to `p81_forms.arv`. A4 (the goroutine leak) cannot be seen by e2e, which runs the binary as a child process. The in-process leak tests catch it.

## Mutation check round 2 (re-check of B21a)

Only the mutation round 1 left uncaught was re-checked, after the fixer's commit `7dc966d` added `arv/check/coverers_test.go`. The mutation was applied to `arv/check/least.go`, then `go test -race ./arv/check/` and `go test -count=1 -timeout 300s . ./e2e/` were run. The file was then restored with `git checkout --` and `git status` was confirmed clean. No test was added and nothing was committed but this note. Load average was about 38.

| Guarantee | Mutation | Caught | Catching tests |
|---|---|---|---|
| sound over-approximation | B21a: `coverers` for env returns `{n.Scope, "*", "PATH"}` (also lists `PATH`) | yes | unit `TestEnvCoverersAreOnlyTheNameAndStar`. It fails on the `coverers` result (3 entries, want 2) and on the recorded `Covers` calls (`env=PATH asked about env=*`, `env=A`, `env=HOME`, `env=B`). CLI: none. e2e: none (the answers do not change, so no output differs) |

All 40 mutations are now caught by at least one test. B21a is caught only by the unit test. That is expected, because the mutation changes the work done (extra `Covers` calls), not any grant or note that the CLI or e2e suite can see.
