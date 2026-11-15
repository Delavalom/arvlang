# Phase 8.4, task p84.8: shared small integers; argument slices at their exact size

> **For agentic workers:** this file is one task, p84.8. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p84.7 is committed on `phase/p84` in `/Users/delavalom/delavalom-labs/arvlang-wt/p84`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race` on the packages it touches. Overview, guarantees S1-S6, decisions and deviations: `p84-plan-1.md`.

## Task p84.8: shared small integers; argument slices at their exact size

Two smaller allocation costs from the profile, both in code the engines share:

- **Integers.** Every arithmetic result, negation and integer literal made a new `*value.Integer` (8 bytes, but one allocation each). `value.Int(n)` returns a shared value for -128 to 1023 and a new one otherwise. Nothing can tell a shared integer from a new one (S1, S5): an `Integer` is never changed after it is made; `==` on two integers compares values (`evalIntegerInfixExpression`), and on an integer and anything else is false either way; a hash key is the value; the codec never numbers integers (only arrays, hashes, errors and strings over 16 bytes are shareable in `value.Encode`); and `alloc_mb` never charged integers (`alloc.go`). Only the places this task names use `Int`; builtins and effects that make integers (`len`, `clock_ms`, `http_get`'s status) keep making new ones.
- **Argument and element slices.** `evalExpressions` and `execAll` grew their result by `append` from nil; they now make it at its exact size. An empty list is still a nil slice, as before. No code appends to an array's `Elements` in place (`push` copies), so the capacity is not visible.

**Files:**
- Create: `arv/value/smallint.go`
- Create (test): `arv/value/smallint_test.go`
- Modify: `arv/evaluator/eval_functions.go`, `arv/evaluator/evaluator.go`, `arv/evaluator/compile.go`, `arv/evaluator/compile_ops.go`

- [ ] **Step 1: Write the failing test**

Create `arv/value/smallint_test.go`:

```go
package value

import "testing"

// Int shares the integers from smallIntMin to smallIntMax and makes a new
// one for any other value; each has the value asked for.
func TestIntSharesSmallIntegers(t *testing.T) {
	for _, n := range []int64{smallIntMin, -1, 0, 1, 7, smallIntMax} {
		if a, b := Int(n), Int(n); a != b || a.Value != n {
			t.Errorf("Int(%d): %p %p (%d), want one shared value", n, a, b, a.Value)
		}
	}
	for _, n := range []int64{smallIntMin - 1, smallIntMax + 1, -1 << 63, 1<<63 - 1} {
		if a, b := Int(n), Int(n); a == b || a.Value != n || b.Value != n {
			t.Errorf("Int(%d): %p %p, want two new values", n, a, b)
		}
	}
}

// Nothing tells a shared integer from a new one: the codec writes both
// the same (it never numbers integers), and both have the same hash key.
func TestSharedIntegersAreIndistinguishable(t *testing.T) {
	shared, fresh := Int(5), &Integer{Value: 5}
	a, err := Encode(&Array{Elements: []Object{shared, shared}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(&Array{Elements: []Object{fresh, &Integer{Value: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) || string(a) != "[5,5]" {
		t.Errorf("encodings %s and %s, want [5,5] for both", a, b)
	}
	if shared.HashKey() != fresh.HashKey() {
		t.Error("hash keys differ")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/value/ -run 'TestIntSharesSmallIntegers|TestSharedIntegersAreIndistinguishable'`
Expected: FAIL, the build fails: `undefined: smallIntMin`, `undefined: smallIntMax`, `undefined: Int`.

- [ ] **Step 3: Share small integers**

Create `arv/value/smallint.go`:

```go
package value

// Small integers are shared, not allocated: an Integer is immutable, and
// nothing tells two equal integers apart (== compares their values, a
// hash key is the value, and the codec never numbers integers). Phase 8.4:
// arithmetic, comparisons' operands and loop counters are mostly small.
const (
	smallIntMin = -128
	smallIntMax = 1023
)

var smallInts = func() []Integer {
	s := make([]Integer, smallIntMax-smallIntMin+1)
	for i := range s {
		s[i].Value = int64(i + smallIntMin)
	}
	return s
}()

// Int is n as an Integer: a shared one when n is small, else a new one.
func Int(n int64) *Integer {
	if n >= smallIntMin && n <= smallIntMax {
		return &smallInts[n-smallIntMin]
	}
	return &Integer{Value: n}
}
```

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./arv/value/`
Expected: `ok`.

- [ ] **Step 4: Use them, and size the slices**

In `arv/evaluator/eval_functions.go`, replace each of these five lines (in `evalMinusPrefixOperatorExpression` and `evalIntegerInfixExpression`):

```go
	return &value.Integer{Value: -val}
		return &value.Integer{Value: leftVal + rightVal}
		return &value.Integer{Value: leftVal - rightVal}
		return &value.Integer{Value: leftVal * rightVal}
		return &value.Integer{Value: leftVal / rightVal}
```

with, respectively:

```go
	return value.Int(-val)
		return value.Int(leftVal + rightVal)
		return value.Int(leftVal - rightVal)
		return value.Int(leftVal * rightVal)
		return value.Int(leftVal / rightVal)
```

In the same file (in `evalExpressions`), replace:

```go
	var result []value.Object
	for _, e := range exps {
```

with:

```go
	var result []value.Object
	if len(exps) > 0 {
		result = make([]value.Object, 0, len(exps))
	}
	for _, e := range exps {
```

In `arv/evaluator/evaluator.go` (in `evalNode`), replace:

```go
		return &value.Integer{Value: node.Value}
```

with:

```go
		return value.Int(node.Value)
```

In `arv/evaluator/compile.go`, replace:

```go
		c.run = func(*Interpreter, *value.Environment) value.Object { return &value.Integer{Value: v} }
```

with:

```go
		c.run = func(*Interpreter, *value.Environment) value.Object { return value.Int(v) }
```

In `arv/evaluator/compile_ops.go` (in `execAll`), replace:

```go
	var result []value.Object
	for _, c := range cs {
```

with:

```go
	var result []value.Object
	if len(cs) > 0 {
		result = make([]value.Object, 0, len(cs))
	}
	for _, c := range cs {
```

- [ ] **Step 5: Run the tests**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./... && grep -c 'value.Int(' arv/evaluator/eval_functions.go arv/evaluator/evaluator.go arv/evaluator/compile.go`
Expected: no gofmt output, vet clean, and the counts `eval_functions.go:5`, `evaluator.go:1`, `compile.go:1` (in any order).

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./arv/... && go test -race -tags arv_treewalk ./arv/evaluator/ ./arv/`
Expected: `ok` for each package.

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -run '^$' -bench 'BenchmarkSpeed/calls' -benchmem -count 3 ./arv/`
Expected: fewer allocations per op than after p84.7 (in the scratch copy 142488 to 65878 allocs/op, about 6.8 ms/op).

- [ ] **Step 6: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./...`
Expected: every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/value arv/evaluator && git commit -m "perf(value,evaluator): share small integers; size argument slices exactly

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
