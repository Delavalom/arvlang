# Phase 8.4, task p84.3: compile statements, literals, names, `if`, `let`, `return`, prefix

> **For agentic workers:** this file is one task, p84.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p84.2 is committed on `phase/p84` in `/Users/delavalom/delavalom-labs/arvlang-wt/p84`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race` on the packages it touches. Overview, guarantees S1-S6, decisions and deviations: `p84-plan-1.md`.

## Task p84.3: compile statements, literals, names, `if`, `let`, `return`, prefix

`compile` gets its own case for the program, expression statements, blocks, the three literals, identifiers, `if` (and so `&&` and `||`, which the parser makes into `if`), `let`, `return` and prefix operators. Each case does exactly what the tree-walker's case does, in the same order, with its children compiled once; a child runs through `exec`, so it is depth-checked and ticked as `Eval` would. Every other node type still runs on the tree-walker (`walked`).

Points to keep exactly as the tree-walker has them (S1, S2, S5):
- A program unwraps a `return` and stops at an error; a block returns a `ReturnValue` or an error as it is, and an empty one is `NIL`. `compileBlockRun` is a block's statements without the block's own `exec`: `evalScope` (p84.5) runs a scope body that way, unticked.
- `if` takes the consequence only when the condition is `TRUE` itself. The condition's error stops it; a `ReturnValue` condition does not (the base bug `TestReturnInConditionIsKept` records; both engines keep it).
- `let` charges `bindingSize` after its value and before it binds, and evaluates to `NIL`.
- A string literal is a new `*value.String` each time (S5): `TestStringLiteralsAreNewEachTime` fails if the compiled case returns one shared value.
- Identifiers go through `evalIdentifier`, so a spawned task's horizon (I8) and the builtins fallback are the tree-walker's own.

**Files:**
- Modify (test): `arv/evaluator/engine_nodes_test.go`
- Replace: `arv/evaluator/compile.go`

- [ ] **Step 1: Write the failing tests**

In `arv/evaluator/engine_nodes_test.go`, replace:

```go
	"github.com/delavalom/arvlang/arv/parser"
)
```

with:

```go
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/value"
)
```

In the same file, replace:

```go
	// p84.3 to p84.5 add entries here.
```

with:

```go
	{`1; 2`, "whole"}, {`1`, "stmt"}, {`1`, "expr"}, {`true`, "expr"}, {`"s"`, "expr"},
	{`return 1`, "stmt"}, {`-1`, "expr"}, {`if (true) { 1 }`, "expr"},
	{`let x = 1`, "stmt"}, {`x`, "expr"}, {`true && false`, "expr"},
	// p84.3 to p84.5 add entries here.
```

Append to the same file:

```go

// A string literal is a new value each time it is evaluated, on both
// engines: the codec numbers long strings by identity (value.Encode), so
// sharing one would change what the effect log holds.
func TestStringLiteralsAreNewEachTime(t *testing.T) {
	for _, walk := range []bool{true, false} {
		var got value.Object
		onEngine(walk, func() {
			got, _ = runWith(t, `let f = fn() { "a long string, longer than sixteen" }; [f(), f()]`)
		})
		arr, ok := got.(*value.Array)
		if !ok || len(arr.Elements) != 2 || arr.Elements[0] == arr.Elements[1] {
			t.Errorf("walk=%v: got %s, two distinct strings wanted", walk, got.Inspect())
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/evaluator/ -run 'TestNodesAreCompiled|TestStringLiteralsAreNewEachTime' -v 2>&1 | grep -E -- '--- (PASS|FAIL)|tree-walker' | head`
Expected: `TestNodesAreCompiled` FAILS, with one line per entry, starting `1; 2 (whole) runs on the tree-walker`; `TestStringLiteralsAreNewEachTime` passes (it guards S5 from now on).

- [ ] **Step 3: Compile the nodes**

Replace the whole of `arv/evaluator/compile.go` with:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// compile turns node and everything under it into code. Each case does
// what eval's case for the node type does, with its children compiled
// once here instead of dispatched on every evaluation. A node type with
// no case here runs on the tree-walker: its code calls eval's case
// itself, after the same checks (enter), so it costs the same.
func compile(node ast.Node) *code {
	c := &code{node: node}
	switch node := node.(type) {
	case *ast.Program:
		c.run = compileProgram(node)
	case *ast.ExpressionStatement:
		c.run = compileExpressionStatement(node)
	case *ast.IntegerLiteral:
		v := node.Value
		c.run = func(*Interpreter, *value.Environment) value.Object { return &value.Integer{Value: v} }
	case *ast.Boolean:
		b := nativeBoolToBooleanObject(node.Value)
		c.run = func(*Interpreter, *value.Environment) value.Object { return b }
	case *ast.StringLiteral:
		// A new value each time, as eval makes: the effect log's codec
		// numbers long strings by identity (value.Encode).
		s := node.Value
		c.run = func(*Interpreter, *value.Environment) value.Object { return &value.String{Value: s} }
	case *ast.ReturnStatement:
		c.run = compileReturn(node)
	case *ast.PrefixExpression:
		c.run = compilePrefix(node)
	case *ast.BlockStatement:
		c.run = compileBlockRun(node)
	case *ast.IfExpression:
		c.run = compileIf(node)
	case *ast.LetStatement:
		c.run = compileLet(node)
	case *ast.Identifier:
		c.run = compileIdentifier(node)
	default:
		// Any other node type (spawn, await) runs the tree-walker's case.
		return walked(node)
	}
	return c
}

// compileAll compiles each of nodes.
func compileAll[T ast.Node](nodes []T) []*code {
	cs := make([]*code, len(nodes))
	for i, n := range nodes {
		cs[i] = compile(n)
	}
	return cs
}

type runFunc = func(in *Interpreter, env *value.Environment) value.Object

func compileProgram(node *ast.Program) runFunc {
	stmts := compileAll(node.Statements)
	return func(in *Interpreter, env *value.Environment) value.Object {
		var result value.Object = NIL
		for _, s := range stmts {
			result = in.exec(s, env)
			switch r := result.(type) {
			case *value.ReturnValue:
				return r.Value
			case *value.Error:
				return r
			}
		}
		return result
	}
}

func compileExpressionStatement(node *ast.ExpressionStatement) runFunc {
	e := compile(node.Expression)
	return func(in *Interpreter, env *value.Environment) value.Object { return in.exec(e, env) }
}

// compileBlockRun is evalBlockStatement: the block's statements, without
// the block's own Eval (evalScope runs a body that way).
func compileBlockRun(node *ast.BlockStatement) runFunc {
	stmts := compileAll(node.Statements)
	return func(in *Interpreter, env *value.Environment) value.Object {
		var result value.Object = NIL
		for _, s := range stmts {
			result = in.exec(s, env)
			if rt := result.Type(); rt == value.RETURN_VALUE_VAL || rt == value.ERROR_VAL {
				return result
			}
		}
		return result
	}
}

func compileReturn(node *ast.ReturnStatement) runFunc {
	e := compile(node.ReturnValue)
	return func(in *Interpreter, env *value.Environment) value.Object {
		val := in.exec(e, env)
		if isError(val) {
			return val
		}
		return &value.ReturnValue{Value: val}
	}
}

func compilePrefix(node *ast.PrefixExpression) runFunc {
	right, op := compile(node.Right), node.Operator
	return func(in *Interpreter, env *value.Environment) value.Object {
		r := in.exec(right, env)
		if isError(r) {
			return r
		}
		return evalPrefixExpression(op, r)
	}
}

func compileIf(node *ast.IfExpression) runFunc {
	cond, yes := compile(node.Condition), compile(node.Consequence)
	var no *code
	if node.Alternative != nil {
		no = compile(node.Alternative)
	}
	return func(in *Interpreter, env *value.Environment) value.Object {
		condition := in.exec(cond, env)
		if isError(condition) {
			return condition
		}
		if condition == TRUE {
			return in.exec(yes, env)
		} else if no != nil {
			return in.exec(no, env)
		}
		return NIL
	}
}

func compileLet(node *ast.LetStatement) runFunc {
	val, name := compile(node.Value), node.Name.Value
	return func(in *Interpreter, env *value.Environment) value.Object {
		v := in.exec(val, env)
		if isError(v) {
			return v
		}
		if e := in.charge(bindingSize); e != nil {
			return e
		}
		env.Set(name, v)
		return NIL
	}
}

func compileIdentifier(node *ast.Identifier) runFunc {
	return func(in *Interpreter, env *value.Environment) value.Object {
		return evalIdentifier(node, env, in.Task)
	}
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./arv/... && go test ./arv/evaluator/ -run 'TestNodesAreCompiled|TestStringLiteralsAreNewEachTime|TestEnginesAgree' -count=1`
Expected: no gofmt output, vet clean, `ok`.

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./arv/evaluator/ ./arv/ && go test -race -tags arv_treewalk ./arv/evaluator/`
Expected: `ok` for each.

- [ ] **Step 5: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./...`
Expected: every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/evaluator && git commit -m "feat(evaluator): compile statements, literals, identifiers, if, let, return, prefix

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
