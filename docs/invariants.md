# Arv invariants: traceability matrix

The invariants I1 to I8 are the product. They are defined in [`01-design.md`](superpowers/plans/2026-11-09-arv-runtime/01-design.md) §2. For each one, this file says what it means, where the code enforces it, and what checks it today: tests, fuzz targets and mutation rounds. It is the starting point for the formal work in [`02-verification.md`](superpowers/plans/2026-11-15-arv-next/02-verification.md) (§V1 to §V5). Until that work is done, the invariants are **tested, not proven**.

**How the counts are made.** A test names the invariant it protects in a comment, such as `// I7: a replay performs no live effect`.
- `go run ./scripts/invariants` prints the counts per package.
- `go run ./scripts/invariants -check` fails when an invariant has no test. CI runs it in the `lint` job.

The counts below were taken on 2026-11-23 at `3f49420`. Re-run the script rather than trusting this table.

| Package | I1 | I2 | I3 | I4 | I5 | I6 | I7 | I8 |
|---|---|---|---|---|---|---|---|---|
| `arv` | 0 | 3 | 0 | 2 | 0 | 3 | 13 | 0 |
| `arv/check` | 0 | 2 | 0 | 0 | 0 | 0 | 0 | 0 |
| `arv/evaluator` | 5 | 4 | 8 | 6 | 6 | 5 | 4 | 4 |
| `arv/runtime` | 0 | 14 | 1 | 0 | 1 | 12 | 4 | 0 |
| `arv/value` | 0 | 0 | 0 | 0 | 0 | 0 | 3 | 2 |
| `e2e` | 8 | 8 | 7 | 8 | 7 | 11 | 7 | 3 |
| **total** | 13 | 31 | 16 | 16 | 14 | 31 | 31 | 9 |

A tag counts the comment lines that name an invariant, not every test that checks it. Many tests check an invariant without naming it. Low counts (I8, and I1 outside `e2e`) say where to add tags first.

## The matrix

The "Formal target" column names the theorem or model property in 02-verification.md §V1 that will carry each invariant.

| # | Invariant | Enforced by | Example tests | Fuzz targets | Formal target |
|---|---|---|---|---|---|
| I1 | **No ambient authority.** The root task holds only what the host grants. Builtins are the only way to reach the OS | `Task.Require` (`arv/runtime/task.go`), `arv.Run` (`arv/arv.go`), the CLI's `--allow` parsing (`cmd/arv/main.go`) | `TestCapabilityConstructorsDoNotTouchFS`, `TestHostEffectCapabilityValueIsNotAGrant`, `TestUnheldModelWithoutAHostIsDenied`, `TestUseTimeDenialRevealsNothing` (`arv/evaluator`) | `FuzzAdmit`, `FuzzRunHostScope` | T2 |
| I2 | **Attenuation only.** A child holds a subset of its parent's grants, checked at `spawn`. With no `with`, it holds nothing | `CapSet.Attenuate` (`arv/runtime/capability.go`), `arv/evaluator/spawncaps.go` | `TestSpawnAttenuatesAgentGrants`, `TestHostEffectAttenuates`, `TestReplayedSpawnGrants`, `TestAttenuatedChildSwapCannotEscape` | `FuzzHostKindCovers`, `FuzzRunHostScope`, `FuzzCheckSound` | T3; spawn under resume (TLA+/P) |
| I3 | **Check at use, on canonical requests.** Paths absolute with symlinks resolved, hosts lowercased, every redirect re-checked, files opened beneath the grant | `arv/runtime/canonical.go`, `arv/runtime/grant.go`, `arv/evaluator/fsopen.go`, `arv/evaluator/netreq.go` | `TestReadFileDirSwapCannotEscape`, `TestWriteFileDirSwapCannotEscape`, `TestNestedGrantDirSwapCannotEscape`, `TestHTTPGetChecksRedirectPorts` | `FuzzCanonical`, `FuzzNetCovers`, `FuzzNetScope` | T2 |
| I4 | **Structure.** No scope, task body or program finishes before every task started in it | `Scope.Wait` (`arv/runtime/scope.go`), scopes in `arv/evaluator/concurrency.go` | `TestTimeoutWaitsForTasks`, `TestTaskPanicWaitsForGrandchildren`, `TestStepsExhaustionCancelsTasks`, `TestAllocExhaustionCancelsTasks` | `FuzzCollectTree`, `FuzzRunSupervise` | TLA+/P structure property |
| I5 | **Fail fast by default.** The first failure cancels the siblings and the scope's body, and the scope evaluates to it | `Scope.fail` (`arv/runtime/scope.go`), the context cause; supervision in `arv/evaluator/collect.go` | `TestFirstErrorWinsOverBodyError`, `TestFailFastMatchDoesNotRescue`, `TestRestartSiblings`, `TestAwaitArrayStopsAtFirstError` | `FuzzRunSupervise` | TLA+/P fail-fast property |
| I6 | **Budgets only shrink.** A child's limit is never more than its parent's remaining budget | `runtime.Budget` (`arv/runtime/budget.go`, `budget_llm.go`), `arv/evaluator/scope_options.go` | `TestNestedLimitsOnlyShrinkProperty` (`arv`), `TestNestedStepsShrink`, `TestNestedTimeoutsShrink`, `TestNestedScopeTasksShrink` | `FuzzScopeOptionsSyntax` (syntax only) | T4 |
| I7 | **Effects are the only nondeterminism.** Time, network, files, environment, randomness, model calls and host effects all pass one logged gate | `Interpreter.effect` (`arv/evaluator/gate.go`), the effect log (`arv/runtime/effectlog.go`, `arv/logged.go`) | `TestEveryBuiltinIsPureOrAnEffect`, `TestEffectsAreReplayed`, `TestForgedLogsAreRefused`, `TestPropAgentRunsReplayIdentically`, `TestEnginesTickAlike` | `FuzzForgedReplay`, `FuzzReadLog`, `FuzzReadLogTamper`, `FuzzCodecRoundTrip`, `FuzzForgedAgentResume`, `FuzzEnginesAgree*` | T1; resume-prefix property; DST oracles O1 and O2 |
| I8 | **No shared mutable Arv state between tasks.** Values are immutable, and environments are frozen at spawn | `Environment` freeze (`arv/value/environment.go`) | `TestEnvironmentFrozenAtSpawn` (`arv/value`), `TestChildNeverSeesALaterRebinding`, `TestRestartSeesTheSpawnTimeEnvironment`, `TestPhase4Invariants` (`e2e`) | none (the race detector runs every suite) | T6 |

**Mutation rounds.** Each phase's notes list mutants by the invariant they target. The totals are in [`18-phases-3-8-report.md`](superpowers/plans/2026-11-09-arv-runtime/18-phases-3-8-report.md) §Mutation testing: 431 mutants were tried in round 1 and 407 caught at once. After the follow-up rounds, every one is caught except one equivalent mutant. Roadmap §V5 turns those mutants into a nightly regression suite. Once it exists, this matrix gets a "mutants" column, and `scripts/invariants -check` fails when an invariant has no mutant.

## Gaps to close next

- **No fuzz target for I8.** It is held by `-race` and by the freeze tests.
- **I6 has no fuzz target on budget values,** only on scope-option syntax. A property test covers nesting (`TestNestedLimitsOnlyShrinkProperty`); restarts and agents need the same.
- **I1 is tagged in only two packages.** Tag the existing denial tests in `arv/runtime` and `arv` as they are touched.
- **Every theorem and model in the last column is still to be written** (roadmap §V1).
- **The known limits** (fs grants that cover a key file, hard links, forged logs, `alloc_mb` measuring churn rather than live memory) are in [`security-model.md`](security-model.md) and in [`18a`](superpowers/plans/2026-11-09-arv-runtime/18a-phases-3-8-fuzzing-and-limits.md). Each is a statement of what an invariant does not cover, not a failing test.
