# Phase 8.2, part 3 of 8: host results are Arv data (task p82.3)

> **For agentic workers:** this file is self-contained. Implement task p82.3 exactly as written, test first. Optional background, in `docs/superpowers/plans/2026-11-09-arv-runtime/`: `15-phase8-tooling.md` §8.2, `16-implementation-notes.md` (it overrides older plan text), the invariants I1–I8 in `01-design.md`, and the overview and deviations in `p82-plan-1.md`.

**Goal:** `hostValue` checks that a host effect's result is Arv data (nil, integers, strings, booleans, arrays and hashes of them), maps the host's booleans and nils to the evaluator's own, and says what the result costs `alloc_mb`.

**Invariants touched:** I1 (a host result can't smuggle in a builtin: an effect nothing checks), I8 (the host's value is never changed), budgets (`alloc_mb`).

**Depends on:** p82.2. Nothing calls `hostValue` until p82.4.

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

### Task p82.3: host results are Arv data

**Why data only.**
- A builtin or a function in a result could be an effect no grant checks. That would break the guarantee that every host effect is checked.
- A task handle or a capability value means nothing outside the run that made it.
- Phase 6 can log only data: its codec covers integers, strings, booleans, nil, arrays, hashes and errors.

An error inside data is refused too. An error as the whole result is handled by the caller (p82.5).

**Why normalise.** `==` on booleans and nil compares identity (`evalInfixExpression` falls through to `left == right`), so a host's `&value.Boolean{Value: true}` would not equal `true`. `hostValue` copies an array or hash only when something in it changes, so it never writes into the host's value: the host may share it between calls.

**Why bound the depth.** A Go host can build a cycle, which Arv can't. `maxHostDepth` (1000) stops the walk on a cycle, and keeps the recursion's Go stack small.

**What it charges.** The same as `allocSize` charges a new builtin result, but all the way down: string bytes, hash keys included (a host builds them anew), plus 16 per array element and 48 per hash pair. Integers are not charged, as elsewhere.

**Files:**
- Create: `arv/evaluator/hostvalue.go`
- Create: `arv/evaluator/hostvalue_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/hostvalue_test.go`:

```go
package evaluator

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

// A host effect's result is Arv data: what Phase 6 can log and what the
// program can compare. hostValue maps the host's booleans and nils to the
// evaluator's own (== compares them by identity) and says what to charge.
func TestHostValueNormalisesData(t *testing.T) {
	s := &value.String{Value: "hello"}
	for _, tt := range []struct {
		name string
		in   value.Object
		want string
		size int64
	}{
		{"go nil", nil, "nil", 0},
		{"nil value", &value.Nil{}, "nil", 0},
		{"integer", &value.Integer{Value: 7}, "7", 0},
		{"string", s, "hello", 5},
		{"array", &value.Array{Elements: []value.Object{s, &value.Integer{Value: 1}}}, "[hello, 1]", 2*arrayElemSize + 5},
		{"nested", &value.Array{Elements: []value.Object{&value.Array{Elements: []value.Object{s}}}}, "[[hello]]", 2*arrayElemSize + 5},
		{"hash", value.NewHash(map[string]value.Object{"msg": s}), "{msg: hello}", hashPairSize + 3 + 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, size, err := hostValue(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Inspect() != tt.want || size != tt.size {
				t.Fatalf("hostValue = %s (size %d), want %s (size %d)", got.Inspect(), size, tt.want, tt.size)
			}
		})
	}
	got, _, _ := hostValue(&value.Boolean{Value: true})
	if got != TRUE {
		t.Fatal("a host's true is not the evaluator's TRUE")
	}
	got, _, _ = hostValue(&value.Nil{})
	if got != NIL {
		t.Fatal("a host's nil is not the evaluator's NIL")
	}
	in := &value.Array{Elements: []value.Object{s}}
	if got, _, _ := hostValue(in); got != in {
		t.Fatal("an array of data was copied")
	}
	in = &value.Array{Elements: []value.Object{s, &value.Boolean{Value: false}, nil}}
	got, _, _ = hostValue(in)
	arr := got.(*value.Array)
	if got == in || arr.Elements[0] != s || arr.Elements[1] != FALSE || arr.Elements[2] != NIL {
		t.Fatalf("got %s, want a copy holding FALSE and NIL", got.Inspect())
	}
	if in.Elements[1] == FALSE || in.Elements[2] != nil {
		t.Fatal("the host's array was changed")
	}
	h := value.NewHash(map[string]value.Object{"ok": &value.Boolean{Value: true}})
	got, _, _ = hostValue(h)
	if got == h || got.(*value.Hash).Pairs[(&value.String{Value: "ok"}).HashKey()].Value != TRUE {
		t.Fatalf("got %s, want a copy holding TRUE", got.Inspect())
	}
}

// Anything that is not data is refused: a function or builtin (it could
// be an effect no grant checks), a task handle, a capability value, an
// error inside data, a nil pointer, a hash whose keys lie, a cycle.
func TestHostValueRefusesNonData(t *testing.T) {
	cyclic := &value.Array{Elements: make([]value.Object, 1)}
	cyclic.Elements[0] = cyclic
	deep := value.Object(&value.Integer{Value: 1})
	for i := 0; i <= maxHostDepth; i++ {
		deep = &value.Array{Elements: []value.Object{deep}}
	}
	lying := &value.Hash{Pairs: map[value.HashKey]value.HashPair{
		(&value.String{Value: "a"}).HashKey(): {Key: &value.String{Value: "b"}, Value: NIL},
	}}
	fnKey := &value.Hash{Pairs: map[value.HashKey]value.HashPair{
		{Type: value.FUNCTION_VAL}: {Key: &value.Function{}, Value: NIL},
	}}
	for _, tt := range []struct {
		name string
		in   value.Object
		want string
	}{
		{"builtin", &value.Builtin{}, "BUILTIN"},
		{"function", &value.Function{}, "FUNCTION"},
		{"task", &value.TaskHandle{}, "TASK"},
		{"capability", &value.Capability{}, "CAPABILITY"},
		{"error in array", &value.Array{Elements: []value.Object{&value.Error{Message: "x"}}}, "ERROR"},
		{"builtin in hash", value.NewHash(map[string]value.Object{"f": &value.Builtin{}}), "BUILTIN"},
		{"nil string", (*value.String)(nil), "nil"},
		{"nil array", (*value.Array)(nil), "nil"},
		{"lying hash key", lying, "key"},
		{"function key", fnKey, "key"},
		{"cycle", cyclic, "deeper than"},
		{"too deep", deep, "deeper than"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, _, err := hostValue(tt.in); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("hostValue = %v, %v; want an error mentioning %q", got, err, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test ./arv/evaluator/ -run 'HostValue'`

Expected: a build failure, `undefined: hostValue` (and `maxHostDepth`).

- [ ] **Step 3: Implement**

Create `arv/evaluator/hostvalue.go`:

```go
package evaluator

import (
	"fmt"

	"github.com/delavalom/arvlang/arv/value"
)

// maxHostDepth bounds how deeply a host effect's result may nest. It also
// stops hostValue on a cyclic value, which a Go host can build and Arv
// cannot.
const maxHostDepth = 1000

// hostValue checks that v, a host effect's result (Phase 8.2), is Arv
// data: nil, integers, strings, booleans, and arrays and hashes of them,
// with string, integer or boolean keys. Anything else is refused: a
// builtin or function could be an effect no grant checks, a task handle
// or a capability value means nothing outside the run that made it, and
// Phase 6 can log data alone. It returns v with the host's booleans and
// nils replaced by TRUE, FALSE and NIL (== compares them by identity),
// copying an array or hash only if something in it was replaced, so the
// host's value is never changed. size is what the result costs the
// allocation budget, as allocSize counts a new builtin result, all the
// way down: its strings' bytes (hash keys too, which a host builds anew),
// 16 per array element and 48 per hash pair.
func hostValue(v value.Object) (out value.Object, size int64, err error) {
	return hostData(v, 0)
}

func hostData(v value.Object, depth int) (value.Object, int64, error) {
	if depth > maxHostDepth {
		return nil, 0, fmt.Errorf("result nested deeper than %d levels", maxHostDepth)
	}
	switch v := v.(type) {
	case nil:
		return NIL, 0, nil
	case *value.Nil:
		return NIL, 0, nil
	case *value.Boolean:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return nativeBoolToBooleanObject(v.Value), 0, nil
	case *value.Integer:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return v, 0, nil
	case *value.String:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return v, int64(len(v.Value)), nil
	case *value.Array:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return hostArray(v, depth)
	case *value.Hash:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return hostHash(v, depth)
	}
	return nil, 0, fmt.Errorf("result holds a %s, which is not data", v.Type())
}

func hostArray(a *value.Array, depth int) (value.Object, int64, error) {
	size := arrayElemSize * int64(len(a.Elements))
	var copied []value.Object
	for i, el := range a.Elements {
		n, s, err := hostData(el, depth+1)
		if err != nil {
			return nil, 0, err
		}
		size += s
		if n != el && copied == nil {
			copied = make([]value.Object, len(a.Elements))
			copy(copied, a.Elements[:i])
		}
		if copied != nil {
			copied[i] = n
		}
	}
	if copied == nil {
		return a, size, nil
	}
	return &value.Array{Elements: copied}, size, nil
}

func hostHash(h *value.Hash, depth int) (value.Object, int64, error) {
	size := hashPairSize * int64(len(h.Pairs))
	var copied map[value.HashKey]value.HashPair
	for hk, p := range h.Pairs {
		key, ks, err := hostData(p.Key, depth+1)
		if err != nil {
			return nil, 0, fmt.Errorf("hash key: %w", err)
		}
		hashable, ok := key.(value.Hashable)
		if !ok || hashable.HashKey() != hk {
			return nil, 0, fmt.Errorf("result holds a hash whose key %s does not match its hash", key.Type())
		}
		val, vs, err := hostData(p.Value, depth+1)
		if err != nil {
			return nil, 0, err
		}
		size += ks + vs
		if copied == nil && (key != p.Key || val != p.Value) {
			copied = make(map[value.HashKey]value.HashPair, len(h.Pairs))
			for k, q := range h.Pairs { // pairs not reached yet are replaced below
				copied[k] = q
			}
		}
		if copied != nil {
			copied[hk] = value.HashPair{Key: key, Value: val}
		}
	}
	if copied == nil {
		return h, size, nil
	}
	return &value.Hash{Pairs: copied}, size, nil
}
```

- [ ] **Step 4: Run them and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test -race ./arv/evaluator/ -run 'HostValue'`

Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... \
  && git add arv/evaluator/hostvalue.go arv/evaluator/hostvalue_test.go \
  && git commit -F - <<'EOF'
feat(evaluator): host effect results must be Arv data

Phase 8.2 (p82.3): hostValue refuses builtins, functions, task handles,
capability values, nested errors, nil pointers, lying hash keys and
cycles; maps host booleans and nils to the evaluator's own, copying only
what changes; and sizes the result for alloc_mb.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
