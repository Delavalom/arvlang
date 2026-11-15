# Phase 5E, task pE.1: tokens for `<=` `>=` `&&` `||` `.`, and `lexer.NewAt`

> **For agentic workers:** this file is one task, pE.1. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your worktree `/Users/delavalom/delavalom-labs/arvlang-wt/pE` is on branch `phase/pE`, branched from `feat/phases-3-8` at 860a158, with only the `pE-plan-*.md` files added. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block in `pE-plan-1.md` to `pE-plan-8.md` was applied, in task order, to a scratch copy of the tree and passed `gofmt -l`, `go vet ./...` and `go test -race ./...` (including `e2e/`) after each task. Design: the "If 1-5 hold but 6 doesn't" paragraph of `12-phase5-gate.md`, and criterion 6 and the owner's decision in `gate-report.md`.

## Phase 5E overview

**Goal:** the four syntax costs the gate named, removed without changing any existing program: `r.up` for `r["up"]`; `"${n} of ${total} down"` string interpolation; `&&` and `||` with short-circuit evaluation; `<=` and `>=`.

**How:** the parser desugars every new form into AST node types that already exist, so code that walks the AST (Phase 8.1 `arv check`, Phase 6 effect recording, Phase 8.4 speed work, all running in parallel) needs no change:

| Written | Means | Built as |
|---|---|---|
| `r.up` | `r["up"]` | `IndexExpression` (token: the dot) with a `StringLiteral` key placed at the name |
| `a <= b`, `a >= b` | integer comparison | `InfixExpression` with the new operator strings `<=` / `>=` |
| `a && b` | `if (a) { b } else { false }` | `IfExpression` whose `Token.Type` is `token.AND`, placed where `a` starts |
| `a \|\| b` | `if (a) { true } else { b }` | `IfExpression` whose `Token.Type` is `token.OR` |
| `"a ${x} b"` | `"a " + str(x) + " b"` | `InfixExpression` `+` chain of `StringLiteral` pieces and a `CallExpression` of the identifier `$str` |

**No new AST node type.** The new things a walker can meet: the operators `<=` and `>=` in `InfixExpression`; an `IfExpression` whose token is `&&` or `||` (it prints as `(a && b)`); a call of the builtin `$str` (`ast.StrBuiltin`), which is `str` under a name no program can bind.

**Guarantees (each task's tests name the ones they cover):**
- G1. Every existing program keeps its meaning: all existing tests and e2e cases pass unchanged. On the base, `git grep -n -F '${' -- '*.go' '*.arv' README.md` prints nothing, so no string literal in any test, e2e case or example holds `${`; and `.`, `&`, `|`, `<=`, `>=` outside strings and comments were parse errors.
- G2. `&&` and `||` short-circuit: when the left side decides, the right never runs (no effect, no steps, no bytes).
- G3. `.field` is exactly `["field"]`: same value, same nil for a missing key, same errors at the same positions, same steps and bytes.
- G4. Interpolation renders each value exactly as `puts` prints it and costs exactly what `"a " + str(x) + " b"` costs, in steps and in bytes. `&&` / `||` cost exactly what their `if` costs.
- G5. Error positions point at the sub-expression: inside `${...}` at the real `line:col`, on either side of `&&` / `||` at that side.
- G6. No new way past capabilities or budgets: nothing new reaches the OS; `str` charges its text to `alloc_mb`.

| Task | File | What |
|---|---|---|
| pE.1 | `pE-plan-1.md` | tokens `<=` `>=` `&&` `\|\|` `.`; `lexer.NewAt` |
| pE.2 | `pE-plan-2.md` | `<=` and `>=` |
| pE.3 | `pE-plan-3.md` | `.field`; the `fit` / `sameCost` test helpers |
| pE.4 | `pE-plan-4.md` | `&&` and `\|\|` |
| pE.5 | `pE-plan-5.md` | the `str` builtin (and `$str`) |
| pE.6 | `pE-plan-6.md` | string interpolation |
| pE.7 | `pE-plan-7.md` | `examples/uptime-ergonomic.arv`, its farm test, README, fuzz seeds |
| pE.8 | `pE-plan-8.md` | notes file `16pe-ergonomics.md`, final checks |

### Decisions, and why

1. **Interpolation is `${expr}`; `\${` is a literal `${`.** Strings have no escapes today (`"\n"` is a backslash and an n), and no existing string holds `${` (G1), so only strings that contain `${` change. Terraform's `$${` escape was rejected: `"$${amount}"` reads as a dollar sign and a value but would print `${amount}`. `$`, `{`, `$x`, `$ {` stay literal. A backslash right before a value is written `"\" + "${v}"`.
2. **Non-string values render as `puts` prints them,** through a new builtin `str(v)`: a string is itself, any other value its `Inspect` text. `str` is public, so the hand-written equivalent of `"a ${x}"` exists (`"a " + str(x)`) and the cost guarantee is testable; interpolation calls it as `$str`, a name the lexer cannot produce, so `let str = ...` never changes what interpolation prints.
3. **The expression in `${...}` ends at the `}` that balances it and cannot hold a string literal:** the lexer is unchanged and ends the string at its first quote. With `.field` this is rarely needed (`"${r.status}"`); otherwise bind the value first. The parser lexes the expression in place with `lexer.NewAt`, so its tokens carry real positions.
4. **`&&` / `||` mean the `if` the gate's author wrote by hand** (`if (status > 199) { status < 300 } else { false }`). As in `if`, only `true` counts as true: `1 && true` is `false`, `true && 5` is `5`, and `5 || 6` is `6` (so `||` is not a default operator: `r.name || "anon"` is `"anon"` for any name; corrected wording after review round 1). Precedence: `||` below `&&` below `==` / `!=`.
5. **Each `.`, `&&`, `||` and each interpolated string counts one level toward `parser.MaxNesting`,** because each nests the AST one level (like a call or index link). A chain of more than 10000 `&&` is a parse error; a `+` chain stays unlimited.
6. **The notes go in a new file, `16pe-ergonomics.md`** (the `pe` suffix avoids a clash with the parallel phases' next `16x` letter). `16-implementation-notes.md` is not edited: it is at 18.3 KB, and the parallel phases append to it too; the merge agent adds its one-line link.

### Deviations from the design

- The design (12-phase5-gate.md) put this week before a re-run of the gate. The owner said yes to criterion 6, so the work runs as Phase 5E beside Phases 6, 7, 8.1 and 8.2, and the gate is not re-run. `examples/uptime.arv` and `bench/` (the measured gate inputs) are not touched; `examples/uptime-ergonomic.arv` is new.
- The design names no rendering rule or builtin; `str` is added (decision 2).
- The ergonomic example's error message carries numbers (`20 of 200 targets are down, more than 5%`); its report and printed line are the original's.

## Task pE.1: tokens and `lexer.NewAt`

The lexer learns five tokens: `<=`, `>=`, `&&`, `||` and `.`. A lone `&` or `|` stays `ILLEGAL`, and `< =` with a space stays two tokens. Strings are lexed exactly as before: a `${` inside one is the parser's business (pE.6). `NewAt` starts a lexer at a given `line:col`, so a sub-lexer's tokens point into the larger source. Nothing parses the new tokens yet, so every program that used them is still a parse error.

**Files:**
- Modify: `arv/token/token.go`
- Modify: `arv/lexer/lexer.go`
- Modify (test seeds): `arv/lexer/fuzz_test.go`
- Create (test): `arv/lexer/ergonomics_test.go`

- [ ] **Step 1: Write the failing test**

Create `arv/lexer/ergonomics_test.go`:

```go
package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

// Phase 5E operators: <= >= && || and the field dot. A lone & or | stays
// ILLEGAL, and < followed by a space and = stays two tokens.
func TestErgonomicTokens(t *testing.T) {
	input := "a <= b >= c && d || r.up < = & | x.y.z 1.5"
	want := []struct {
		typ token.TokenType
		lit string
		col int
	}{
		{token.IDENT, "a", 1}, {token.LT_EQ, "<=", 3}, {token.IDENT, "b", 6},
		{token.GT_EQ, ">=", 8}, {token.IDENT, "c", 11}, {token.AND, "&&", 13},
		{token.IDENT, "d", 16}, {token.OR, "||", 18}, {token.IDENT, "r", 21},
		{token.DOT, ".", 22}, {token.IDENT, "up", 23}, {token.LT, "<", 26},
		{token.ASSIGN, "=", 28}, {token.ILLEGAL, "&", 30}, {token.ILLEGAL, "|", 32},
		{token.IDENT, "x", 34}, {token.DOT, ".", 35}, {token.IDENT, "y", 36},
		{token.DOT, ".", 37}, {token.IDENT, "z", 38},
		{token.INT, "1", 40}, {token.DOT, ".", 41}, {token.INT, "5", 42},
		{token.EOF, "", 43},
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

// Strings are lexed as before: a ${ inside one is the parser's business.
func TestStringWithInterpolationIsOneToken(t *testing.T) {
	l := New(`"a ${x.y} $${z}"`)
	if tok := l.NextToken(); tok.Type != token.STRING || tok.Literal != "a ${x.y} $${z}" {
		t.Fatalf("got %s %q", tok.Type, tok.Literal)
	}
}

// NewAt starts counting at the given position, so a sub-lexer's tokens
// point into the larger source; a newline still starts column 1.
func TestNewAtOffsetsPositions(t *testing.T) {
	l := NewAt("x +\n  y", 3, 10)
	want := []struct {
		lit       string
		line, col int
	}{{"x", 3, 10}, {"+", 3, 12}, {"y", 4, 3}, {"", 4, 4}}
	for i, w := range want {
		tok := l.NextToken()
		if tok.Literal != w.lit || tok.Line != w.line || tok.Col != w.col {
			t.Fatalf("token %d = %q at %d:%d, want %q at %d:%d", i, tok.Literal, tok.Line, tok.Col, w.lit, w.line, w.col)
		}
	}
	if tok := NewAt("", 2, 5).NextToken(); tok.Type != token.EOF || tok.Line != 2 || tok.Col != 5 {
		t.Fatalf("empty input: EOF at %d:%d, want 2:5", tok.Line, tok.Col)
	}
}
```

In `arv/lexer/fuzz_test.go`, replace:

```go
	"=> = > ==> =>= === =",
}
```

with:

```go
	"=> = > ==> =>= === =",
	`a <= b >= c && d || r.up & | . <== >== &&& ||| "${x} \${y}"`,
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/lexer/`
Expected: FAIL to compile: `undefined: token.LT_EQ` (and the other new tokens), `undefined: NewAt`.

- [ ] **Step 3: Add the tokens**

In `arv/token/token.go`, replace:

```go
	LT       = "<"
	GT       = ">"
```

with:

```go
	LT       = "<"
	GT       = ">"
	LT_EQ    = "<="
	GT_EQ    = ">="
	AND      = "&&"
	OR       = "||"
	DOT      = "." // field access: r.up is r["up"]
```

- [ ] **Step 4: Lex them, and add `NewAt`**

In `arv/lexer/lexer.go`, replace:

```go
func New(input string) *Lexer {
	l := &Lexer{input: input, line: 1}
	l.readChar()
	return l
}
```

with:

```go
func New(input string) *Lexer { return NewAt(input, 1, 1) }

// NewAt is New for input that starts at line:col of a larger source, so
// every token carries its position in that source. The parser lexes the
// expression inside each ${...} of a string literal this way.
func NewAt(input string, line, col int) *Lexer {
	l := &Lexer{input: input, line: line, col: col - 1}
	l.readChar()
	return l
}
```

In `arv/lexer/lexer.go`, replace:

```go
	case '<':
		tok = newToken(token.LT, l.ch)
	case '>':
		tok = newToken(token.GT, l.ch)
```

with:

```go
	case '<':
		tok = l.pair('=', token.LT_EQ, token.LT)
	case '>':
		tok = l.pair('=', token.GT_EQ, token.GT)
	case '&':
		tok = l.pair('&', token.AND, token.ILLEGAL)
	case '|':
		tok = l.pair('|', token.OR, token.ILLEGAL)
	case '.':
		tok = newToken(token.DOT, l.ch)
```

In `arv/lexer/lexer.go`, replace:

```go
func newToken(tokenType token.TokenType, ch byte) token.Token {
```

with:

```go
// pair is the two-byte token two when the next byte is next (it reads
// that byte), else the one-byte token one.
func (l *Lexer) pair(next byte, two, one token.TokenType) token.Token {
	if l.peekChar() != next {
		return newToken(one, l.ch)
	}
	first := l.ch
	l.readChar()
	return token.Token{Type: two, Literal: string(first) + string(l.ch)}
}

func newToken(tokenType token.TokenType, ch byte) token.Token {
```

Then run `gofmt -w arv/lexer/lexer.go`: it adds the blank line the file lacks before the new comment.

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/lexer/ ./arv/parser/`
Expected: PASS. Nothing parses the new tokens yet; the parser tests are unchanged and green.

- [ ] **Step 6: Commit**

Run the pre-commit checks (header), then:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && git add arv/token/token.go arv/lexer/lexer.go arv/lexer/fuzz_test.go arv/lexer/ergonomics_test.go && git commit -m "feat(lexer): tokens for <= >= && || and ., and lexer.NewAt

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
