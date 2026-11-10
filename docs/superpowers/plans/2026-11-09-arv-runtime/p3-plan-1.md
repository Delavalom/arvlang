# Phase 3, task p3.1: Positions in tokens and parse errors

> **For agentic workers:** this file is one task, p3.1. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your tree is `feat/phases-3-8` (Phases 0–2), with no Phase 3 work yet. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

## Phase 3 overview

**Goal:** `scope (timeout_ms: 2000, tasks: 20, steps: 1000000, alloc_mb: 64) { ... }` enforces every limit with a clean, positioned Arv error. Limits only shrink when inherited (I6). A spent budget cancels its scope and waits for its tasks (I4). `for … in` and `await [...]` make fan-out one expression. The CLI sets the run's budget.

**Invariants touched:** I4, I5, I6. The tests name them.

| Task | File | What |
|---|---|---|
| p3.1 | `p3-plan-1.md` | `line:col` in tokens and parse errors |
| p3.2 | `p3-plan-2.md` | runtime errors carry a position; `*arv.Error`; CLI prints it |
| p3.3 | `p3-plan-3.md` | `for x in xs { }` comprehension |
| p3.4 | `p3-plan-4.md` | `await [t1, t2]` |
| p3.5 | `p3-plan-5.md` | `runtime.Budget`, `Limits`, `ExceededError` |
| p3.6 | `p3-plan-6.md` | scope options syntax |
| p3.7 | `p3-plan-7.md` | option validation; per-scope budget frames |
| p3.8 | `p3-plan-8.md` | `timeout_ms`, `RunLimited` |
| p3.9 | `p3-plan-9.md` | `steps` |
| p3.10 | `p3-plan-10.md` | `tasks` slots and the live-task cap |
| p3.11 | `p3-plan-11.md` | `alloc_mb` |
| p3.12 | `p3-plan-12.md` | CLI flags, `arv.Options.Limits` |
| p3.13 | `p3-plan-13.md` | `examples/fanout.arv`, README |

Each task's tests cover the acceptance table of `10-phase3-budgets.md` in process: evaluator, `arv` and CLI tests. Black-box cases in `e2e/` belong to the phase's separate e2e step.

**16-implementation-notes follow-through:**
- `tasks:` slots are released in a `defer` (p3.10).
- Scopes get their budgets in `Run` and `evalScope`, and in `runTask` too (p3.7).
- The default live-task cap is 1000, not unlimited (p3.5, p3.10).
- `timeout_ms` is checked for overflow the way `sleep` is (p3.7).
- Per-scope state lives in `frame`s in `in.scopes`, never in exported `Task` fields (p3.7, p3.9).

### Deviations from `10-phase3-budgets.md`, and why

1. **Error format.**
   - The design: `fanout.arv:12:7: capability denied: …`.
   - Built: `error: capability denied: … (at fanout.arv:12:7)`.
   - Why: the kickoff requires the Phase 0–2 e2e expectations to stay unchanged. They match `error: identifier not found: …`, and the harness requires stderr to start with `error: `. `arv.Run` returns `*arv.Error{Msg, Pos}`, whose `Error()` is the message alone, so exact `err.Error()` checks hold. Parse errors get ` (at line:col)`.
   - The acceptance row "stderr starts with `<file>:3:`" becomes "stderr contains `(at <file>:3:`".
2. **Every error is positioned, not only those from calls and spawns.**
   - `Interpreter.Eval` stamps the innermost node with a position. Infix, call and index nodes use their leftmost operand.
   - This is what makes "every budget error has a position" true for errors raised outside calls, such as steps running out mid-expression.
   - Stamping copies the error: task results are shared between goroutines.
3. **Order and split.**
   - `runtime.Budget` comes before scope options, so options can produce `runtime.Limits`.
   - Options are split into syntax (p3.6) and evaluation (p3.7).
   - CLI and docs are split (p3.12, p3.13).
   - Why: each file stays under 18 KB.
4. **Steps are charged after use, not taken in advance as fuel.**
   - The interpreter charges every 256 nodes, and again when a scope opens or closes.
   - Overshoot is still at most 255 per task, but a task can't hoard fuel and starve its siblings, and one task's total is exact.
   - API: `Budget.Steps(n) error` and `Budget.Alloc(n) error` instead of `Take(kind, n)`.
   - The count lives in the `Interpreter`, not in `Task.Tick`, per 16-notes.
5. **There are two task limits.**
   - `tasks:` is direct-children slots, as designed, and `spawn` blocks.
   - Slots chain through the enclosing scopes of the same task (I6), but stop at the task boundary, to avoid the design's deadlock.
   - The new `Limits.LiveTasks` is a run-wide cap that fails instead of blocking. 16-notes: spawn recursion is unbounded, so the default must not be unlimited.
   - Its default is `runtime.DefaultLiveTasks = 1000`, under the race detector's limit of 8128 goroutines.
   - `arv run --tasks N` sets this cap, not root-scope slots.
6. **Timeout reporting.**
   - `runtime.NewTimedScope` and `Scope.Expired` let a scope report its own expiry as `budget exceeded: timeout (limit 100 ms)` instead of `sleep cancelled: …`.
   - The root uses the same path through `Interpreter.RunLimited`.
7. **Option values.**
   - Values must be positive; `0` is an error, not "no limit".
   - Duplicates are errors.
   - Each value has an overflow bound.
8. **Alloc details** (p3.11): string literals are free, `last` is exempt through `value.Builtin.Shares`, and an exceeded pool stays exceeded.
9. **Units in messages.**
   - `… timeout (limit 100 ms)`, `… alloc (limit 1048576 bytes)`, `… tasks (limit N live tasks)`.
   - `budget exceeded: steps (limit 1000)` is exactly as designed.
10. **`--timeout 0`** disables the default 10-minute timeout.

Seen while validating, not caused by Phase 3: `arv/ast` `TestLongInfixChainPrintsInLinearTime` has a 2 s bound. It failed once (2.3 s) while two `-race` suites ran in parallel on the loaded machine, and passed when rerun.

---

### Task p3.1: Positions in tokens and parse errors

Every token knows the `line:col` where it starts, and parse errors end with ` (at line:col)`. The runtime errors in p3.2 build on this. Columns count bytes from 1; a newline inside a string literal moves to the next line.

**Files:**
- Modify: `arv/lexer/lexer.go`
- Create (test): `arv/lexer/position_test.go`
- Modify: `arv/parser/concurrency.go`
- Modify: `arv/parser/parser.go`
- Modify: `arv/parser/parsing_functions.go`
- Create (test): `arv/parser/position_test.go`
- Modify: `arv/token/token.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/lexer/position_test.go`:

```go
package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

// Every token records where it starts: line from 1, column in bytes from 1.
// Comments, blank lines, tabs and multi-line strings all move the position.
func TestTokenPositions(t *testing.T) {
	input := "let x = 5;\n// note\n\n\tputs(\"a\nb\", x)\n  y"
	want := []struct {
		lit       string
		line, col int
	}{
		{"let", 1, 1}, {"x", 1, 5}, {"=", 1, 7}, {"5", 1, 9}, {";", 1, 10},
		{"puts", 4, 2}, {"(", 4, 6}, {"a\nb", 4, 7}, {",", 5, 3}, {"x", 5, 5}, {")", 5, 6},
		{"y", 6, 3}, {"", 6, 4},
	}
	l := New(input)
	for i, w := range want {
		tok := l.NextToken()
		if tok.Literal != w.lit || tok.Line != w.line || tok.Col != w.col {
			t.Fatalf("token %d = %q at %d:%d, want %q at %d:%d",
				i, tok.Literal, tok.Line, tok.Col, w.lit, w.line, w.col)
		}
	}
}

func TestTwoCharTokenPosition(t *testing.T) {
	tok := New("  == b").NextToken()
	if tok.Type != token.EQ || tok.Line != 1 || tok.Col != 3 {
		t.Fatalf("got %+v, want == at 1:3", tok)
	}
}
```

Create `arv/parser/position_test.go`:

```go
package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
)

// Parse errors say where they happened, as "(at line:col)".
func TestParseErrorsHavePositions(t *testing.T) {
	tests := []struct{ input, want string }{
		{"let x = 1;\nlet y = ;", "no prefix parse function for ; found (at 2:9)"},
		{"let x 5;", "expected next token to be =, got INT instead (at 1:7)"},
		{"1;\n  spawn x", "spawn expects a function call, e.g. spawn f(x) (at 2:3)"},
		{"99999999999999999999", `could not parse "99999999999999999999" as integer (at 1:1)`},
	}
	for _, tt := range tests {
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

Run: `cd <worktree> && go test ./arv/lexer/ ./arv/parser/`

Expected: `arv/lexer` fails to build with `tok.Line undefined (type token.Token has no field or method Line)`, and `TestParseErrorsHavePositions` fails because the errors have no ` (at 2:9)` suffix, for example:

```
"let x = 1;\nlet y = ;": errors ["no prefix parse function for ; found"], want first "no prefix parse function for ; found (at 2:9)"
```

- [ ] **Step 3: Implement**

In `arv/lexer/lexer.go`, replace

```go

type Lexer struct {
	input        string
	position     int  // current position in input (points to current char) readPosition int // current reading position in input (after current char) ch byte // current char under examination
	readPosition int  // current reading position in input (after current char) ch byte // current char under examination
	ch           byte // current char under examination
}

func New(input string) *Lexer {
	l := &Lexer{input: input}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
```

with

```go

type Lexer struct {
	input        string
	position     int  // current position in input (points to current char)
	readPosition int  // current reading position in input (after current char)
	ch           byte // current char under examination
	line, col    int  // where ch is: line from 1, column in bytes from 1
}

func New(input string) *Lexer {
	l := &Lexer{input: input, line: 1}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.ch == '\n' { // leaving a newline: ch moves to the next line
		l.line++
		l.col = 0
	}
	l.col++
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
```

In `arv/lexer/lexer.go`, replace

```go
		l.skipWhitespace()
	}

	var tok token.Token
	switch l.ch {
	case '=':
```

with

```go
		l.skipWhitespace()
	}

	line, col := l.line, l.col
	tok := l.lex()
	tok.Line, tok.Col = line, col
	return tok
}

// lex reads the token starting at the current character.
func (l *Lexer) lex() token.Token {
	var tok token.Token
	switch l.ch {
	case '=':
```


In `arv/parser/concurrency.go`, replace

```go
	p.nextToken()
	call, ok := p.parseExpression(LOWEST).(*ast.CallExpression)
	if !ok {
		p.addError("spawn expects a function call, e.g. spawn f(x)")
		return nil
	}
	exp.Call = call
```

with

```go
	p.nextToken()
	call, ok := p.parseExpression(LOWEST).(*ast.CallExpression)
	if !ok {
		p.errorAt(exp.Token, "spawn expects a function call, e.g. spawn f(x)")
		return nil
	}
	exp.Call = call
```


In `arv/parser/parser.go`, replace

```go
	return p.errors
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead",
		t, p.peekToken.Type)
	p.addError(msg)
}

func (p *Parser) noPrefixParseFnError(t token.TokenType) {
	msg := fmt.Sprintf("no prefix parse function for %s found", t)
	p.addError(msg)
}

func (p *Parser) ParseProgram() *ast.Program {
```

with

```go
	return p.errors
}

// errorAt records msg with the position of tok: "msg (at line:col)".
func (p *Parser) errorAt(tok token.Token, msg string) {
	p.addError(fmt.Sprintf("%s (at %d:%d)", msg, tok.Line, tok.Col))
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead",
		t, p.peekToken.Type)
	p.errorAt(p.peekToken, msg)
}

func (p *Parser) noPrefixParseFnError(t token.TokenType) {
	msg := fmt.Sprintf("no prefix parse function for %s found", t)
	p.errorAt(p.curToken, msg)
}

func (p *Parser) ParseProgram() *ast.Program {
```


In `arv/parser/parsing_functions.go`, replace

```go
	value, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		msg := fmt.Sprintf("could not parse %q as integer", p.curToken.Literal)
		p.addError(msg)
		return nil
	}
```

with

```go
	value, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		msg := fmt.Sprintf("could not parse %q as integer", p.curToken.Literal)
		p.errorAt(p.curToken, msg)
		return nil
	}
```


In `arv/token/token.go`, replace

```go

type TokenType string

type Token struct {
	Type    TokenType
	Literal string
}

const (
```

with

```go

type TokenType string

// Token is one lexeme. Line and Col (both from 1, Col in bytes) are where
// it starts in the source; error messages report them as "line:col".
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}

const (
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test ./arv/lexer/ ./arv/parser/`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

The suffix keeps every existing message as a prefix, so the Phase 1 e2e check `error: parse error: no prefix parse function for ; found` still matches.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/lexer/lexer.go arv/lexer/position_test.go arv/parser/concurrency.go arv/parser/parser.go arv/parser/parsing_functions.go arv/parser/position_test.go arv/token/token.go && git commit -F - <<'MSG'
feat(lexer): tokens carry line:col, and parse errors say where

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
