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
| `hostValue` | `arv/evaluator/hostvalue.go` | A result must be data (nil, integers, strings, booleans, arrays, hashes). Booleans and nils become the evaluator's own; a copy is made only if something changed. Charged in full: strings, 16 per element, 48 per pair, hash keys too. `hostWalk` visits each string, array and hash once (a shared one is counted and copied once), charges in chunks and stops once the task is cancelled. |
| `evaluator.Effect`, `HostBindings` | `arv/evaluator/hostbind.go` | Refuses an effect without a kind, with a built-in or undeclared kind, without `Scope` or `Fn`, or whose name is not an identifier, is a keyword, a builtin or a kind's constructor. Returns each effect and a constructor per kind (`k8s("staging")`). |
| `hostEffect`, `hostGate` | `arv/evaluator/hostcall.go` | A call runs `Scope(args)`, then `Task.Admit`, then `Fn` through `hostGate`, then `hostResult` on whatever the gate returned. |
| `arv.Effect`, `Options.Effects`, `Options.Kinds` | `arv/effects.go`, `arv/arv.go` | An effect's kind that `Options.Kinds` leaves out is declared with exact match. Bindings go in the run's global environment. A bad setup is an error before anything runs. |
| Example host | `examples/host/` | `k8s_apply` on a fake cluster; `go run ./examples/host -allow staging`. Built and run by `e2e/p82_host_test.go`. |

## Guarantees and the tests that hold them

| Guarantee | Tests |
|---|---|
| Fn never runs unless the calling task holds a covering grant | `TestHostEffectNeedsItsGrant`, `TestRunHostEffect`, `TestHostEffectGoesThroughTheGate` (a denied call never reaches the gate) |
| No effect without a kind, Scope or Fn; no name that hides a builtin | `TestHostBindingsRefusesUncheckedEffects`, `TestRunRejectsBadHostSetup` |
| A result cannot smuggle in a builtin or function | `TestHostValueRefusesNonData`, `TestHostEffectErrorsAreArvErrors` |
| I2 for host kinds | `TestAttenuateHostKinds`, `TestAttenuateHostKindsNeverWidens` (3000 random sets), `TestHostEffectAttenuates`, `TestRunHostKindCovers` |
| Budgets: steps settled before the call, result charged | `TestRunHostEffectBudgets`, `TestRunHostEffectStopsBeforeFn`, `TestHostResultChecksAsItWalks` |
| A result sharing sub-values (an echoed DAG) costs its nodes, not its paths | `TestHostValueWalksSharedNodesOnce`, `TestHostValueSharedNodeKeepsDepthLimit`, `TestRunHostEffectEchoesDAG` |
| Cancellation reaches Fn; no goroutine outlives Run | `TestRunHostEffectIsCancelled`, `TestRunHostEffectStopsBeforeFn`, `TestRunHostPanicsAnywhere` |
| Fn's errors and panics become Arv errors | `TestHostEffectErrorsAreArvErrors`, `TestHostEffectScopeErrors`, `TestRunHostEffectPanics`, `TestRunHostPanicsAnywhere` (Scope, a child's Fn, Covers) |

## Deviations from §8.2

1. **`Fn` returns `(value.Object, error)` and `Scope` returns `(string, error)`.** The design's `Scope` type-asserted `args[0]` and would panic on a bad call; its `Fn` could fail only by building an error value. An error from `Fn` becomes `name: message` of kind `effect` (or the kind the error carries, e.g. `capability` for a `*runtime.DeniedError`, `cancelled` once the task is), cut to 1024 bytes.
2. **The registry is per run, not global in `runtime`.** Two hosts in one process may declare the same kind differently. It lives in the `CapSet`, which every check already goes through.
3. **A host effect needs a host kind.** `net`, `fs_read`, `fs_write` and `env` are refused: their checks (canonical paths, the dial-time address check) hold only for the runtime's own effects, so a host effect claiming `net` would be checked on a name and could connect anywhere.
4. **Each host kind gets a constructor** (`k8s("staging")`), so a program can hand the grant to a child. The design didn't say how.
5. **Results must be data.** Otherwise `Fn` could return a builtin, an effect no grant checks.
6. **"Recorded and replayed" in the acceptance test waits for Phase 6** (see the seam below). Denied and allowed are covered by the example host's unit and e2e tests.

## The Phase 6 seam (for the 6x7 integration)

Phase 6 was not merged when this was built. `hostGate` in `arv/evaluator/hostcall.go` (marked `SEAM(p6)`) is the one place every admitted host call passes, with the task, the effect's name and its arguments; today it calls `do()`, which calls `Fn` and nothing else. `hostResult` (the data check, normalisation and the `alloc_mb` charge) runs after the gate on whatever it returns, so a replay that returns a logged value is charged as the recorded run was. The integration should:

- Route it through Phase 6's gate. The gate is a method on `Interpreter`, and a builtin sees only the task. Either let `applyFunction` recognise a host builtin (for example by a field on `value.Builtin` or a per-run map) and call `in.effect(name, args, do)`, or keep the effect sequence number on the `Task`.
- Keep the capability check before the gate, so a replay cannot skip it. A replay of a host run must declare the same `Options.Kinds` and `Effects`, since kinds are per run. The header's grants include host kinds, e.g. `k8s("staging")`.
- Decide what to log for arguments. Results are data and encode with the Phase 6 codec. Arguments aren't checked and may hold a function or a capability value. Either log only data arguments (an error otherwise) or check them at the call.
- Add host effects to Phase 6's "every effect goes through the gate" test (`TestHostEffectGoesThroughTheGate` is a start), and record and replay `examples/host`.

## Known limits

- **A host `Fn` that ignores `t.Ctx` delays its scope's end.** The scope waits for it (I4), as for any builtin. The wrapper doesn't run `Fn` on another goroutine, since that goroutine could outlive the run.
- **A result is charged after `Fn` returns.** Until then the host's memory is the host's. Each admitted call also allocates, uncharged, the gate's and the walk's closures (measured after round 1: 2 allocations, 104 B per call with a number or string result, darwin/arm64), plus whatever `Scope` builds. Per the `alloc_mb` rule this is a bounded constant per call: Minor.
- **`hostWalk` remembers the nodes it has walked**, in a map freed when the call returns: about 126 B per string, array or hash below the top (190 B at 1000 nodes, 126 B at 100000; 504 B per call for a hash holding one string). A string met as a hash key also keeps its `HashKey` in a second map (after round 2: 357 B per one-key hash at 100000 hashes, against 126 B per plain node), so a key shared by many hashes is hashed once. Each node is charged before it is remembered, so the maps stay a constant factor of what was charged: Minor.
- **A result holding the program's own values is charged again**, once per node, as any new builtin result is. A shared node is charged once.
- **A result belongs to the run once returned.** It is not copied, so a host that later changes it (a cache it refreshes) races with the tasks reading it (I8). `Effect.Fn`'s doc and the README say so; copying would cost every call and still race during the walk.
- **`Scope`, `Fn` and `Covers` run on many task goroutines at once** and must be safe for concurrent use (`Effect`, `KindSpec` docs, README).
- **A scope longer than `runtime.MaxScopeLen` is a runtime error,** like a `Scope` error: the program passed a bad argument; nothing was denied.
- **`Covers` must be reflexive and transitive.** Attenuation checks each request against the parent's grants. With a non-transitive `Covers`, a child could be allowed what its parent isn't. Exact match (the default) qualifies.
- **`Fn` must not change its arguments.** They are the program's immutable values (I8), and other tasks may hold them.
- **A program can shadow an effect's name** with `let`, as it can shadow `net`. That changes only which value the name means, never what a call is allowed.
- Host kinds aren't available to the CLI (`arv run --allow`), and `arv check` (8.1) doesn't know them.
- A denial lists the task's grants, host kinds included. Phase 7's redaction applies to them too.

## Review round 1 (fixer)

| Finding | Disposition |
|---|---|
| Critical (two reports): an echoed DAG made `hostValue` walk 2^n paths, uncancellable and uncharged until the end | Fixed: `hostWalk` (memo by pointer with nesting height, chunked charge, `t.Err` check). Before: depth 40 had not returned after 30 s against a 200 ms timeout. After: `TestRunHostEffectEchoesDAG` (depth 40) passes at once. |
| Important: `TestRunHostEffectIsCancelled` hung on `<-arrived` when the timeout beat the call | Fixed: non-blocking drain, calls counted per run. Reproduced with a 300 ms effect before `wait`: the old test hung, the new one passes. |
| Minor: recorded and replayed not met | Left to the 6x7 integration (deviation 6, seam above). No record or replay test of `examples/host` exists yet. |
| Minor: `do()` held the charge and normalisation | Fixed: the gate wraps `Fn` alone; `TestHostEffectGoesThroughTheGate` replays a logged value and sees it normalised and charged. |
| Minor: panics untested outside the root's `Fn` | Tested: `TestRunHostPanicsAnywhere`, with leak checks. No code change was needed. |
| Minor: Scope clipping, `CheckScopeLen`, built-in kind via `Run`, no `Fn` after cancellation | Tested: `TestHostEffectScopeErrors`, `TestRunRejectsBadHostSetup`, `TestRunHostEffectStopsBeforeFn`. |
| Minor: concurrency and result ownership undocumented (two reports) | Documented (known limits above). |
| Minor: `clipErr` cut mid-rune | Fixed: it backs up to a rune boundary. |
| Minor: markdown at 18 KB | `p82-plan-4.md` trimmed to under 18,000 bytes. `16-implementation-notes.md` was already 18,261 bytes on `feat/phases-3-8` before this phase added its 4-line pointer; Phases 6, 7 and 8.1 append to it in parallel, so the merge agent should move a section out rather than each branch rewriting it. |

## Review round 2 (fixer)

| Finding | Disposition |
|---|---|
| Important: a hash key string shared by many hashes was re-hashed per hash (`String.HashKey` reads it all) while counted once, so an echoed result overshot its timeout by the program's own loop time (39 s for a 16 MiB key in 1023 hashes) | Fixed: `hostWalk.keyHash` remembers each string key's `HashKey` by pointer, so the hashing done is bounded by the bytes counted and charged. `TestHostValueHashesSharedKeyOnce` (bytes hashed: 1,072,693,248 before, 1,048,576 after) and `TestRunHostEffectEchoesSharedBigKey` (1 MiB key; before, Run returned 1.44 s after `Fn`, as long as the program's 1.51 s; after, 1.6 ms). The e2e bound is a quarter of the program's own time, so load slows both sides. |
| Minor: no test of the check every `hostCheckEvery` nodes | Tested: `TestHostResultChecksEveryFewNodes` (3000 empty arrays and strings, 48000 bytes): 3 charges, and a cancel at the first check stops the walk at node 2048. The bytes-only mutant fails it. |
| Minor: `Fn` ran on a task cancelled while `Scope` or `Covers` ran | Fixed: `hostEffect` checks `t.Err()` after `Admit`. `TestRunHostEffectStopsAfterSlowScope` (Scope sleeps 300 ms under a 50 ms timeout; `Task.Err` reads the deadline, so no timing is involved): `Fn` was called once before, never after. |
