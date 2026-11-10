# Phase 3, task p3.6: Scope options: syntax

> **For agentic workers:** this file is one task, p3.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.5 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.6: Scope options: syntax

`scope (<name>: <expr>, ...) { ... }` parses into `ScopeExpression.Options` (`[]ast.ScopeOption{Token, Name, Value}`), in source order. A block starts with `{`, so the `(` is unambiguous, and `scope { {"a": 1} }` still parses as a block holding a hash. `scope () { }` is allowed and has an empty, non-nil `Options`. A trailing comma is not allowed. The option list counts as one nesting level, like a parenthesised expression. This task only parses options. p3.7 evaluates them, and the evaluator ignores them until then.

**Files:**
- Modify: `arv/ast/concurrency.go`
- Modify: `arv/parser/concurrency.go`
- Create (test): `arv/parser/scope_options_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/parser/scope_options_test.go`:

```go
package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

func TestParseScopeOptions(t *testing.T) {
	tests := []struct{ input, want string }{
		{`scope (timeout_ms: 100) { 1 }`, `scope (timeout_ms: 100) { 1 }`},
		{`scope (tasks: 2, steps: n * 1000) { f() }`, `scope (tasks: 2, steps: (n * 1000)) { f() }`},
		{`scope () { 1 }`, `scope () { 1 }`},
		{`scope { 1 }`, `scope { 1 }`},
		// a block that holds a hash literal is still a block
		{`scope { {"a": 1} }`, `scope { {a:1} }`},
		{`let s = scope (tasks: 20, timeout_ms: 5000) { await for u in us { spawn f(u) } };`,
			`let s = scope (tasks: 20, timeout_ms: 5000) { await for u in us { spawn f(u) } };`},
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

func TestScopeOptionNodes(t *testing.T) {
	p := New(lexer.New("scope (\n  tasks: 2,\n  steps: 10) { 1 }"))
	program := p.ParseProgram()
	sc := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.ScopeExpression)
	if len(sc.Options) != 2 {
		t.Fatalf("got %d options, want 2", len(sc.Options))
	}
	o := sc.Options[1]
	if o.Name != "steps" || o.Value.String() != "10" || o.Token.Line != 3 || o.Token.Col != 3 {
		t.Fatalf("option = %s: %s at %d:%d, want steps: 10 at 3:3", o.Name, o.Value, o.Token.Line, o.Token.Col)
	}
}

func TestScopeOptionSyntaxErrors(t *testing.T) {
	for _, input := range []string{
		`scope (tasks 2) { 1 }`,
		`scope (1: 2) { 1 }`,
		`scope (tasks: 2 { 1 }`,
		`scope (tasks: 2,) { 1 }`,
		`scope (tasks: ) { 1 }`,
		`scope (tasks: 2) 1`,
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

Run: `cd <worktree> && go test ./arv/parser/`

Expected: `arv/parser` fails to build with `sc.Options undefined (type *ast.ScopeExpression has no field or method Options)`.

- [ ] **Step 3: Implement**

In `arv/ast/concurrency.go`, replace

```go
package ast

import "github.com/delavalom/arvlang/arv/token"

// SpawnExpression starts Call on a new task: spawn f(x) with [net("h")].
// Caps is nil when there is no with clause; the child then holds nothing.
```

with

```go
package ast

import (
	"strings"

	"github.com/delavalom/arvlang/arv/token"
)

// SpawnExpression starts Call on a new task: spawn f(x) with [net("h")].
// Caps is nil when there is no with clause; the child then holds nothing.
```

In `arv/ast/concurrency.go`, replace

```go
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

with

```go
func (ae *AwaitExpression) String() string       { return "await " + ae.Task.String() }

// ScopeExpression owns every task spawned inside Body. It does not finish
// until they have all finished, and one failure cancels the rest. Options
// are the (name: value, ...) list after scope, in source order.
type ScopeExpression struct {
	Token   token.Token // the 'scope' token
	Options []ScopeOption
	Body    *BlockStatement
}

// ScopeOption is one name: value pair, such as timeout_ms: 2000.
type ScopeOption struct {
	Token token.Token // the name's token
	Name  string
	Value Expression
}

func (sc *ScopeExpression) expressionNode()      {}
func (sc *ScopeExpression) TokenLiteral() string { return sc.Token.Literal }
func (sc *ScopeExpression) String() string {
	out := "scope "
	if sc.Options != nil {
		opts := make([]string, len(sc.Options))
		for i, o := range sc.Options {
			opts[i] = o.Name + ": " + o.Value.String()
		}
		out += "(" + strings.Join(opts, ", ") + ") "
	}
	return out + "{ " + sc.Body.String() + " }"
}
```


In `arv/parser/concurrency.go`, replace

```go
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

with

```go
	return exp
}

// parseScopeExpression parses scope [(<name>: <expression>, ...)] { <statements> }
// example: scope (timeout_ms: 2000, tasks: 20) { spawn f() }
// A block starts with {, so the ( of an option list is unambiguous.
func (p *Parser) parseScopeExpression() ast.Expression {
	exp := &ast.ScopeExpression{Token: p.curToken}
	if p.peekTokenIs(token.LPAREN) {
		p.nextToken()
		if !p.parseScopeOptions(exp) {
			return nil
		}
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	exp.Body = p.parseBlockStatement()
	return exp
}

// parseScopeOptions parses (<name>: <expression>, ...) with curToken on
// the (. An empty list, (), is allowed. It reports whether parsing went on
// to the closing ).
func (p *Parser) parseScopeOptions(exp *ast.ScopeExpression) bool {
	if !p.enter() { // like a parenthesised expression: one nesting level
		return false
	}
	defer p.leave()
	exp.Options = []ast.ScopeOption{}
	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return true
	}
	for {
		if !p.expectPeek(token.IDENT) {
			return false
		}
		opt := ast.ScopeOption{Token: p.curToken, Name: p.curToken.Literal}
		if !p.expectPeek(token.COLON) {
			return false
		}
		p.nextToken()
		if opt.Value = p.parseExpression(LOWEST); opt.Value == nil {
			return false
		}
		exp.Options = append(exp.Options, opt)
		if !p.peekTokenIs(token.COMMA) {
			return p.expectPeek(token.RPAREN)
		}
		p.nextToken()
	}
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/parser/`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/ast/concurrency.go arv/parser/concurrency.go arv/parser/scope_options_test.go && git commit -F - <<'MSG'
feat(parser): scope (name: value, ...) options

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
