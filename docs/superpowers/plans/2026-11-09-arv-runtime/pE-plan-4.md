# Phase 5E, task pE.4: `&&` and `||`

> **For agentic workers:** this file is one task, pE.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks pE.1 to pE.3 are committed on `phase/pE` in `/Users/delavalom/delavalom-labs/arvlang-wt/pE`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Overview, guarantees G1-G6, decisions and deviations: `pE-plan-1.md`.

### Task pE.4: `a && b` and `a || b`

The parser turns each into the `if` it means (decision 4 in `pE-plan-1.md`):

```arv
a && b      // if (a) { b } else { false }
a || b      // if (a) { true } else { b }
```

So the right side runs only when the left does not decide (G2): no call, no effect, no steps and no bytes for it, and the whole costs exactly what that `if` costs (G4). As in `if`, only `true` counts as true. `||` binds looser than `&&`, which binds looser than `==` and `!=`. The `IfExpression` sits where `a` starts, as an infix expression does, so an error raised by the node itself (a budget running out) is placed there; errors on either side keep that side's own position (G5). Its token is the operator, so `String()` prints `(a && b)`. Each `&&` and `||` nests the AST one level (through the next one's condition), so each counts toward `parser.MaxNesting`.

**Files:**
- Modify: `arv/parser/parser.go`
- Modify: `arv/parser/parsing_functions.go`
- Create: `arv/parser/logical.go`
- Modify: `arv/ast/if.go`
- Create (test): `arv/parser/logical_test.go`
- Modify (test): `arv/parser/nesting_test.go`
- Create (test): `arv/evaluator/logical_test.go`
- Modify (test): `arv/evaluator/nesting_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/parser/logical_test.go`:

```go
package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/token"
)

// && binds tighter than ||, and both bind looser than comparisons; each
// prints as the operator it was written with.
func TestParseAndOrPrecedence(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`a && b`, `(a && b)`},
		{`a || b`, `(a || b)`},
		{`a && b || c`, `((a && b) || c)`},
		{`a || b && c`, `(a || (b && c))`},
		{`a && b && c`, `((a && b) && c)`},
		{`a == b && c < d`, `((a == b) && (c < d))`},
		{`x >= 200 && x <= 299 || x == 0`, `(((x >= 200) && (x <= 299)) || (x == 0))`},
		{`!a && -b < c`, `((!a) && ((-b) < c))`},
		{`a && (b || c)`, `(a && (b || c))`},
		{`f(a && b, c || d)`, `f((a && b), (c || d))`},
		{`r.up && r.ok`, `((r[up]) && (r[ok]))`},
		{`let ok = a || b;`, `let ok = (a || b);`},
	} {
		if got := printed(t, tt.input); got != tt.want {
			t.Errorf("%q printed %q, want %q", tt.input, got, tt.want)
		}
	}
}

// a && b is if (a) { b } else { false }, and a || b is
// if (a) { true } else { b }, placed where a starts; b keeps its place.
func TestAndOrNodeShape(t *testing.T) {
	for _, tt := range []struct {
		input   string
		op      token.TokenType
		yes, no string
	}{
		{input: "\n  x &&\n y", op: token.AND, yes: "y", no: "false"},
		{input: "\n  x ||\n y", op: token.OR, yes: "true", no: "y"},
	} {
		p := New(lexer.New(tt.input))
		program := p.ParseProgram()
		checkParserErrors(t, p)
		expr := program.Statements[0].(*ast.ExpressionStatement).Expression
		ie, ok := expr.(*ast.IfExpression)
		if !ok {
			t.Fatalf("%q: got %T, want an IfExpression", tt.input, expr)
		}
		if ie.Token.Type != tt.op || ie.Condition.String() != "x" ||
			ie.Consequence.String() != tt.yes || ie.Alternative.String() != tt.no {
			t.Fatalf("%q: if (%s) { %s } else { %s } with %s, want if (x) { %s } else { %s } with %s",
				tt.input, ie.Condition, ie.Consequence, ie.Alternative, ie.Token.Type, tt.yes, tt.no, tt.op)
		}
		if line, col := ast.Pos(ie); line != 2 || col != 3 {
			t.Errorf("%q: Pos = %d:%d, want 2:3 (where x starts)", tt.input, line, col)
		}
		right := ie.Consequence
		if tt.op == token.OR {
			right = ie.Alternative
		}
		y := right.Statements[0].(*ast.ExpressionStatement).Expression
		if line, col := ast.Pos(y); line != 3 || col != 2 {
			t.Errorf("%q: y at %d:%d, want 3:2", tt.input, line, col)
		}
	}
}

func TestAndOrSyntaxErrors(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`a &&`, "no prefix parse function for EOF found (at 1:5)"},
		{`a & b`, "no prefix parse function for ILLEGAL found (at 1:3)"},
		{`a | b`, "no prefix parse function for ILLEGAL found (at 1:3)"},
		{`|| b`, "no prefix parse function for || found (at 1:1)"},
	} {
		errs := parse(tt.input)
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
}
```

In `arv/parser/nesting_test.go`, replace:

```go
	"field chain":   func(n int) string { return "a" + strings.Repeat(".b", n) },
```

with:

```go
	"field chain":   func(n int) string { return "a" + strings.Repeat(".b", n) },
	"and chain":     func(n int) string { return "a" + strings.Repeat(" && a", n) },
	"or chain":      func(n int) string { return "a" + strings.Repeat(" || a", n) },
```

Create `arv/evaluator/logical_test.go`:

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// a && b is if (a) { b } else { false }; a || b is if (a) { true } else
// { b }. As in if, only true counts as true.
func TestAndOr(t *testing.T) {
	tests := []struct{ input, want string }{
		{`true && true`, "true"}, {`true && false`, "false"},
		{`false && true`, "false"}, {`false && false`, "false"},
		{`true || false`, "true"}, {`false || true`, "true"},
		{`false || false`, "false"}, {`true || true`, "true"},
		{`let s = 250; s >= 200 && s < 300`, "true"},
		{`let s = 404; s >= 200 && s < 300`, "false"},
		{`1 < 2 || 1 > 2 && false`, "true"}, // && binds tighter than ||
		{`true && 5`, "5"},                  // the right side's value
		{`1 && true`, "false"},              // 1 is not true
		{`let n = if (false) { 1 }; n || "default"`, "default"},
		{`5 || 6`, "6"},
		{`let n = if (false) { 1 }; !false && !n`, "true"},
	}
	for _, tt := range tests {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// The right side never runs when the left decides: no call, no output,
// no effect, and no steps or bytes for it.
func TestAndOrShortCircuit(t *testing.T) {
	calls := 0
	hit := map[string]value.BuiltinFunction{"hit": func(*runtime.Task, ...value.Object) value.Object {
		calls++
		return TRUE
	}}
	for _, src := range []string{`false && hit()`, `true || hit()`, `[] && hit()`, `false && (true || hit())`} {
		if got := runLimitedEnv(t, src, hit, runtime.Limits{}); isError(got) {
			t.Fatalf("%s: %s", src, got.Inspect())
		}
	}
	if calls != 0 {
		t.Fatalf("the right side ran %d times", calls)
	}
	if got := runLimitedEnv(t, `true && hit()`, hit, runtime.Limits{}); got != TRUE || calls != 1 {
		t.Fatalf("true && hit() = %s after %d calls, want true after 1", got.Inspect(), calls)
	}
	// An effect on the right is not attempted: no denial, no output.
	got, out := runWith(t, `false && http_get("http://example.invalid/")`)
	if got != FALSE || out != "" {
		t.Fatalf("got %s, printed %q", got.Inspect(), out)
	}
	got, out = runWith(t, `true || puts("ran")`)
	if got != TRUE || out != "" {
		t.Fatalf("got %s, printed %q", got.Inspect(), out)
	}
	// However big the skipped side, it costs nothing.
	big := `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10] == [{"a": 1}, fn(x) { x }]`
	sameCost(t, `false && `+big, `false && 1`)
	sameCost(t, `true || `+big, `true || 1`)
}

// Each form costs what the if it means costs, in steps and bytes.
func TestAndOrCostWhatIfCosts(t *testing.T) {
	const pre = `let a = 250; `
	for _, c := range [][2]string{
		{`a > 1 && a < 300`, `if (a > 1) { a < 300 } else { false }`},
		{`a < 1 && a < 300`, `if (a < 1) { a < 300 } else { false }`},
		{`a > 1 || [a, a]`, `if (a > 1) { true } else { [a, a] }`},
		{`a < 1 || [a, a]`, `if (a < 1) { true } else { [a, a] }`},
	} {
		sameCost(t, pre+c[0], pre+c[1])
	}
}

// An error on either side is placed where that side's failing node is.
func TestAndOrErrorPositions(t *testing.T) {
	got, _ := runWith(t, "true &&\n  nope")
	wantErrorAt(t, got, "identifier not found: nope", "2:3")
	got, _ = runWith(t, "false ||   1 / 0")
	wantErrorAt(t, got, "division by zero", "1:12")
	got, _ = runWith(t, " nope || true")
	wantErrorAt(t, got, "identifier not found: nope", "1:2")
}
```

In `arv/evaluator/nesting_test.go`, replace:

```go
		"field chain":   {"let h = " + nest(`{"k": `, "1", "}", n) + "; h" + strings.Repeat(".k", n), "1"},
```

with:

```go
		"field chain":   {"let h = " + nest(`{"k": `, "1", "}", n) + "; h" + strings.Repeat(".k", n), "1"},
		"and chain":     {"true" + strings.Repeat(" && true", n), "true"},
		"or chain":      {"false" + strings.Repeat(" || false", n), "false"},
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/parser/; go test ./arv/evaluator/ -run 'TestAndOr|TestNesting'`
Expected: FAIL. The parser reports `no prefix parse function for && found` (and `||`); the evaluator tests stop at the same parse errors in `runWith`.

- [ ] **Step 3: Parse them into `if`**

Create `arv/parser/logical.go`:

```go
package parser

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/token"
)

// parseLogicalExpression parses a && b and a || b into the if expressions
// they mean, so nothing downstream sees a new node:
//
//	a && b   is   if (a) { b } else { false }
//	a || b   is   if (a) { true } else { b }
//
// b runs only when a does not decide. As in if, only true counts as true.
// The if sits where a starts, as an infix expression does; its token is
// the operator, so it prints as (a && b).
func (p *Parser) parseLogicalExpression(left ast.Expression) ast.Expression {
	op := p.curToken
	precedence := p.curPrecedence()
	p.nextToken()
	start := p.curToken
	right := p.parseExpression(precedence)

	at := op
	if line, col := ast.Pos(left); line != 0 {
		at.Line, at.Col = line, col
	}
	constant := &ast.Boolean{Token: token.Token{Type: token.FALSE, Literal: "false", Line: op.Line, Col: op.Col}}
	yes, no := oneExpression(start, right), oneExpression(op, constant)
	if op.Type == token.OR {
		constant.Token.Type, constant.Token.Literal, constant.Value = token.TRUE, "true", true
		yes, no = no, yes
	}
	return &ast.IfExpression{Token: at, Condition: left, Consequence: yes, Alternative: no}
}

// oneExpression is the block { e }, placed at tok.
func oneExpression(tok token.Token, e ast.Expression) *ast.BlockStatement {
	return &ast.BlockStatement{
		Token:      tok,
		Statements: []ast.Statement{&ast.ExpressionStatement{Token: tok, Expression: e}},
	}
}
```

In `arv/parser/parser.go`, replace:

```go
	LOWEST
	EQUALS      // ==
```

with:

```go
	LOWEST
	LOGOR       // ||
	LOGAND      // &&
	EQUALS      // ==
```

In `arv/parser/parser.go`, replace:

```go
	token.GT_EQ:    LESSGREATER,
```

with:

```go
	token.GT_EQ:    LESSGREATER,
	token.AND:      LOGAND,
	token.OR:       LOGOR,
```

In `arv/parser/parser.go`, replace:

```go
	p.registerInfix(token.GT_EQ, p.parseInfixExpression)
```

with:

```go
	p.registerInfix(token.GT_EQ, p.parseInfixExpression)
	p.registerInfix(token.AND, p.parseLogicalExpression)
	p.registerInfix(token.OR, p.parseLogicalExpression)
```

In `arv/parser/parser.go` (the `MaxNesting` comment), replace:

```go
// the same reason.
```

with:

```go
// the same reason. So does each && and ||: the parser makes each an if
// expression, nested in the next one's condition.
```

In `arv/parser/parsing_functions.go`, replace:

```go
// an index or a field (r.a.b).
func nests(t token.TokenType) bool {
	switch t {
	case token.LPAREN, token.LBRACKET, token.DOT:
```

with:

```go
// an index or a field (r.a.b), and && and ||, which become if expressions.
func nests(t token.TokenType) bool {
	switch t {
	case token.LPAREN, token.LBRACKET, token.DOT, token.AND, token.OR:
```

- [ ] **Step 4: Print them as written**

In `arv/ast/if.go`, replace:

```go
func (ie *IfExpression) String() string {
	out := "if"
```

with:

```go
func (ie *IfExpression) String() string {
	if s, ok := ie.logical(); ok {
		return s
	}
	out := "if"
```

In `arv/ast/if.go`, replace:

```go
	return out
}
```

with:

```go
	return out
}

// logical prints an if expression the parser made from a && b or a || b
// (its token is the operator) as (a && b) or (a || b).
func (ie *IfExpression) logical() (string, bool) {
	right := ie.Consequence
	switch ie.Token.Type {
	case token.AND:
	case token.OR:
		right = ie.Alternative
	default:
		return "", false
	}
	if right == nil || len(right.Statements) != 1 {
		return "", false
	}
	return "(" + ie.Condition.String() + " " + ie.Token.Literal + " " + right.Statements[0].String() + ")", true
}
```

No evaluator code changes: the tree is the `if` the operator means.

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/ast/ ./arv/parser/ ./arv/evaluator/`
Expected: PASS.

- [ ] **Step 6: Commit**

Run the pre-commit checks (header), then:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && git add arv/ast/if.go arv/parser/ arv/evaluator/logical_test.go arv/evaluator/nesting_test.go && git commit -m "feat(lang): && and || short-circuit, as the if they mean

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
