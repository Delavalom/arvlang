# Phase 8.2, part 6 of 8: arv.Options.Effects and Options.Kinds (task p82.6)

> **For agentic workers:** this file is self-contained. Implement task p82.6 exactly as written, test first. Optional background, in `docs/superpowers/plans/2026-11-09-arv-runtime/`: `15-phase8-tooling.md` §8.2, `16-implementation-notes.md` (it overrides older plan text), the invariants I1–I8 in `01-design.md`, and the overview and deviations in `p82-plan-1.md`.

**Goal:** A Go host passes its effects and kinds to `arv.Run`. A bad setup is an error before anything runs, and the guarantees hold end to end through the embedding API: grants, attenuation, budgets, cancellation, no leaked goroutine, concurrency and panics.

**Invariants touched:** I1, I2, I4 (no goroutine outlives `Run`), I5 (a failing sibling cancels a waiting host call), I6 (budgets).

**Depends on:** p82.5.

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

### Task p82.6: arv.Options.Effects and Options.Kinds

**Design.**
- **Kinds.** `hostSetup` declares `Options.Kinds`, plus the kind of every effect that `Options.Kinds` leaves out, with exact match (the design's example declares no kinds). It skips an empty or built-in kind, so `HostBindings` can refuse that effect by name.
- **Bindings** go in the run's global environment before the program runs. They are visible in every task, since they predate every spawn (I8 horizon), and the global `builtins` map stays untouched. A program may shadow a name with `let`, as it may shadow `net`; that changes which value the name means, never what a call is allowed.
- **Grants** in `Options.Caps` are validated through `Kinds.New`, which handles host kinds and otherwise calls `runtime.New` as before. An undeclared kind is still `unknown capability kind`.
- **A run without effects or kinds** gets a nil registry, and `NewCapSetKinds(nil, ...)` equals `NewCapSet(...)`, so nothing changes for existing hosts or the CLI.

**Load-proof ordering.** `TestRunHostEffectIsCancelled` fails the scope only after both waiting calls have reached `Fn`, which a second host effect, `both`, learns from a channel rather than from a sleep. Its first run's `wait` leaves one token in `arrived`, which the test drains (`<-arrived`) before the second run.

**Files:**
- Create: `arv/effects.go`
- Create: `arv/hosteffect_test.go`
- Modify: `arv/arv.go` (4 hunks)

- [ ] **Step 1: Write the failing tests**

Create `arv/hosteffect_test.go` (`noLeak` is an existing helper in `arv/fuzz_test.go`):

```go
package arv

import (
	"bytes"
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// nsScope is the Scope of a k8s effect: its first argument, a namespace.
func nsScope(args []value.Object) (string, error) {
	if len(args) == 0 {
		return "", errors.New("want a namespace")
	}
	s, ok := args[0].(*value.String)
	if !ok {
		return "", errors.New("namespace must be STRING")
	}
	return s.Value, nil
}

// counted is a k8s effect that counts the calls reaching fn.
func counted(n *atomic.Int64, fn func(t *runtime.Task, args ...value.Object) (value.Object, error)) Effect {
	return Effect{Kind: "k8s", Scope: nsScope, Fn: func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		n.Add(1)
		return fn(t, args...)
	}}
}

func applied(t *runtime.Task, args ...value.Object) (value.Object, error) {
	return &value.String{Value: "applied to " + args[0].Inspect()}, nil
}

var staging = runtime.Capability{Kind: "k8s", Scope: "staging"}

// The acceptance test of 15-phase8-tooling.md §8.2, through the
// embedding API: denied without the grant, allowed with it.
func TestRunHostEffect(t *testing.T) {
	var n atomic.Int64
	opts := Options{Effects: map[string]Effect{"k8s_apply": counted(&n, applied)}}
	_, err := Run(`k8s_apply("staging", "web.yaml")`, opts)
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindCapability || !strings.Contains(ae.Msg, `k8s("staging")`) {
		t.Fatalf("without a grant: err = %v", err)
	}
	if n.Load() != 0 {
		t.Fatal("Fn ran without a grant")
	}
	opts.Caps = []runtime.Capability{staging}
	got, err := Run(`k8s_apply("staging", "web.yaml")`, opts)
	if err != nil || got.Inspect() != "applied to staging" || n.Load() != 1 {
		t.Fatalf("with the grant: %v, %v after %d calls", got, err, n.Load())
	}
	_, err = Run(`k8s_apply("prod", "web.yaml")`, opts)
	if !errors.As(err, &ae) || ae.Kind != runtime.KindCapability || n.Load() != 1 {
		t.Fatalf("another namespace: err = %v after %d calls", err, n.Load())
	}
	got, err = Run(`let d = fn() { k8s_apply("staging", "m") }; await spawn d() with [k8s("staging")]`, opts)
	if err != nil || got.Inspect() != "applied to staging" {
		t.Fatalf("attenuated child: %v, %v", got, err)
	}
}

// A bad setup is an error before anything runs.
func TestRunRejectsBadHostSetup(t *testing.T) {
	ok := counted(new(atomic.Int64), applied)
	noKind := ok
	noKind.Kind = ""
	for _, tt := range []struct {
		name string
		opts Options
		want string
	}{
		{"effect without a kind", Options{Effects: map[string]Effect{"k8s_apply": noKind}}, "no capability kind"},
		{"effect hiding a builtin", Options{Effects: map[string]Effect{"read_file": ok}}, "builtin"},
		{"built-in kind declared", Options{Kinds: []runtime.KindSpec{{Kind: runtime.Net}}}, "built in"},
		{"grant of an undeclared kind", Options{Caps: []runtime.Capability{{Kind: "queue", Scope: "q"}}}, "unknown capability kind"},
		{"empty host grant", Options{Effects: map[string]Effect{"k8s_apply": ok}, Caps: []runtime.Capability{{Kind: "k8s"}}}, "empty scope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			tt.opts.Out = &out
			_, err := Run(`puts("ran")`, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
			if out.Len() != 0 {
				t.Fatal("the program ran")
			}
		})
	}
}

// A kind the host declares with its own Covers: team("infra") covers
// team("infra/db"), and a child can be narrowed to it.
func TestRunHostKindCovers(t *testing.T) {
	sub := func(g, r string) bool { return r == g || strings.HasPrefix(r, g+"/") }
	team := Effect{Kind: "team", Scope: nsScope, Fn: applied}
	opts := Options{
		Kinds:   []runtime.KindSpec{{Kind: "team", Covers: sub}},
		Effects: map[string]Effect{"page": team},
		Caps:    []runtime.Capability{{Kind: "team", Scope: "infra"}},
	}
	src := `let p = fn(t) { page(t) }; [page("infra/db"), await spawn p("infra/db/eu") with [team("infra/db")]]`
	got, err := Run(src, opts)
	if err != nil || got.Inspect() != "[applied to infra/db, applied to infra/db/eu]" {
		t.Fatalf("got %v, %v", got, err)
	}
	_, err = Run(`let p = fn(t) { page(t) }; await spawn p("infra/web") with [team("infra/db")]`, opts)
	if err == nil || !strings.Contains(err.Error(), `team("infra/web")`) {
		t.Fatalf("a narrowed child reached infra/web: %v", err)
	}
}

// Budgets: every call settles the steps before it, so a spent steps
// budget stops the calls; a result is charged to alloc_mb.
func TestRunHostEffectBudgets(t *testing.T) {
	var n atomic.Int64
	opts := Options{Caps: []runtime.Capability{staging}, Limits: runtime.Limits{Steps: 2000}}
	opts.Effects = map[string]Effect{"k8s_apply": counted(&n, applied)}
	src := `let loop = fn(i) { k8s_apply("staging", "m"); loop(i + 1) }; loop(0)`
	_, err := Run(src, opts)
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindBudget || n.Load() == 0 || n.Load() > 2000 {
		t.Fatalf("err = %v after %d calls", err, n.Load())
	}
	big := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		return &value.Array{Elements: []value.Object{&value.String{Value: strings.Repeat("x", 2<<20)}}}, nil
	}
	opts = Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"big": counted(&n, big)}}
	_, err = Run(`scope (alloc_mb: 1) { big("staging") }`, opts)
	if !errors.As(err, &ae) || ae.Kind != runtime.KindBudget || !strings.Contains(ae.Msg, "alloc") {
		t.Fatalf("a 2 MiB result under alloc_mb: 1: err = %v", err)
	}
}

// Cancellation reaches Fn through t.Ctx: a timeout and a failing sibling
// both end a call that waits, and nothing outlives the run. The script
// fails only once both waits have reached Fn (both), not after a sleep.
func TestRunHostEffectIsCancelled(t *testing.T) {
	var n, saw atomic.Int64
	arrived := make(chan struct{}, 2)
	wait := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		arrived <- struct{}{}
		<-t.Ctx.Done()
		saw.Add(1)
		return nil, context.Cause(t.Ctx)
	}
	both := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		for i := 0; i < 2; i++ {
			select {
			case <-arrived:
			case <-t.Ctx.Done():
				return nil, context.Cause(t.Ctx)
			}
		}
		return nil, nil
	}
	opts := Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{
		"wait": counted(&n, wait), "both": counted(new(atomic.Int64), both)}}
	before := goruntime.NumGoroutine()
	_, err := Run(`scope (timeout_ms: 100) { wait("staging") }`, opts)
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindBudget || !strings.Contains(ae.Msg, "timeout") {
		t.Fatalf("timeout: err = %v", err)
	}
	<-arrived
	src := `let w = fn() { wait("staging") }; scope { spawn w() with [k8s("staging")]; spawn w() with [k8s("staging")]; both("staging"); 1 / 0 }`
	_, err = Run(src, opts)
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("fail fast: err = %v", err)
	}
	if n.Load() != 3 || saw.Load() != 3 {
		t.Fatalf("%d calls, %d saw the cancellation; want 3 and 3", n.Load(), saw.Load())
	}
	noLeak(t, before, src)
}

// Many tasks call one effect at once, under -race.
func TestRunHostEffectConcurrent(t *testing.T) {
	var n atomic.Int64
	opts := Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"k8s_apply": counted(&n, applied)}}
	src := `let d = fn(i) { k8s_apply("staging", "m") }; let ts = for i in ` + zeroes(50) + ` { spawn d(i) with [k8s("staging")] }; len(await ts)`
	got, err := Run(src, opts)
	if err != nil || got.Inspect() != "50" || n.Load() != 50 {
		t.Fatalf("got %v, %v after %d calls", got, err, n.Load())
	}
}

// A panicking host effect is an error, never a panic in the host.
func TestRunHostEffectPanics(t *testing.T) {
	boom := func(t *runtime.Task, args ...value.Object) (value.Object, error) { panic("host bug") }
	opts := Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"boom": counted(new(atomic.Int64), boom)}}
	_, err := Run(`boom("staging")`, opts)
	if err == nil || !strings.Contains(err.Error(), "host bug") {
		t.Fatalf("err = %v", err)
	}
}

func zeroes(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0, ", n), ", ") + "]" }
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test ./arv/ -run 'Host'`

Expected: a build failure, `undefined: Effect`.

- [ ] **Step 3: Implement**

Create `arv/effects.go`:

```go
package arv

import (
	"maps"
	"slices"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Effect is a Go host's own effect (Phase 8.2): Options.Effects binds it
// under a name, and every call needs a grant of its Kind covering
// Scope(args) before Fn runs. See evaluator.Effect.
type Effect = evaluator.Effect

// hostSetup declares the host's kinds (opts.Kinds, plus, with exact-match
// coverage, every effect's kind declared nowhere else) and returns them
// with the builtins Run binds: each effect, and a constructor per kind.
// An effect whose kind is empty or built in is left to HostBindings,
// which refuses it by name.
func hostSetup(opts Options) (*runtime.Kinds, map[string]*value.Builtin, error) {
	if len(opts.Kinds) == 0 && len(opts.Effects) == 0 {
		return nil, nil, nil
	}
	specs := slices.Clone(opts.Kinds)
	declared := make(map[runtime.Kind]bool, len(specs))
	for _, s := range specs {
		declared[s.Kind] = true
	}
	for _, name := range slices.Sorted(maps.Keys(opts.Effects)) {
		k := opts.Effects[name].Kind
		if k != "" && !declared[k] && !runtime.IsBuiltin(k) {
			declared[k] = true
			specs = append(specs, runtime.KindSpec{Kind: k})
		}
	}
	kinds, err := runtime.NewKinds(specs...)
	if err != nil {
		return nil, nil, err
	}
	host, err := evaluator.HostBindings(kinds, opts.Effects)
	if err != nil {
		return nil, nil, err
	}
	return kinds, host, nil
}
```

In `arv/arv.go`, four hunks.

1. Replace:

```go
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks); a negative one is an error
}
```

with:

```go
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks); a negative one is an error

	// Effects are the host's own effects, by the name the program calls
	// them (Phase 8.2). Each needs a grant of its Kind; Caps may grant
	// host kinds, and the program names them with a constructor per kind,
	// e.g. spawn f() with [k8s("staging")].
	Effects map[string]Effect
	// Kinds declares host kinds with their own coverage rule. A kind that
	// an effect names and Kinds does not is declared with exact match.
	Kinds []runtime.KindSpec
}
```

2. Replace:

```go
// Capability literal bypasses it); an invalid one is an error and nothing runs.
```

with:

```go
// Capability literal bypasses it); an invalid one is an error and nothing runs.
// So is an invalid host effect or kind (opts.Effects, opts.Kinds).
```

3. Replace:

```go
	caps := make([]runtime.Capability, len(opts.Caps))
	for i, c := range opts.Caps {
		valid, err := runtime.New(c.Kind, c.Scope)
```

with:

```go
	kinds, host, err := hostSetup(opts)
	if err != nil {
		return nil, fmt.Errorf("arv: %w", err)
	}
	caps := make([]runtime.Capability, len(opts.Caps))
	for i, c := range opts.Caps {
		valid, err := kinds.New(c.Kind, c.Scope)
```

4. Replace:

```go
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(caps...), opts.Out)
	result = evaluator.New(root).RunLimited(program, value.NewEnvironment(), opts.Limits)
```

with:

```go
	env := value.NewEnvironment()
	for name, b := range host {
		env.Set(name, b)
	}
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSetKinds(kinds, caps...), opts.Out)
	result = evaluator.New(root).RunLimited(program, env, opts.Limits)
```

- [ ] **Step 4: Run them and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test -race -count=3 ./arv/ -run 'Host|TestRun'`

Expected: `ok`. Then the whole suite: `go test -race -count=1 ./...`. If `hostResult` charges `size * 0`, `TestRunHostEffectBudgets` fails.

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... \
  && git add arv/effects.go arv/hosteffect_test.go arv/arv.go \
  && git commit -F - <<'EOF'
feat(arv): Options.Effects and Options.Kinds for Go hosts

Phase 8.2 (p82.6): arv.Run declares the host's kinds, validates host
grants, and binds each effect and kind constructor in the global
environment. Tests through the embedding API: grants, attenuation, a
host Covers, budgets, cancellation without leaks, concurrency, panics.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
