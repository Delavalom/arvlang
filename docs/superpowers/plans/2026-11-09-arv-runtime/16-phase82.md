# Phase 8.2: host effects (as built)

Built from `p82-plan-1.md` to `p82-plan-8.md` on `phase/p82`, in parallel with Phases 6, 7 and 8.1. This file overrides §8.2 of `15-phase8-tooling.md` where they differ.

## What a host gets

```go
arv.Run(src, arv.Options{
	Caps:    []runtime.Capability{{Kind: "k8s", Scope: "staging"}},
	Kinds:   []runtime.KindSpec{{Kind: "team", Covers: subtree}}, // optional
	Effects: map[string]arv.Effect{
		"k8s_apply": {Kind: "k8s", Scope: namespaceArg, Fn: applyManifest},
	},
})
```

| Piece | Where | What it does |
|---|---|---|
| `runtime.KindSpec`, `runtime.Kinds`, `NewKinds`, `IsBuiltin` | `arv/runtime/kinds.go` | A per-run registry of host kinds. A name is `[a-z][a-z0-9_]*`, at most 64 bytes, never a built-in kind, never twice. A host scope is kept as written: non-empty, at most `MaxScopeLen`. |
| `NewCapSetKinds`, `CapSet.Kinds` | `arv/runtime/capset_kinds.go`, 4 hunks in `capability.go` | A set checks host kinds with the kind's `Covers` (exact match when nil). `Attenuate` keeps the registry, so a child's set does too. A set without the registry allows no host kind. |
| `hostValue` | `arv/evaluator/hostvalue.go` | A result must be data (nil, integers, strings, booleans, arrays, hashes). Booleans and nils become the evaluator's own; a copy is made only if something changed. Charged in full: strings, 16 per element, 48 per pair, hash keys too. |
| `evaluator.Effect`, `HostBindings` | `arv/evaluator/hostbind.go` | Refuses an effect without a kind, with a built-in or undeclared kind, without `Scope` or `Fn`, or whose name is not an identifier, is a keyword, a builtin or a kind's constructor. Returns each effect and a constructor per kind (`k8s("staging")`). |
| `hostEffect`, `hostGate` | `arv/evaluator/hostcall.go` | A call runs `Scope(args)`, then `Task.Admit`, then `Fn` through `hostGate`. |
| `arv.Effect`, `Options.Effects`, `Options.Kinds` | `arv/effects.go`, `arv/arv.go` | An effect's kind that `Options.Kinds` leaves out is declared with exact match. Bindings go in the run's global environment. A bad setup is an error before anything runs. |
| Example host | `examples/host/` | `k8s_apply` on a fake cluster; `go run ./examples/host -allow staging`. Built and run by `e2e/p82_host_test.go`. |

## Guarantees and the tests that hold them

| Guarantee | Tests |
|---|---|
| Fn never runs unless the calling task holds a covering grant | `TestHostEffectNeedsItsGrant`, `TestRunHostEffect`, `TestHostEffectGoesThroughTheGate` (a denied call never reaches the gate) |
| No effect without a kind, Scope or Fn; no name that hides a builtin | `TestHostBindingsRefusesUncheckedEffects`, `TestRunRejectsBadHostSetup` |
| A result cannot smuggle in a builtin or function | `TestHostValueRefusesNonData`, `TestHostEffectErrorsAreArvErrors` |
| I2 for host kinds | `TestAttenuateHostKinds`, `TestAttenuateHostKindsNeverWidens` (3000 random sets), `TestHostEffectAttenuates`, `TestRunHostKindCovers` |
| Budgets: steps settled before the call, result charged | `TestRunHostEffectBudgets` |
| Cancellation reaches Fn; no goroutine outlives Run | `TestRunHostEffectIsCancelled` |
| Fn's errors and panics become Arv errors | `TestHostEffectErrorsAreArvErrors`, `TestRunHostEffectPanics` |

## Deviations from §8.2

1. **`Fn` returns `(value.Object, error)` and `Scope` returns `(string, error)`.** The design's `Scope` type-asserted `args[0]` and would panic on a bad call; its `Fn` could fail only by building an error value. An error from `Fn` becomes `name: message` of kind `effect` (or the kind the error carries, e.g. `capability` for a `*runtime.DeniedError`, `cancelled` once the task is), cut to 1024 bytes.
2. **The registry is per run, not global in `runtime`.** Two hosts in one process may declare the same kind differently. It lives in the `CapSet`, which every check already goes through.
3. **A host effect needs a host kind.** `net`, `fs_read`, `fs_write` and `env` are refused: their checks (canonical paths, the dial-time address check) hold only for the runtime's own effects, so a host effect claiming `net` would be checked on a name and could connect anywhere.
4. **Each host kind gets a constructor** (`k8s("staging")`), so a program can hand the grant to a child. The design didn't say how.
5. **Results must be data.** Otherwise `Fn` could return a builtin, an effect no grant checks.
6. **"Recorded and replayed" in the acceptance test waits for Phase 6** (see the seam below). Denied and allowed are covered by the example host's unit and e2e tests.

## The Phase 6 seam (for the 6x7 integration)

Phase 6 was not merged when this was built. `hostGate` in `arv/evaluator/hostcall.go` (marked `SEAM(p6)`) is the one place every admitted host call passes, with the task, the effect's name and its arguments; today it calls `do()`. The integration should:

- Route it through Phase 6's gate. The gate is a method on `Interpreter`, and a builtin sees only the task. Either let `applyFunction` recognise a host builtin (for example by a field on `value.Builtin` or a per-run map) and call `in.effect(name, args, do)`, or keep the effect sequence number on the `Task`.
- Keep the capability check before the gate, so a replay cannot skip it. A replay of a host run must declare the same `Options.Kinds` and `Effects`, since kinds are per run. The header's grants include host kinds, e.g. `k8s("staging")`.
- Decide what to log for arguments. Results are data and encode with the Phase 6 codec. Arguments aren't checked and may hold a function or a capability value. Either log only data arguments (an error otherwise) or check them at the call.
- Add host effects to Phase 6's "every effect goes through the gate" test (`TestHostEffectGoesThroughTheGate` is a start), and record and replay `examples/host`.

## Known limits

- **A host `Fn` that ignores `t.Ctx` delays its scope's end.** The scope waits for it (I4), as for any builtin. The wrapper doesn't run `Fn` on another goroutine, since that goroutine could outlive the run.
- **A result is charged after `Fn` returns.** Until then the host's memory is the host's. Each admitted call also allocates about 96 bytes for the gate's closure, not charged (measured: 1 allocation, 96 B per call, darwin/arm64), plus whatever `Scope` builds. Per the `alloc_mb` rule this is a bounded constant per call: Minor.
- **`Covers` must be reflexive and transitive.** Attenuation checks each request against the parent's grants. With a non-transitive `Covers`, a child could be allowed what its parent isn't. Exact match (the default) qualifies.
- **`Fn` must not change its arguments.** They are the program's immutable values (I8), and other tasks may hold them.
- **A program can shadow an effect's name** with `let`, as it can shadow `net`. That changes only which value the name means, never what a call is allowed.
- Host kinds aren't available to the CLI (`arv run --allow`), and `arv check` (8.1) doesn't know them.
- A denial lists the task's grants, host kinds included. Phase 7's redaction applies to them too.
