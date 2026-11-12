# Phase 8.4, task p84.5: compile `for`, `match` and `scope`

> **For agentic workers:** this file is one task, p84.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p84.4 is committed on `phase/p84` in `/Users/delavalom/delavalom-labs/arvlang-wt/p84`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race` on the packages it touches. Overview, guarantees S1-S6, decisions and deviations: `p84-plan-1.md`.

## Task p84.5: compile `for`, `match` and `scope`

These three do more than evaluate their children: a loop charges and binds per element and collects results, a `match` decides whether it may catch an error (settling steps, checking cancellation, building the error hash), and a scope opens a budget level, a supervisor and a deadline. That logic is split out of the tree-walker into three helpers both engines call, so it exists once (decision 3 in `p84-plan-1.md`); only the evaluation of the children differs:

- `forEach(name, iter, env, body)`: the loop of `evalFor` after the iterable is evaluated (an error iterable is returned as it is).
- `matchArm(node, env, subject)`: which arm runs and its environment, or `done`, the match's value when no arm runs.
- `openScope(node, env, body)`: `evalScope` with `body` running the scope's block. The block runs without its own `Eval`, as before, so compiled code passes `compileBlockRun`'s function, not an `exec`. Scope options still run on the tree-walker (once per scope).

The tree-walker's `evalFor`, `evalMatch` and `evalScope` become thin callers of the helpers; their behaviour does not change (S1). After this task `spawn` and `await` are the only node types left on the tree-walker.

**Files:**
- Modify (test): `arv/evaluator/engine_nodes_test.go`
- Modify: `arv/evaluator/loops.go`, `arv/evaluator/match.go`, `arv/evaluator/concurrency.go`, `arv/evaluator/compile.go`
- Create: `arv/evaluator/compile_flow.go`

- [ ] **Step 1: Write the failing test**

In `arv/evaluator/engine_nodes_test.go`, replace:

```go
	// p84.3 to p84.5 add entries here.
```

with:

```go
	{`for x in [1] { x }`, "expr"}, {`match 1 { ok(v) => v, err(e) => e }`, "expr"},
	{`scope { 1 }`, "expr"},
	// p84.3 to p84.5 add entries here.
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/evaluator/ -run TestNodesAreCompiled 2>&1 | grep 'tree-walker'`
Expected: FAIL with three lines: `for x in [1] { x } (expr) runs on the tree-walker`, the same for the `match` and the `scope`.

- [ ] **Step 3: Split out the helpers**

In `arv/evaluator/loops.go`, replace:

```go
func (in *Interpreter) evalFor(node *ast.ForExpression, env *value.Environment) value.Object {
	iter := in.Eval(node.Iterable, env)
	if isError(iter) {
```

with:

```go
func (in *Interpreter) evalFor(node *ast.ForExpression, env *value.Environment) value.Object {
	iter := in.Eval(node.Iterable, env)
	return in.forEach(node.Var.Value, iter, env, func(loopEnv *value.Environment) value.Object {
		return in.Eval(node.Body, loopEnv)
	})
}

// forEach is the loop of evalFor once the iterable is evaluated (iter):
// body runs in each element's environment, binding name. Compiled code
// (compileFor) shares it.
func (in *Interpreter) forEach(name string, iter value.Object, env *value.Environment, body func(*value.Environment) value.Object) value.Object {
	if isError(iter) {
```

In the same file, replace:

```go
		loopEnv.Set(node.Var.Value, el)
		v := in.Eval(node.Body, loopEnv)
```

with:

```go
		loopEnv.Set(name, el)
		v := body(loopEnv)
```

In `arv/evaluator/match.go`, replace the whole function `evalMatch`:

```go
func (in *Interpreter) evalMatch(node *ast.MatchExpression, env *value.Environment) value.Object {
	subject := in.Eval(node.Subject, env)
	if subject.Type() == value.RETURN_VALUE_VAL {
		return subject
	}
	name, body := node.OkName, node.Ok
	if e, ok := subject.(*value.Error); ok {
		if in.Task.Err() != nil {
			return e
		}
		if errv := in.settleSteps(); errv != nil {
			return errv
		}
		name, body = node.ErrName, node.Err
		h, errv := in.errorHash(e)
		if errv != nil {
			return errv
		}
		subject = h
	}
	if e := in.charge(envSize + bindingSize); e != nil {
		return e
	}
	armEnv := value.NewEnclosedEnvironment(env)
	armEnv.Set(name.Value, subject)
	return in.Eval(body, armEnv)
}
```

with:

```go
func (in *Interpreter) evalMatch(node *ast.MatchExpression, env *value.Environment) value.Object {
	body, armEnv, done := in.matchArm(node, env, in.Eval(node.Subject, env))
	if done != nil {
		return done
	}
	return in.Eval(body, armEnv)
}

// matchArm picks the arm that subject, the evaluated subject, runs, and
// builds that arm's environment; done is the match's value instead when
// no arm runs. Compiled code (compileMatch) shares it.
func (in *Interpreter) matchArm(node *ast.MatchExpression, env *value.Environment, subject value.Object) (body ast.Expression, armEnv *value.Environment, done value.Object) {
	if subject.Type() == value.RETURN_VALUE_VAL {
		return nil, nil, subject
	}
	name, body := node.OkName, node.Ok
	if e, ok := subject.(*value.Error); ok {
		if in.Task.Err() != nil {
			return nil, nil, e
		}
		if errv := in.settleSteps(); errv != nil {
			return nil, nil, errv
		}
		name, body = node.ErrName, node.Err
		h, errv := in.errorHash(e)
		if errv != nil {
			return nil, nil, errv
		}
		subject = h
	}
	if e := in.charge(envSize + bindingSize); e != nil {
		return nil, nil, e
	}
	armEnv = value.NewEnclosedEnvironment(env)
	armEnv.Set(name.Value, subject)
	return body, armEnv, nil
}
```

In `arv/evaluator/concurrency.go`, replace:

```go
func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	limits, policy, errv := in.scopeOptions(node.Options, env)
```

with:

```go
func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	return in.openScope(node, env, func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
}

// openScope is evalScope with body as what runs node.Body. Compiled code
// (compileScope) shares it.
func (in *Interpreter) openScope(node *ast.ScopeExpression, env *value.Environment, body func() value.Object) value.Object {
	limits, policy, errv := in.scopeOptions(node.Options, env)
```

and, at the end of the same function, replace:

```go
	return in.withScope(f, func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
}
```

with:

```go
	return in.withScope(f, body)
}
```

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go vet ./arv/evaluator/ && go test -race ./arv/evaluator/`
Expected: `ok`, except `TestNodesAreCompiled` with the same three lines as in step 2 (the tree-walker is unchanged).

- [ ] **Step 4: Compile the nodes**

Create `arv/evaluator/compile_flow.go`:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// compileFor is evalFor: the loop itself is forEach, shared.
func compileFor(node *ast.ForExpression) runFunc {
	iter, body, name := compile(node.Iterable), compile(node.Body), node.Var.Value
	return func(in *Interpreter, env *value.Environment) value.Object {
		return in.forEach(name, in.exec(iter, env), env, func(loopEnv *value.Environment) value.Object {
			return in.exec(body, loopEnv)
		})
	}
}

// compileMatch is evalMatch: the choice of arm is matchArm, shared.
func compileMatch(node *ast.MatchExpression) runFunc {
	subject, ok, err := compile(node.Subject), compile(node.Ok), compile(node.Err)
	return func(in *Interpreter, env *value.Environment) value.Object {
		body, armEnv, done := in.matchArm(node, env, in.exec(subject, env))
		if done != nil {
			return done
		}
		if body == node.Ok {
			return in.exec(ok, armEnv)
		}
		return in.exec(err, armEnv)
	}
}

// compileScope is evalScope: the scope itself is openScope, shared; its
// options run on the tree-walker (once per scope, not per node).
func compileScope(node *ast.ScopeExpression) runFunc {
	body := compileBlockRun(node.Body)
	return func(in *Interpreter, env *value.Environment) value.Object {
		return in.openScope(node, env, func() value.Object { return body(in, env) })
	}
}
```

In `arv/evaluator/compile.go`, replace:

```go
	case *ast.HashLiteral:
		c.run = compileHash(node)
```

with:

```go
	case *ast.HashLiteral:
		c.run = compileHash(node)
	case *ast.ForExpression:
		c.run = compileFor(node)
	case *ast.MatchExpression:
		c.run = compileMatch(node)
	case *ast.ScopeExpression:
		c.run = compileScope(node)
```

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./... && go test ./arv/evaluator/ -run 'TestNodesAreCompiled|TestEnginesAgree' -count=1`
Expected: no gofmt output, vet clean, `ok`.

Check that the corpus bites: in `compile_flow.go` (in `compileScope`), change `return body(in, env)` to `return in.exec(&code{node: node.Body, run: body}, env)` (the body ticked once more) and run `go test ./arv/evaluator/ -run TestEnginesAgreeOnCorpus`. Expected: FAIL, a `steps:` line, or a `limits {... Steps:7 ...}` line, for a `scope` program. Change it back.

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./arv/... && go test -race -tags arv_treewalk ./arv/...`
Expected: `ok` for each package.

- [ ] **Step 6: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./...`
Expected: every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/evaluator && git commit -m "feat(evaluator): compile for, match and scope through shared helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
