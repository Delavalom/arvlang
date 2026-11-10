# Phase 3, task p3.3: `for … in` comprehensions

> **For agentic workers:** this file is one task, p3.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.2 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.3: `for … in` comprehensions

`for x in xs { body }` evaluates `body` once per element of `xs` and yields the array of the body values, so fan-out is one expression. Each iteration gets a fresh environment enclosing the current one, which means closures capture their own `x`, and neither `x` nor a `let` in the body leaks out. An error or a `return` stops the loop and becomes its result. Only arrays are iterable. `for` and `in` become keywords; no existing test or e2e script uses either as a name.

**Files:**
- Create: `arv/ast/for.go`
- Modify: `arv/ast/position.go`
- Modify: `arv/evaluator/evaluator.go`
- Create: `arv/evaluator/loops.go`
- Create (test): `arv/evaluator/loops_test.go`
- Modify: `arv/lexer/lexer.go`
- Create: `arv/parser/loops.go`
- Create (test): `arv/parser/loops_test.go`
- Modify: `arv/parser/parser.go`
- Modify: `arv/token/token.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/loops_test.go`:

```go
package evaluator

import "testing"

func TestForComprehension(t *testing.T) {
	tests := []struct{ input, want string }{
		{`for x in [1, 2, 3] { x * 2 }`, `[2, 4, 6]`},
		{`for x in [] { x }`, `[]`},
		{`let xs = ["a", "b"]; for x in xs { x + "!" }`, `[a!, b!]`},
		{`for x in [1, 2] { let y = x; }`, `[nil, nil]`},
		{`for x in [[1, 2], [3]] { for y in x { y + 10 } }`, `[[11, 12], [13]]`},
		// each iteration has its own environment: closures keep their own x
		{`let fs = for x in [1, 2] { fn() { x } }; fs[0]() + fs[1]()`, `3`},
		// the loop variable and body lets do not leak
		{`let x = 10; for x in [1] { x }; x`, `10`},
		// return leaves the enclosing function, not just the loop
		{`let f = fn() { for x in [1, 2, 3] { if (x == 2) { return 20 } x } }; f()`, `20`},
	}
	for _, tt := range tests {
		got, _ := runWith(t, tt.input)
		if got.Inspect() != tt.want {
			t.Errorf("%s\n  = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

func TestForErrors(t *testing.T) {
	got, _ := runWith(t, "let n = 5;\n  for x in n { x }")
	wantErrorAt(t, got, "for: want ARRAY, got INTEGER", "2:3")

	got, _ = runWith(t, `for x in [1] { let y = x }; y`)
	wantErrorAt(t, got, "identifier not found: y", "1:29")

	// the first error stops the loop
	got, out := runWith(t, "for x in [1, 0, 2] {\n puts(x); 10 / x }")
	wantErrorAt(t, got, "division by zero", "2:11")
	if out != "1\n0\n" {
		t.Fatalf("output = %q, want the loop to stop at the error", out)
	}
}

// for is how fan-out is written now: one spawn per element.
func TestForSpawnsOneTaskPerElement(t *testing.T) {
	got, _ := runWith(t, `
		let double = fn(n) { n * 2 };
		scope {
			let ts = for i in [1, 2, 3] { spawn double(i) };
			for t in ts { await t }
		}`)
	if got.Inspect() != "[2, 4, 6]" {
		t.Fatalf("got %s", got.Inspect())
	}
}
```

Create `arv/parser/loops_test.go`:

```go
package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

func TestParseFor(t *testing.T) {
	tests := []struct{ input, want string }{
		{`for x in xs { x }`, `for x in xs { x }`},
		{`for u in urls { spawn check(u) with [net("h")] }`, `for u in urls { spawn check(u) with [net(h)] }`},
		{`await for u in us { spawn f(u) }`, `await for u in us { spawn f(u) }`},
		{`let ys = for x in [1, 2] { x * 2 };`, `let ys = for x in [1, 2] { (x * 2) };`},
		{`for x in f(1) { }`, `for x in f(1) {  }`},
	}
	for _, tt := range tests {
		p := New(lexer.New(tt.input))
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("%q: parse errors %v", tt.input, p.Errors())
		}
		if got := program.String(); got != tt.want {
			t.Errorf("%q: String() = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestForNodeShape(t *testing.T) {
	p := New(lexer.New("\n  for item in items { puts(item) }"))
	program := p.ParseProgram()
	fe, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.ForExpression)
	if !ok {
		t.Fatalf("not a ForExpression: %T", program.Statements[0])
	}
	if fe.Var.Value != "item" || fe.Iterable.String() != "items" || len(fe.Body.Statements) != 1 {
		t.Fatalf("bad node: %s", fe.String())
	}
	if line, col := ast.Pos(fe); line != 2 || col != 3 {
		t.Fatalf("Pos = %d:%d, want 2:3", line, col)
	}
}

func TestForSyntaxErrors(t *testing.T) {
	for _, input := range []string{
		`for in xs { x }`,
		`for x xs { x }`,
		`for x in { x }`,
		`for x in xs x`,
		`for 1 in xs { x }`,
	} {
		p := New(lexer.New(input))
		p.ParseProgram()
		if len(p.Errors()) == 0 || !strings.Contains(p.Errors()[0], "(at 1:") {
			t.Errorf("%q: errors %q, want a positioned parse error", input, p.Errors())
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test ./arv/parser/ ./arv/evaluator/`

Expected: `arv/parser` fails to build with `undefined: ast.ForExpression`, and the evaluator tests fail with parse errors such as `no prefix parse function for , found (at 1:12)`, because `for` is still an identifier.

- [ ] **Step 3: Implement**

Create `arv/ast/for.go`:

```go
package ast

import "github.com/delavalom/arvlang/arv/token"

// ForExpression is a comprehension: for x in xs { body } evaluates body
// once per element of xs, with x bound to it, and yields the array of the
// body's values.
type ForExpression struct {
	Token    token.Token // the 'for' token
	Var      *Identifier
	Iterable Expression
	Body     *BlockStatement
}

func (fe *ForExpression) expressionNode()      {}
func (fe *ForExpression) TokenLiteral() string { return fe.Token.Literal }
func (fe *ForExpression) String() string {
	return "for " + fe.Var.String() + " in " + fe.Iterable.String() + " { " + fe.Body.String() + " }"
}
```

In `arv/ast/position.go`, replace

```go
			return x.Token.Line, x.Token.Col
		case *AwaitExpression:
			return x.Token.Line, x.Token.Col
		default: // a Program, nil, or an unknown node
			return 0, 0
		}
```

with

```go
			return x.Token.Line, x.Token.Col
		case *AwaitExpression:
			return x.Token.Line, x.Token.Col
		case *ForExpression:
			return x.Token.Line, x.Token.Col
		default: // a Program, nil, or an unknown node
			return 0, 0
		}
```


In `arv/evaluator/evaluator.go`, replace

```go
		return in.evalSpawn(node, env)
	case *ast.AwaitExpression:
		return in.evalAwait(node, env)
	}
	// let statements and any node without a value evaluate to nil. Eval
	// never returns Go nil, so builtins and operators need no nil checks.
```

with

```go
		return in.evalSpawn(node, env)
	case *ast.AwaitExpression:
		return in.evalAwait(node, env)
	case *ast.ForExpression:
		return in.evalFor(node, env)
	}
	// let statements and any node without a value evaluate to nil. Eval
	// never returns Go nil, so builtins and operators need no nil checks.
```


Create `arv/evaluator/loops.go`:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// evalFor evaluates for x in xs { body }: body once per element, each time
// in a fresh environment enclosing env with x bound, so neither x nor a
// let in the body leaks out and closures capture their own x. The result
// is the array of body values. An error or a return stops the loop and is
// the result. Only arrays are iterable.
func (in *Interpreter) evalFor(node *ast.ForExpression, env *value.Environment) value.Object {
	iter := in.Eval(node.Iterable, env)
	if isError(iter) {
		return iter
	}
	arr, ok := iter.(*value.Array)
	if !ok {
		return newError("for: want ARRAY, got %s", iter.Type())
	}
	out := make([]value.Object, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		loopEnv := value.NewEnclosedEnvironment(env)
		loopEnv.Set(node.Var.Value, el)
		v := in.Eval(node.Body, loopEnv)
		if t := v.Type(); t == value.RETURN_VALUE_VAL || t == value.ERROR_VAL {
			return v
		}
		out = append(out, v)
	}
	return &value.Array{Elements: out}
}
```

In `arv/lexer/lexer.go`, replace

```go
	"with":   token.WITH,
	"await":  token.AWAIT,
	"scope":  token.SCOPE,
}

func LookupIdent(ident string) token.TokenType {
```

with

```go
	"with":   token.WITH,
	"await":  token.AWAIT,
	"scope":  token.SCOPE,
	"for":    token.FOR,
	"in":     token.IN,
}

func LookupIdent(ident string) token.TokenType {
```


Create `arv/parser/loops.go`:

```go
package parser

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/token"
)

// parseForExpression parses for <identifier> in <expression> { <statements> }
// example: for url in urls { spawn check(url) }
func (p *Parser) parseForExpression() ast.Expression {
	exp := &ast.ForExpression{Token: p.curToken}
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	exp.Var = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	if !p.expectPeek(token.IN) {
		return nil
	}
	if !p.enter() { // like an if condition: one nesting level
		return nil
	}
	p.nextToken()
	exp.Iterable = p.parseExpression(LOWEST)
	p.leave()
	if exp.Iterable == nil {
		return nil
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	exp.Body = p.parseBlockStatement()
	return exp
}
```

In `arv/parser/parser.go`, replace

```go
	p.registerPrefix(token.SPAWN, p.parseSpawnExpression)
	p.registerPrefix(token.AWAIT, p.parseAwaitExpression)
	p.registerPrefix(token.SCOPE, p.parseScopeExpression)

	p.infixParseFns = make(map[token.TokenType]infixParseFn)
	p.registerInfix(token.PLUS, p.parseInfixExpression)
```

with

```go
	p.registerPrefix(token.SPAWN, p.parseSpawnExpression)
	p.registerPrefix(token.AWAIT, p.parseAwaitExpression)
	p.registerPrefix(token.SCOPE, p.parseScopeExpression)
	p.registerPrefix(token.FOR, p.parseForExpression)

	p.infixParseFns = make(map[token.TokenType]infixParseFn)
	p.registerInfix(token.PLUS, p.parseInfixExpression)
```


In `arv/token/token.go`, replace

```go
	WITH     = "WITH"
	AWAIT    = "AWAIT"
	SCOPE    = "SCOPE"

	STRING = "STRING"
```

with

```go
	WITH     = "WITH"
	AWAIT    = "AWAIT"
	SCOPE    = "SCOPE"
	FOR      = "FOR"
	IN       = "IN"

	STRING = "STRING"
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/parser/ ./arv/evaluator/`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/ast/for.go arv/ast/position.go arv/evaluator/evaluator.go arv/evaluator/loops.go arv/evaluator/loops_test.go arv/lexer/lexer.go arv/parser/loops.go arv/parser/loops_test.go arv/parser/parser.go arv/token/token.go && git commit -F - <<'MSG'
feat: for … in comprehensions

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
