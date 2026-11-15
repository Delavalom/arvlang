# Phase 8.2, part 8 of 8: notes (task p82.8)

> **For agentic workers:** this file is self-contained. Implement task p82.8 exactly as written, test first. Optional background, in `docs/superpowers/plans/2026-11-09-arv-runtime/`: `15-phase8-tooling.md` §8.2, `16-implementation-notes.md` (it overrides older plan text), the invariants I1–I8 in `01-design.md`, and the overview and deviations in `p82-plan-1.md`.

**Goal:** Record what Phase 8.2 built, its deviations from §8.2, the Phase 6 seam for the 6x7 integration, and its known limits, in a notes file of its own with a pointer from `16-implementation-notes.md`.

**Invariants touched:** none (documentation).

**Depends on:** p82.1–p82.7.

**Where:** worktree `/Users/delavalom/delavalom-labs/arvlang-wt/p82`, branch `phase/p82` (branched from `feat/phases-3-8`). If the worktree is missing: `git -C /Users/delavalom/delavalom-labs/arvlang worktree add /Users/delavalom/delavalom-labs/arvlang-wt/p82 phase/p82`. Don't edit the main checkout.

**Rules:**
- Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 &&`.
- Keep Go files under 15 KB and markdown under 18 KB. A pre-commit hook blocks any staged file over about 20 KB. Never skip or bypass git hooks.
- Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean, and `go test -race -count=1 ./...` is green. Use `-count=1`: the e2e suite builds binaries itself, so the test cache can't see source changes.
- Check `df -h /System/Volumes/Data` before long runs. If less than 1 GB is free, stop and report `blocked` (disk). The only thing you may delete is the Go build cache (`go clean -cache`).
- Don't weaken a test to make it pass. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it.
- `arv/runtime` stays stdlib-only and never imports `value` or `evaluator`. Never push or touch remotes.
- The machine is heavily loaded (load average above 300). Order events with gates (channels), not sleeps, and keep wall-clock bounds generous.
- Phases 6, 7 and 8.1 run in parallel on other branches. Keep changes additive and local: only the hunks shown here touch existing files.
- Known flake, older than this phase: `TestNoEffectAfterSiblingSpent/steps/spawn_effect(gate())` hung for 2 min once under load while this plan was validated, at a step that touches no code it runs (its `hold` builtin likely waits on `<-outer` forever when the budget refuses `gate` first). Rerun it once. If it recurs, report it, and don't edit it.

**Validated:** every block below was compiled and tested in a scratch copy, with p82.1–p82.8 applied in order, on go1.26.2 darwin/arm64 (`go.mod` says `go 1.24.0`): `gofmt`, `go vet` (darwin and linux) and `go test -race -count=1 ./...`. If an `old` block doesn't match exactly, read the surrounding code and make the equivalent change; don't guess.

---

### Task p82.8: notes

**Why a new file.** `16-implementation-notes.md` is about 18.2 KB, near the 18 KB markdown rule, and Phases 6, 7 and 8.1 add to the `16*` notes in parallel. So Phase 8.2's notes go in `16-phase82.md`, named per phase as the Wave 1 handoff (`18-handoff-after-wave-1.md`) suggests, which avoids a clash over the next free letter (`16o`, ...). The shared file gets one short section pointing at it. If merging leaves `16-implementation-notes.md` over 18 KB, the merge agent moves text out, not this pointer.

**The measured number** in Known limits (96 bytes, one allocation per admitted call, uncharged) came from a throwaway benchmark of the `hostEffect` builtin with an `Fn` returning `NIL`, run with `go test -bench . -benchtime 20000x` on darwin/arm64. Don't add the benchmark. If you want to check it, write it in a scratch file and delete it afterwards.

**Files:**
- Create: `docs/superpowers/plans/2026-11-09-arv-runtime/16-phase82.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md` (append one section)

- [ ] **Step 1: Write the notes**

Create `docs/superpowers/plans/2026-11-09-arv-runtime/16-phase82.md`:

````markdown
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
````

Append to the end of `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`, after the `## Phase 5: gate` section, with one blank line before it:

```markdown
## Phase 8.2: host effects

Notes and the Phase 6 seam (`hostGate`): [`16-phase82.md`](16-phase82.md).
```

- [ ] **Step 2: Check sizes and names**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && wc -c docs/superpowers/plans/2026-11-09-arv-runtime/16-phase82.md docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`

Expected: both under 18432 bytes (about 7 KB and 18.4 KB). Then check that every test name the notes cite exists:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && for n in TestHostEffectNeedsItsGrant TestRunHostEffect TestHostEffectGoesThroughTheGate \
  TestHostBindingsRefusesUncheckedEffects TestRunRejectsBadHostSetup TestHostValueRefusesNonData \
  TestHostEffectErrorsAreArvErrors TestAttenuateHostKinds TestAttenuateHostKindsNeverWidens \
  TestHostEffectAttenuates TestRunHostKindCovers TestRunHostEffectBudgets TestRunHostEffectIsCancelled \
  TestRunHostEffectPanics; do grep -rqw "func $n" arv || echo "missing $n"; done
```

Expected: no output.

- [ ] **Step 3: Run the whole suite**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... && ARV_E2E_RACE=1 go test -race -count=1 ./e2e/`

Expected: `gofmt` prints nothing; every package `ok`.

- [ ] **Step 4: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... \
  && git add docs/superpowers/plans/2026-11-09-arv-runtime/16-phase82.md docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md \
  && git commit -F - <<'EOF'
docs(notes): Phase 8.2 host effects, the Phase 6 seam and known limits

Phase 8.2 (p82.8): 16-phase82.md records what was built, the tests that
hold each guarantee, the deviations from 15-phase8-tooling.md §8.2, what
the 6x7 integration must do with hostGate, and the known limits.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
