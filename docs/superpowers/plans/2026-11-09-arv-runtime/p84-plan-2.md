# Phase 8.4, task p84.2: the engine seam

> **For agentic workers:** this file is one task, p84.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p84.1 is committed on `phase/p84` in `/Users/delavalom/delavalom-labs/arvlang-wt/p84`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race` on the packages it touches. Overview, guarantees S1-S6, decisions and deviations: `p84-plan-1.md`.

## Task p84.2: the engine seam

After this task a program runs through `compile` and `exec` instead of straight through `Eval`, but `compile` still hands every node to the tree-walker (`walked`), so nothing changes yet. What it adds:

- `enter`: the body of `Eval` (depth check, run-pool reservation, the node's evaluation, release, error stamp), for both engines. `Eval(node)` is `enter(node, nil)`; `exec(c)` is `enter(c.node, c)`, which ticks and then calls `c.run`. `eval` keeps its tick and calls `evalNode`, the old type switch.
- `code` (a node, its `run`, and `walks`, true when `run` is the tree-walker's case), `walked`, `exec`, `start` (what `RunLimited` now calls), `SetTreeWalk`, and the build tag `arv_treewalk`, which makes the tree-walker the default.
- The differential corpus `TestEnginesAgreeOnCorpus` (S1, S2, S4): 57 programs covering every node type, the errors each can raise, the Phase 5E forms, scopes, tasks and budgets, each run on both engines with no limit and under six tight limits, and, when it succeeds, fitted for its exact steps and bytes on each (`fit`, from `fit_test.go`). It passes trivially now; tasks p84.3 to p84.5 make it bite.

**Files:**
- Create (test): `arv/evaluator/engine_diff_test.go`, `arv/evaluator/engine_nodes_test.go`
- Modify: `arv/evaluator/evaluator.go`, `arv/evaluator/concurrency.go`
- Create: `arv/evaluator/engine.go`, `arv/evaluator/engine_default.go`, `arv/evaluator/engine_treewalk.go`, `arv/evaluator/compile.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/engine_diff_test.go` with the Write tool (it holds `${`):

```go
package evaluator

import (
	"bytes"
	"context"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// onEngine runs f with the tree-walker on (walk) or off, then puts the
// setting back. Tests that call it are not parallel: the setting is
// global (SetTreeWalk).
func onEngine(walk bool, f func()) {
	old := SetTreeWalk(walk)
	defer SetTreeWalk(old)
	f()
}

// engineOutcome is everything a run shows: its value or error (with the
// error's kind, position and task) and its output.
type engineOutcome struct {
	inspect, kind, pos, task, out string
}

// runEngine runs src under limits l on one engine.
func runEngine(t *testing.T, walk bool, src string, l runtime.Limits) engineOutcome {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v\n%s", p.Errors(), src)
	}
	var o engineOutcome
	onEngine(walk, func() {
		var out bytes.Buffer
		root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &out)
		v := New(root).RunLimited(program, value.NewEnvironment(), l)
		o.inspect, o.out = v.Inspect(), out.String()
		if e, ok := v.(*value.Error); ok {
			o.kind, o.pos, o.task = e.ErrKind(), e.Pos, e.Task
		}
	})
	return o
}

// engineLimits are the budgets each corpus program also runs under: tight
// ones stop it part way, at a step or a byte both engines must agree on.
var engineLimits = []runtime.Limits{
	{}, {Steps: 7}, {Steps: 60}, {Steps: 700}, {AllocBytes: 40}, {AllocBytes: 400}, {AllocBytes: 4000},
}

// sameOnBothEngines fails unless src gives the same outcome on both
// engines under every limit in engineLimits, and, when it succeeds, is
// charged the same steps and the same bytes.
func sameOnBothEngines(t *testing.T, src string) {
	t.Helper()
	for _, l := range engineLimits {
		w, c := runEngine(t, true, src, l), runEngine(t, false, src, l)
		if w != c {
			t.Fatalf("limits %+v:\ntree-walker %+v\ncompiled    %+v\n%s", l, w, c, src)
		}
	}
	if runEngine(t, true, src, runtime.Limits{}).kind != "" {
		return
	}
	for _, k := range []struct {
		name  string
		limit func(int64) runtime.Limits
	}{{"steps", stepsLimit}, {"bytes", bytesLimit}} {
		var w, c int64
		onEngine(true, func() { w = fit(t, src, k.limit) })
		onEngine(false, func() { c = fit(t, src, k.limit) })
		if w != c {
			t.Fatalf("%s: tree-walker %d, compiled %d\n%s", k.name, w, c, src)
		}
	}
}

// engineCorpus has every node type, every error a node can raise, and the
// Phase 5E forms, each where its position or cost could differ.
var engineCorpus = []string{
	`1; "s"; true; false; [1, "a", [true]]; {"a": 1, 2: "b", true: [3]}`,
	`1 + 2 * 3 - 4 / 2 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 + 1`,
	`"a" + "b" + "c" + "d"`,
	`[1 < 2, 2 > 1, 1 == 1, 1 != 2, "a" == "a", "a" != "b", true == true, [1] == [1], 1 == true]`,
	`!true; !!0; -5; !nil`,
	`-"a"`,
	`"a" - "b"`,
	`1 + true`,
	`1 / 0`,
	"let x = 2;\n  x + y",
	`if (1 < 2) { 10 } else { 20 }; if (false) { 1 }; if (1) { 2 } else { 3 }`,
	`let f = fn(a, b) { let c = a + b; c * 2 }; f(1, 2) + f(3, 4)`,
	`let f = fn(a) { a }; f(1, 2)`,
	`let add = fn(n) { fn(m) { n + m } }; let g = add(10); [g(1), g(2), add(3)(4)]`,
	`let f = fn(n) { if (n == 0) { return 99; } else { return f(n - 1); } }; f(20)`,
	`let f = fn() { for x in [1, 2, 3] { if (x == 2) { return x * 100 } else { x } } }; f()`,
	`return 5; 6`,
	`let fib = fn(n) { if (n < 2) { n } else { fib(n - 1) + fib(n - 2) } }; fib(12)`,
	`let f = fn(n) { f(n + 1) }; f(0)`,
	`let f = fn(n) { 1 + (1 + (1 + (1 + (1 + f(n + 1))))) }; f(0)`,
	`let f = fn(n) { 1 + (1 + (1 + (1 + (1 + (1 + (1 + (1 + (1 + (1 + f(n + 1)))))))))) }; f(0)`,
	`3(1)`,
	`[1, 2, 3][1]; [1][5]; {"a": 1}["a"]; {"a": 1}["b"]`,
	`{"a": 1}[fn() { 1 }]`,
	`1[0]`,
	`{fn() { 1 }: 2}`,
	`{"a": 1 / 0}`,
	`[1, 2 / 0, 3]`,
	`len("abc") + len([1, 2]); push([1], 2); last([1, 2]); str([1, "a"]); split("a,b", ",")`,
	`len(1)`,
	`puts("x", 1, [2]); puts(); 5`,
	`for x in [1, 2, 3] { for y in [10, 20] { x * y } }`,
	`for x in 5 { x }`,
	`for x in [1, 2, 3] { if (x == 2) { 1 / 0 } else { x } }`,
	`let xs = for i in [1, 2, 3] { let j = i * 2; j }; xs`,
	`match 1 / 0 { ok(v) => v, err(e) => [e.kind, e.message, e.pos, e.task] }`,
	`match 42 { ok(v) => v + 1, err(e) => 0 }`,
	`match error("mine") { ok(v) => v, err(e) => e["kind"] + ":" + e["message"] }`,
	`let f = fn() { match (if (true) { return 7 } else { 0 }) { ok(v) => v, err(e) => e } }; f()`,
	`let r = {"up": true, "n": 3, "sub": {"k": [1, 2]}}; [r.up, r.n, r.sub.k, r.missing, r.sub.k[1]]`,
	`let x = 7; let s = "x"; "a ${x} b ${s}${[x, s]} ${x >= 7 && x <= 9} ${x < 0 || x == 7}"`,
	`let x = nil; "v=${x.y}"`,
	`[true && 1, 1 && true, false || 5, true || (1 / 0), false && (1 / 0), 5 || 6]`,
	`fn() { (if (true) { return 5 } else { 0 }) && 2 }()`,
	`"a" <= "b"`,
	"let n = 1;\n  n >= true",
	`scope { let t = spawn len("abc"); await t }`,
	`let f = fn(x) { x * 2 }; scope { let ts = for i in [1, 2, 3] { spawn f(i) }; await ts }`,
	`scope (steps: 100000, alloc_mb: 1) { for i in [1, 2] { i } }`,
	`scope (bogus: 1) { 1 }`,
	`scope (supervise: "collect") { let t = spawn error("no"); match await t { ok(v) => v, err(e) => e.message } }`,
	`scope { scope { 1 + 1 } }`,
	`let f = fn() { 1 / 0 }; scope { let t = spawn f(); await t }`,
	`scope (steps: 30) { let f = fn(n) { f(n + 1) }; f(0) }`,
	`let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } }; len(d("ab", 6))`,
	`json({"a": [1, "b"]}); lines("x\ny")`,
	`let x = 1; let x = x + 1; let f = fn() { x }; let x = 10; [x, f()]`,
}

// Every corpus program gives the same outcome on both engines, and the
// same steps and bytes (Phase 8.4).
func TestEnginesAgreeOnCorpus(t *testing.T) {
	for _, src := range engineCorpus {
		t.Run(src, func(t *testing.T) { sameOnBothEngines(t, src) })
	}
}
```

Create `arv/evaluator/engine_nodes_test.go` (the next tasks fill `compiledNodes`):

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
)

// parsed is src's program; a parse error fails the test.
func parsed(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	return program
}

// lastNode is the program itself (whole), its last statement (stmt), or
// that statement's expression.
func lastNode(t *testing.T, src, part string) ast.Node {
	t.Helper()
	program := parsed(t, src)
	stmt := program.Statements[len(program.Statements)-1]
	switch part {
	case "whole":
		return program
	case "stmt":
		return stmt
	}
	return stmt.(*ast.ExpressionStatement).Expression
}

// compiledNodes are nodes compile turns into their own code: the node
// types the speed programs spend their time in (Phase 8.4).
var compiledNodes = []struct{ src, part string }{
	// p84.3 to p84.5 add entries here.
}

// walkedNodes stay on the tree-walker's case: they run once per task or
// per wait, not per step of a loop.
var walkedNodes = []struct{ src, part string }{
	{`spawn f()`, "expr"}, {`await t`, "expr"},
}

func TestNodesAreCompiled(t *testing.T) {
	for _, n := range compiledNodes {
		if c := compile(lastNode(t, n.src, n.part)); c.walks {
			t.Errorf("%s (%s) runs on the tree-walker", n.src, n.part)
		}
	}
	for _, n := range walkedNodes {
		if c := compile(lastNode(t, n.src, n.part)); !c.walks {
			t.Errorf("%s (%s) was compiled", n.src, n.part)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/evaluator/ -run 'TestEnginesAgree|TestNodesAreCompiled'`
Expected: FAIL, the build fails: `undefined: SetTreeWalk`, `undefined: compile`.

- [ ] **Step 3: Split Eval into enter**

In `arv/evaluator/evaluator.go`, replace:

```go
func (in *Interpreter) Eval(node ast.Node, env *value.Environment) value.Object {
	if in.evalDepth >= runtime.MaxEvalDepth {
```

with:

```go
func (in *Interpreter) Eval(node ast.Node, env *value.Environment) value.Object {
	return in.enter(node, nil, env)
}

// enter is Eval for node, run by the tree-walker (c nil) or as c, its
// compiled code (exec). Both do the same checks in the same order, so a
// node costs the same depth and steps, and its errors get the same
// position, whichever engine runs it (Phase 8.4).
func (in *Interpreter) enter(node ast.Node, c *code, env *value.Environment) value.Object {
	if in.evalDepth >= runtime.MaxEvalDepth {
```

In the same function, replace:

```go
	in.evalDepth++
	result := in.eval(node, env)
	in.evalDepth--
```

with:

```go
	in.evalDepth++
	var result value.Object
	if c == nil {
		result = in.eval(node, env)
	} else if e := in.tick(); e != nil {
		result = e
	} else {
		result = c.run(in, env)
	}
	in.evalDepth--
```

Further down, replace:

```go
func (in *Interpreter) eval(node ast.Node, env *value.Environment) value.Object {
	if e := in.tick(); e != nil {
		return e
	}
	switch node := node.(type) {
```

with:

```go
func (in *Interpreter) eval(node ast.Node, env *value.Environment) value.Object {
	if e := in.tick(); e != nil {
		return e
	}
	return in.evalNode(node, env)
}

// evalNode is the tree-walker's case for node, after eval's tick.
func (in *Interpreter) evalNode(node ast.Node, env *value.Environment) value.Object {
	switch node := node.(type) {
```

- [ ] **Step 4: Add the engine**

Create `arv/evaluator/engine.go`:

```go
package evaluator

import (
	"sync/atomic"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// Phase 8.4 runs a program as compiled code: each AST node becomes a Go
// closure once, before the program runs, instead of being dispatched by
// eval's type switch every time it is evaluated. The tree-walker (eval)
// stays as the reference: both engines must give every program the same
// value, error, position, output, steps and bytes, and the tests run on
// both (SetTreeWalk, the arv_treewalk build tag).

// treeWalk, when true, makes Run evaluate with the tree-walker.
var treeWalk atomic.Bool

func init() { treeWalk.Store(defaultTreeWalk) }

// SetTreeWalk makes every Run that starts after it use the tree-walker
// (on) or compiled code (off), and returns the previous setting. It is
// for tests, which run programs on both engines and compare them; the
// default is compiled code, or the tree-walker in a build with the
// arv_treewalk tag.
func SetTreeWalk(on bool) bool { return treeWalk.Swap(on) }

// code is one AST node compiled. run does what eval's case for the node
// does; exec wraps it in what Eval does around every node. walks is true
// when run is the tree-walker's case itself (walked).
type code struct {
	node  ast.Node
	run   func(in *Interpreter, env *value.Environment) value.Object
	walks bool
}

// walked is node's code that runs the tree-walker's case for it: for the
// node types compile has no case for. Their children run on the
// tree-walker too, which means the same.
func walked(node ast.Node) *code {
	return &code{node: node, walks: true, run: func(in *Interpreter, env *value.Environment) value.Object {
		return in.evalNode(node, env)
	}}
}

// exec evaluates compiled code as Eval evaluates its node.
func (in *Interpreter) exec(c *code, env *value.Environment) value.Object {
	return in.enter(c.node, c, env)
}

// start evaluates node, the whole program, on the engine treeWalk picks.
func (in *Interpreter) start(node ast.Node, env *value.Environment) value.Object {
	if treeWalk.Load() {
		return in.Eval(node, env)
	}
	return in.exec(compile(node), env)
}
```

Create `arv/evaluator/engine_default.go`:

```go
//go:build !arv_treewalk

package evaluator

// defaultTreeWalk is false: programs run as compiled code (engine.go).
const defaultTreeWalk = false
```

Create `arv/evaluator/engine_treewalk.go`:

```go
//go:build arv_treewalk

package evaluator

// defaultTreeWalk is true in a build with the arv_treewalk tag, so the
// whole test suite can run on the reference engine:
// go test -tags arv_treewalk ./...
const defaultTreeWalk = true
```

Create `arv/evaluator/compile.go` (p84.3 replaces it):

```go
package evaluator

import "github.com/delavalom/arvlang/arv/ast"

// compile turns node and everything under it into code. Until the node
// types get their own cases (p84.3 to p84.5), every node runs on the
// tree-walker.
func compile(node ast.Node) *code { return walked(node) }
```

In `arv/evaluator/concurrency.go` (in `RunLimited`), replace:

```go
	result := in.withScope(root, func() value.Object {
		return in.Eval(node, env)
	})
```

with:

```go
	result := in.withScope(root, func() value.Object {
		return in.start(node, env)
	})
```

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./... && go test ./arv/evaluator/ -run 'TestEnginesAgree|TestNodesAreCompiled' -v 2>&1 | grep -c -- '--- PASS'`
Expected: no gofmt output, vet clean, `59` (the two tests and 57 corpus subtests).

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race -tags arv_treewalk ./arv/evaluator/ && go test -race ./arv/evaluator/`
Expected: `ok` twice.

- [ ] **Step 6: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./...`
Expected: every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/evaluator && git commit -m "feat(evaluator): engine seam for compiled code; differential corpus

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
