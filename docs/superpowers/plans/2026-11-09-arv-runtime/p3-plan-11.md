# Phase 3, task p3.11: Allocation budget

> **For agentic workers:** this file is one task, p3.11. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.10 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.11: Allocation budget

`alloc_mb` counts bytes of Arv values created under a scope. Values are charged where they are created, with rough Go sizes:
- **Array** literals, `for` results and `await [...]` results: 16 bytes per element.
- **Hash** literals: 48 bytes per pair.
- **String** concatenation: the length of the result. String literals cost nothing, because they share the source.
- **Builtin results**, through `allocSize`: strings by length, arrays at 16 per element, hashes at 48 per pair plus the bytes of their string values (http_get's `{status, body}` is new throughout). `last` returns an existing value, so the new `value.Builtin.Shares` flag exempts it.

Nothing is refunded, so this bounds churn, not live memory (design §6). The acceptance program grows an array by `push` to 5000 elements, which allocates about 191 MiB.

**Files:**
- Create: `arv/evaluator/alloc.go`
- Create (test): `arv/evaluator/alloc_test.go`
- Modify: `arv/evaluator/builtins.go`
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/eval_functions.go`
- Modify: `arv/evaluator/evaluator.go`
- Modify: `arv/evaluator/loops.go`
- Modify: `arv/value/value.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/alloc_test.go`:

```go
package evaluator

import (
	"fmt"
	goruntime "runtime"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// push copies its array, so growing one to 5000 elements allocates
// 16 * (1 + 2 + ... + 5000) bytes, about 191 MiB.
const grow = `let grow = fn(n, acc) { if (n == 0) { acc } else { grow(n - 1, push(acc, "x")) } };`

func TestAllocLimit(t *testing.T) {
	got, _ := runWith(t, grow+"\nscope (alloc_mb: 1) { len(grow(5000, [])) }")
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1048576 bytes)")
	if e := got.(*value.Error); e.Message != "budget exceeded: alloc (limit 1048576 bytes)" || e.Pos == "" {
		t.Fatalf("got %q at %q, want the bare budget error with a position", e.Message, e.Pos)
	}
	got, _ = runWith(t, grow+"scope (alloc_mb: 1024) { len(grow(5000, [])) }")
	if got.Inspect() != "5000" {
		t.Fatalf("under alloc_mb: 1024: got %s", got.Inspect())
	}
}

// What each construct costs, exactly: the program fits in that many bytes
// and fails with one byte less.
func TestAllocSizes(t *testing.T) {
	tests := []struct {
		input string
		bytes int64
	}{
		{`[1, 2, 3]`, 48},
		{`{"a": 1, "b": "long string literal"}`, 96},
		{`"ab" + "cde"`, 5},
		{`"a" + "b" + "c"`, 5}, // "ab", then "abc"
		{`for x in [1, 2] { x }`, 64},
		{`let f = fn() { 1 }; await [spawn f(), spawn f()]`, 64},
		{`push([1], 2)`, 48},
		{`let a = [[1, 2, 3, 4]]; last(a)`, 80}, // last shares, so costs nothing
		{`"literal"`, 0},
		{`len("abc")`, 0},
	}
	for _, tt := range tests {
		if tt.bytes == 0 {
			if got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: 1}), tt.input, nil); value.Object(got).Type() == value.ERROR_VAL {
				t.Errorf("%s: %s, want it to allocate nothing", tt.input, got.Inspect())
			}
			continue
		}
		if got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: tt.bytes}), tt.input, nil); isError(got) {
			t.Errorf("%s: %s under %d bytes", tt.input, got.Inspect(), tt.bytes)
		}
		got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: tt.bytes - 1}), tt.input, nil)
		if !isError(got) || got.(*value.Error).Message != fmt.Sprintf("budget exceeded: alloc (limit %d bytes)", tt.bytes-1) {
			t.Errorf("%s under %d bytes: got %s, want alloc exceeded", tt.input, tt.bytes-1, got.Inspect())
		}
	}
}

// A builtin's new record counts its strings too: a 1000-byte body costs
// its bytes plus the hash's two pairs.
func TestAllocBuiltinRecord(t *testing.T) {
	big := make([]byte, 1000)
	for i := range big {
		big[i] = 'a'
	}
	binds := map[string]value.BuiltinFunction{
		"fetch": func(*runtime.Task, ...value.Object) value.Object {
			return value.NewHash(map[string]value.Object{"status": &value.Integer{Value: 200}, "body": &value.String{Value: string(big)}})
		},
	}
	if got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: 1096}), `fetch()["status"]`, binds); got.Inspect() != "200" {
		t.Fatalf("under 1096 bytes: %s", got.Inspect())
	}
	got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: 1095}), `fetch()["status"]`, binds)
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1095 bytes)")
}

// I6 and tasks: the smaller limit wins, and a task's allocations count
// against the scope it was spawned in.
func TestNestedAllocShrinks(t *testing.T) {
	got, _ := runWith(t, grow+`scope (alloc_mb: 1) { scope (alloc_mb: 1024) { len(grow(5000, [])) } }`)
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1048576 bytes)")
	got, _ = runWith(t, grow+`scope (alloc_mb: 1) { let t = spawn grow(5000, []); len(await t) }`)
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1048576 bytes)")
}

// I4: running out of allocation fails the scope, which cancels and waits
// for its other tasks.
func TestAllocExhaustionCancelsTasks(t *testing.T) {
	before := goruntime.NumGoroutine()
	got, _ := runWith(t, grow+`scope (alloc_mb: 1) { spawn sleep(10000); len(grow(5000, [])) }`)
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1048576 bytes)")
	settled(t, before)
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test ./arv/evaluator/ -run Alloc`

Expected: every new test fails because nothing is charged, for example `got INTEGER 5000, want an error mentioning "budget exceeded: alloc (limit 1048576 bytes)"` and `[1, 2, 3] under 47 bytes: got [1, 2, 3], want alloc exceeded`.

- [ ] **Step 3: Implement**

Create `arv/evaluator/alloc.go`:

```go
package evaluator

import "github.com/delavalom/arvlang/arv/value"

// Rough Go sizes of Arv values, for the allocation budget (alloc_mb). It
// counts bytes of values created and never refunds them, so it bounds
// churn, not live memory (see 01-design.md, section 6). String literals
// cost nothing: they share the program's source.
const (
	arrayElemSize = 16 // one interface value
	hashPairSize  = 48 // key and value interfaces plus map overhead
)

// alloc charges n bytes for v, a value just created, to the innermost
// scope's allocation budget, and returns v or the budget's error.
func (in *Interpreter) alloc(v value.Object, n int64) value.Object {
	if n == 0 || len(in.scopes) == 0 {
		return v
	}
	if err := in.top().budget.Alloc(n); err != nil {
		return newError("%s", err)
	}
	return v
}

// allocSize is what a builtin's new result costs: the bytes of a string,
// 16 per array element (the elements existed before), and 48 per hash
// pair plus the bytes of its string values, since a builtin's record, like
// http_get's {status, body}, is new throughout.
func allocSize(v value.Object) int64 {
	switch v := v.(type) {
	case *value.String:
		return int64(len(v.Value))
	case *value.Array:
		return arrayElemSize * int64(len(v.Elements))
	case *value.Hash:
		n := hashPairSize * int64(len(v.Pairs))
		for _, p := range v.Pairs {
			if s, ok := p.Value.(*value.String); ok {
				n += int64(len(s.Value))
			}
		}
		return n
	}
	return 0
}
```

In `arv/evaluator/builtins.go`, replace

```go
		},
	},
	"last": {
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			if len(args) != 1 {
				return newError("wrong number of arguments. got=%d, want=1", len(args))
```

with

```go
		},
	},
	"last": {
		Shares: true,
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			if len(args) != 1 {
				return newError("wrong number of arguments. got=%d, want=1", len(args))
```


In `arv/evaluator/concurrency.go`, replace

```go
		}
		results[i] = r
	}
	return &value.Array{Elements: results}
}

// checkAwait enforces the structured rule: a task may await only handles
```

with

```go
		}
		results[i] = r
	}
	return in.alloc(&value.Array{Elements: results}, arrayElemSize*int64(len(results)))
}

// checkAwait enforces the structured rule: a task may await only handles
```


In `arv/evaluator/eval_functions.go`, replace

```go
		hashed := hashKey.HashKey()
		pairs[hashed] = value.HashPair{Key: key, Value: val}
	}
	return &value.Hash{Pairs: pairs}
}

// evalHashIndexExpression evaluates a hash index expression value from the value system
```

with

```go
		hashed := hashKey.HashKey()
		pairs[hashed] = value.HashPair{Key: key, Value: val}
	}
	return in.alloc(&value.Hash{Pairs: pairs}, hashPairSize*int64(len(pairs)))
}

// evalHashIndexExpression evaluates a hash index expression value from the value system
```


In `arv/evaluator/evaluator.go`, replace

```go
		if len(elements) == 1 && isError(elements[0]) {
			return elements[0]
		}
		return &value.Array{Elements: elements}
	case *ast.IndexExpression:
		left := in.Eval(node.Left, env)
		if isError(left) {
```

with

```go
		if len(elements) == 1 && isError(elements[0]) {
			return elements[0]
		}
		return in.alloc(&value.Array{Elements: elements}, arrayElemSize*int64(len(elements)))
	case *ast.IndexExpression:
		left := in.Eval(node.Left, env)
		if isError(left) {
```

In `arv/evaluator/evaluator.go`, replace

```go
			return right
		}
		left = evalInfixExpression(spine[i].Operator, left, right)
	}
	return left
}
```

with

```go
			return right
		}
		left = evalInfixExpression(spine[i].Operator, left, right)
		if s, ok := left.(*value.String); ok { // a concatenation: new bytes
			left = in.alloc(s, int64(len(s.Value)))
		}
	}
	return left
}
```

In `arv/evaluator/evaluator.go`, replace

```go
		evaluated := in.Eval(fn.Body, extendedEnv)
		return unwrapReturnValue(evaluated)
	case *value.Builtin:
		return fn.Fn(in.Task, args...)
	default:
		return newError("not a function: %s", fn.Type())
	}
```

with

```go
		evaluated := in.Eval(fn.Body, extendedEnv)
		return unwrapReturnValue(evaluated)
	case *value.Builtin:
		result := fn.Fn(in.Task, args...)
		if fn.Shares || isError(result) {
			return result
		}
		return in.alloc(result, allocSize(result))
	default:
		return newError("not a function: %s", fn.Type())
	}
```


In `arv/evaluator/loops.go`, replace

```go
		}
		out = append(out, v)
	}
	return &value.Array{Elements: out}
}
```

with

```go
		}
		out = append(out, v)
	}
	return in.alloc(&value.Array{Elements: out}, arrayElemSize*int64(len(out)))
}
```


In `arv/value/value.go`, replace

```go
type BuiltinFunction func(t *runtime.Task, args ...Object) Object
type Builtin struct {
	Fn BuiltinFunction
}

func (b *Builtin) Type() ObjectType { return BUILTIN_VAL }
```

with

```go
type BuiltinFunction func(t *runtime.Task, args ...Object) Object
type Builtin struct {
	Fn BuiltinFunction
	// Shares is set when Fn may return an existing value from its
	// arguments (as last does) instead of a new one, so its result is not
	// charged to the allocation budget.
	Shares bool
}

func (b *Builtin) Type() ObjectType { return BUILTIN_VAL }
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/evaluator/ -run Alloc`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/alloc.go arv/evaluator/alloc_test.go arv/evaluator/builtins.go arv/evaluator/concurrency.go arv/evaluator/eval_functions.go arv/evaluator/evaluator.go arv/evaluator/loops.go arv/value/value.go && git commit -F - <<'MSG'
feat(evaluator): alloc_mb budget

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
