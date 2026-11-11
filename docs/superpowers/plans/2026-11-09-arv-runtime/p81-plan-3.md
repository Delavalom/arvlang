# Phase 8.1, task p81.3: The report: needs, sites and notes

> **For agentic workers:** this file is one task, p81.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.2 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.3: The report: needs, sites and notes

`Program` walks the tree once. A call whose function is an identifier naming an effect or constructor is classified at the call: a known argument (a string literal, for now; p81.5 adds constants) gives a `Need`, anything else a `Note` (`host not statically known`, a wrong argument count, or a scope the runtime would refuse, `never allowed: ...`). Every other occurrence of such a name, such as `let get = http_get` or passing it to a function, is a note, because the value can then be called with anything; a builtin that `arv/check` does not know is a note too. That is what makes the report sound: a builtin value is only reachable through an occurrence of its name, and every occurrence is classified. A user binding that shadows a builtin's name is treated as the builtin, which can only add needs.

`Needs` are the distinct grants sorted by kind (net, fs_read, fs_write, env, then any other) and scope; `Sites` keeps every call in source order (p81.6 compares them with a manifest). Positions are the called name's `line:col`.

**Files:**
- Create: `arv/check/check.go`
- Create (test): `arv/check/check_test.go`

- [ ] **Step 1: Write the failing test**

Create `arv/check/check_test.go`:

```go
package check

import (
	"reflect"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
)

// checked is src's report as two lists: the needs in --allow form and
// the notes.
func checked(t *testing.T, src string) (needs, notes []string) {
	t.Helper()
	r, err := Source(src)
	if err != nil {
		t.Fatalf("Source(%q): %v", src, err)
	}
	for _, n := range r.Needs {
		needs = append(needs, n.String())
	}
	for _, n := range r.Notes {
		notes = append(notes, n.String())
	}
	return needs, notes
}

func TestLiteralCallsBecomeNeeds(t *testing.T) {
	needs, notes := checked(t, `
read_file("data/in.txt");
write_file("out/report.json", "x");
getenv("HOME");
http_get("https://example.com/zen");
let t = spawn f() with [net("api.github.com"), fs_read("/etc"), fs_write("out"), env("USER")];
`)
	want := []string{
		"net=api.github.com", "net=example.com:443",
		"fs_read=/etc", "fs_read=data/in.txt",
		"fs_write=out", "fs_write=out/report.json",
		"env=HOME", "env=USER",
	}
	if !reflect.DeepEqual(needs, want) || notes != nil {
		t.Errorf("needs = %q, notes = %q\nwant needs %q", needs, notes, want)
	}
}

// Every syntactic position an expression can take is searched: the
// report covers code that never runs, too.
func TestEverySyntaxFormIsSearched(t *testing.T) {
	for _, src := range []string{
		`let x = getenv("A");`,
		`fn() { return getenv("A") };`,
		`-len(getenv("A"));`,
		`!getenv("A");`,
		`1 + len(getenv("A"));`,
		`len(getenv("A")) + 1 + 2 + 3;`,
		`if (getenv("A") == "") { 1 };`,
		`if (true) { getenv("A") };`,
		`if (false) { 1 } else { getenv("A") };`,
		`let f = fn(x) { getenv("A") };`,
		`len(getenv("A"));`,
		`[1, getenv("A")];`,
		`[1][len(getenv("A"))];`,
		`{"k": 1}[getenv("A")];`,
		`{"k": getenv("A")};`,
		`{getenv("A"): 1};`,
		`scope { getenv("A") };`,
		`scope (timeout_ms: len(getenv("A"))) { 1 };`,
		`scope (supervise: "collect") { await spawn f() with [env("A")] };`,
		`scope (supervise: "restart", retries: 2, retry_on: [getenv("A")]) { 1 };`,
		`spawn f(getenv("A"));`,
		`spawn f() with [env("A")];`,
		`await spawn f() with [env("A")];`,
		`await getenv("A");`,
		`for x in [getenv("A")] { x };`,
		`for x in [1] { getenv("A") };`,
		`match getenv("A") { ok(v) => v, err(e) => e };`,
		`match 1 { ok(v) => getenv("A"), err(e) => e };`,
		`match 1 { ok(v) => v, err(e) => getenv("A") };`,
		`match scope (supervise: "restart") { await spawn f() with [env("A")] } { ok(v) => v, err(e) => e };`,
	} {
		needs, notes := checked(t, src)
		if !reflect.DeepEqual(needs, []string{"env=A"}) || notes != nil {
			t.Errorf("%s\nneeds = %q, notes = %q; want [env=A]", src, needs, notes)
		}
	}
}

func TestUnknownArgumentsAreNoted(t *testing.T) {
	needs, notes := checked(t, `let get = fn(url) { http_get(url) };
let load = fn(name) { read_file(name + ".txt") };
let check = fn(host) { spawn get(host) with [net(host)] };
getenv(1);`)
	want := []string{
		"http_get at 1:21: host not statically known",
		"read_file at 2:23: path not statically known",
		"net at 3:46: scope not statically known",
		"getenv at 4:1: name not statically known",
	}
	if needs != nil || !reflect.DeepEqual(notes, want) {
		t.Errorf("needs = %q\nnotes = %q\nwant  %q", needs, notes, want)
	}
}

// A builtin passed around as a value can be called with anything.
func TestBuiltinUsedAsAValueIsNoted(t *testing.T) {
	needs, notes := checked(t, `let get = http_get;
let apply = fn(f, x) { f(x) };
apply(read_file, "a");
{"f": getenv}["f"]("HOME");
let net = fn(x) { x };`)
	want := []string{
		"http_get at 1:11: used as a value: what it needs is not statically known",
		"read_file at 3:7: used as a value: what it needs is not statically known",
		"getenv at 4:7: used as a value: what it needs is not statically known",
	}
	if needs != nil || !reflect.DeepEqual(notes, want) {
		t.Errorf("needs = %q\nnotes = %q\nwant  %q", needs, notes, want)
	}
}

// A user binding that shadows an effect's name is still treated as the
// effect: that can only over-report.
func TestShadowedNamesOverReport(t *testing.T) {
	needs, _ := checked(t, `let getenv = fn(x) { x }; getenv("A");`)
	if !reflect.DeepEqual(needs, []string{"env=A"}) {
		t.Errorf("needs = %q", needs)
	}
}

func TestCallsTheRuntimeRefusesAreNoted(t *testing.T) {
	needs, notes := checked(t, `http_get("ftp://x/");
http_get("a", "b");
read_file("");
net("a b");
write_file("x");`)
	if needs != nil || len(notes) != 5 {
		t.Fatalf("needs = %q, notes = %q", needs, notes)
	}
	for _, n := range notes {
		if !strings.Contains(n, ": never allowed: ") {
			t.Errorf("note %q does not say never allowed", n)
		}
	}
	if notes[1] != "http_get at 2:1: never allowed: 2 arguments (it takes 1)" {
		t.Errorf("notes[1] = %q", notes[1])
	}
}

func TestPureProgramNeedsNothing(t *testing.T) {
	needs, notes := checked(t, `let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };
puts(len(split("a,b", ",")), json([1]), clock_ms()); sleep(1); error("x");`)
	if needs != nil || notes != nil {
		t.Errorf("needs = %q, notes = %q", needs, notes)
	}
}

func TestParseErrorIsReturned(t *testing.T) {
	_, err := Source(`let = ;`)
	pe, ok := err.(*ParseError)
	if !ok || len(pe.Errs) == 0 {
		t.Fatalf("Source = %v, want a *ParseError", err)
	}
}

// A long infix chain is walked without a Go frame per term.
func TestLongInfixChain(t *testing.T) {
	src := "getenv(\"A\")" + strings.Repeat(" + 1", 200000) + ";"
	needs, _ := checked(t, src)
	if !reflect.DeepEqual(needs, []string{"env=A"}) {
		t.Errorf("needs = %q", needs)
	}
}

// Program fails closed on syntax it does not know (see walk.go).
func TestProgramFailsOnUnknownSyntax(t *testing.T) {
	prog := &ast.Program{Statements: []ast.Statement{
		&ast.ExpressionStatement{Expression: alienNode{}},
	}}
	if r, err := Program(prog); err == nil || r != nil {
		t.Fatalf("Program = %+v, %v; want an error", r, err)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./arv/check/`
Expected: build failure: `undefined: Source`, `undefined: Program`.

- [ ] **Step 3: Implement**

Create `arv/check/check.go`:

```go
package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Need is a grant the program can need: a capability constructor, or an
// effect, called with an argument known before running.
type Need struct {
	Kind runtime.Kind
	// Scope is normalised for net (as the runtime compares it), and as
	// written for a path (relative to the working directory, as --allow
	// takes it) or an environment variable.
	Scope     string
	Line, Col int    // the call
	Fn        string // the builtin called
}

// String is the need in --allow form, kind=scope.
func (n Need) String() string { return string(n.Kind) + "=" + n.Scope }

// Note is a call arv check cannot turn into a need: its argument is not
// known before running, the builtin is used as a value, or the runtime
// would refuse the call before admitting anything.
type Note struct {
	Line, Col int
	Fn        string
	Kind      runtime.Kind // what the call can need; "" if arv check does not know
	Msg       string
}

func (n Note) String() string {
	return fmt.Sprintf("%s at %d:%d: %s", n.Fn, n.Line, n.Col, n.Msg)
}

// Report is what a program can be admitted for.
type Report struct {
	Needs []Need // one per distinct grant, sorted by kind, then scope
	Sites []Need // every call that needs a grant, in source order
	Notes []Note // in source order
}

// ParseError is a program that did not parse; Errs are the parser's.
type ParseError struct{ Errs []string }

func (e *ParseError) Error() string { return "parse error: " + strings.Join(e.Errs, "; ") }

// Source parses src and checks it. A program that does not parse is a
// *ParseError.
func Source(src string) (*Report, error) {
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, &ParseError{Errs: errs}
	}
	return Program(prog)
}

// Program checks prog. Every call of an effect or a capability constructor
// in the program text becomes a Need or a Note, wherever it is: inside a
// function never called, a branch never taken, a scope option, a match
// arm. Using such a builtin other than by calling it by name, as in
// let f = http_get, is a Note, since the value can then be called with
// anything. So the report covers every grant the program can be admitted
// for, except where a Note says it cannot tell.
func Program(prog *ast.Program) (*Report, error) {
	a := &analysis{builtins: map[string]bool{}, callees: map[*ast.Identifier]bool{}}
	for _, name := range evaluator.BuiltinNames() {
		a.builtins[name] = true
	}
	if err := walk(prog, a.visit); err != nil {
		return nil, err
	}
	return a.report(), nil
}

type analysis struct {
	builtins map[string]bool
	callees  map[*ast.Identifier]bool // names called directly, handled at the call
	sites    []Need
	notes    []Note
}

func (a *analysis) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CallExpression:
		if id, ok := n.Function.(*ast.Identifier); ok {
			if e, ok := effects[id.Value]; ok {
				a.callees[id] = true
				a.call(id, e, n.Arguments)
			}
		}
	case *ast.Identifier:
		if !a.callees[n] {
			a.reference(n)
		}
	}
	return true
}

// call records what calling the builtin id with args needs.
func (a *analysis) call(id *ast.Identifier, e effect, args []ast.Expression) {
	if len(args) != e.nargs {
		a.note(id, e.kind, fmt.Sprintf("never allowed: %d arguments (it takes %d)", len(args), e.nargs))
		return
	}
	arg, ok := a.constString(args[0])
	if !ok {
		a.note(id, e.kind, e.what+" not statically known")
		return
	}
	scope, err := e.scope(e.kind, arg)
	if err != nil {
		a.note(id, e.kind, "never allowed: "+err.Error())
		return
	}
	line, col := ast.Pos(id)
	a.sites = append(a.sites, Need{Kind: e.kind, Scope: scope, Line: line, Col: col, Fn: id.Value})
}

// reference handles a name used other than by calling it. A user binding
// may shadow a builtin's name; it is treated as the builtin all the same,
// which can only add notes.
func (a *analysis) reference(id *ast.Identifier) {
	if e, ok := effects[id.Value]; ok {
		a.note(id, e.kind, "used as a value: what it needs is not statically known")
	} else if a.builtins[id.Value] && !pure[id.Value] {
		a.note(id, "", "a builtin arv check does not know: what it needs is not known")
	}
}

// constString is e's value if it is a string known before running.
func (a *analysis) constString(e ast.Expression) (string, bool) {
	if s, ok := e.(*ast.StringLiteral); ok {
		return s.Value, true
	}
	return "", false
}

func (a *analysis) note(id *ast.Identifier, kind runtime.Kind, msg string) {
	line, col := ast.Pos(id)
	a.notes = append(a.notes, Note{Line: line, Col: col, Fn: id.Value, Kind: kind, Msg: msg})
}

// report sorts the sites and notes by position (the walk's order is not
// the source's) and collects the distinct needs.
func (a *analysis) report() *Report {
	sort.SliceStable(a.sites, func(i, j int) bool { return before(a.sites[i].Line, a.sites[i].Col, a.sites[j].Line, a.sites[j].Col) })
	sort.SliceStable(a.notes, func(i, j int) bool { return before(a.notes[i].Line, a.notes[i].Col, a.notes[j].Line, a.notes[j].Col) })
	return &Report{Needs: distinct(a.sites), Sites: a.sites, Notes: a.notes}
}

func before(l1, c1, l2, c2 int) bool { return l1 < l2 || l1 == l2 && c1 < c2 }

// rank is the order needs are printed in: the README's order of kinds,
// then any other kind.
func rank(k runtime.Kind) int {
	for i, o := range []runtime.Kind{runtime.Net, runtime.FSRead, runtime.FSWrite, runtime.Env} {
		if k == o {
			return i
		}
	}
	return 4
}

// distinct keeps the first site of each grant, sorted by kind, then scope.
func distinct(sites []Need) []Need {
	seen := map[[2]string]bool{}
	var out []Need
	for _, s := range sites {
		k := [2]string{string(s.Kind), s.Scope}
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return rank(out[i].Kind) < rank(out[j].Kind)
		}
		return out[i].Scope < out[j].Scope
	})
	return out
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -race ./arv/check/`
Expected: `ok`. Then `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 5: Commit**

```bash
git add arv/check
git commit -m "feat(check): report the grants a program names, and what it cannot tell" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
