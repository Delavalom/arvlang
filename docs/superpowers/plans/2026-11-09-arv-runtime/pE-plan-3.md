# Phase 5E, task pE.3: `.field` access

> **For agentic workers:** this file is one task, pE.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks pE.1 and pE.2 are committed on `phase/pE` in `/Users/delavalom/delavalom-labs/arvlang-wt/pE`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Overview, guarantees G1-G6, decisions and deviations: `pE-plan-1.md`.

### Task pE.3: `r.f` is `r["f"]`

`.` is an infix operator with the precedence of `[`. `r.name` parses into exactly the `IndexExpression` that `r["name"]` parses into (its token is the dot, its key a `StringLiteral` placed at the name), so it has the same value, the same `nil` for a missing key, the same errors at the same places (the start of `r`), and the same steps and bytes (G3). Any identifier or keyword names a field (`r.in`, `r.match`), since JSON keys can be either. A chain of fields counts one level per link toward `parser.MaxNesting`, as a chain of indexes does; the new helper `nests` says which infix operators do.

This task also adds `fit` and `sameCost` to the evaluator tests: `fit` finds the exact steps or bytes a program is charged (the smallest limit it runs under, by bisection; the root scope charges its last partial chunk when it closes, so the count is exact), and `sameCost` checks two programs cost the same. pE.4 and pE.6 use them for G2 and G4.

**Files:**
- Modify: `arv/parser/parser.go`
- Modify: `arv/parser/parsing_functions.go`
- Create: `arv/parser/field.go`
- Create (test): `arv/parser/field_test.go`
- Modify (test): `arv/parser/nesting_test.go`
- Create (test): `arv/evaluator/fit_test.go`
- Create (test): `arv/evaluator/field_test.go`
- Modify (test): `arv/evaluator/nesting_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/parser/field_test.go`:

```go
package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// r.f parses to the same tree as r["f"], so it prints the same.
func TestParseFieldIsIndex(t *testing.T) {
	for _, tt := range [][2]string{
		{`r.up`, `r["up"]`},
		{`r.in.deep[0]`, `r["in"]["deep"][0]`},
		{`f().x + 1`, `f()["x"] + 1`},
		{`-r.n`, `-r["n"]`},
		{`r.a(1).b`, `r["a"](1)["b"]`},
		{`r.if.fn.match`, `r["if"]["fn"]["match"]`},
		{"r\n  .up", `r["up"]`},
	} {
		if got, want := printed(t, tt[0]), printed(t, tt[1]); got != want {
			t.Errorf("%q printed %q, want %q as %q does", tt[0], got, want, tt[1])
		}
	}
}

// The key is a string literal placed at the name, the node at the dot.
func TestFieldNodeShape(t *testing.T) {
	p := New(lexer.New("\n  rec.status"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	ie, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.IndexExpression)
	if !ok {
		t.Fatalf("got %T, want an IndexExpression", program.Statements[0].(*ast.ExpressionStatement).Expression)
	}
	key, ok := ie.Index.(*ast.StringLiteral)
	if !ok || key.Value != "status" || key.Token.Line != 2 || key.Token.Col != 7 {
		t.Fatalf("key = %#v, want the string status at 2:7", ie.Index)
	}
	if line, col := ast.Pos(ie); line != 2 || col != 3 {
		t.Fatalf("Pos = %d:%d, want 2:3 (where rec starts)", line, col)
	}
}

// After a dot only a name may follow; the error says where it is missing.
func TestFieldSyntaxErrors(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`r.`, "expected a field name after ., got EOF instead (at 1:3)"},
		{`r.5`, "expected a field name after ., got INT instead (at 1:3)"},
		{`r."up"`, "expected a field name after ., got STRING instead (at 1:3)"},
		{`1.5`, "expected a field name after ., got INT instead (at 1:3)"},
		{`r..up`, "expected a field name after ., got . instead (at 1:3)"},
	} {
		errs := parse(tt.input)
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
	if errs := parse(". up"); len(errs) == 0 || !strings.Contains(errs[0], "(at 1:1)") {
		t.Errorf("a leading dot: errors %q", errs)
	}
}
```

In `arv/parser/nesting_test.go`, replace:

```go
	"index chain":   func(n int) string { return "a" + strings.Repeat("[0]", n) },
```

with:

```go
	"index chain":   func(n int) string { return "a" + strings.Repeat("[0]", n) },
	"field chain":   func(n int) string { return "a" + strings.Repeat(".b", n) },
```

Create `arv/evaluator/fit_test.go`:

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// fit is the smallest limit, made by limit, under which src runs without
// an error: the exact steps or bytes src is charged, since the root scope
// charges its last partial chunk when it closes. A program that allocates
// nothing fits 1 byte (0 means no limit).
func fit(t *testing.T, src string, limit func(n int64) runtime.Limits) int64 {
	t.Helper()
	lo, hi := int64(1), int64(1<<24)
	if got := runLimited(t, src, limit(hi)); isError(got) {
		t.Fatalf("%s fails under %d: %s", src, hi, got.Inspect())
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if isError(runLimited(t, src, limit(mid))) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func stepsLimit(n int64) runtime.Limits { return runtime.Limits{Steps: n} }
func bytesLimit(n int64) runtime.Limits { return runtime.Limits{AllocBytes: n} }

// sameCost fails unless a and b are charged the same steps and the same
// bytes. Each pair says one form is exactly the other.
func sameCost(t *testing.T, a, b string) {
	t.Helper()
	if sa, sb := fit(t, a, stepsLimit), fit(t, b, stepsLimit); sa != sb {
		t.Errorf("steps: %s takes %d, %s takes %d", a, sa, b, sb)
	}
	if ba, bb := fit(t, a, bytesLimit), fit(t, b, bytesLimit); ba != bb {
		t.Errorf("bytes: %s takes %d, %s takes %d", a, ba, b, bb)
	}
}
```

Create `arv/evaluator/field_test.go`:

```go
package evaluator

import "testing"

const record = `let r = {"up": true, "status": 200, "in": {"deep": [1, 2]}};`

// r.f is r["f"]: the same value, nil for a missing key, keywords as names.
func TestFieldAccess(t *testing.T) {
	tests := []struct{ input, want string }{
		{record + `r.up`, "true"},
		{record + `r.status + 1`, "201"},
		{record + `r.missing`, "nil"},
		{record + `r.in.deep[1]`, "2"},
		{record + `r.in.nothing`, "nil"},
		{record + `r.up == r["up"]`, "true"},
		{`let f = fn() { {"x": 7} }; f().x`, "7"},
		{`{"if": 1}.if`, "1"},
	}
	for _, tt := range tests {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// Anything r["f"] refuses, r.f refuses with the same error at the same
// place: the start of r.
func TestFieldErrorsMatchIndexErrors(t *testing.T) {
	for _, c := range []struct{ field, index, msg string }{
		{"let n = 5;\n  n.up", "let n = 5;\n  n[\"up\"]", "index operator not supported: INTEGER"},
		{"let a = [1];\n  a.up", "let a = [1];\n  a[\"up\"]", "index operator not supported: ARRAY"},
		{record + "\n  r.missing.x", record + "\n  r[\"missing\"][\"x\"]", "index operator not supported: NIL"},
		{"\n  nope.x", "\n  nope[\"x\"]", "identifier not found: nope"},
	} {
		got, _ := runWith(t, c.field)
		wantErrorAt(t, got, c.msg, "2:3")
		got, _ = runWith(t, c.index)
		wantErrorAt(t, got, c.msg, "2:3")
	}
}

// r.f costs exactly what r["f"] costs, in steps and in bytes.
func TestFieldCostsWhatIndexCosts(t *testing.T) {
	sameCost(t, record+`r.up`, record+`r["up"]`)
	sameCost(t, record+`r.in.deep[0]`, record+`r["in"]["deep"][0]`)
}
```

In `arv/evaluator/nesting_test.go`, replace:

```go
		"fn bodies":     {"len([" + nest("fn() { ", "1", " }", n-2) + "])", "1"},
	} {
```

with:

```go
		"fn bodies":     {"len([" + nest("fn() { ", "1", " }", n-2) + "])", "1"},
		"field chain":   {"let h = " + nest(`{"k": `, "1", "}", n) + "; h" + strings.Repeat(".k", n), "1"},
	} {
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/parser/; go test ./arv/evaluator/ -run 'TestField|TestNesting'`
Expected: FAIL. The parser reports `no prefix parse function for . found` for every `r.up`, and the "field chain" nesting case gets thousands of errors instead of one; the evaluator tests stop at the same parse errors in `runWith`.

- [ ] **Step 3: Parse `.name`**

Create `arv/parser/field.go`:

```go
package parser

import (
	"fmt"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/token"
)

// parseFieldExpression parses r.name, which means r["name"]: it builds
// the IndexExpression r["name"] would, with the key a string literal
// placed at the name, so nothing downstream sees a new node. Any
// identifier or keyword names a field (r.in, r.match).
func (p *Parser) parseFieldExpression(left ast.Expression) ast.Expression {
	dot := p.curToken
	name := p.peekToken
	if lexer.LookupIdent(name.Literal) != name.Type {
		p.errorAt(name, fmt.Sprintf("expected a field name after ., got %s instead", name.Type))
		return nil
	}
	p.nextToken()
	key := &ast.StringLiteral{
		Token: token.Token{Type: token.STRING, Literal: name.Literal, Line: name.Line, Col: name.Col},
		Value: name.Literal,
	}
	return &ast.IndexExpression{Token: dot, Left: left, Index: key}
}
```

`lexer.LookupIdent(lit) == tok.Type` holds exactly for an identifier (`IDENT`) or a keyword token: a string `"fn"` is a `STRING` token, and an `INT`, `EOF` or `ILLEGAL` token never matches.

In `arv/parser/parser.go`, replace:

```go
	token.GT_EQ:    LESSGREATER,
```

with:

```go
	token.GT_EQ:    LESSGREATER,
	token.DOT:      INDEX,
```

In `arv/parser/parser.go`, replace:

```go
	p.registerInfix(token.GT_EQ, p.parseInfixExpression)
```

with:

```go
	p.registerInfix(token.GT_EQ, p.parseInfixExpression)
	p.registerInfix(token.DOT, p.parseFieldExpression)
```

In `arv/parser/parser.go` (the `MaxNesting` comment), replace:

```go
// and the AST (so the evaluator) one level deeper; a chain of calls or
// indexes, f()()() or a[0][0], counts once per link for the same reason.
```

with:

```go
// and the AST (so the evaluator) one level deeper; a chain of calls,
// indexes or fields, f()()(), a[0][0] or r.a.b, counts once per link for
// the same reason.
```

- [ ] **Step 4: Count a field chain as nesting**

In `arv/parser/parsing_functions.go` (in `parseExpression`), replace:

```go
		if t := p.peekToken.Type; (t == token.LPAREN || t == token.LBRACKET) && !p.enter() {
```

with:

```go
		if nests(p.peekToken.Type) && !p.enter() {
```

In `arv/parser/parsing_functions.go`, replace:

```go
	return leftExp
}

func (p *Parser) peekPrecedence() int {
```

with:

```go
	return leftExp
}

// nests reports whether the infix operator t nests the AST one level
// deeper on each use, so a chain of it counts toward MaxNesting: a call,
// an index or a field (r.a.b).
func nests(t token.TokenType) bool {
	switch t {
	case token.LPAREN, token.LBRACKET, token.DOT:
		return true
	}
	return false
}

func (p *Parser) peekPrecedence() int {
```

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/parser/ ./arv/evaluator/`
Expected: PASS. No evaluator code changed: the tree is the one `r["f"]` makes.

- [ ] **Step 6: Commit**

Run the pre-commit checks (header), then:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && git add arv/parser/ arv/evaluator/fit_test.go arv/evaluator/field_test.go arv/evaluator/nesting_test.go && git commit -m "feat(lang): r.field is r[\"field\"]

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
