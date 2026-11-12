# Phase 8.2: mutation check, round 1

This file holds the mutation table for the "Phase 8.2: host effects" section of `16-implementation-notes.md`. It is a file of its own because `16-implementation-notes.md` is already near the 18 KB limit. It continues [`16-phase82.md`](16-phase82.md) (what was built) and [`16-phase82-hardening.md`](16-phase82-hardening.md) (e2e, fuzz, property and leak tests). The two mutations recorded in the hardening file (`admitHost` skipping the cover check, `Kinds.covers` ignoring kind and empty scope) were not repeated.

## Method

Each mutation was applied by itself on `phase/p82` (at `7b6b2d1`). Then `go test -count=1 -timeout 420s ./arv/... ./e2e/... ./examples/...` ran the unit tests, the fuzz seeds, and the e2e suite, which builds `e2e/p82host` and the arv binary from the mutated tree. After each run the file was restored with `git checkout -- <file>`, and `git status` was clean before the next mutation. No mutation was committed. The machine was shared; its load average was 8 to 35. "Timed out" means the 420 s limit was reached, and a hang counts as caught. The catching tests listed are the top-level tests that failed.

## Mutation table

| # | Guarantee / invariant | Mutation | Caught | Catching tests |
|---|---|---|---|---|
| M01 | Fn needs a covering grant (I1) | `hostEffect` ignores the error from `t.Admit` | yes | `TestHostEffectNeedsItsGrant`, `TestRunHostEffect`, `TestHostEffectGoesThroughTheGate`, `FuzzHostBindings`, `FuzzRunHostScope`; e2e `TestP82Accept`, `TestP82Attenuation`, `TestP82Coverage`, `TestP82HostSecret`, `TestP82HostExample` and 5 more |
| M02 | Fn needs a covering grant | `allowsHost`: any grant of the same kind covers (scope ignored) | yes | `TestCapSetChecksHostKinds`, `TestAttenuateHostKinds`, `FuzzHostKindCovers`, `TestHostEffectNeedsItsGrant`; e2e `TestP82Accept`, `TestP82Coverage` and 12 more |
| M03 | Fn needs a covering grant | `Kinds.covers` drops `g.Kind != r.Kind`, so a grant of one host kind covers another | yes | `FuzzHostKindCovers`, `TestAttenuateHostKindsNeverWidens`, `TestPropAttenuateHostKindsNeverGrantsMore`; e2e `TestP82Accept` |
| M04 | Fn needs a covering grant | exact match (nil `Covers`) becomes prefix match | yes | `TestCapSetChecksHostKinds`, `FuzzHostKindCovers`, `FuzzRunHostScope`; e2e `TestP82Coverage` |
| M05 | Fn needs a covering grant | `CapSet.Allows` returns true for every host-kind request | yes, unit only | `TestCapSetChecksHostKinds`, `TestAttenuateHostKinds`, `FuzzHostKindCovers`. The effect path uses `Admit`, not `Allows`, so e2e cannot see it. |
| M05b | Fn needs a covering grant | `Kinds.covers` drops its empty-scope guards | **no (equivalent through `Run`)** | none. `Kinds.Literal` refuses an empty scope for every grant (`Run` validates `Caps` with `kinds.New`, and constructors use `Literal`) and for every request (`admitHost`), so `covers` never sees an empty scope on these paths. It is reachable only by calling the runtime API directly: `NewCapSetKinds(k, Capability{Kind: "k8s"}).Allows(Capability{Kind: "k8s"})` is true under the mutant. `FuzzHostKindCovers` says `Covers` is never called with an empty scope, but its seeds do not build an empty grant. |
| M07 | Fn needs a covering grant (I1: a capability value is not authority) | `hostEffect` skips the denial when an argument is a capability value of the effect's kind and scope | **no: test gap** | none. With no grant, `k8s_echo("prod", k8s("prod"))` would reach `Fn`. No unit, fuzz or e2e test passes a host capability value to a host effect. The e2e case "a value is not a grant" builds `k8s("prod")` but never passes it to an effect. Proposed test: with no grant, `k8s_echo("prod", k8s("prod"))` and a child spawned without `with` that receives `k8s("prod")` as a spawn argument must both give a capability error, and `Fn` must not run. |
| M08 | I1: a capability value is not authority | the kind constructor adds the value it builds to the calling task's grants | yes | `TestHostEffectAttenuates`, `FuzzHostBindings`, `FuzzRunHostScope`; e2e `TestP82CapabilityValues`, `TestP82Attenuation` |
| M09 | No registration without a kind | `HostBindings` drops the empty-kind and undeclared-kind cases | yes | `TestHostBindingsRefusesUncheckedEffects`, `TestRunRejectsBadHostSetup`, `FuzzHostBindings`; e2e `TestP82Registration` |
| M09b | No registration without a kind | `HostBindings` drops only the empty-kind case | yes | `TestHostBindingsRefusesUncheckedEffects`, `TestRunRejectsBadHostSetup`; e2e `TestP82Registration`. The undeclared-kind case still refuses the effect, and the tests catch it by the message. |
| M10 | No registration without a host kind | drops the built-in-kind and undeclared-kind cases, so an effect may claim `net` | yes | `TestHostBindingsRefusesUncheckedEffects`, `TestRunRejectsBadHostSetup`, `FuzzHostBindings`; e2e `TestP82Registration` (`netkind`, `envkind`) |
| M10b | No registration without a declared kind | drops the undeclared-kind case | yes, unit only | `TestHostBindingsRefusesUncheckedEffects`, `FuzzHostBindings`. `Run` declares every effect's kind (`hostSetup`), so the case is reachable only through `HostBindings`. |
| M11 | No unchecked registration | `HostBindings` accepts a nil `Scope` | yes | `TestHostBindingsRefusesUncheckedEffects`, `FuzzHostBindings`; e2e `TestP82Registration` |
| M12 | No unchecked registration | `HostBindings` accepts a nil `Fn` | yes | same as M11 |
| M13 | No name that hides a builtin | `checkHostName` drops its builtin check | yes | `TestHostBindingsRefusesUncheckedEffects`, `TestRunRejectsBadHostSetup`, `FuzzHostBindings`; e2e `TestP82Registration` |
| M14 | No name that hides a constructor | drops the case where an effect has a kind constructor's name | yes | `TestHostBindingsRefusesUncheckedEffects`, `FuzzHostBindings`; e2e `TestP82Registration` |
| M15 | Kind names are validated | `checkKindName` accepts anything | yes | `TestNewKindsValidates`, `FuzzHostKindCovers`; e2e `TestP82Registration` |
| M16 | Kinds declared once | `NewKinds` drops its duplicate check | yes | `TestNewKindsValidates`; e2e `TestP82Registration` |
| M17 | A host cannot redefine a built-in kind (I3) | `NewKinds` accepts a built-in kind such as `net` | yes | `TestNewKindsValidates`, `TestRunRejectsBadHostSetup`, `FuzzHostKindCovers`; e2e `TestP82Registration` |
| M18 | Budgets: result charged (I6) | `hostResult` never calls `charge` | yes (one test timed out) | `TestRunHostEffectBudgets`, `TestHostResultChecksAsItWalks`, `TestHostResultChecksEveryFewNodes`, `TestHostEffectGoesThroughTheGate`; e2e `TestP82Budgets`; `TestNoGoroutineOutlivesRunWithHostEffects` hung (its sibling-cancelling result never failed) |
| M19 | Budgets: off by one | 16 bytes per array element charged for `len-1` elements | yes, unit only | `TestHostValueNormalisesData`, `TestHostValueWalksSharedNodesOnce`, `TestHostResultChecksEveryFewNodes` |
| M19b | Budgets: off by one | 48 bytes per hash pair charged for `len-1` pairs | yes, unit only | `TestHostValueNormalisesData` |
| M19c | Budgets: off by one | a string charged at `len-1` bytes | yes, unit only | `TestHostValueNormalisesData`, `TestHostValueWalksSharedNodesOnce`, `TestHostResultChecksEveryFewNodes` |
| M20 | Budgets: last chunk charged | `hostResult` drops the final `w.settle()` | yes, unit only | `TestHostResultChecksEveryFewNodes` |
| M21 | Budgets and cancellation before the call | `applyFunction` calls a charging builtin before `settleSteps` and `Task.Err` | yes, by a pre-existing test only | `TestNoEffectAfterDeadline` (`http_get`). No host test fails: `hostEffect`'s own `t.Err()` check after `Admit` stops the cancelled cases in `TestRunHostEffectStopsBeforeFn`. The only thing lost is settling the caller's own unsettled steps before `Fn`, which no host test isolates. |
| M22 | Budgets: shared nodes charged once | `hostWalk.remember` records nothing | yes (one test timed out) | `TestRunHostEffectEchoesDAG`; e2e `TestP82Budgets` (echoed DAG); `TestHostValueWalksSharedNodesOnce` hung (2^n walk) |
| M23 | Cancellation: no Fn after a slow Scope/Covers | `hostEffect` drops the `t.Err()` check after `Admit` | yes, unit only | `TestRunHostEffectStopsAfterSlowScope` |
| M24 | Cancellation: the walk stops | `hostResult`'s check ignores `t.Err()` | yes, unit only | `TestHostResultChecksAsItWalks`, `TestHostResultChecksEveryFewNodes` |
| M48 | Cancellation reaches Fn (I4) | `Fn` gets a copy of the task with `context.Background()` | yes (one test timed out) | e2e `TestP82Cancellation` (5 cases: run timeout, failing sibling, panicking child, scope timeout, eight waiting calls); `TestRunHostEffectStopsBeforeFn` hung |
| M25 | No goroutine leaks; I4 | `Fn` runs on its own goroutine, and `hostEffect` returns at `t.Ctx.Done()` without waiting for it | yes | the test binaries crash: a panic in `Fn` is no longer recovered (`TestP82Errors`, `TestP82Cancellation`, `arv`) |
| M25b | No goroutine leaks; I4 | as M25, with the goroutine recovering panics | yes | `TestNoGoroutineOutlivesRunWithHostEffects` (4 cases), `TestRunHostEffectIsCancelled`, `TestRunHostEffectPanics`, `TestRunHostPanicsAnywhere`; e2e `TestP82Errors`, `TestP82Cancellation` (the panic cases) |
| M26 | Fn errors become Arv errors; I5 | an error from `Fn` becomes `nil` | yes (one test timed out) | `TestHostEffectErrorsAreArvErrors`, `TestHostEffectScopeErrors`; e2e `TestP82Errors`; `TestNoGoroutineOutlivesRunWithHostEffects` hung (a failing call no longer fails its siblings) |
| M27 | Fn errors become Arv errors | a `*value.Error` returned with a nil error is not turned into an error | yes, unit only | `TestHostEffectErrorsAreArvErrors` |
| M28 | Fn errors: bounded size | `clipErr` never cuts | yes | `TestHostEffectErrorsAreArvErrors`, `TestHostEffectScopeErrors`; e2e `TestP82Errors` |
| M29 | Fn errors: off by one | `clipErr` cuts at 1025 bytes | yes, unit only | `TestHostEffectErrorsAreArvErrors`, `TestHostEffectScopeErrors` |
| M30 | Fn errors keep their kind | `readError` replaced by a plain `newError` (loses `capability`, `cancelled`, `budget`) | yes | `TestHostEffectErrorsAreArvErrors`, `TestHostEffectScopeErrors`; e2e `TestP82Errors` |
| M31 | I2 | `AttenuateEach` drops the kind registry from the child's set | yes | `TestAttenuateHostKinds`, `TestHostEffectAttenuates`, `TestPropAttenuateHostKindsNeverGrantsMore`, `FuzzHostKindCovers`; e2e `TestP82Attenuation` and 13 more |
| M32 | I2: a child holds only its request | `admitHost` returns the parent's covering grant instead of the request (widening) | yes | `TestAttenuateHostKinds`, `TestCapSetChecksHostKinds`, `TestPropAttenuateHostKindsNeverGrantsMore`, `TestRunHostKindCovers`, `FuzzHostKindCovers`; e2e `TestP82Attenuation` |
| M33 | I3: a host scope is kept as written | `Kinds.Literal` lower-cases the scope | yes | `TestKindsLiteralAndNew`, `TestCapSetChecksHostKinds`; e2e `TestP82Coverage` |
| M34 | Scope length bounded | `hostEffect` drops `CheckScopeLen` | yes | `TestHostEffectScopeErrors`; e2e `TestP82Coverage` |
| M35 | I7 seam: every admitted call passes `hostGate` | `hostEffect` calls `Fn` directly | yes, unit only | `TestHostEffectGoesThroughTheGate` |
| M36 | I7 seam: the gate sees name and arguments | `hostGate` gets `""` and `nil` | yes, unit only | `TestHostEffectGoesThroughTheGate` |
| M37 | I8, data only: no builtin smuggled in | `hostWalk.data` accepts any value | yes | `TestHostValueRefusesNonData`, `TestHostEffectErrorsAreArvErrors`, `TestHostEffectGoesThroughTheGate`; e2e `TestP82HostSecret` |
| M38 | Results normalised | a host boolean is not replaced by `TRUE` or `FALSE` | yes | `TestHostValueNormalisesData`, `TestHostEffectNeedsItsGrant`, `TestHostEffectGoesThroughTheGate`, `TestRunHostEffectEchoesDAG` |
| M39 | I8: the host's value is never changed | normalising an array writes into the host's slice | yes, unit only | `TestHostValueNormalisesData` |
| M40 | Depth limit: off by one | `depth > maxHostDepth+1` | yes, unit only | `TestHostValueRefusesNonData` |
| M41 | `Options.Kinds` coverage rules are used | `hostSetup` ignores `opts.Kinds`, so effect kinds get exact match | yes | `TestRunHostKindCovers`, `TestRunRejectsBadHostSetup`, `TestRunHostPanicsAnywhere`, `FuzzRunHostScope`; e2e `TestP82Coverage`, `TestP82Attenuation`, `TestP82Registration` |
| M42 | Root set checks host kinds | `Run` builds the root with `NewCapSet`, without the registry | yes | 23 tests, among them `TestRunHostEffect` and e2e `TestP82Accept` |
| M45 | Data integrity | the check that a hash key matches its `HashKey` is dropped | yes, unit only | `TestHostValueRefusesNonData`, `TestHostValueHashesSharedKeyOnce` |
| M46 | Empty host scope refused | `Kinds.Literal` accepts an empty scope | yes | `TestKindsLiteralAndNew`, `TestHostEffectAttenuates`, `TestRunRejectsBadHostSetup`, `FuzzHostKindCovers`; e2e `TestP82CapabilityValues`, `TestP82Registration` |

## Summary

- 50 mutations: 48 caught, 1 equivalent through `Run` (M05b), 1 test gap (M07).
- Every listed guarantee has at least one caught mutation:
  - the grant check: M01-M04, M08;
  - no registration without a kind: M09-M17;
  - budgets: M18-M22;
  - cancellation: M23, M24, M48;
  - Fn errors become Arv errors: M26-M30;
  - no goroutine leaks: M25b.
- Every invariant this phase touches has at least one caught mutation:
  - I1: M01, M08;
  - I2: M31, M32;
  - I3: M17, M33;
  - I4: M48, M25b;
  - I5: M26;
  - I6: M18;
  - I7 (the seam): M35, M36;
  - I8: M37, M39.
- **Test gap (M07).** No test passes a host capability value to a host effect. The runtime is correct: `hostEffect` never looks at its arguments' capability values. But a regression that treated a passed value as authority would go unnoticed. No test was added, because this role does not write tests. The proposed test is in the M07 row.
- **Weak spots, not gaps.**
  - M05b: `FuzzHostKindCovers` never builds an empty grant literal.
  - M10b: reachable only through `HostBindings`.
  - M21: no host test isolates settling the caller's own steps before `Fn`; only the pre-existing `TestNoEffectAfterDeadline` catches it.
  - M35/M36: the seam is held by one unit test, which is expected until the 6x7 integration adds record and replay.
