# Phase 8.2, part 1 of 8: host kinds registry (task p82.1)

> **For agentic workers:** this file is self-contained. Implement task p82.1 exactly as written, test first. Optional background, in `docs/superpowers/plans/2026-11-09-arv-runtime/`: `15-phase8-tooling.md` §8.2, `16-implementation-notes.md` (it overrides older plan text), the invariants I1–I8 in `01-design.md`, and the overview and deviations in `p82-plan-1.md`.

**Goal:** A per-run registry of capability kinds a Go host declares for its own effects (`runtime.Kinds`), validated so a host kind can never change what a built-in kind means.

**Invariants touched:** I1 (a host kind is authority only the host grants), I2 (groundwork; p82.2 checks it).

**Depends on:** nothing (first task).

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

**Phase 8.2 at a glance** (one plan file per task, `p82-plan-N.md`, all run in order on `phase/p82`):

| Task | What |
|---|---|
| p82.1 | `runtime.KindSpec`, `Kinds`, `NewKinds`, `IsBuiltin`: the registry of host kinds |
| p82.2 | `CapSet` checks host kinds (`NewCapSetKinds`); attenuation keeps the registry; I2 property test |
| p82.3 | `hostValue`: a host result must be Arv data; normalised and sized for `alloc_mb` |
| p82.4 | `evaluator.Effect`, `HostBindings` (no unchecked effect can be bound), kind constructors, the checked call |
| p82.5 | `hostGate`, the Phase 6 seam; host errors become clipped Arv errors |
| p82.6 | `arv.Options.Effects` and `Options.Kinds`; budgets, cancellation, leaks, concurrency through `arv.Run` |
| p82.7 | `examples/host` (fake `k8s_apply`), its e2e test, README |
| p82.8 | notes: `16-phase82.md` and a pointer in `16-implementation-notes.md` |

**How it fits together.** The host passes `Options.Effects` (name → `arv.Effect{Kind, Scope, Fn}`) and optionally `Options.Kinds`. `arv.Run` declares every kind in a per-run `runtime.Kinds`. That registry goes into the root task's `CapSet` (`NewCapSetKinds`), and `Attenuate` hands it to every child. `evaluator.HostBindings` turns each effect into a builtin, plus a constructor per kind (`k8s("staging")`), and `Run` binds them in the program's global environment. A call runs `Scope(args)`, then `Task.Admit` on `Capability{Kind, scope}`, and only then `Fn`, through `hostGate`, the seam the Phase 6 effect gate takes over.

**Deviations from `15-phase8-tooling.md` §8.2,** each explained in the task that makes it:
- `Fn` returns `(value.Object, error)` and `Scope` returns `(string, error)` (p82.4). The design's `Scope` type-asserted `args[0]` and would panic on a bad call.
- The kinds registry is per run and lives in the `CapSet`, not a global table in `runtime` (p82.1, p82.2). Two hosts in one process may declare the same kind differently.
- A host effect needs a host kind. The built-in kinds are refused, because their checks (canonical paths, the dial-time address check) hold only for the runtime's own effects (p82.4).
- Each host kind gets a capability constructor, so a program can hand the grant on with `spawn ... with` (p82.4).
- A host result must be data, so `Fn` can't return a builtin, which would be an effect nothing checks (p82.3).
- "Recorded and replayed" in the acceptance test waits for Phase 6. `hostGate` is the seam, and `16-phase82.md` says what the 6x7 integration must do (p82.5, p82.8).

---

### Task p82.1: host kinds registry

**Why a registry and not the `kinds` map:** `runtime.kinds` is package state shared by every run in the process, and Phase 7 adds `llm` and `tool` to it on another branch. A host's kinds are per run. `IsBuiltin` reads `kinds` at call time, so a kind Phase 7 adds becomes refused as a host kind without any change here.

**Files:**
- Create: `arv/runtime/kinds.go`
- Create: `arv/runtime/kinds_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/kinds_test.go`:

```go
package runtime

import (
	"strings"
	"testing"
)

// A host declares its own kinds; the built-in ones, a malformed name or a
// repeated one are refused, so a host kind can never change what net,
// fs_read, fs_write or env mean.
func TestNewKindsValidates(t *testing.T) {
	for _, tt := range []struct {
		name  string
		specs []KindSpec
		want  string // "" for success
	}{
		{"none", nil, ""},
		{"one", []KindSpec{{Kind: "k8s"}}, ""},
		{"two", []KindSpec{{Kind: "k8s"}, {Kind: "queue_v2"}}, ""},
		{"empty", []KindSpec{{Kind: ""}}, "empty name"},
		{"built in net", []KindSpec{{Kind: Net}}, `"net" is built in`},
		{"built in fs_read", []KindSpec{{Kind: FSRead}}, `"fs_read" is built in`},
		{"built in env", []KindSpec{{Kind: Env}}, `"env" is built in`},
		{"twice", []KindSpec{{Kind: "k8s"}, {Kind: "k8s"}}, `"k8s" declared twice`},
		{"upper case", []KindSpec{{Kind: "K8s"}}, "lower-case"},
		{"leading digit", []KindSpec{{Kind: "8ks"}}, "lower-case"},
		{"dash", []KindSpec{{Kind: "k-8"}}, "lower-case"},
		{"space", []KindSpec{{Kind: "k 8"}}, "lower-case"},
		{"too long", []KindSpec{{Kind: Kind(strings.Repeat("k", MaxKindLen+1))}}, "longer than"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			k, err := NewKinds(tt.specs...)
			if tt.want == "" {
				if err != nil || k == nil {
					t.Fatalf("NewKinds = %v, %v; want a registry", k, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewKinds err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

func TestKindsHas(t *testing.T) {
	k, err := NewKinds(KindSpec{Kind: "k8s"})
	if err != nil {
		t.Fatal(err)
	}
	if !k.Has("k8s") || k.Has("queue") || k.Has(Net) {
		t.Fatal("Has answers for the wrong kinds")
	}
	var none *Kinds
	if none.Has("k8s") {
		t.Fatal("a nil registry declares nothing")
	}
	if !IsBuiltin(Net) || !IsBuiltin(Env) || IsBuiltin("k8s") {
		t.Fatal("IsBuiltin is wrong")
	}
	if got := k.List(); len(got) != 1 || got[0] != "k8s" {
		t.Fatalf("List = %v", got)
	}
}

// A host kind's scope is kept as written: non-empty and at most
// MaxScopeLen bytes. Other kinds go through Literal and New unchanged.
func TestKindsLiteralAndNew(t *testing.T) {
	k, err := NewKinds(KindSpec{Kind: "k8s"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := k.Literal("k8s", "Staging/EU ")
	if err != nil || c != (Capability{Kind: "k8s", Scope: "Staging/EU "}) {
		t.Fatalf("Literal = %v, %v", c, err)
	}
	if _, err := k.Literal("k8s", ""); err == nil || !strings.Contains(err.Error(), "empty scope") {
		t.Fatalf("empty scope: err = %v", err)
	}
	long := strings.Repeat("s", MaxScopeLen+1)
	if _, err := k.Literal("k8s", long); err == nil || strings.Contains(err.Error(), long) {
		t.Fatalf("long scope: err = %v, want an error that does not quote it", err)
	}
	if _, err := k.New("k8s", "prod"); err != nil {
		t.Fatalf("New: %v", err)
	}
	if c, err := k.New(Net, "API.example.com"); err != nil || c.Scope != "api.example.com" {
		t.Fatalf("New(net) = %v, %v; want the built-in normalisation", c, err)
	}
	if _, err := k.Literal("queue", "q"); err == nil || !strings.Contains(err.Error(), "unknown capability kind") {
		t.Fatalf("undeclared kind: err = %v", err)
	}
	var none *Kinds
	if _, err := none.New("k8s", "prod"); err == nil {
		t.Fatal("a nil registry accepted a host kind")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test ./arv/runtime/ -run 'Kinds'`

Expected: a build failure, `undefined: KindSpec` (and `NewKinds`, `MaxKindLen`, `IsBuiltin`).

- [ ] **Step 3: Implement**

Create `arv/runtime/kinds.go`:

```go
package runtime

import (
	"errors"
	"fmt"
	"slices"
)

// KindSpec declares a capability kind of a Go host's own (Phase 8.2), for
// the host's custom effects. Covers reports whether a grant's scope covers
// a request's scope; nil means they must be equal. A host's Covers must be
// reflexive and transitive, as exact match is: attenuation (spawn ... with)
// checks each request against the parent's grants, so a child can hold
// only what the parent's grants cover, and transitivity is what makes
// everything the child's grants cover covered by the parent's too (I2).
type KindSpec struct {
	Kind   Kind
	Covers func(grant, request string) bool
}

// MaxKindLen bounds the name of a host kind.
const MaxKindLen = 64

// Kinds is an immutable registry of host kinds. A nil *Kinds declares
// none, so every method is safe on nil.
type Kinds struct {
	specs map[Kind]KindSpec
}

// NewKinds validates specs and returns their registry. A kind's name is
// a lower-case letter, then lower-case letters, digits or '_', at most
// MaxKindLen bytes; it may not be a built-in kind or appear twice.
func NewKinds(specs ...KindSpec) (*Kinds, error) {
	k := &Kinds{specs: make(map[Kind]KindSpec, len(specs))}
	for _, s := range specs {
		if err := checkKindName(s.Kind); err != nil {
			return nil, err
		}
		if IsBuiltin(s.Kind) {
			return nil, fmt.Errorf("custom kind %q is built in; a host kind needs a name of its own", s.Kind)
		}
		if _, dup := k.specs[s.Kind]; dup {
			return nil, fmt.Errorf("custom kind %q declared twice", s.Kind)
		}
		k.specs[s.Kind] = s
	}
	return k, nil
}

func checkKindName(k Kind) error {
	if k == "" {
		return errors.New("custom kind: empty name")
	}
	if len(k) > MaxKindLen {
		return fmt.Errorf("custom kind: name is longer than %d bytes", MaxKindLen)
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		lower := 'a' <= c && c <= 'z'
		if !lower && (i == 0 || !('0' <= c && c <= '9' || c == '_')) {
			return fmt.Errorf("custom kind %q: want a lower-case letter, then lower-case letters, digits or '_'", k)
		}
	}
	return nil
}

// IsBuiltin reports whether k is one of the runtime's own kinds.
func IsBuiltin(k Kind) bool { return kinds[k] }

// Has reports whether k declares kind.
func (k *Kinds) Has(kind Kind) bool {
	if k == nil {
		return false
	}
	_, ok := k.specs[kind]
	return ok
}

// List returns the declared kinds, sorted.
func (k *Kinds) List() []Kind {
	if k == nil {
		return nil
	}
	out := make([]Kind, 0, len(k.specs))
	for kind := range k.specs {
		out = append(out, kind)
	}
	slices.Sort(out)
	return out
}

// Literal is runtime.Literal for every kind, k's included: a host kind's
// scope must be non-empty and at most MaxScopeLen bytes, and is kept as
// written. It never touches the OS.
func (k *Kinds) Literal(kind Kind, scope string) (Capability, error) {
	if !k.Has(kind) {
		return Literal(kind, scope)
	}
	if scope == "" {
		return Capability{}, fmt.Errorf("%s: empty scope", kind)
	}
	if err := CheckScopeLen(kind, scope); err != nil {
		return Capability{}, err
	}
	return Capability{Kind: kind, Scope: scope}, nil
}

// New is runtime.New for every kind, k's included (which it validates as
// Literal does: a host kind has nothing to canonicalise).
func (k *Kinds) New(kind Kind, scope string) (Capability, error) {
	if !k.Has(kind) {
		return New(kind, scope)
	}
	return k.Literal(kind, scope)
}
```

- [ ] **Step 4: Run them and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test -race ./arv/runtime/ -run 'Kinds'`

Expected: `ok`. Nothing uses the registry yet; p82.2 makes `CapSet` check it.

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... \
  && git add arv/runtime/kinds.go arv/runtime/kinds_test.go \
  && git commit -F - <<'EOF'
feat(runtime): a per-run registry of host capability kinds

Phase 8.2 (p82.1): runtime.KindSpec, Kinds, NewKinds and IsBuiltin.
A host kind is a lower-case name that is never a built-in kind; its
scope is kept as written.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
