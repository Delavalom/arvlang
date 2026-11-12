# Phase 8.2, part 5 of 8: the Phase 6 seam; host errors as Arv errors (task p82.5)

> **For agentic workers:** this file is self-contained. Implement task p82.5 exactly as written, test first. Optional background, in `docs/superpowers/plans/2026-11-09-arv-runtime/`: `15-phase8-tooling.md` §8.2, `16-implementation-notes.md` (it overrides older plan text), the invariants I1–I8 in `01-design.md`, and the overview and deviations in `p82-plan-1.md`.

**Goal:** Every admitted host call goes through `hostGate`, the one seam Phase 6's effect gate takes over in the 6x7 integration, and what `Fn` fails with (a Go error or an Arv error value, of any size) becomes a bounded Arv error.

**Invariants touched:** I7 (groundwork: one gate for host effects, after the capability check), I1 (a denied call never reaches the gate).

**Depends on:** p82.4 (`hostEffect`, `hostResult`, the `fakeK8s` test helper).

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

### Task p82.5: the Phase 6 seam; host errors as Arv errors

**The seam.** Phase 6 (record, replay, resume) is built in parallel and isn't merged. Its design (`13-phase6-durable-replay.md`) puts every effect through one gate, `Interpreter.effect(kind, req, do)`. A builtin sees only its task, not the interpreter, so this task leaves a marked seam instead of guessing Phase 6's API: `hostGate(t, name, args, do)`, a package variable that today just calls `do()`.
- It runs after the capability check, so a replay can't skip the check.
- A test can replace it. `TestHostEffectGoesThroughTheGate` shows the gate sees each admitted call by task, name and first argument, and never a denied one.
- p82.8 writes down what the 6x7 integration must do with it.

**Errors.**
- **A `*value.Error` returned as the result** counts as a failure: its message becomes the error, of kind `effect`. A host can't pass off its own kind, say `budget`, this way.
- **A Go error keeps its kind,** through `readError`: a `*runtime.DeniedError` is a `capability` error, `cancelled` once the task is cancelled.
- **Error values aren't charged to `alloc_mb`,** so builtins keep them small (`16-implementation-notes.md`, Phase 3 known limits). A host error, and a `Scope` error, is cut to `maxHostErr` (1024) bytes plus `...`.

**Files:**
- Create: `arv/evaluator/hostcall_test.go`
- Modify: `arv/evaluator/hostcall.go` (replace the whole file)

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/hostcall_test.go` (it uses `fakeK8s`, `k8sKinds` and `runHost` from `hostbind_test.go`):

```go
package evaluator

import (
	"errors"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// What Fn returns or fails with becomes an Arv value or error.
func TestHostEffectErrorsAreArvErrors(t *testing.T) {
	k := k8sKinds(t)
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}
	run := func(f *fakeK8s, src string) value.Object {
		return runHost(t, k, map[string]Effect{"apply": f.effect()}, src, staging)
	}
	e := wantKind(t, run(&fakeK8s{fail: errors.New("boom")}, `apply("staging", "m")`), runtime.KindEffect)
	if e.Message != "apply: boom" || e.Pos != "1:1" {
		t.Fatalf("error %q at %q", e.Message, e.Pos)
	}
	e = wantKind(t, run(&fakeK8s{ret: &value.Error{Message: "bad manifest", Kind: runtime.KindBudget}}, `apply("staging", "m")`), runtime.KindEffect)
	if e.Message != "apply: bad manifest" {
		t.Fatalf("error %q", e.Message)
	}
	denied := &runtime.DeniedError{Want: runtime.Capability{Kind: "k8s", Scope: "x"}}
	wantKind(t, run(&fakeK8s{fail: denied}, `apply("staging", "m")`), runtime.KindCapability)
	e = wantKind(t, run(&fakeK8s{fail: errors.New(strings.Repeat("x", 1<<20))}, `apply("staging", "m")`), runtime.KindEffect)
	if len(e.Message) > maxHostErr+3 {
		t.Fatalf("a 1 MiB host error became a %d-byte Arv error", len(e.Message))
	}
	f := &fakeK8s{}
	e = wantKind(t, run(f, `apply("staging")`), runtime.KindRuntime)
	if !strings.Contains(e.Message, "want 2 arguments") || f.count() != 0 {
		t.Fatalf("error %q after %d calls", e.Message, f.count())
	}
	e = wantKind(t, run(&fakeK8s{ret: &value.Builtin{}}, `apply("staging", "m")`), runtime.KindRuntime)
	if !strings.Contains(e.Message, "not data") {
		t.Fatalf("error %q", e.Message)
	}
	got := run(&fakeK8s{fail: errors.New("boom")}, `match apply("staging", "m") { ok(v) => v, err(e) => e["kind"] }`)
	if got.Inspect() != "effect" {
		t.Fatalf("match saw %s", got.Inspect())
	}
	wantKind(t, run(&fakeK8s{}, `apply(k8s("staging"), "m")`), runtime.KindRuntime)
}

// hostGate is the seam the Phase 6 effect gate takes over (see 16-phase82.md):
// it sees each admitted call, by name and arguments, and no denied one.
func TestHostEffectGoesThroughTheGate(t *testing.T) {
	old := hostGate
	defer func() { hostGate = old }()
	var gated []string
	hostGate = func(t *runtime.Task, name string, args []value.Object, do func() value.Object) value.Object {
		gated = append(gated, t.ID+" "+name+" "+args[0].Inspect())
		return do()
	}
	k := k8sKinds(t)
	f := &fakeK8s{}
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}
	runHost(t, k, map[string]Effect{"apply": f.effect()}, `apply("prod", "m")`, staging)
	got := runHost(t, k, map[string]Effect{"apply": f.effect()}, `apply("staging", "m")["applied"]`, staging)
	if got.Inspect() != "m" || len(gated) != 1 || gated[0] != "root apply staging" {
		t.Fatalf("got %s, gate saw %q", got.Inspect(), gated)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test ./arv/evaluator/ -run 'HostEffect'`

Expected: a build failure, `undefined: maxHostErr` (and `hostGate`).

- [ ] **Step 3: Implement**

Replace the whole of `arv/evaluator/hostcall.go` with:

```go
package evaluator

import (
	"errors"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxHostErr caps the message of an error from a host's Scope or Fn:
// error values are not charged to alloc_mb, so builtins keep them small.
const maxHostErr = 1024

// hostGate runs do, a host effect's call, once the calling task's grants
// have admitted it. SEAM(p6): this is where the Phase 6 effect gate goes
// in the 6x7 integration, so record, replay and resume see every host
// call by name and arguments, after the capability check and in its
// place in the task's sequence of effects (see 16-phase82.md).
var hostGate = func(t *runtime.Task, name string, args []value.Object, do func() value.Object) value.Object {
	return do()
}

// hostEffect is the builtin that runs e as name. applyFunction has already
// charged the steps that led here and checked that the task may go on; it
// finds the call's scope, admits it against the task's grants, and only
// then calls Fn, through hostGate.
func hostEffect(name string, e Effect) value.ChargingFunction {
	return func(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
		scope, err := e.Scope(args)
		if err != nil {
			return clipErr(newError("%s: %s", name, err))
		}
		if err := runtime.CheckScopeLen(e.Kind, scope); err != nil {
			return newError("%s: %s", name, err)
		}
		if _, err := t.Admit(runtime.Capability{Kind: e.Kind, Scope: scope}, nil); err != nil {
			return errorOf(err, runtime.KindCapability)
		}
		return hostGate(t, name, args, func() value.Object {
			v, err := e.Fn(t, args...)
			return hostResult(t, name, v, err, charge)
		})
	}
}

// hostResult turns what Fn returned into an Arv value: an error (or an
// Arv error value) as readError reports a failed read, cut to maxHostErr
// bytes, a result that is not data as a runtime error, and data, charged,
// as itself.
func hostResult(t *runtime.Task, name string, v value.Object, err error, charge func(int64) error) value.Object {
	if ev, ok := v.(*value.Error); ok && ev != nil && err == nil {
		err = errors.New(ev.Message)
	}
	if err != nil {
		return clipErr(readError(t, name, err))
	}
	out, size, err := hostValue(v)
	if err != nil {
		return newError("%s: %s", name, err)
	}
	if charge != nil {
		if err := charge(size); err != nil {
			return errorOf(err, runtime.KindBudget)
		}
	}
	return out
}

// clipErr cuts e's message to maxHostErr bytes.
func clipErr(e *value.Error) *value.Error {
	if len(e.Message) > maxHostErr {
		e.Message = e.Message[:maxHostErr] + "..."
	}
	return e
}
```

What changed from p82.4:
- `maxHostErr`, `hostGate` and `clipErr` are new.
- `Fn` is called inside `hostGate`.
- A `Scope` error is clipped.
- `hostResult` turns a `*value.Error` result into an error, and clips the error it returns.

- [ ] **Step 4: Run them and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test -race ./arv/evaluator/ -run 'HostBindings|HostEffect|HostValue'`

Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... \
  && git add arv/evaluator/hostcall.go arv/evaluator/hostcall_test.go \
  && git commit -F - <<'EOF'
feat(evaluator): hostGate, the Phase 6 seam for host effects

Phase 8.2 (p82.5): every admitted host call goes through hostGate, which
the Phase 6 effect gate takes over in the 6x7 integration. An Arv error
value from Fn is a failure of kind effect, and host error messages are
cut to 1024 bytes.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
