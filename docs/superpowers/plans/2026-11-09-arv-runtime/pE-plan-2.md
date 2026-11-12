# Phase 5E, task pE.2: `<=` and `>=`

> **For agentic workers:** this file is one task, pE.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task pE.1 is committed on `phase/pE` in `/Users/delavalom/delavalom-labs/arvlang-wt/pE`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Overview, guarantees G1-G6, decisions and deviations: `pE-plan-1.md`.

### Task pE.2: `<=` and `>=`

`<=` and `>=` are infix operators with the precedence of `<` and `>`. They parse into the existing `InfixExpression` with the operator strings `<=` and `>=` (the only new thing a walker sees), and compare integers. Any other operands give the errors `<` and `>` give: `unknown operator: STRING <= STRING`, `type mismatch: INTEGER >= BOOLEAN`, placed where the left operand starts (as every infix error is). Before this task `a <= b` was a parse error, so no existing program changes (G1).

**Files:**
- Modify: `arv/parser/parser.go`
- Modify: `arv/evaluator/eval_functions.go`
- Create (test): `arv/parser/compare_test.go`
- Create (test): `arv/evaluator/compare_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/parser/compare_test.go` (its `printed` helper is used by the later parser tests too):

```go
package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// printed parses input, fails on any parse error, and returns the
// program's printed form.
func printed(t *testing.T, input string) string {
	t.Helper()
	p := New(lexer.New(input))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	return program.String()
}

// <= and >= are infix operators with the precedence of < and >.
func TestParseLessOrEqualAndGreaterOrEqual(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`a <= b`, `(a <= b)`},
		{`a >= b`, `(a >= b)`},
		{`1 + 2 <= 3 == true`, `(((1 + 2) <= 3) == true)`},
		{`a < b >= c`, `((a < b) >= c)`},
		{`-a >= b * 2`, `((-a) >= (b * 2))`},
	} {
		if got := printed(t, tt.input); got != tt.want {
			t.Errorf("%q printed %q, want %q", tt.input, got, tt.want)
		}
	}
	p := New(lexer.New(`x <= 5`))
	program := p.ParseProgram()
	ie, ok := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.InfixExpression)
	if !ok || ie.Operator != "<=" {
		t.Fatalf("got %T %s, want an infix <=", program.Statements[0], program)
	}
}
```

Create `arv/evaluator/compare_test.go`:

```go
package evaluator

import "testing"

// <= and >= compare integers, like < and >; any other operands are the
// errors < and > give.
func TestLessOrEqualAndGreaterOrEqual(t *testing.T) {
	tests := []struct{ input, want string }{
		{`1 <= 2`, "true"}, {`2 <= 2`, "true"}, {`3 <= 2`, "false"},
		{`1 >= 2`, "false"}, {`2 >= 2`, "true"}, {`3 >= 2`, "true"},
		{`-5 <= -5`, "true"}, {`1 + 1 >= 2 == true`, "true"},
		{`let s = 200; if (s >= 200) { s <= 299 } else { false }`, "true"},
	}
	for _, tt := range tests {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
	got, _ := runWith(t, `"a" <= "b"`)
	wantErrorAt(t, got, "unknown operator: STRING <= STRING", "1:1")
	got, _ = runWith(t, "let n = 1;\n  n >= true")
	wantErrorAt(t, got, "type mismatch: INTEGER >= BOOLEAN", "2:3")
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/parser/ -run TestParseLessOrEqual; go test ./arv/evaluator/ -run TestLessOrEqual`
Expected: FAIL. The parser test reports `parser error: "no prefix parse function for <= found (at 1:3)"`; the evaluator test fails in `runWith` with the same parse error.

- [ ] **Step 3: Parse them**

In `arv/parser/parser.go`, replace:

```go
	LESSGREATER // > or <
```

with:

```go
	LESSGREATER // > < >= <=
```

In `arv/parser/parser.go`, replace:

```go
	token.GT:       LESSGREATER,
```

with:

```go
	token.GT:       LESSGREATER,
	token.LT_EQ:    LESSGREATER,
	token.GT_EQ:    LESSGREATER,
```

In `arv/parser/parser.go`, replace:

```go
	p.registerInfix(token.GT, p.parseInfixExpression)
```

with:

```go
	p.registerInfix(token.GT, p.parseInfixExpression)
	p.registerInfix(token.LT_EQ, p.parseInfixExpression)
	p.registerInfix(token.GT_EQ, p.parseInfixExpression)
```

- [ ] **Step 4: Evaluate them**

In `arv/evaluator/eval_functions.go` (in `evalIntegerInfixExpression`), replace:

```go
	case ">":
		return nativeBoolToBooleanObject(leftVal > rightVal)
```

with:

```go
	case ">":
		return nativeBoolToBooleanObject(leftVal > rightVal)
	case "<=":
		return nativeBoolToBooleanObject(leftVal <= rightVal)
	case ">=":
		return nativeBoolToBooleanObject(leftVal >= rightVal)
```

Strings and mixed types need nothing: `evalStringInfixExpression` and `evalInfixExpression` already answer any other operator with `unknown operator` or `type mismatch`.

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/parser/ ./arv/evaluator/`
Expected: PASS.

- [ ] **Step 6: Commit**

Run the pre-commit checks (header), then:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && git add arv/parser/parser.go arv/evaluator/eval_functions.go arv/parser/compare_test.go arv/evaluator/compare_test.go && git commit -m "feat(lang): <= and >= on integers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
