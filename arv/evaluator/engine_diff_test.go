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
	`{1: 1, 1: 2}; {"a": 1, "b": 2, "a": 3, "a": 4}`,
	`let k = "a"; let h = {k: 1, "a": 2, k: [3]}; [h, len(str(h))]`,
}

// Every corpus program gives the same outcome on both engines, and the
// same steps and bytes (Phase 8.4).
func TestEnginesAgreeOnCorpus(t *testing.T) {
	for _, src := range engineCorpus {
		t.Run(src, func(t *testing.T) { sameOnBothEngines(t, src) })
	}
}
