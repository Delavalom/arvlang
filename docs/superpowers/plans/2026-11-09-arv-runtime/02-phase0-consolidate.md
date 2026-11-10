# Phase 0: Consolidate to one interpreter

**Goal:** One working pipeline (lexer → parser → evaluator) in `arv/`. The older experiments are preserved under a tag and removed from the tree. The lexer learns `//` comments and identifiers with digits.

**Why first:** Today the repo has three front ends. `main.go` runs `lang/newlexer`, which only tokenizes. The only code that evaluates anything is `lang/monkeylexer`. Every later phase changes the evaluator, so there must be exactly one.

---

### Task 0.1: Tag the research state and branch

**Files:** none

- [ ] **Step 1: Make sure the tree is clean and on main**

Run: `cd ~/delavalom-labs/arvlang && git status --short && git rev-parse --abbrev-ref HEAD`
Expected: no output from status; branch `main`.

- [ ] **Step 2: Tag and branch**

```bash
git tag -a v0-research -m "Research state before the runtime work: newlexer, lexer/parser, monkeylexer"
git checkout -b feat/runtime
```

Pushing the tag is optional: `git push origin v0-research`.

---

### Task 0.2: Move the Monkey interpreter to `arv/` and delete the rest

**Files:**
- Move: `lang/monkeylexer/{ast,evaluator,lexer,parser,repl,token,value}` → `arv/…`
- Delete: `lang/monkeylexer/main.go`, `lang/newlexer`, `lang/lexer`, `lang/parser`, `lang/ast`, `lang/tokens`, `lang/order`, `lang/syntax`, `lang/queue`, `lang/utils`
- Rewrite: `main.go`
- Modify: `go.mod`, `go.sum` (testify drops out; only deleted packages used it)

- [ ] **Step 1: Move and delete**

```bash
git mv lang/monkeylexer arv
git rm -rq lang
git rm -qf arv/main.go
```

`-f` is needed on the last command because `git mv` already staged that file.

- [ ] **Step 2: Rewrite import paths**

```bash
grep -rl --include='*.go' 'arvlang/lang/monkeylexer' . \
  | xargs sed -i '' 's#arvlang/lang/monkeylexer/#arvlang/arv/#g'
```

(`sed -i ''` is the macOS form. On Linux use `sed -i`.)

- [ ] **Step 3: Replace `main.go`** *before* running `go mod tidy`, because the old one imports a deleted package

```go
package main

import (
	"fmt"
	"os"

	"github.com/delavalom/arvlang/arv/repl"
)

func main() {
	fmt.Println("Arv REPL. Ctrl-D to exit.")
	repl.Start(os.Stdin, os.Stdout)
}
```

- [ ] **Step 4: Tidy, vet, test**

Run: `go mod tidy && go vet ./... && go test ./...`
Expected: `ok` for `arv/evaluator`, `arv/lexer`, `arv/parser`, `arv/value`; `go.mod` no longer requires `testify`.

- [ ] **Step 5: Check the REPL by hand**

Run: `printf 'let add = fn(a, b) { a + b }; add(2, 3)\n' | go run .`
Expected: `Arv REPL. Ctrl-D to exit.` then `>> 5`.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: keep one interpreter under arv/, drop older lexer experiments

The previous experiments are preserved at tag v0-research."
```

---

### Task 0.3: `//` comments and identifiers with digits

Phase 2 scripts need comments, and names like `host2` must lex as one identifier.

**Files:**
- Create: `arv/lexer/comments_test.go`
- Modify: `arv/lexer/lexer.go` (`readIdentifier`, `NextToken`)

- [ ] **Step 1: Write the failing test**

```go
package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

func TestCommentsAndDigitIdentifiers(t *testing.T) {
	input := `// a comment line
let host2 = 5; // trailing comment
host2 / 1`
	want := []struct {
		typ token.TokenType
		lit string
	}{
		{token.LET, "let"},
		{token.IDENT, "host2"},
		{token.ASSIGN, "="},
		{token.INT, "5"},
		{token.SEMICOLON, ";"},
		{token.IDENT, "host2"},
		{token.SLASH, "/"},
		{token.INT, "1"},
		{token.EOF, ""},
	}
	l := New(input)
	for i, w := range want {
		tok := l.NextToken()
		if tok.Type != w.typ || tok.Literal != w.lit {
			t.Fatalf("token %d: got (%q, %q), want (%q, %q)", i, tok.Type, tok.Literal, w.typ, w.lit)
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./arv/lexer/ -run TestCommentsAndDigitIdentifiers -v`
Expected: FAIL at token 0 (the `/` of the comment comes back as `SLASH`).

- [ ] **Step 3: Implement.** In `arv/lexer/lexer.go`, let identifiers continue with digits:

```go
func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}
```

Then add `skipComment` and call it at the top of `NextToken`. Replace the opening of `NextToken` up to and including `l.skipWhitespace()` with:

```go
// skipComment skips a // comment up to (not including) the newline.
func (l *Lexer) skipComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

func (l *Lexer) NextToken() token.Token {
	l.skipWhitespace()
	for l.ch == '/' && l.peekChar() == '/' {
		l.skipComment()
		l.skipWhitespace()
	}
```

Identifiers still have to *start* with a letter or `_`: the `default:` branch checks `isLetter` before `isDigit`, so `2x` still lexes as `INT 2`, `IDENT x`.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 5: Commit**

```bash
git add arv/lexer
git commit -m "feat(lexer): line comments and identifiers containing digits"
```
