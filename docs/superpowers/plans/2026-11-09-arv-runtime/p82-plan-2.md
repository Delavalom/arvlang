# Phase 8.2, part 2 of 8: CapSet checks host kinds (task p82.2)

> **For agentic workers:** this file is self-contained. Implement task p82.2 exactly as written, test first. Optional background, in `docs/superpowers/plans/2026-11-09-arv-runtime/`: `15-phase8-tooling.md` §8.2, `16-implementation-notes.md` (it overrides older plan text), the invariants I1–I8 in `01-design.md`, and the overview and deviations in `p82-plan-1.md`.

**Goal:** A `CapSet` built with `NewCapSetKinds` allows, admits and attenuates grants of host kinds with the kind's own coverage rule (exact match by default), and every set derived from it keeps the registry.

**Invariants touched:** I2 (a child never holds more than its parent, now for host kinds: property test), I1 (a set without the registry allows no host kind).

**Depends on:** p82.1 (`runtime.Kinds`).

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

### Task p82.2: CapSet checks host kinds

**Design.** The registry rides in the `CapSet`, because every check already goes through one: `Task.Require` and `Task.Admit` (effects), and `AttenuateEach` (spawn). `Capability` stays a comparable `{Kind, Scope}` pair. A `func` field would make it unusable as a map key, which `AttenuateEach` relies on. Host-kind requests skip `newPrepaid`: there is nothing to canonicalise and nothing to charge. Any failure, an invalid scope included, is a denial naming the request, like an fs request that fails to canonicalise outside every grant. Only four hunks touch `capability.go`, which Phase 7 may also edit (to add `llm` and `tool`), so the merge stays small.

**Files:**
- Create: `arv/runtime/capset_kinds.go`
- Create: `arv/runtime/capset_kinds_test.go`
- Modify: `arv/runtime/capability.go` (4 hunks)

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/capset_kinds_test.go`:

```go
package runtime

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
)

func mustKinds(t *testing.T, specs ...KindSpec) *Kinds {
	t.Helper()
	k, err := NewKinds(specs...)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// subtree covers a request equal to the grant or below it ("team/a"
// under "team"): reflexive and transitive, as KindSpec requires.
func subtree(g, r string) bool { return r == g || strings.HasPrefix(r, g+"/") }

func TestCapSetChecksHostKinds(t *testing.T) {
	k := mustKinds(t, KindSpec{Kind: "k8s"}, KindSpec{Kind: "team", Covers: subtree})
	s := NewCapSetKinds(k, Capability{"k8s", "staging"}, Capability{"team", "infra"})
	if s.Kinds() != k {
		t.Fatal("Kinds() lost the registry")
	}
	for _, tt := range []struct {
		want Capability
		ok   bool
	}{
		{Capability{"k8s", "staging"}, true},
		{Capability{"k8s", "prod"}, false},
		{Capability{"k8s", "Staging"}, false}, // exact match: no normalisation
		{Capability{"k8s", "staging/x"}, false},
		{Capability{"team", "infra"}, true},
		{Capability{"team", "infra/db"}, true},
		{Capability{"team", "infrastructure"}, false},
		{Capability{"team", "ops"}, false},
		{Capability{"k8s", ""}, false},
	} {
		if got := s.Allows(tt.want); got != tt.ok {
			t.Errorf("Allows(%s) = %v, want %v", tt.want, got, tt.ok)
		}
		c, err := s.Admit(tt.want)
		var denied *DeniedError
		switch {
		case tt.ok && (err != nil || c != tt.want):
			t.Errorf("Admit(%s) = %v, %v; want it as is", tt.want, c, err)
		case !tt.ok && !errors.As(err, &denied):
			t.Errorf("Admit(%s) err = %v, want a denial", tt.want, err)
		case !tt.ok && denied.Want != tt.want:
			t.Errorf("Admit(%s) denial names %s", tt.want, denied.Want)
		}
	}
}

// Without the registry a host kind is unknown: a Capability literal of it
// grants nothing, and a request for it is denied.
func TestCapSetWithoutKindsDeniesHostKinds(t *testing.T) {
	s := NewCapSet(Capability{"k8s", "staging"})
	if s.Allows(Capability{"k8s", "staging"}) {
		t.Fatal("a host kind was allowed without its registry")
	}
	var denied *DeniedError
	if _, err := s.Admit(Capability{"k8s", "staging"}); !errors.As(err, &denied) {
		t.Fatalf("Admit err = %v, want a denial", err)
	}
}

// A child set keeps the registry, holds exactly what it asked for, and
// can be narrowed again (I2).
func TestAttenuateHostKinds(t *testing.T) {
	k := mustKinds(t, KindSpec{Kind: "k8s"}, KindSpec{Kind: "team", Covers: subtree})
	s := NewCapSetKinds(k, Capability{"k8s", "staging"}, Capability{"team", "infra"}, Capability{Net, "example.com"})
	child, err := s.Attenuate([]Capability{{"team", "infra/db"}, {"k8s", "staging"}})
	if err != nil {
		t.Fatal(err)
	}
	if child.Kinds() != k {
		t.Fatal("the child lost the registry")
	}
	if !child.Allows(Capability{"team", "infra/db/eu"}) || child.Allows(Capability{"team", "infra/web"}) {
		t.Fatal("the child holds the wrong team grants")
	}
	if child.Allows(Capability{Net, "example.com"}) {
		t.Fatal("the child holds a grant it did not ask for")
	}
	if _, err := child.Attenuate([]Capability{{"team", "infra"}}); err == nil {
		t.Fatal("a grandchild widened team/infra/db to team/infra")
	}
	if _, err := s.Attenuate([]Capability{{"k8s", "prod"}}); err == nil {
		t.Fatal("a child got k8s(prod) from a parent holding k8s(staging)")
	}
	root := NewRoot(context.Background(), child, io.Discard)
	if err := root.Require(Capability{"k8s", "staging"}); err != nil {
		t.Fatalf("Require: %v", err)
	}
	if err := root.Require(Capability{"k8s", "prod"}); err == nil || !strings.Contains(err.Error(), `k8s("prod")`) {
		t.Fatalf("Require(prod) = %v", err)
	}
}

// I2 for host kinds: whatever a set admits, its child allows nothing its
// parent does not, for a random mix of exact and subtree kinds.
func TestAttenuateHostKindsNeverWidens(t *testing.T) {
	k := mustKinds(t, KindSpec{Kind: "k8s"}, KindSpec{Kind: "team", Covers: subtree})
	r := rand.New(rand.NewPCG(82, 2))
	scopes := []string{"a", "b", "a/x", "a/y", "a/x/1", "b/x", "ax", "a/"}
	kindsOf := []Kind{"k8s", "team"}
	pick := func() Capability {
		return Capability{kindsOf[r.IntN(2)], scopes[r.IntN(len(scopes))]}
	}
	for i := 0; i < 3000; i++ {
		grants := make([]Capability, 1+r.IntN(3))
		for j := range grants {
			grants[j] = pick()
		}
		parent := NewCapSetKinds(k, grants...)
		reqs := make([]Capability, 1+r.IntN(3))
		for j := range reqs {
			reqs[j] = pick()
		}
		child, err := parent.Attenuate(reqs)
		if err != nil {
			continue
		}
		for _, probe := range kindsOf {
			for _, sc := range scopes {
				c := Capability{probe, sc}
				if child.Allows(c) && !parent.Allows(c) {
					t.Fatalf("parent %v, requests %v: the child allows %s", grants, reqs, c)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test ./arv/runtime/ -run 'HostKinds'`

Expected: a build failure, `undefined: NewCapSetKinds`.

- [ ] **Step 3: Implement**

Create `arv/runtime/capset_kinds.go`:

```go
package runtime

// NewCapSetKinds is NewCapSet for a run whose host declared kinds k
// (Phase 8.2): grants and requests of those kinds are checked with k's
// coverage rule. Every set Attenuate derives from it keeps k.
func NewCapSetKinds(k *Kinds, caps ...Capability) CapSet {
	s := NewCapSet(caps...)
	s.kinds = k
	return s
}

// Kinds returns the host kinds s checks (nil if none).
func (s CapSet) Kinds() *Kinds { return s.kinds }

// allowsHost is Allows for a request of a host kind.
func (s CapSet) allowsHost(r Capability) bool {
	for _, c := range s.caps {
		if s.kinds.covers(c, r) {
			return true
		}
	}
	return false
}

// admitHost is AdmitCharged for a request of a host kind: there is
// nothing to canonicalise or charge, so it validates r (Kinds.Literal)
// and checks it. Any failure is a denial naming r.
func (s CapSet) admitHost(r Capability) (Capability, error) {
	c, err := s.kinds.Literal(r.Kind, r.Scope)
	if err != nil || !s.allowsHost(c) {
		return Capability{}, &DeniedError{Want: r, Held: s.List()}
	}
	return c, nil
}

// covers is Capability.Covers for a host kind: g covers r if both are of
// that kind, neither scope is empty, and the kind's Covers (or equality)
// says so.
func (k *Kinds) covers(g, r Capability) bool {
	if k == nil {
		return false
	}
	spec, ok := k.specs[g.Kind]
	if !ok || g.Kind != r.Kind || g.Scope == "" || r.Scope == "" {
		return false
	}
	if spec.Covers == nil {
		return g.Scope == r.Scope
	}
	return spec.Covers(g.Scope, r.Scope)
}
```

In `arv/runtime/capability.go`, four hunks.

1. Replace:

```go
// CapSet is an immutable set of capabilities held by one task.
type CapSet struct{ caps []Capability }
```

with:

```go
// CapSet is an immutable set of capabilities held by one task. kinds, if
// set, declares the host's own kinds (NewCapSetKinds).
type CapSet struct {
	caps  []Capability
	kinds *Kinds
}
```

2. Replace:

```go
func (s CapSet) Allows(r Capability) bool {
	for _, c := range s.caps {
```

with:

```go
func (s CapSet) Allows(r Capability) bool {
	if s.kinds.Has(r.Kind) {
		return s.allowsHost(r)
	}
	for _, c := range s.caps {
```

3. Replace:

```go
func (s CapSet) AdmitCharged(r Capability, charge func(int64) error) (Capability, error) {
	var spent error
```

with:

```go
func (s CapSet) AdmitCharged(r Capability, charge func(int64) error) (Capability, error) {
	if s.kinds.Has(r.Kind) {
		return s.admitHost(r)
	}
	var spent error
```

4. In `AttenuateEach`, Replace:

```go
	return CapSet{caps: canon}, nil
```

with:

```go
	return CapSet{caps: canon, kinds: s.kinds}, nil
```

`NewCapSet` is unchanged (a set with no registry), so every existing caller keeps its behaviour. `Grant`, `AllowsDial` and `Lineage` concern fs and net only and need no change.

- [ ] **Step 4: Run them and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test -race ./arv/runtime/`

Expected: `ok`, the existing runtime tests included. Dropping `kinds: s.kinds` in hunk 4 fails `TestAttenuateHostKinds` ("the child lost the registry"); dropping hunk 2 or 3 fails `TestCapSetChecksHostKinds`.

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... \
  && git add arv/runtime/capset_kinds.go arv/runtime/capset_kinds_test.go arv/runtime/capability.go \
  && git commit -F - <<'EOF'
feat(runtime): CapSet checks host kinds; attenuation keeps them

Phase 8.2 (p82.2): NewCapSetKinds carries a run's host kinds. Allows,
Admit and Attenuate check a host kind with its Covers (exact match by
default), and a child set keeps the registry. I2 property test over
3000 random sets of exact and subtree kinds.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
