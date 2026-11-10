# Phase 2a: Syntax for `scope`, `spawn … with`, `await`

**Goal:** The parser accepts the structured-concurrency syntax and builds AST nodes for it. Nothing evaluates yet. Phase 2b does that.

```arv
scope {
  let t = spawn check(url) with [net("api.github.com")];
  await t
}
```

Grammar:
- `spawn <call-expression> [with <expression>]`. The operand **must** be a call; `spawn 5` is a parse error. `with` takes any expression, which must evaluate to an array of capabilities at runtime.
- `await <expression>`, parsed at prefix precedence, so `[await a, await b]` works.
- `scope <block>`. Phase 3 extends this to `scope (<name>: <expr>, ...) <block>`.

---

### Task 2.1: Tokens and keywords

**Files:**
- Modify: `arv/token/token.go`
- Modify: `arv/lexer/lexer.go` (`keywords` map)

- [ ] **Step 1: Add token types.** In `arv/token/token.go`, after `RETURN   = "RETURN"`:

```go
	SPAWN    = "SPAWN"
	WITH     = "WITH"
	AWAIT    = "AWAIT"
	SCOPE    = "SCOPE"
```

- [ ] **Step 2: Add keywords.** In `arv/lexer/lexer.go`, extend the `keywords` map:

```go
	"spawn":  token.SPAWN,
	"with":   token.WITH,
	"await":  token.AWAIT,
	"scope":  token.SCOPE,
```

- [ ] **Step 3: Build**

Run: `go build ./... && go test ./arv/lexer/`
Expected: `ok`. (These four words are now reserved. No existing test uses them as identifiers.)

---

### Task 2.2: AST nodes

**Files:**
- Create: `arv/ast/concurrency.go`

- [ ] **Step 1: Create the nodes**

```go
package ast

import "github.com/delavalom/arvlang/arv/token"

// SpawnExpression starts Call on a new task: spawn f(x) with [net("h")].
// Caps is nil when there is no with clause; the child then holds nothing.
type SpawnExpression struct {
	Token token.Token // the 'spawn' token
	Call  *CallExpression
	Caps  Expression
}

func (se *SpawnExpression) expressionNode()      {}
func (se *SpawnExpression) TokenLiteral() string { return se.Token.Literal }
func (se *SpawnExpression) String() string {
	out := "spawn " + se.Call.String()
	if se.Caps != nil {
		out += " with " + se.Caps.String()
	}
	return out
}

// AwaitExpression waits for a task and yields its result: await t.
type AwaitExpression struct {
	Token token.Token // the 'await' token
	Task  Expression
}

func (ae *AwaitExpression) expressionNode()      {}
func (ae *AwaitExpression) TokenLiteral() string { return ae.Token.Literal }
func (ae *AwaitExpression) String() string       { return "await " + ae.Task.String() }

// ScopeExpression owns every task spawned inside Body. It does not finish
// until they have all finished, and one failure cancels the rest.
type ScopeExpression struct {
	Token token.Token // the 'scope' token
	Body  *BlockStatement
}

func (sc *ScopeExpression) expressionNode()      {}
func (sc *ScopeExpression) TokenLiteral() string { return sc.Token.Literal }
func (sc *ScopeExpression) String() string       { return "scope { " + sc.Body.String() + " }" }
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: no output.

---

### Task 2.3: Parser

**Files:**
- Create: `arv/parser/concurrency.go`
- Modify: `arv/parser/parser.go` (register three prefix functions)
- Test: `arv/parser/concurrency_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

func TestParseConcurrency(t *testing.T) {
	tests := []struct{ input, want string }{
		{`spawn f(1)`, `spawn f(1)`},
		{`spawn f(x) with [net("h")]`, `spawn f(x) with [net(h)]`},
		{`await t`, `await t`},
		{`[await a, await b]`, `[await a, await b]`},
		{`scope { let t = spawn f(); await t }`, `scope { let t = spawn f();await t }`},
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

func TestSpawnRequiresACall(t *testing.T) {
	p := New(lexer.New(`spawn 5`))
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatal("spawn 5 parsed without error")
	}
}

func TestSpawnNodeShape(t *testing.T) {
	p := New(lexer.New(`spawn f(1, 2) with caps`))
	program := p.ParseProgram()
	se, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.SpawnExpression)
	if !ok {
		t.Fatalf("not a SpawnExpression: %T", program.Statements[0])
	}
	if len(se.Call.Arguments) != 2 || se.Caps.String() != "caps" {
		t.Fatalf("bad node: %s", se.String())
	}
}
```

(`StringLiteral.String()` prints without quotes, which is why `net("h")` shows up as `net(h)`.)

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./arv/parser/ -run 'Concurrency|Spawn'`
Expected: FAIL with `no prefix parse function for SPAWN found`.

- [ ] **Step 3: Implement the parse functions**

```go
package parser

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/token"
)

// parseSpawnExpression parses spawn <call> [with <expression>]
// example: spawn check(url) with [net("api.github.com")]
func (p *Parser) parseSpawnExpression() ast.Expression {
	exp := &ast.SpawnExpression{Token: p.curToken}
	p.nextToken()
	call, ok := p.parseExpression(LOWEST).(*ast.CallExpression)
	if !ok {
		p.errors = append(p.errors, "spawn expects a function call, e.g. spawn f(x)")
		return nil
	}
	exp.Call = call
	if p.peekTokenIs(token.WITH) {
		p.nextToken()
		p.nextToken()
		exp.Caps = p.parseExpression(LOWEST)
	}
	return exp
}

// parseAwaitExpression parses await <expression>
// example: await t
func (p *Parser) parseAwaitExpression() ast.Expression {
	exp := &ast.AwaitExpression{Token: p.curToken}
	p.nextToken()
	exp.Task = p.parseExpression(PREFIX)
	return exp
}

// parseScopeExpression parses scope { <statements> }
func (p *Parser) parseScopeExpression() ast.Expression {
	exp := &ast.ScopeExpression{Token: p.curToken}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	exp.Body = p.parseBlockStatement()
	return exp
}
```

Why `with` needs no special precedence: `parseExpression(LOWEST)` stops at any token without an infix function. `WITH` has none, so `f(x)` ends right before it.

- [ ] **Step 4: Register them.** In `parser.New`, after `p.registerPrefix(token.LBRACE, p.parseHashLiteral)`:

```go
	p.registerPrefix(token.SPAWN, p.parseSpawnExpression)
	p.registerPrefix(token.AWAIT, p.parseAwaitExpression)
	p.registerPrefix(token.SCOPE, p.parseScopeExpression)
```

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 6: Commit**

```bash
git add arv/token arv/lexer arv/ast arv/parser
git commit -m "feat(parser): scope, spawn ... with, await"
```
