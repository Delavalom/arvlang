# Phase 8.2, part 4 of 8: binding host effects; the checked call (task p82.4)

> **For agentic workers:** this file is self-contained. Implement task p82.4 exactly as written, test first. Optional background, in `docs/superpowers/plans/2026-11-09-arv-runtime/`: `15-phase8-tooling.md` §8.2, `16-implementation-notes.md` (it overrides older plan text), the invariants I1–I8 in `01-design.md`, and the overview and deviations in `p82-plan-1.md`.

**Goal:** `evaluator.Effect` and `HostBindings`: a host effect is bound only with a declared host kind, a `Scope` and an `Fn`, and every call is admitted against the calling task's grants before `Fn` runs.

**Invariants touched:** I1 and I3 (`Fn` never runs without a covering grant), I2 (host grants pass only through `spawn ... with`).

**Depends on:** p82.2, p82.3.

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

### Task p82.4: binding host effects; the checked call

**Deviations** (see `p82-plan-1.md`):
- `Scope` returns `(string, error)` (its error is a runtime error and `Fn` never runs); §8.2's type-asserted `args[0]` and would panic. `Fn` returns `(value.Object, error)`; its error becomes an Arv error as in `readError` (kind `effect`, the error's own kind, or `cancelled`).
- Built-in kinds are refused: a host effect claiming `net` would be checked on a name and then connect anywhere, since the dial-time check and `os.Root` hold only for the runtime's own effects.
- Each host kind gets a constructor, `k8s("staging")`, which builds a capability value as `net("host")` does: no authority until `spawn ... with` admits it.

**No unchecked effect can be bound.** `HostBindings` is the only way a run gets host builtins (p82.6). It refuses an effect with no kind, a built-in or undeclared kind, no `Scope` or no `Fn`, and a name (an effect's or a kind's) that isn't an identifier, is a keyword or hides a builtin, such as `read_file`.

**The call.** `applyFunction` has already settled the steps budget and checked `Task.Err` (Phase 3). The call then:
1. runs `Scope(args)`;
2. checks the scope's length;
3. runs `Task.Admit` on `Capability{Kind, scope}`;
4. calls `Fn` and passes the result through `hostValue`, charged.

`withScope` turns a panic in `Scope` or `Fn` into an Arv error (tested in p82.6). p82.5 adds the Phase 6 seam and error clipping.

**Files:**
- Create: `arv/evaluator/hostbind.go`
- Create: `arv/evaluator/hostcall.go`
- Create: `arv/evaluator/hostbind_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/hostbind_test.go` (`runHost` binds the effects as `arv.Run` will in p82.6; `wantDenied`, `wantKind` and `parse` already exist in this package):

```go
package evaluator

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// fakeK8s is a host effect, apply(namespace, manifest), needing
// k8s(namespace). It counts the calls that reached Fn.
type fakeK8s struct {
	mu    sync.Mutex
	calls []string
	fail  error        // returned by Fn when set
	ret   value.Object // returned by Fn when set
}

func (f *fakeK8s) effect() Effect {
	return Effect{
		Kind: "k8s",
		Scope: func(args []value.Object) (string, error) {
			if len(args) != 2 {
				return "", errors.New("want 2 arguments (namespace, manifest)")
			}
			ns, ok := args[0].(*value.String)
			if !ok {
				return "", errors.New("namespace must be STRING")
			}
			return ns.Value, nil
		},
		Fn: func(t *runtime.Task, args ...value.Object) (value.Object, error) {
			f.mu.Lock()
			f.calls = append(f.calls, args[0].Inspect()+"/"+args[1].Inspect())
			f.mu.Unlock()
			if f.fail != nil {
				return nil, f.fail
			}
			if f.ret != nil {
				return f.ret, nil
			}
			return value.NewHash(map[string]value.Object{"applied": args[1], "ok": &value.Boolean{Value: true}}), nil
		},
	}
}

func (f *fakeK8s) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func k8sKinds(t *testing.T) *runtime.Kinds {
	t.Helper()
	k, err := runtime.NewKinds(runtime.KindSpec{Kind: "k8s"})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// runHost runs src as the root task holding caps, with effects bound in
// its global environment as arv.Run binds them.
func runHost(t *testing.T, kinds *runtime.Kinds, effects map[string]Effect, src string, caps ...runtime.Capability) value.Object {
	t.Helper()
	host, err := HostBindings(kinds, effects)
	if err != nil {
		t.Fatal(err)
	}
	env := value.NewEnvironment()
	for name, b := range host {
		env.Set(name, b)
	}
	root := runtime.NewRoot(context.Background(), runtime.NewCapSetKinds(kinds, caps...), io.Discard)
	return New(root).Run(parse(t, src), env)
}

// There is no way to bind an effect no grant checks: an effect must name
// a declared host kind and say how a call's scope is found, and its name
// may not hide a builtin, a keyword or a kind's constructor.
func TestHostBindingsRefusesUncheckedEffects(t *testing.T) {
	k := k8sKinds(t)
	ok := (&fakeK8s{}).effect()
	with := func(f func(*Effect)) Effect { e := ok; f(&e); return e }
	for _, tt := range []struct {
		name, effect string
		e            Effect
		want         string
	}{
		{"no kind", "apply", with(func(e *Effect) { e.Kind = "" }), "no capability kind"},
		{"built-in kind", "apply", with(func(e *Effect) { e.Kind = runtime.Net }), "built in"},
		{"undeclared kind", "apply", with(func(e *Effect) { e.Kind = "queue" }), "not declared"},
		{"no scope", "apply", with(func(e *Effect) { e.Scope = nil }), "no Scope"},
		{"no fn", "apply", with(func(e *Effect) { e.Fn = nil }), "no Fn"},
		{"empty name", "", ok, "empty name"},
		{"bad name", "k8s-apply", ok, "letter"},
		{"digit first", "1apply", ok, "letter"},
		{"keyword", "spawn", ok, "keyword"},
		{"builtin", "http_get", ok, "builtin"},
		{"puts", "puts", ok, "builtin"},
		{"kind constructor", "k8s", ok, "custom kind"},
		{"too long", strings.Repeat("a", MaxEffectName+1), ok, "longer than"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := HostBindings(k, map[string]Effect{tt.effect: tt.e})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("HostBindings err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
	for _, bad := range []runtime.Kind{"len", "match"} {
		kk, err := runtime.NewKinds(runtime.KindSpec{Kind: bad})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := HostBindings(kk, nil); err == nil {
			t.Errorf("custom kind %q hides a builtin or keyword, and was accepted", bad)
		}
	}
	host, err := HostBindings(k, map[string]Effect{"apply": ok})
	if err != nil || host["apply"] == nil || host["k8s"] == nil || len(host) != 2 {
		t.Fatalf("HostBindings = %v, %v; want apply and the k8s constructor", host, err)
	}
}

// The calling task's grants are checked before Fn: a call without a
// covering grant never reaches the host.
func TestHostEffectNeedsItsGrant(t *testing.T) {
	k := k8sKinds(t)
	f := &fakeK8s{}
	eff := map[string]Effect{"apply": f.effect()}
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}

	wantDenied(t, runHost(t, k, eff, `apply("staging", "web.yaml")`), `k8s("staging")`)
	wantDenied(t, runHost(t, k, eff, `apply("prod", "web.yaml")`, staging), `k8s("prod")`)
	if f.count() != 0 {
		t.Fatalf("Fn ran %d times without a grant", f.count())
	}
	got := runHost(t, k, eff, `let r = apply("staging", "web.yaml"); [r["applied"], r["ok"] == true]`, staging)
	if got.Inspect() != "[web.yaml, true]" || f.count() != 1 {
		t.Fatalf("got %s after %d calls, want [web.yaml, true] after 1", got.Inspect(), f.count())
	}
}

// A child holds a host grant only if spawn passed it one its parent
// covers (I2), and the kind's constructor names it.
func TestHostEffectAttenuates(t *testing.T) {
	k := k8sKinds(t)
	f := &fakeK8s{}
	eff := map[string]Effect{"apply": f.effect()}
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}
	const deploy = "let deploy = fn(ns) { apply(ns, \"m.yaml\")[\"applied\"] };\n"

	got := runHost(t, k, eff, deploy+`await spawn deploy("staging") with [k8s("staging")]`, staging)
	if got.Inspect() != "m.yaml" {
		t.Fatalf("child with the grant: got %s", got.Inspect())
	}
	wantDenied(t, runHost(t, k, eff, deploy+`await spawn deploy("staging")`, staging), `k8s("staging")`)
	wantDenied(t, runHost(t, k, eff, deploy+`await spawn deploy("prod") with [k8s("prod")]`, staging), `k8s("prod")`)
	nested := deploy + "let outer = fn() { await spawn deploy(\"staging\") with [k8s(\"staging\")] };\n"
	wantDenied(t, runHost(t, k, eff, nested+`await spawn outer() with []`, staging), `k8s("staging")`)
	if f.count() != 1 {
		t.Fatalf("Fn ran %d times, want once", f.count())
	}
	if got := runHost(t, k, eff, `k8s("a/b")`); got.Inspect() != `k8s("a/b")` {
		t.Fatalf("constructor: got %s", got.Inspect())
	}
	wantKind(t, runHost(t, k, eff, `k8s("")`), runtime.KindRuntime)
	wantKind(t, runHost(t, k, eff, `k8s(1)`), runtime.KindRuntime)
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test ./arv/evaluator/ -run 'HostBindings|HostEffect'`

Expected: a build failure, `undefined: Effect` (and `HostBindings`, `MaxEffectName`).

- [ ] **Step 3: Implement**

Create `arv/evaluator/hostbind.go`:

```go
package evaluator

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/token"
	"github.com/delavalom/arvlang/arv/value"
)

// Effect is an effect a Go host adds to a run (Phase 8.2). A call runs Fn
// only if the calling task holds a grant of Kind covering Scope(args);
// otherwise it is a capability error. HostBindings refuses an Effect
// without a Kind, a Scope or an Fn: nothing can add an unchecked effect.
type Effect struct {
	// Kind is the capability kind every call needs: a host kind
	// (runtime.Kinds), never a built-in one, whose checks (canonical paths,
	// dial-time address checks) hold only for the runtime's own effects.
	Kind runtime.Kind
	// Scope names the scope of Kind a call with args needs, e.g. the
	// namespace a manifest goes to. An error refuses the call as a
	// runtime error (the program passed bad arguments).
	Scope func(args []value.Object) (string, error)
	// Fn performs the effect, as the calling task t, once t's grants
	// admit the call. It must honour t.Ctx (the scope waits for it, so a
	// call that ignores cancellation delays the scope's end), must not
	// change args, and returns Arv data (see hostValue) or an error, which
	// becomes an Arv error ("name: message", of kind effect unless err
	// says otherwise, or cancelled once t is).
	Fn func(t *runtime.Task, args ...value.Object) (value.Object, error)
}

// MaxEffectName bounds a host effect's name.
const MaxEffectName = 64

// HostBindings validates a host's kinds and effects and returns what a
// run binds in its global environment: a capability constructor for each
// kind in kinds (k8s("staging"), like net("host")), and each effect under
// its name. A name must be an identifier, not a keyword and not a
// builtin's; an effect needs a declared host kind, a Scope and an Fn.
func HostBindings(kinds *runtime.Kinds, effects map[string]Effect) (map[string]*value.Builtin, error) {
	out := make(map[string]*value.Builtin)
	for _, kind := range kinds.List() {
		if err := checkHostName(string(kind)); err != nil {
			return nil, fmt.Errorf("custom kind %q: %w", kind, err)
		}
		out[string(kind)] = &value.Builtin{Fn: kindConstructor(kinds, kind)}
	}
	for _, name := range slices.Sorted(maps.Keys(effects)) {
		e := effects[name]
		if err := checkHostName(name); err != nil {
			return nil, fmt.Errorf("effect %q: %w", name, err)
		}
		var err error
		switch {
		case out[name] != nil:
			err = errors.New("a custom kind's constructor has that name")
		case e.Kind == "":
			err = errors.New("no capability kind: every host effect must name the kind it needs")
		case runtime.IsBuiltin(e.Kind):
			err = fmt.Errorf("kind %q is built in; a host effect needs a host kind", e.Kind)
		case !kinds.Has(e.Kind):
			err = fmt.Errorf("kind %q is not declared", e.Kind)
		case e.Scope == nil:
			err = errors.New("no Scope function")
		case e.Fn == nil:
			err = errors.New("no Fn")
		}
		if err != nil {
			return nil, fmt.Errorf("effect %q: %w", name, err)
		}
		out[name] = &value.Builtin{Charging: hostEffect(name, e)}
	}
	return out, nil
}

func checkHostName(name string) error {
	if name == "" {
		return errors.New("empty name")
	}
	if len(name) > MaxEffectName {
		return fmt.Errorf("name is longer than %d bytes", MaxEffectName)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return errors.New("want a letter or '_', then letters, digits or '_'")
		}
	}
	if lexer.LookupIdent(name) != token.IDENT {
		return errors.New("is a keyword")
	}
	if builtins[name] != nil {
		return errors.New("is a builtin")
	}
	return nil
}

// kindConstructor builds a host kind's capability values, as
// capabilityConstructor does net("host"): it names a grant and gives no
// authority until spawn ... with checks it.
func kindConstructor(kinds *runtime.Kinds, kind runtime.Kind) value.BuiltinFunction {
	return func(t *runtime.Task, args ...value.Object) value.Object {
		scope, errObj := oneString(string(kind), args)
		if errObj != nil {
			return errObj
		}
		c, err := kinds.Literal(kind, scope)
		if err != nil {
			return newError("%s", err)
		}
		return &value.Capability{Cap: c}
	}
}
```

Create `arv/evaluator/hostcall.go` (p82.5 replaces it with the version that has the Phase 6 seam):

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// hostEffect is the builtin that runs e as name. applyFunction has already
// charged the steps that led here and checked that the task may go on; it
// finds the call's scope, admits it against the task's grants, and only
// then calls Fn.
func hostEffect(name string, e Effect) value.ChargingFunction {
	return func(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
		scope, err := e.Scope(args)
		if err != nil {
			return newError("%s: %s", name, err)
		}
		if err := runtime.CheckScopeLen(e.Kind, scope); err != nil {
			return newError("%s: %s", name, err)
		}
		if _, err := t.Admit(runtime.Capability{Kind: e.Kind, Scope: scope}, nil); err != nil {
			return errorOf(err, runtime.KindCapability)
		}
		v, err := e.Fn(t, args...)
		return hostResult(t, name, v, err, charge)
	}
}

// hostResult turns what Fn returned into an Arv value: an error as
// readError reports a failed read, a result that is not data as a runtime
// error, and data, charged, as itself.
func hostResult(t *runtime.Task, name string, v value.Object, err error, charge func(int64) error) value.Object {
	if err != nil {
		return readError(t, name, err)
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
```

- [ ] **Step 4: Run them and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test -race ./arv/evaluator/ -run 'HostBindings|HostEffect|HostValue'`

Expected: `ok`. Without the `t.Admit` check, `TestHostEffectNeedsItsGrant` fails.

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... \
  && git add arv/evaluator/hostbind.go arv/evaluator/hostcall.go arv/evaluator/hostbind_test.go \
  && git commit -F - <<'EOF'
feat(evaluator): host effects, checked before Fn runs

Phase 8.2 (p82.4): evaluator.Effect and HostBindings. An effect needs a
declared host kind, a Scope and an Fn, and a name that hides no builtin
or keyword. A call admits Capability{Kind, Scope(args)} against the
calling task's grants before Fn runs. Each host kind gets a capability
constructor, so spawn ... with can hand it on.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
