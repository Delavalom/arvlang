# Phase 3, task p3.2: Runtime errors carry their position

> **For agentic workers:** this file is one task, p3.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p3.1 is committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.2: Runtime errors carry their position

An Arv error value gets `Pos` ("line:col") from the innermost node it arose in or passed through. `Interpreter.Eval` stamps it on the way out, so every error that passes through any node gets one. Infix, call and index expressions are positioned at their leftmost operand (`ast.Pos`), so `a + "s"` points at `a` and `f(x)` at `f`.

Rules this task pins down:
- **Copy, never mutate.** A task's result error is read by every task that awaits it, so `at` stamps a copy.
- **Positions survive scopes.** `runtime.Scope` stores Go errors. A small `failure` wrapper carries the `*value.Error` through it and back, so a child's error keeps its position through `await` and through every scope it fails. Phase 4 needs the same wrapper to keep the error `Kind` (see 16-notes).
- **`spawn len(1)` runs no Arv node,** so the spawn closure stamps the spawn's position.
- **Hosts get `*arv.Error{Msg, Pos}`.** `Error()` is the message alone, so every existing `err.Error() == "..."` check still holds. The CLI prints `error: <msg> (at <file>:<line>:<col>)`.

**Files:**
- Modify: `arv/arv.go`
- Modify (test): `arv/arv_test.go`
- Create: `arv/ast/position.go`
- Create (test): `arv/ast/position_test.go`
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/evaluator.go`
- Create (test): `arv/evaluator/position_test.go`
- Modify: `arv/value/value.go`
- Modify: `main.go`
- Modify (test): `main_test.go`

- [ ] **Step 1: Write the failing tests**

In `arv/arv_test.go`, replace

```go

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
```

with

```go

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
```

At the end of `arv/arv_test.go`, after

```go
		t.Errorf("empty block: err = %v, out = %q; want nil twice", err, out.String())
	}
}
```

add:

```go
// An Arv error comes back as *Error: the message alone, plus where it arose.
func TestRunErrorHasPosition(t *testing.T) {
	_, err := Run("let x = 1;\n\n  getenv(\"HOME\")", Options{})
	var ae *Error
	if !errors.As(err, &ae) {
		t.Fatalf("err = %#v, want *arv.Error", err)
	}
	if ae.Pos != "3:3" || !strings.HasPrefix(ae.Msg, "capability denied") || err.Error() != ae.Msg {
		t.Fatalf("got %q at %q, want capability denied at 3:3", ae.Msg, ae.Pos)
	}
}
```


Create `arv/ast/position_test.go`:

```go
package ast

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

func at(line, col int) token.Token { return token.Token{Line: line, Col: col} }

func TestPosUsesLeftmostOperand(t *testing.T) {
	a := &Identifier{Token: at(2, 3), Value: "a"}
	sum := &InfixExpression{Token: at(2, 5), Operator: "+", Left: a, Right: &StringLiteral{Token: at(2, 7)}}
	call := &CallExpression{Token: at(4, 2), Function: &Identifier{Token: at(4, 1)}}
	index := &IndexExpression{Token: at(5, 4), Left: call, Index: &IntegerLiteral{Token: at(5, 5)}}
	for _, tt := range []struct {
		n         Node
		line, col int
	}{
		{a, 2, 3}, {sum, 2, 3}, {call, 4, 1}, {index, 4, 1},
		{&ScopeExpression{Token: at(7, 9)}, 7, 9},
		{&Program{}, 0, 0}, {nil, 0, 0}, {&Identifier{}, 0, 0},
	} {
		if line, col := Pos(tt.n); line != tt.line || col != tt.col {
			t.Errorf("Pos(%T) = %d:%d, want %d:%d", tt.n, line, col, tt.line, tt.col)
		}
	}
}

// A chain as long as the parser allows is walked without recursion.
func TestPosLongInfixChain(t *testing.T) {
	var e Expression = &IntegerLiteral{Token: at(1, 1)}
	for i := 0; i < 1_000_000; i++ {
		e = &InfixExpression{Token: at(1, 3), Operator: "+", Left: e, Right: &IntegerLiteral{Token: at(1, 5)}}
	}
	if line, col := Pos(e); line != 1 || col != 1 {
		t.Fatalf("Pos = %d:%d, want 1:1", line, col)
	}
}
```

Create `arv/evaluator/position_test.go`:

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

// wantErrorAt fails unless got is an error whose message is msg and whose
// position is pos ("line:col").
func wantErrorAt(t *testing.T, got value.Object, msg, pos string) {
	t.Helper()
	e, ok := got.(*value.Error)
	if !ok {
		t.Fatalf("got %s, want error %q at %s", got.Inspect(), msg, pos)
	}
	if e.Message != msg || e.Pos != pos {
		t.Fatalf("got error %q at %q, want %q at %q", e.Message, e.Pos, msg, pos)
	}
}

// Errors carry the line:col of the innermost node they arose in.
func TestErrorPositions(t *testing.T) {
	tests := []struct{ name, input, msg, pos string }{
		{"identifier", "let x = 1;\nlet y = x + zz;", "identifier not found: zz", "2:13"},
		{"infix points at its left operand", "let a = 1;\n  a + \"s\"", "type mismatch: INTEGER + STRING", "2:3"},
		{"builtin call inside a function", "let f = fn() {\n  1;\n    len(1)\n};\nf()",
			"argument to `len` not supported, got INTEGER", "3:5"},
		{"division", "\n\n   10 / 0", "division by zero", "3:4"},
		{"call of a non-function", "let n = 5;\n n(1)", "not a function: INTEGER", "2:2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := runWith(t, tt.input)
			wantErrorAt(t, got, tt.msg, tt.pos)
		})
	}
}

// A task's error keeps its position through await and through the scopes
// it fails; a builtin spawned directly gets the spawn's position.
func TestTaskErrorPositions(t *testing.T) {
	got, _ := runWith(t, "let f = fn() {\n    len(1) };\nscope { spawn f() }")
	wantErrorAt(t, got, "argument to `len` not supported, got INTEGER", "2:5")

	got, _ = runWith(t, "let f = fn() {\n    len(1) };\nlet t = spawn f();\nawait t")
	wantErrorAt(t, got, "argument to `len` not supported, got INTEGER", "2:5")

	got, _ = runWith(t, "1;\nlet t = spawn len(1);\nawait t")
	wantErrorAt(t, got, "argument to `len` not supported, got INTEGER", "2:9")
}
```

At the end of `main_test.go`, after

```go
		t.Fatalf("fan-out took %s; requests were not concurrent", elapsed)
	}
}
```

add:

```go
// A runtime error names the script file and the line:col it arose at.
func TestRunCLIErrorPosition(t *testing.T) {
	script := writeScript(t, "// greet\nlet x = 1;\n  puts(getenv(\"ARV_GREETING\"));")
	var out, errOut bytes.Buffer
	if code := runCLI([]string{"run", script}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	want := fmt.Sprintf("(at %s:3:8)\n", script)
	if !strings.HasPrefix(errOut.String(), "error: capability denied") || !strings.HasSuffix(errOut.String(), want) {
		t.Fatalf("stderr = %q, want error: capability denied ... %s", errOut.String(), want)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test ./arv/... .`

Expected: build failures `undefined: Pos` (ast), `e.Pos undefined (type *value.Error has no field or method Pos)` (evaluator), `undefined: Error` (arv), and in the root package `TestRunCLIErrorPosition` fails because stderr has no `(at …/main.arv:3:8)`.

- [ ] **Step 3: Implement**

In `arv/arv.go`, replace

```go
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(caps...), opts.Out)
	result = evaluator.New(root).Run(program, value.NewEnvironment())
	if e, ok := result.(*value.Error); ok {
		return result, errors.New(e.Message)
	}
	return result, nil
}
```

with

```go
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(caps...), opts.Out)
	result = evaluator.New(root).Run(program, value.NewEnvironment())
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos}
	}
	return result, nil
}

// Error is an Arv error that reached the top of a run. Error() is the
// message alone; Pos ("line:col", or "" if unknown) says where in the
// source it arose, for the host to report with the file name.
type Error struct {
	Msg string
	Pos string
}

func (e *Error) Error() string { return e.Msg }
```


Create `arv/ast/position.go`:

```go
package ast

// Pos returns where n starts in the source (line and column from 1), or
// 0, 0 for a node with no token, such as a Program. An infix, call or index
// expression starts where its leftmost operand does, so a + "s" and f(x)
// point at a and f, not at the operator or the parenthesis. Long infix
// chains are walked without recursion.
func Pos(n Node) (line, col int) {
	for {
		switch x := n.(type) {
		case *InfixExpression:
			n = x.Left
		case *CallExpression:
			n = x.Function
		case *IndexExpression:
			n = x.Left
		case *ExpressionStatement:
			return x.Token.Line, x.Token.Col
		case *LetStatement:
			return x.Token.Line, x.Token.Col
		case *ReturnStatement:
			return x.Token.Line, x.Token.Col
		case *BlockStatement:
			return x.Token.Line, x.Token.Col
		case *Identifier:
			return x.Token.Line, x.Token.Col
		case *IntegerLiteral:
			return x.Token.Line, x.Token.Col
		case *StringLiteral:
			return x.Token.Line, x.Token.Col
		case *Boolean:
			return x.Token.Line, x.Token.Col
		case *PrefixExpression:
			return x.Token.Line, x.Token.Col
		case *IfExpression:
			return x.Token.Line, x.Token.Col
		case *FunctionLiteral:
			return x.Token.Line, x.Token.Col
		case *ArrayLiteral:
			return x.Token.Line, x.Token.Col
		case *HashLiteral:
			return x.Token.Line, x.Token.Col
		case *ScopeExpression:
			return x.Token.Line, x.Token.Col
		case *SpawnExpression:
			return x.Token.Line, x.Token.Col
		case *AwaitExpression:
			return x.Token.Line, x.Token.Col
		default: // a Program, nil, or an unknown node
			return 0, 0
		}
	}
}
```

In `arv/evaluator/concurrency.go`, replace

```go
	result = body()
	var bodyErr error
	if e, ok := result.(*value.Error); ok {
		bodyErr = errors.New(e.Message)
		s.Fail(bodyErr)
	}
	switch err := s.Wait(); {
```

with

```go
	result = body()
	var bodyErr error
	if e, ok := result.(*value.Error); ok {
		bodyErr = &failure{e}
		s.Fail(bodyErr)
	}
	switch err := s.Wait(); {
```

In `arv/evaluator/concurrency.go`, replace

```go
	case err == bodyErr:
		return result // body failed first: keep its error value as is
	default:
		return newError("%s", err)
	}
}

func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	return in.withScope(runtime.NewScope(in.Task.Ctx), func() value.Object {
		return in.evalBlockStatement(node.Body, env)
```

with

```go
	case err == bodyErr:
		return result // body failed first: keep its error value as is
	default:
		return errorValue(err)
	}
}

// failure carries an Arv error value through runtime.Scope, which stores
// Go errors, so the value (with its position) comes out the other side.
type failure struct{ v *value.Error }

func (f *failure) Error() string { return f.v.Message }

// errorValue turns a scope's failure back into an Arv error value.
func errorValue(err error) *value.Error {
	var f *failure
	if errors.As(err, &f) {
		return f.v
	}
	return newError("%s", err)
}

func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	return in.withScope(runtime.NewScope(in.Task.Ctx), func() value.Object {
		return in.evalBlockStatement(node.Body, env)
```

In `arv/evaluator/concurrency.go`, replace

```go
		defer func() { handle.Finish(result) }()
		result = runTask(child, fn, args)
		if e, ok := result.(*value.Error); ok {
			return errors.New(e.Message)
		}
		return nil
	})
```

with

```go
		defer func() { handle.Finish(result) }()
		result = runTask(child, fn, args)
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node)
				result = e
			}
			return &failure{e}
		}
		return nil
	})
```


In `arv/evaluator/evaluator.go`, replace

```go
	in.evalDepth++
	result := in.eval(node, env)
	in.evalDepth--
	return result
}

func (in *Interpreter) eval(node ast.Node, env *value.Environment) value.Object {
	if err := in.Task.Tick(); err != nil {
		return newError("task %s cancelled: %s", in.Task.ID, err)
```

with

```go
	in.evalDepth++
	result := in.eval(node, env)
	in.evalDepth--
	if e, ok := result.(*value.Error); ok && e.Pos == "" {
		return at(e, node)
	}
	return result
}

// at returns e stamped with where node starts, or e itself if node has no
// position. It copies: an error value may already be shared (a task's
// result is read by every task that awaits it), so it is never mutated.
func at(e *value.Error, node ast.Node) *value.Error {
	line, col := ast.Pos(node)
	if line == 0 {
		return e
	}
	return &value.Error{Message: e.Message, StackTrace: e.StackTrace, Pos: fmt.Sprintf("%d:%d", line, col)}
}

func (in *Interpreter) eval(node ast.Node, env *value.Environment) value.Object {
	if err := in.Task.Tick(); err != nil {
		return newError("task %s cancelled: %s", in.Task.ID, err)
```


In `arv/value/value.go`, replace

```go
func (rv *ReturnValue) Type() ObjectType { return RETURN_VALUE_VAL }
func (rv *ReturnValue) Inspect() string  { return rv.Value.Inspect() }

type Error struct {
	Message    string
	StackTrace string
}

func (e *Error) Type() ObjectType { return ERROR_VAL }
```

with

```go
func (rv *ReturnValue) Type() ObjectType { return RETURN_VALUE_VAL }
func (rv *ReturnValue) Inspect() string  { return rv.Value.Inspect() }

// Error is an Arv error value. Pos is "line:col" of the innermost node it
// arose in or passed through ("" if none); the evaluator sets it once, on
// a copy, and never changes it after.
type Error struct {
	Message    string
	StackTrace string
	Pos        string
}

func (e *Error) Type() ObjectType { return ERROR_VAL }
```


In `main.go`, replace

```go
package main

import (
	"fmt"
	"io"
	"os"
```

with

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
```

In `main.go`, replace

```go
		return 1
	}
	if _, err := arv.Run(string(src), arv.Options{Caps: caps, Out: stdout}); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
```

with

```go
		return 1
	}
	if _, err := arv.Run(string(src), arv.Options{Caps: caps, Out: stdout}); err != nil {
		var ae *arv.Error
		if errors.As(err, &ae) && ae.Pos != "" {
			fmt.Fprintf(stderr, "error: %s (at %s:%s)\n", ae.Msg, rest[0], ae.Pos)
			return 1
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/... .`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

Do not change `value.Error.Inspect()` or any message text: Phase 0–2 tests match on them.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/arv.go arv/arv_test.go arv/ast/position.go arv/ast/position_test.go arv/evaluator/concurrency.go arv/evaluator/evaluator.go arv/evaluator/position_test.go arv/value/value.go main.go main_test.go && git commit -F - <<'MSG'
feat(evaluator): errors carry the line:col they arose at

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
