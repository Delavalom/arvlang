# Phase 4, task p4.6: `match` in the lexer, AST and parser

> **For agentic workers:** this file is one task, p4.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.5 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.6: `match` in the lexer, AST and parser

`match <subject> { ok(v) => <expr>, err(e) => <expr> }`.
- New tokens `MATCH` (keyword) and `ARROW`. The lexer forms `=>` from `=` followed by `>`; `==>` still lexes as `==`, `>`.
- `ok` and `err` are pattern names inside `match`, not reserved words: `let ok = 1` still works.
- Both arms are required, in either order, with an optional trailing comma. A missing arm, a repeated arm or anything else in arm position is a parse error with a position.

The fuzz seeds get `match` and arrow cases. The evaluator comes in p4.7.

**Files:**
- Create: `arv/ast/match.go`
- Modify: `arv/ast/position.go`
- Modify: `arv/lexer/lexer.go`
- Create: `arv/parser/match.go`
- Modify: `arv/parser/parser.go`
- Modify: `arv/token/token.go`
- Modify (test): `arv/lexer/fuzz_test.go`
- Create (test): `arv/lexer/match_test.go`
- Modify (test): `arv/parser/fuzz_test.go`
- Create (test): `arv/parser/match_test.go`

- [ ] **Step 1: Write the failing tests**

In `arv/lexer/fuzz_test.go`, replace:

```go
	"{\"k\": [1, {}], 2: true}[\"k\"]",
	"\r\n\r\n\tlet\r\ny",
}
```

with:

```go
	"{\"k\": [1, {}], 2: true}[\"k\"]",
	"\r\n\r\n\tlet\r\ny",
	"match await t { ok(v) => v, err(e) => e[\"kind\"] }",
	"=> = > ==> =>= === =",
}
```

Create `arv/lexer/match_test.go`:

```go
package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

// match is a keyword; => is one token, formed from = followed by >; ok and
// err are plain identifiers. = > with a space between stays two tokens.
func TestMatchTokens(t *testing.T) {
	input := "match x { ok(v) => v, err(e) => 0 } a == b = > c=>d"
	want := []struct {
		typ token.TokenType
		lit string
		col int
	}{
		{token.MATCH, "match", 1}, {token.IDENT, "x", 7}, {token.LBRACE, "{", 9},
		{token.IDENT, "ok", 11}, {token.LPAREN, "(", 13}, {token.IDENT, "v", 14}, {token.RPAREN, ")", 15},
		{token.ARROW, "=>", 17}, {token.IDENT, "v", 20}, {token.COMMA, ",", 21},
		{token.IDENT, "err", 23}, {token.LPAREN, "(", 26}, {token.IDENT, "e", 27}, {token.RPAREN, ")", 28},
		{token.ARROW, "=>", 30}, {token.INT, "0", 33}, {token.RBRACE, "}", 35},
		{token.IDENT, "a", 37}, {token.EQ, "==", 39}, {token.IDENT, "b", 42},
		{token.ASSIGN, "=", 44}, {token.GT, ">", 46},
		{token.IDENT, "c", 48}, {token.ARROW, "=>", 49}, {token.IDENT, "d", 51},
		{token.EOF, "", 52},
	}
	l := New(input)
	for i, w := range want {
		tok := l.NextToken()
		if tok.Type != w.typ || tok.Literal != w.lit || tok.Line != 1 || tok.Col != w.col {
			t.Fatalf("token %d = %s %q at %d:%d, want %s %q at 1:%d",
				i, tok.Type, tok.Literal, tok.Line, tok.Col, w.typ, w.lit, w.col)
		}
	}
}
```

In `arv/parser/fuzz_test.go`, replace:

```go
	"99999999999999999999",
	"((((((((((1))))))))))",
}
```

with:

```go
	"99999999999999999999",
	"((((((((((1))))))))))",
	"match await t { ok(v) => v, err(e) => e[\"kind\"] }",
	"match x { err(e) => 0, ok(v) => match v { ok(w) => w, err(f) => 1 }, }",
	"match x { ok(v) => v }",
	"match x { ok(v) => v, ok(w) => w }",
	"match x ok",
	"a => b = > c",
}
```

Create `arv/parser/match_test.go`:

```go
package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

func parseMatch(t *testing.T, input string) *ast.MatchExpression {
	t.Helper()
	p := New(lexer.New(input))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	stmt, ok := program.Statements[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatalf("statement is %T, want *ast.ExpressionStatement", program.Statements[0])
	}
	m, ok := stmt.Expression.(*ast.MatchExpression)
	if !ok {
		t.Fatalf("expression is %T, want *ast.MatchExpression", stmt.Expression)
	}
	return m
}

func TestParseMatch(t *testing.T) {
	want := `match await t { ok(v) => (v + 1), err(e) => (e[message]) }`
	for _, input := range []string{
		`match await t { ok(v) => v + 1, err(e) => e["message"] }`,
		`match await t { err(e) => e["message"], ok(v) => v + 1 }`,
		"match await t {\n  ok(v) => v + 1,\n  err(e) => e[\"message\"],\n}",
	} {
		m := parseMatch(t, input)
		if m.OkName.Value != "v" || m.ErrName.Value != "e" {
			t.Errorf("%q: names %s, %s", input, m.OkName, m.ErrName)
		}
		if got := m.String(); got != want {
			t.Errorf("%q printed as %q, want %q", input, got, want)
		}
		if line, col := ast.Pos(m); line != 1 || col != 1 {
			t.Errorf("%q: Pos = %d:%d, want 1:1", input, line, col)
		}
	}
	// An arm body can be any expression, a match among them; the subject
	// can be a call.
	m := parseMatch(t, `match f(1) { ok(v) => match v { ok(w) => w, err(x) => 0 }, err(e) => if (e["kind"] == "user") { 1 } else { 2 } }`)
	if _, ok := m.Ok.(*ast.MatchExpression); !ok {
		t.Errorf("ok arm is %T, want a nested match", m.Ok)
	}
}

// ok and err are names only inside match.
func TestOkAndErrAreNotReserved(t *testing.T) {
	p := New(lexer.New(`let ok = 1; let err = fn(e) { e }; err(ok)`))
	p.ParseProgram()
	checkParserErrors(t, p)
}

// Both arms are required, once each, so error handling can't be half
// written; anything else in an arm is an error at its position.
func TestMatchParseErrors(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`match x { ok(v) => v }`, "match needs an ok arm and an err arm; err is missing (at 1:1)"},
		{`match x { err(e) => 0 }`, "match needs an ok arm and an err arm; ok is missing (at 1:1)"},
		{"1;\n  match x { }", "match needs an ok arm and an err arm; ok is missing (at 2:3)"},
		{`match x { ok(v) => v, ok(w) => w, err(e) => 0 }`, "duplicate match arm ok (at 1:23)"},
		{`match x { some(v) => v, err(e) => 0 }`, "match arm must be ok(name) or err(name), got some (at 1:11)"},
		{`match x { ok(v) v, err(e) => 0 }`, "expected next token to be =>, got IDENT instead (at 1:17)"},
		{`match x { ok v => v, err(e) => 0 }`, "expected next token to be (, got IDENT instead (at 1:14)"},
		{`match x { ok(1) => v, err(e) => 0 }`, "expected next token to be IDENT, got INT instead (at 1:14)"},
		{`match x { ok(v) => v err(e) => 0 }`, "expected next token to be }, got IDENT instead (at 1:22)"},
		{`match x ok(v) => v`, "expected next token to be {, got IDENT instead (at 1:9)"},
		{`match x { ok(v) => v, err(e) => }`, "no prefix parse function for } found (at 1:33)"},
	} {
		p := New(lexer.New(tt.input))
		p.ParseProgram()
		errs := p.Errors()
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/lexer/ ./arv/parser/ -run 'Match|NotReserved|Fuzz'`

Expected: FAIL. The build fails: (the first lines; ports and times vary)

```text
arv/lexer/match_test.go:18:10: undefined: token.MATCH
arv/lexer/match_test.go:20:10: undefined: token.ARROW
arv/lexer/match_test.go:22:10: undefined: token.ARROW
arv/lexer/match_test.go:25:34: undefined: token.ARROW
```

- [ ] **Step 3: Implement**

Create `arv/ast/match.go`:

```go
package ast

import "github.com/delavalom/arvlang/arv/token"

// MatchExpression looks at a value that may be an error:
// match <subject> { ok(<name>) => <expression>, err(<name>) => <expression> }.
// It has exactly one arm of each kind. The ok arm sees the value as Ok's
// name, the err arm sees the error, as a hash, as Err's name.
type MatchExpression struct {
	Token   token.Token // the 'match' token
	Subject Expression
	OkName  *Identifier
	Ok      Expression
	ErrName *Identifier
	Err     Expression
}

func (me *MatchExpression) expressionNode()      {}
func (me *MatchExpression) TokenLiteral() string { return me.Token.Literal }
func (me *MatchExpression) String() string {
	return "match " + me.Subject.String() + " { ok(" + me.OkName.String() + ") => " + me.Ok.String() +
		", err(" + me.ErrName.String() + ") => " + me.Err.String() + " }"
}
```

In `arv/ast/position.go`, replace:

```go
		case *ForExpression:
			return x.Token.Line, x.Token.Col
		default: // a Program, nil, or an unknown node
			return 0, 0
```

with:

```go
		case *ForExpression:
			return x.Token.Line, x.Token.Col
		case *MatchExpression:
			return x.Token.Line, x.Token.Col
		default: // a Program, nil, or an unknown node
			return 0, 0
```

In `arv/lexer/lexer.go`, replace:

```go
	"for":    token.FOR,
	"in":     token.IN,
}
```

with:

```go
	"for":    token.FOR,
	"in":     token.IN,
	"match":  token.MATCH,
}
```

In `arv/lexer/lexer.go`, replace:

```go
			l.readChar()
			tok = token.Token{Type: token.EQ, Literal: string(ch) + string(l.ch)}
		} else {
			tok = newToken(token.ASSIGN, l.ch)
```

with:

```go
			l.readChar()
			tok = token.Token{Type: token.EQ, Literal: string(ch) + string(l.ch)}
		} else if l.peekChar() == '>' {
			l.readChar()
			tok = token.Token{Type: token.ARROW, Literal: "=>"}
		} else {
			tok = newToken(token.ASSIGN, l.ch)
```

Create `arv/parser/match.go`:

```go
package parser

import (
	"fmt"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/token"
)

// parseMatchExpression parses
// match <expression> { ok(<ident>) => <expression>, err(<ident>) => <expression> }
// example: match await t { ok(v) => v, err(e) => e["message"] }
// The arms may come in either order, with an optional trailing comma, but
// each must be there exactly once: a match without its err arm (or its ok
// arm) is a parse error, so error handling cannot be half written. ok and
// err are names only here, not reserved words.
func (p *Parser) parseMatchExpression() ast.Expression {
	exp := &ast.MatchExpression{Token: p.curToken}
	if !p.enter() { // like an if: one nesting level
		return nil
	}
	defer p.leave()
	p.nextToken()
	if exp.Subject = p.parseExpression(LOWEST); exp.Subject == nil {
		return nil
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	for !p.peekTokenIs(token.RBRACE) {
		if !p.parseMatchArm(exp) {
			return nil
		}
		if !p.peekTokenIs(token.COMMA) {
			break
		}
		p.nextToken()
	}
	if !p.expectPeek(token.RBRACE) {
		return nil
	}
	if exp.Ok == nil || exp.Err == nil {
		missing := "ok"
		if exp.Ok != nil {
			missing = "err"
		}
		p.errorAt(exp.Token, fmt.Sprintf("match needs an ok arm and an err arm; %s is missing", missing))
		return nil
	}
	return exp
}

// parseMatchArm parses one arm, ok(<ident>) => <expression> or
// err(<ident>) => <expression>, which starts at peekToken, into exp.
func (p *Parser) parseMatchArm(exp *ast.MatchExpression) bool {
	if !p.expectPeek(token.IDENT) {
		return false
	}
	arm := p.curToken
	switch {
	case arm.Literal != "ok" && arm.Literal != "err":
		p.errorAt(arm, fmt.Sprintf("match arm must be ok(name) or err(name), got %s", arm.Literal))
		return false
	case arm.Literal == "ok" && exp.Ok != nil, arm.Literal == "err" && exp.Err != nil:
		p.errorAt(arm, fmt.Sprintf("duplicate match arm %s", arm.Literal))
		return false
	}
	if !p.expectPeek(token.LPAREN) || !p.expectPeek(token.IDENT) {
		return false
	}
	name := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	if !p.expectPeek(token.RPAREN) || !p.expectPeek(token.ARROW) {
		return false
	}
	p.nextToken()
	body := p.parseExpression(LOWEST)
	if body == nil {
		return false
	}
	if arm.Literal == "ok" {
		exp.OkName, exp.Ok = name, body
	} else {
		exp.ErrName, exp.Err = name, body
	}
	return true
}
```

In `arv/parser/parser.go`, replace:

```go
	p.registerPrefix(token.SCOPE, p.parseScopeExpression)
	p.registerPrefix(token.FOR, p.parseForExpression)

	p.infixParseFns = make(map[token.TokenType]infixParseFn)
```

with:

```go
	p.registerPrefix(token.SCOPE, p.parseScopeExpression)
	p.registerPrefix(token.FOR, p.parseForExpression)
	p.registerPrefix(token.MATCH, p.parseMatchExpression)

	p.infixParseFns = make(map[token.TokenType]infixParseFn)
```

In `arv/token/token.go`, replace:

```go
	EQ       = "=="
	NOT_EQ   = "!="
	LT       = "<"
	GT       = ">"
```

with:

```go
	EQ       = "=="
	NOT_EQ   = "!="
	ARROW    = "=>" // a match arm: ok(v) => v
	LT       = "<"
	GT       = ">"
```

In `arv/token/token.go`, replace:

```go
	FOR      = "FOR"
	IN       = "IN"

	STRING = "STRING"
```

with:

```go
	FOR      = "FOR"
	IN       = "IN"
	MATCH    = "MATCH"

	STRING = "STRING"
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/lexer/ ./arv/parser/ -run 'Match|NotReserved|Fuzz'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/ast/match.go arv/ast/position.go arv/lexer/fuzz_test.go arv/lexer/lexer.go arv/lexer/match_test.go arv/parser/fuzz_test.go arv/parser/match.go arv/parser/match_test.go arv/parser/parser.go arv/token/token.go && git commit -F - <<'MSG'
feat(parser): match expression with ok and err arms

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
