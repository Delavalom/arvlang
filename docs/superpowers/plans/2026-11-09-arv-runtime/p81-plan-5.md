# Phase 8.1, task p81.5: Constant strings

> **For agentic workers:** this file is one task, p81.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.4 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.5: Constant strings

Literal arguments alone make `arv check` say "not statically known" for `let base = "https://x"; http_get(base + "/v1")`. A name that the whole program binds exactly once, by a `let` whose value is a known string, can only evaluate to that string: no other `let`, parameter, `for` variable or `match` name binds it, so no scope can see another value, and before the `let` runs a reference fails before admitting anything. `+` of two known strings is their concatenation, as `evalStringInfixExpression` computes it. Everything else stays unknown, which is always sound.

Folding is bounded: a string over `maxConstLen` (8 KiB, `http_get`'s URL limit), more than `maxFolded` (1 MiB) folded in all, or more than `maxHops` (100) names deep is not known, so `let s1 = s0 + s0; ...` doubling 60 times stays cheap. The `+` chain is walked along its left spine without recursing.

`TestNodeFieldsAreKnown` (p81.2) also guards this file: a new binding field in an AST node fails it until `newFolder` counts it.

**Files:**
- Create: `arv/check/consts.go`
- Modify: `arv/check/check.go` (three hunks)
- Create (test): `arv/check/consts_test.go`

- [ ] **Step 1: Write the failing test**

Create `arv/check/consts_test.go`:

```go
package check

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestConstantsAreFolded(t *testing.T) {
	needs, notes := checked(t, `let base = "https://api.example.com";
let dir = "out";
let file = dir + "/" + "report.json";
http_get(base + "/v1/status");
write_file(file, "x");
let f = fn() { getenv(name) };
let name = "HOME";`)
	want := []string{"net=api.example.com:443", "fs_write=out/report.json", "env=HOME"}
	if !reflect.DeepEqual(needs, want) || notes != nil {
		t.Errorf("needs = %q, notes = %q; want %q", needs, notes, want)
	}
}

// A name bound more than once anywhere, by any binding form, is not a
// constant: some scope may see the other value.
func TestReboundNamesAreNotConstants(t *testing.T) {
	for _, src := range []string{
		`let u = "http://a/"; let u = "http://b/"; http_get(u);`,
		`let u = "http://a/"; let f = fn(u) { http_get(u) };`,
		`let u = "http://a/"; for u in ["http://b/"] { http_get(u) };`,
		`let u = "http://a/"; match 1 { ok(u) => http_get(u), err(e) => e };`,
		`let u = "http://a/"; match 1 { ok(v) => v, err(u) => http_get(u) };`,
		`let f = fn() { let u = "http://a/"; http_get(u) }; let u = 1;`,
	} {
		needs, notes := checked(t, src)
		if needs != nil || len(notes) != 1 || !strings.Contains(notes[0], "host not statically known") {
			t.Errorf("%s\nneeds = %q, notes = %q", src, needs, notes)
		}
	}
}

func TestOnlyStringPlusFolds(t *testing.T) {
	for _, src := range []string{
		`let u = "a" - "b"; getenv(u);`,
		`let u = "a" + 1; getenv(u);`,
		`let u = len("a"); getenv(u);`,
		`let u = u + "x"; getenv(u);`,
		`let a = b + "x"; let b = a + "y"; getenv(a);`,
	} {
		needs, notes := checked(t, src)
		if needs != nil || len(notes) != 1 {
			t.Errorf("%s\nneeds = %q, notes = %q", src, needs, notes)
		}
	}
}

// Doubling a string 60 times would be an exabyte: folding gives up past
// maxConstLen and maxFolded, and the call is noted instead.
func TestFoldingIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`let s0 = "ab";` + "\n")
	for i := 1; i <= 60; i++ {
		b.WriteString("let s" + strconv.Itoa(i) + " = s" + strconv.Itoa(i-1) + " + s" + strconv.Itoa(i-1) + ";\n")
	}
	for i := 0; i <= 60; i++ {
		b.WriteString("getenv(s" + strconv.Itoa(i) + ");\n")
	}
	start := time.Now()
	r, err := Source(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("Source took %s", time.Since(start))
	}
	if len(r.Needs) == 0 || len(r.Notes) == 0 || len(r.Needs)+len(r.Notes) != 61 {
		t.Errorf("%d needs and %d notes, want some of each and 61 in all", len(r.Needs), len(r.Notes))
	}
	for _, n := range r.Needs {
		if len(n.Scope) > maxConstLen {
			t.Errorf("a need of %d bytes", len(n.Scope))
		}
	}
}

// A long chain of names is followed only maxHops deep.
func TestNameChainsAreBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`let n0 = "A";` + "\n")
	for i := 1; i <= 5000; i++ {
		b.WriteString("let n" + strconv.Itoa(i) + " = n" + strconv.Itoa(i-1) + ";\n")
	}
	b.WriteString("getenv(n5000); getenv(n50);")
	needs, notes := checked(t, b.String())
	if !reflect.DeepEqual(needs, []string{"env=A"}) || len(notes) != 1 {
		t.Errorf("needs = %q, notes = %q", needs, notes)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./arv/check/`
Expected: build failure: `undefined: maxConstLen`.

- [ ] **Step 3: Implement**

Create `arv/check/consts.go`:

```go
package check

import "github.com/delavalom/arvlang/arv/ast"

// Constant strings. A name that the whole program binds exactly once, by
// a let whose value is a known string, can only ever evaluate to that
// string: no other let, parameter, for variable or match arm binds it, so
// no scope can shadow it with another value. (Before its let has run the
// name is not found, or names a builtin, and either way the call fails
// before admitting anything.) A + of two known strings is their
// concatenation, as the evaluator computes it. Anything else is not known.

const (
	maxConstLen = maxURLLen // longer than any scope the runtime accepts
	maxFolded   = 1 << 20   // bytes folded in all, so doubling lets stay cheap
	maxHops     = 100       // names followed to fold one argument
)

type folded struct {
	s  string
	ok bool
}

type folder struct {
	lets  map[string]ast.Expression // names bound once in the program, by a let
	memo  map[string]folded
	busy  map[string]bool
	spent int
}

// newFolder counts every binding in prog and keeps the lets of names
// bound exactly once.
func newFolder(prog *ast.Program) (*folder, error) {
	count := map[string]int{}
	lets := map[string]ast.Expression{}
	bind := func(id *ast.Identifier) {
		if id != nil {
			count[id.Value]++
		}
	}
	err := walk(prog, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.LetStatement:
			bind(n.Name)
			if n.Name != nil {
				lets[n.Name.Value] = n.Value
			}
		case *ast.FunctionLiteral:
			for _, p := range n.Parameters {
				bind(p)
			}
		case *ast.ForExpression:
			bind(n.Var)
		case *ast.MatchExpression:
			bind(n.OkName)
			bind(n.ErrName)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	for name := range lets {
		if count[name] != 1 {
			delete(lets, name)
		}
	}
	return &folder{lets: lets, memo: map[string]folded{}, busy: map[string]bool{}}, nil
}

// str is e's value if it is a string known before running.
func (f *folder) str(e ast.Expression, hops int) (string, bool) {
	switch e := e.(type) {
	case *ast.StringLiteral:
		return e.Value, true
	case *ast.Identifier:
		return f.name(e.Value, hops)
	case *ast.InfixExpression:
		return f.concat(e, hops)
	}
	return "", false
}

func (f *folder) name(name string, hops int) (string, bool) {
	if r, ok := f.memo[name]; ok {
		return r.s, r.ok
	}
	val, ok := f.lets[name]
	if !ok || f.busy[name] || hops >= maxHops {
		return "", false // a cycle (let a = a + "x") is never bound
	}
	f.busy[name] = true
	s, ok := f.str(val, hops+1)
	delete(f.busy, name)
	f.memo[name] = folded{s, ok}
	return s, ok
}

// concat folds a chain of +, walking its left spine without recursing,
// as the evaluator does.
func (f *folder) concat(e *ast.InfixExpression, hops int) (string, bool) {
	spine := []*ast.InfixExpression{e}
	for {
		left, ok := spine[len(spine)-1].Left.(*ast.InfixExpression)
		if !ok {
			break
		}
		spine = append(spine, left)
	}
	acc, ok := f.str(spine[len(spine)-1].Left, hops)
	if !ok {
		return "", false
	}
	for i := len(spine) - 1; i >= 0; i-- {
		if spine[i].Operator != "+" {
			return "", false
		}
		r, ok := f.str(spine[i].Right, hops)
		if !ok {
			return "", false
		}
		n := len(acc) + len(r)
		if n > maxConstLen || f.spent+n > maxFolded {
			return "", false
		}
		f.spent += n
		acc += r
	}
	return acc, true
}
```

In `arv/check/check.go`, replace:

```go
	a := &analysis{builtins: map[string]bool{}, callees: map[*ast.Identifier]bool{}}
```

with:

```go
	consts, err := newFolder(prog)
	if err != nil {
		return nil, err
	}
	a := &analysis{builtins: map[string]bool{}, callees: map[*ast.Identifier]bool{}, consts: consts}
```

In `arv/check/check.go`, replace:

```go
	callees  map[*ast.Identifier]bool // names called directly, handled at the call
```

with:

```go
	callees  map[*ast.Identifier]bool // names called directly, handled at the call
	consts   *folder
```

In `arv/check/check.go`, replace:

```go
// constString is e's value if it is a string known before running.
func (a *analysis) constString(e ast.Expression) (string, bool) {
	if s, ok := e.(*ast.StringLiteral); ok {
		return s.Value, true
	}
	return "", false
}
```

with:

```go
// constString is e's value if it is a string known before running (see
// consts.go).
func (a *analysis) constString(e ast.Expression) (string, bool) {
	return a.consts.str(e, 0)
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -race ./arv/check/`
Expected: `ok`. Then `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 5: Commit**

```bash
git add arv/check
git commit -m "feat(check): fold strings bound once by let, and their concatenations" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
