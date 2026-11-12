# Phase 5E, task pE.6: string interpolation

> **For agentic workers:** this file is one task, pE.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks pE.1 to pE.5 are committed on `phase/pE` in `/Users/delavalom/delavalom-labs/arvlang-wt/pE`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Overview, guarantees G1-G6, decisions and deviations: `pE-plan-1.md`.

### Task pE.6: `"${n} of ${total} down"`

A string literal holding `${` is parsed into the concatenation it means, from nodes that already exist (decisions 1 and 3 in `pE-plan-1.md`):

```arv
"a ${x} b"     // "a " + $str(x) + " b"
"${x}"         // $str(x)
"\${x}"        // the string ${x}: \${ is a literal ${
```

`$str` is pE.5's `str` under the name no program can bind, so every value prints as `puts` prints it, and the string costs exactly what `"a " + str(x) + " b"` costs, in steps and in bytes (G4): the trees are the same but for the name. Empty pieces are left out. A string with no `${` is the `StringLiteral` it always was, and no string in the tree's tests, e2e cases or examples holds one (G1, `pE-plan-1.md`).

**The expression** ends at the `}` that balances the `${` (so `${ {1: 2}[1] }` and `${ if (ok) { 1 } else { 2 } }` work) and cannot hold a string literal, because the lexer is unchanged and ends the string at its first quote: `"${f("x")}"` is an `unterminated ${` error. The parser lexes the expression in place with `lexer.NewAt`, swapping its own lexer and tokens for the duration, so the expression's tokens and errors carry their real `line:col` (G5), and nesting and errors count exactly as anywhere else. A string literal holding `${` counts one level toward `MaxNesting` (the `$str` call nests the AST).

**Positions (G5).** An error inside `${...}` is at its node. A failure to render a value (`alloc_mb` running out in `str`) is at the `$` of its `${`. A concatenation that runs out of `alloc_mb` is placed at its leftmost piece, as the hand-written form's infix error is at its leftmost operand: the opening quote when the string starts with text, the `$` of the first `${` when it starts with one (corrected after review round 1; see `16pe-ergonomics.md`).

**Files:**
- Create: `arv/parser/interp.go`
- Modify: `arv/parser/parsing_functions.go`
- Modify: `arv/parser/parser.go`
- Create (test): `arv/parser/interp_test.go`
- Modify (test): `arv/parser/nesting_test.go`
- Create (test): `arv/evaluator/interp_test.go`
- Modify (test): `arv/evaluator/nesting_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/parser/interp_test.go`:

```go
package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// An interpolated string is the concatenation of its pieces and a $str
// call per ${...}; a string without ${ is the literal it always was.
func TestParseInterpolation(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`"a ${x} b"`, `((a  + $str(x)) +  b)`},
		{`"${x}"`, `$str(x)`},
		{`"${a}${b}"`, `($str(a) + $str(b))`},
		{`"${r.up} ${n + 1}"`, `(($str((r[up])) +  ) + $str((n + 1)))`},
		{`"${ if (ok) { 1 } else { 2 } }"`, `$str(ifok 1else2)`},
		{`"${ {1: 2}[1] }"`, `$str(({1:2}[1]))`},
		{`"${x}" + "${y}"`, `($str(x) + $str(y))`},
		{`"no interpolation: $ { } $x {x}"`, `no interpolation: $ { } $x {x}`},
	} {
		if got := printed(t, tt.input); got != tt.want {
			t.Errorf("%q printed %q, want %q", tt.input, got, tt.want)
		}
	}
}

// \${ is a literal ${. A string whose only ${ are escaped stays one
// string literal, with the escape removed from its value.
func TestInterpolationEscape(t *testing.T) {
	for _, tt := range []struct{ input, value string }{
		{`"\${x}"`, "${x}"},
		{`"cost: $5 \${a} \\${b}"`, `cost: $5 ${a} \${b}`},
	} {
		p := New(lexer.New(tt.input))
		program := p.ParseProgram()
		checkParserErrors(t, p)
		sl, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.StringLiteral)
		if !ok || sl.Value != tt.value {
			t.Errorf("%q = %#v, want the string literal %q", tt.input, program.Statements[0], tt.value)
		}
	}
	if got := printed(t, `"\${a} ${b}"`); got != `(${a}  + $str(b))` {
		t.Errorf("printed %q", got)
	}
}

// The expression's tokens carry their real positions, across lines; the
// $str call and the piece after it sit where they start in the source.
func TestInterpolationPositions(t *testing.T) {
	p := New(lexer.New("let s =\n  \"one\n  ${ x } two\";"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	cat := program.Statements[0].(*ast.LetStatement).Value.(*ast.InfixExpression)
	inner := cat.Left.(*ast.InfixExpression)
	first := inner.Left.(*ast.StringLiteral)
	call := inner.Right.(*ast.CallExpression)
	x := call.Arguments[0].(*ast.Identifier)
	last := cat.Right.(*ast.StringLiteral)
	for _, c := range []struct {
		what      string
		node      ast.Node
		line, col int
	}{
		{"the first piece (at the quote)", first, 2, 3},
		{"the call (at the $)", call, 3, 3},
		{"x", x, 3, 6},
		{"the last piece", last, 3, 9},
	} {
		if line, col := ast.Pos(c.node); line != c.line || col != c.col {
			t.Errorf("%s at %d:%d, want %d:%d", c.what, line, col, c.line, c.col)
		}
	}
	if first.Value != "one\n  " || last.Value != " two" || x.Value != "x" {
		t.Errorf("pieces %q, %q around %s", first.Value, last.Value, x.Value)
	}
	if fn := call.Function.(*ast.Identifier); fn.Value != ast.StrBuiltin {
		t.Errorf("calls %q, want %q", fn.Value, ast.StrBuiltin)
	}
}

// Errors inside ${...} point into the string.
func TestInterpolationSyntaxErrors(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`"a ${x"`, "unterminated ${ in string (at 1:4)"},
		{`"a ${}"`, "empty ${} in string (at 1:4)"},
		{`"a ${  }"`, "empty ${} in string (at 1:4)"},
		{`"a ${x; y}"`, "expected } to end ${, got ; instead (at 1:7)"},
		{`"a ${x y}"`, "expected } to end ${, got IDENT instead (at 1:8)"},
		{`"${1 +}"`, "no prefix parse function for EOF found (at 1:7)"},
		{"\"one\n  ${ (x }\"", "expected next token to be ), got EOF instead (at 2:9)"},
		// a quote ends the string, so the ${ before it is unterminated
		{`"${f("x")}"`, "unterminated ${ in string (at 1:2)"},
	} {
		errs := parse(tt.input)
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
	// After a bad string, parsing goes on and finds the next error too.
	if errs := parse("\"${}\";\nlet = 1"); len(errs) < 2 || !strings.Contains(errs[1], "(at 2:5)") {
		t.Errorf("errors %q, want the empty ${} and then the let", errs)
	}
}
```

In `arv/parser/nesting_test.go`, replace:

```go
	"or chain":      func(n int) string { return "a" + strings.Repeat(" || a", n) },
```

with:

```go
	"or chain":      func(n int) string { return "a" + strings.Repeat(" || a", n) },
	"interpolation": func(n int) string { return `"${` + nest("(", "1", ")", n-1) + `}"` },
```

Create `arv/evaluator/interp_test.go` (it uses `values` from pE.5's `str_test.go`, and `fit`, `bytesLimit` and `sameCost` from pE.3's `fit_test.go`):

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// "<${v}>" holds exactly the text puts prints for v.
func TestInterpolationRendersAsPuts(t *testing.T) {
	for _, name := range []string{"i", "s", "b", "n", "a", "h", "f"} {
		_, printed := runWith(t, values+"puts("+name+")")
		got, _ := runWith(t, values+`"<${`+name+`}>"`)
		s, ok := got.(*value.String)
		if !ok || s.Value != "<"+printed[:len(printed)-1]+">" {
			t.Errorf("<${%s}> = %s, puts printed %q", name, got.Inspect(), printed)
		}
	}
	tests := []struct{ input, want string }{
		{`let r = {"up": true, "status": 503}; "${r.status} up=${r.up}"`, "503 up=true"},
		{`let x = 2; "${x * 10 + 1}${x}"`, "212"},
		{`let ok = 1 < 2; "${if (ok) { 1 } else { 0 }}"`, "1"},
		{`"\${x} costs $5, {not} ${1}"`, "${x} costs $5, {not} 1"},
		{`"plain $ and { and }"`, "plain $ and { and }"},
		// a program's own str changes nothing
		{`let str = fn(x) { "mine" }; "${1}" + str(1)`, "1mine"},
	}
	for _, tt := range tests {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// An interpolated string is the concatenation it stands for, so it costs
// exactly what that costs, in steps and bytes.
func TestInterpolationCostsWhatConcatenationCosts(t *testing.T) {
	sameCost(t, values+`"a ${i} b ${s}${a}!"`, values+`"a " + str(i) + " b " + str(s) + str(a) + "!"`)
	sameCost(t, values+`"${h}"`, values+`str(h)`)
	// The binding (48), str(12345) (5), "n=12345" (7) and "n=12345!" (8).
	if n := fit(t, `let x = 12345; "n=${x}!"`, bytesLimit); n != 48+5+7+8 {
		t.Errorf("costs %d bytes, want 68", n)
	}
}

// Errors point into the string: at the failing node inside ${...}, at the
// ${ whose value could not be rendered, or, for the concatenation, where
// the string starts.
func TestInterpolationErrorPositions(t *testing.T) {
	got, _ := runWith(t, "let a = 1;\n  \"v=${a + nope}\"")
	wantErrorAt(t, got, "identifier not found: nope", "2:12")
	got, _ = runWith(t, `"x ${1 / 0}"`)
	wantErrorAt(t, got, "division by zero", "1:6")
	got, _ = runWith(t, "\"line one\nline ${two}\"")
	wantErrorAt(t, got, "identifier not found: two", "2:8")

	// "[1, 2]" (6 bytes) does not fit after the array (32) and binding (48).
	got = runLimited(t, "let v = [1, 2];\n  \"x ${v}\"", runtime.Limits{AllocBytes: 85})
	wantErrorAt(t, got, "budget exceeded: alloc (limit 85 bytes)", "2:6")
	// "x" + "abc" (4 bytes) does not fit after the binding (48).
	got = runLimited(t, "let s = \"abc\";\n\"x${s}\"", runtime.Limits{AllocBytes: 51})
	wantErrorAt(t, got, "budget exceeded: alloc (limit 51 bytes)", "2:1")
}
```

In `arv/evaluator/nesting_test.go`, replace:

```go
		"or chain":      {"false" + strings.Repeat(" || false", n), "false"},
```

with:

```go
		"or chain":      {"false" + strings.Repeat(" || false", n), "false"},
		"interpolation": {`"${` + nest("(", "1", ")", n-1) + `}"`, "1"},
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/parser/; go test ./arv/evaluator/ -run 'TestInterpolation|TestNesting'`
Expected: FAIL. The parser tests print the strings unchanged (`"a ${x} b"` prints `a ${x} b`, and no syntax-error case has an error; `TestInterpolationPositions` panics on its type assertion, `*ast.StringLiteral, not *ast.InfixExpression`, which stops the parser package's run); the evaluator tests get the literal text back (`<${i}>`), and the cost and position tests fail.

- [ ] **Step 3: Parse interpolated strings**

Create `arv/parser/interp.go`:

```go
package parser

import (
	"fmt"
	"strings"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/token"
)

// parseStringLiteral parses a string literal. One holding ${ is
// interpolated (parseInterpolated); any other means what it always did.
func (p *Parser) parseStringLiteral() ast.Expression {
	tok := p.curToken
	if !strings.Contains(tok.Literal, "${") {
		return &ast.StringLiteral{Token: tok, Value: tok.Literal}
	}
	return p.parseInterpolated(tok)
}

// parseInterpolated parses a string literal holding ${...} into the
// concatenation it means, built from nodes that already exist:
//
//	"a ${x} b"   is   "a " + $str(x) + " b"
//
// $str is the str builtin under a name no program can bind
// (ast.StrBuiltin), so each value prints as puts prints it. Empty pieces
// are left out: "${x}" is $str(x). \${ is a literal ${, and a literal
// holding only that kind of ${ stays one string literal. The expression
// ends at the } that balances the ${, and is lexed where it sits in the
// source, so its tokens and errors carry their real positions. It cannot
// hold a string literal: the lexer ends the string at its first quote.
func (p *Parser) parseInterpolated(tok token.Token) ast.Expression {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	s := &spans{src: tok.Literal, line: tok.Line, col: tok.Col + 1}
	var parts []ast.Expression
	var piece strings.Builder
	pieceAt := tok // the first piece sits at the opening quote
	interpolated := false
	flush := func() {
		if piece.Len() > 0 {
			text := piece.String()
			parts = append(parts, &ast.StringLiteral{Token: token.Token{
				Type: token.STRING, Literal: text, Line: pieceAt.Line, Col: pieceAt.Col,
			}, Value: text})
			piece.Reset()
		}
	}
	for s.i < len(s.src) {
		if piece.Len() == 0 && len(parts) > 0 {
			pieceAt = s.at()
		}
		rest := s.src[s.i:]
		switch {
		case strings.HasPrefix(rest, `\${`):
			piece.WriteString("${")
			s.skip(3)
		case strings.HasPrefix(rest, "${"):
			flush()
			at := s.at()
			e := p.parseEmbedded(s, at)
			if e == nil {
				return nil
			}
			parts = append(parts, strCall(at, e))
			interpolated = true
		default:
			piece.WriteByte(rest[0])
			s.skip(1)
		}
	}
	flush()
	if !interpolated {
		return &ast.StringLiteral{Token: tok, Value: parts[0].(*ast.StringLiteral).Value}
	}
	result := parts[0]
	for _, part := range parts[1:] {
		line, col := ast.Pos(part)
		result = &ast.InfixExpression{
			Token:    token.Token{Type: token.PLUS, Literal: "+", Line: line, Col: col},
			Operator: "+", Left: result, Right: part,
		}
	}
	return result
}

// parseEmbedded parses the expression of the ${ at s's position (at), and
// moves s past its closing }. It lexes the expression in place, with this
// parser's own state, so nesting and errors count as anywhere else. It
// returns nil after recording an error.
func (p *Parser) parseEmbedded(s *spans, at token.Token) ast.Expression {
	end := closingBrace(s.src, s.i+2)
	if end < 0 {
		p.errorAt(at, "unterminated ${ in string")
		return nil
	}
	s.skip(2)
	src := s.src[s.i:end]
	if strings.TrimSpace(src) == "" {
		p.errorAt(at, "empty ${} in string")
		return nil
	}
	l, cur, peek, errs := p.l, p.curToken, p.peekToken, len(p.errors)
	p.l = lexer.NewAt(src, s.line, s.col)
	p.nextToken()
	p.nextToken()
	e := p.parseExpression(LOWEST)
	if len(p.errors) == errs && !p.peekTokenIs(token.EOF) {
		p.errorAt(p.peekToken, fmt.Sprintf("expected } to end ${, got %s instead", p.peekToken.Type))
	}
	p.l = l
	if p.tooDeep { // the nesting limit stopped parsing: the tokens stay EOF
		return nil
	}
	p.curToken, p.peekToken = cur, peek
	s.skip(end + 1 - s.i)
	if len(p.errors) > errs {
		return nil
	}
	return e
}

// closingBrace is the index in s of the } that closes a ${ whose
// expression starts at from, or -1 if none does.
func closingBrace(s string, from int) int {
	depth := 1
	for j := from; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// strCall is $str(e), placed at the ${ that held e.
func strCall(at token.Token, e ast.Expression) ast.Expression {
	at.Type, at.Literal = token.IDENT, ast.StrBuiltin
	fn := &ast.Identifier{Token: at, Value: ast.StrBuiltin}
	at.Type, at.Literal = token.LPAREN, "("
	return &ast.CallExpression{Token: at, Function: fn, Arguments: []ast.Expression{e}}
}

// spans walks the text of a string literal, keeping the source position
// (line and column, as the lexer counts them) of the byte at i.
type spans struct {
	src       string
	i         int
	line, col int
}

func (s *spans) at() token.Token { return token.Token{Line: s.line, Col: s.col} }

func (s *spans) skip(n int) {
	for ; n > 0; n-- {
		if s.src[s.i] == '\n' {
			s.line++
			s.col = 1
		} else {
			s.col++
		}
		s.i++
	}
}
```

In `arv/parser/parsing_functions.go`, delete the old string parser (`interp.go` now has `parseStringLiteral`). Replace:

```go
// parseStringLiteral parses a string
// "<characters>"
// example: "hello"
func (p *Parser) parseStringLiteral() ast.Expression {
	return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
}

// parsePrefixExpression parses a prefix expression
```

with:

```go
// parsePrefixExpression parses a prefix expression
```

In `arv/parser/parser.go` (the `MaxNesting` comment), replace:

```go
// the same reason. So does each && and ||: the parser makes each an if
// expression, nested in the next one's condition.
```

with:

```go
// the same reason. So does each && and ||: the parser makes each an if
// expression, nested in the next one's condition. A string literal
// holding ${...} counts once (see parseInterpolated).
```

No evaluator code changes: the tree is a concatenation and calls of a builtin that exists.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/parser/ ./arv/evaluator/ ./arv/`
Expected: PASS. `FuzzParser`'s seeds (every `examples/*.arv` and `e2e/testdata/*.arv`) parse as before: none holds `${`.

- [ ] **Step 5: Commit**

Run the pre-commit checks (header), then:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && git add arv/parser/ arv/evaluator/interp_test.go arv/evaluator/nesting_test.go && git commit -m "feat(lang): string interpolation, rendered as puts prints

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
