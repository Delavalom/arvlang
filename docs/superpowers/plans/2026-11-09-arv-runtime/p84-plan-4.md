# Phase 8.4, task p84.4: compile infix chains, functions, calls, arrays, index, hashes

> **For agentic workers:** this file is one task, p84.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p84.3 is committed on `phase/p84` in `/Users/delavalom/delavalom-labs/arvlang-wt/p84`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race` on the packages it touches. Overview, guarantees S1-S6, decisions and deviations: `p84-plan-1.md`.

## Task p84.4: compile infix chains, functions, calls, arrays, index, hashes

The operators and data constructors get compiled cases (new file `compile_ops.go`), and functions carry their compiled body. After this task the `calls`, `loops` (but for the `for` itself), `hashes` and `ergonomic` programs run compiled, except for the nodes p84.5 adds.

Points to keep exactly as the tree-walker has them (S1, S2, S4):
- **Infix chains.** The parser makes `1 + 2 + 3 + ...` a left spine of any length, and `evalInfixChain` walks it without recursing, ticking each spine node below the first once (the first was ticked by its `Eval`). `compileInfixChain` flattens the spine once at compile time and ticks `len(spine) - 1` times before evaluating the leftmost operand, then evaluates right operands innermost first, charging a string concatenation (`concatSize`) before building it. Interpolation (`"a ${x}"`) is such a chain of `+`.
- **Functions.** `compileFunction` makes the same `*value.Function` (charged `functionSize`), plus `Code`, its compiled body. `value.Function` gets the field `Code any` (`value` cannot import `evaluator`). `applyFunction` runs `Code` when it is a `*code` and the body on the tree-walker otherwise; everything else in it (argument count, `MaxDepth`, the charge for the call's environment, `depth`) is unchanged. A function made by the tree-walker (inside a `spawn` argument, say) has no `Code`, and its calls walk, which means the same.
- **Calls, arrays.** Arguments and elements are evaluated in order, and the first error is returned as a one-element slice (`execAll`, as `evalExpressions`); a call with one argument that is an error returns that error. An array is charged `arrayElemSize` per element after its elements.
- **Index and hashes.** `evalIndexExpression` decides the result; a hash literal evaluates keys and values in source order (`node.Keys`), checks each key is hashable before its value runs, and is charged `hashPairSize` per pair after.

**Files:**
- Modify (test): `arv/evaluator/engine_nodes_test.go`
- Create: `arv/evaluator/compile_ops.go`
- Modify: `arv/evaluator/compile.go`, `arv/evaluator/evaluator.go`, `arv/value/value.go`

- [ ] **Step 1: Write the failing tests**

In `arv/evaluator/engine_nodes_test.go`, replace:

```go
	// p84.3 to p84.5 add entries here.
```

with:

```go
	{`1 + 2 + 3`, "expr"}, {`fn(x) { x }`, "expr"}, {`f(1)`, "expr"}, {`[1, 2]`, "expr"},
	{`a[0]`, "expr"}, {`r.up`, "expr"}, {`{"a": 1}`, "expr"}, {`"n=${1}"`, "expr"},
	// p84.3 to p84.5 add entries here.
```

Append to the same file:

```go

// A function made by compiled code carries its compiled body; one made by
// the tree-walker does not, and is called on the tree-walker.
func TestFunctionsCarryTheirEngine(t *testing.T) {
	for _, walk := range []bool{true, false} {
		var got value.Object
		onEngine(walk, func() { got, _ = runWith(t, `fn(x) { x }`) })
		fn, ok := got.(*value.Function)
		if !ok {
			t.Fatalf("got %s", got.Inspect())
		}
		if _, compiled := fn.Code.(*code); compiled == walk {
			t.Errorf("walk=%v: compiled body %v", walk, compiled)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/evaluator/ -run 'TestNodesAreCompiled|TestFunctionsCarryTheirEngine'`
Expected: FAIL, the build fails: `fn.Code undefined (type *value.Function has no field or method Code)`.

- [ ] **Step 3: Give functions their compiled body**

In `arv/value/value.go`, replace:

```go
type Function struct {
	Parameters []*ast.Identifier
	Body       *ast.BlockStatement
	Env        *Environment
}
```

with:

```go
type Function struct {
	Parameters []*ast.Identifier
	Body       *ast.BlockStatement
	Env        *Environment
	// Code is Body compiled (Phase 8.4), set when compiled code made the
	// function; nil when the tree-walker did. Only package evaluator
	// reads it: both mean the same, so a call runs whichever is there.
	Code any
}
```

In `arv/evaluator/evaluator.go` (in `applyFunction`), replace:

```go
		extendedEnv := extendFunctionEnv(fn, args)
		evaluated := in.Eval(fn.Body, extendedEnv)
```

with:

```go
		extendedEnv := extendFunctionEnv(fn, args)
		var evaluated value.Object
		if c, ok := fn.Code.(*code); ok {
			evaluated = in.exec(c, extendedEnv) // made by compiled code
		} else {
			evaluated = in.Eval(fn.Body, extendedEnv)
		}
```

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/evaluator/ -run 'TestNodesAreCompiled|TestFunctionsCarryTheirEngine' 2>&1 | grep -E 'runs on the tree-walker|compiled body' | head -3`
Expected: `1 + 2 + 3 (expr) runs on the tree-walker` and the other new entries, and `walk=false: compiled body false`.

- [ ] **Step 4: Compile the nodes**

Create `arv/evaluator/compile_ops.go`:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// compileInfixChain is evalInfixChain: the infix expressions down node's
// left spine, flattened once here. Each spine node below the first costs
// one tick, as there.
func compileInfixChain(node *ast.InfixExpression) runFunc {
	spine := []*ast.InfixExpression{node}
	for {
		next, ok := spine[len(spine)-1].Left.(*ast.InfixExpression)
		if !ok {
			break
		}
		spine = append(spine, next)
	}
	first := compile(spine[len(spine)-1].Left)
	// ops and rights in evaluation order: the innermost operator first.
	ops := make([]string, len(spine))
	rights := make([]*code, len(spine))
	for i := range spine {
		s := spine[len(spine)-1-i]
		ops[i], rights[i] = s.Operator, compile(s.Right)
	}
	ticks := len(spine) - 1
	return func(in *Interpreter, env *value.Environment) value.Object {
		for i := 0; i < ticks; i++ {
			if e := in.tick(); e != nil {
				return e
			}
		}
		left := in.exec(first, env)
		for i := 0; i < len(ops) && !isError(left); i++ {
			right := in.exec(rights[i], env)
			if isError(right) {
				return right
			}
			if n, ok := concatSize(ops[i], left, right); ok {
				if e := in.charge(n); e != nil {
					return e
				}
			}
			left = evalInfixExpression(ops[i], left, right)
		}
		return left
	}
}

func compileFunction(node *ast.FunctionLiteral) runFunc {
	body := compile(node.Body)
	return func(in *Interpreter, env *value.Environment) value.Object {
		fn := &value.Function{Parameters: node.Parameters, Env: env, Body: node.Body, Code: body}
		return in.alloc(fn, functionSize)
	}
}

// execAll is evalExpressions for compiled code: the values in order, or a
// one-element slice holding the first error.
func (in *Interpreter) execAll(cs []*code, env *value.Environment) []value.Object {
	var result []value.Object
	for _, c := range cs {
		v := in.exec(c, env)
		if isError(v) {
			return []value.Object{v}
		}
		result = append(result, v)
	}
	return result
}

func compileCall(node *ast.CallExpression) runFunc {
	fn, args := compile(node.Function), compileAll(node.Arguments)
	return func(in *Interpreter, env *value.Environment) value.Object {
		function := in.exec(fn, env)
		if isError(function) {
			return function
		}
		vals := in.execAll(args, env)
		if len(vals) == 1 && isError(vals[0]) {
			return vals[0]
		}
		return in.applyFunction(function, vals)
	}
}

func compileArray(node *ast.ArrayLiteral) runFunc {
	elems := compileAll(node.Elements)
	return func(in *Interpreter, env *value.Environment) value.Object {
		elements := in.execAll(elems, env)
		if len(elements) == 1 && isError(elements[0]) {
			return elements[0]
		}
		return in.alloc(&value.Array{Elements: elements}, arrayElemSize*int64(len(elements)))
	}
}

func compileIndex(node *ast.IndexExpression) runFunc {
	left, index := compile(node.Left), compile(node.Index)
	return func(in *Interpreter, env *value.Environment) value.Object {
		l := in.exec(left, env)
		if isError(l) {
			return l
		}
		i := in.exec(index, env)
		if isError(i) {
			return i
		}
		return evalIndexExpression(l, i)
	}
}

// compileHash is evalHashLiteral: keys and values in source order.
func compileHash(node *ast.HashLiteral) runFunc {
	keys := compileAll(node.Keys)
	vals := make([]*code, len(node.Keys))
	for i, k := range node.Keys {
		vals[i] = compile(node.Pairs[k])
	}
	return func(in *Interpreter, env *value.Environment) value.Object {
		h := value.NewHashSize(len(keys))
		for i, kc := range keys {
			key := in.exec(kc, env)
			if isError(key) {
				return key
			}
			hashKey, ok := key.(value.Hashable)
			if !ok {
				return newError("unusable as hash key: %s", key.Type())
			}
			val := in.exec(vals[i], env)
			if isError(val) {
				return val
			}
			h.Set(hashKey.HashKey(), value.HashPair{Key: key, Value: val})
		}
		return in.alloc(h, hashPairSize*int64(len(h.Pairs)))
	}
}
```

In `arv/evaluator/compile.go`, replace:

```go
	case *ast.Identifier:
		c.run = compileIdentifier(node)
```

with:

```go
	case *ast.Identifier:
		c.run = compileIdentifier(node)
	case *ast.InfixExpression:
		c.run = compileInfixChain(node)
	case *ast.FunctionLiteral:
		c.run = compileFunction(node)
	case *ast.CallExpression:
		c.run = compileCall(node)
	case *ast.ArrayLiteral:
		c.run = compileArray(node)
	case *ast.IndexExpression:
		c.run = compileIndex(node)
	case *ast.HashLiteral:
		c.run = compileHash(node)
```

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./arv/... && go test ./arv/evaluator/ -run 'TestNodesAreCompiled|TestFunctionsCarryTheirEngine|TestStringLiteralsAreNewEachTime|TestEnginesAgree' -count=1`
Expected: no gofmt output, vet clean, `ok`.

Check that the corpus bites: in `compile_ops.go` change `ticks := len(spine) - 1` to `ticks := len(spine) - 2` and run `go test ./arv/evaluator/ -run TestEnginesAgreeOnCorpus`. Expected: FAIL, `steps: tree-walker 33, compiled 32` for the long `1 + 2 * 3 ...` chain. Change it back.

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./arv/evaluator/ ./arv/ ./arv/value/ && go test -race -tags arv_treewalk ./arv/evaluator/`
Expected: `ok` for each.

- [ ] **Step 6: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git diff --stat && go test -race ./...`
Expected: only the five files above changed; every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/evaluator arv/value/value.go && git commit -m "feat(evaluator): compile infix chains, functions, calls, arrays, index, hashes

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
