package evaluator

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// returnCase is a program with a return in a value position and what it
// must give on both engines: its value and its output.
type returnCase struct {
	name, src, want, out string
}

// ret(v) is a block expression that returns v from the enclosing function.
func ret(v string) string { return "(if (true) { return " + v + " } else { 0 })" }

// fnCase wraps body in a function and calls it.
func fnCase(name, body, want, out string) returnCase {
	return returnCase{name, "let f = fn() { " + body + " }; f()", want, out}
}

// returnCases puts a return (through a block expression) in every value
// position (roadmap A1). Each must leave the enclosing function at once:
// nothing to its right runs, and no effect starts after it (I7). A return
// inside a spawned call's argument or its with list is evaluated by the
// spawning task, so it leaves that task's function before any task is
// started (I2: no task, no grant).
var returnCases = []returnCase{
	fnCase("if condition", "if ("+ret("3")+`) { puts("yes") } else { puts("no") }; 4`, "3", ""),
	fnCase("&& left", ret("3")+` && puts("right"); 4`, "3", ""),
	fnCase("|| left", ret("3")+` || puts("right"); 4`, "3", ""),
	fnCase("let value", "let x = "+ret("1")+`; puts("after"); 2`, "1", ""),
	fnCase("call argument", "puts("+ret("3")+"); 4", "3", ""),
	fnCase("later call argument", `let g = fn(a, b) { puts("called") }; g(puts("first"), `+ret("3")+"); 4", "3", "first\n"),
	fnCase("callee", "(if (true) { return 3 } else { puts })(9); 4", "3", ""),
	fnCase("array element", "puts([1, "+ret("3")+`, puts("later")]); 4`, "3", ""),
	fnCase("hash key", "puts({"+ret("3")+`: puts("v")}); 4`, "3", ""),
	fnCase("hash value", `puts({"a": `+ret("3")+`, puts("k"): 1}); 4`, "3", ""),
	fnCase("index left", "puts("+ret("3")+"[0]); 4", "3", ""),
	fnCase("index", "puts([1, 2]["+ret("3")+"]); 4", "3", ""),
	fnCase("field", "puts("+ret("3")+".a); 4", "3", ""),
	fnCase("prefix minus", "puts(-"+ret("3")+"); 4", "3", ""),
	fnCase("prefix bang", "puts(!"+ret("3")+"); 4", "3", ""),
	fnCase("infix left", "puts("+ret("3")+` + puts("right")); 4`, "3", ""),
	fnCase("infix right", "puts(1 + "+ret("3")+"); 4", "3", ""),
	fnCase("infix chain", `puts(1 + 2 + `+ret("3")+` + puts("later")); 4`, "3", ""),
	fnCase("comparison", "puts(1 < "+ret("3")+"); 4", "3", ""),
	fnCase("interpolation in let", `let s = "a${`+ret("5")+`}b"; 6`, "5", ""),
	fnCase("interpolation in puts", `puts("a${`+ret("5")+`}b"); 6`, "5", ""),
	fnCase("match subject", "match "+ret("3")+" { ok(v) => puts(v), err(e) => puts(e) }; 4", "3", ""),
	fnCase("match arm value", "let v = match 1 { ok(v) => "+ret("3")+", err(e) => 0 }; puts(v); 4", "3", ""),
	fnCase("scope option", "scope (steps: "+ret("3")+`) { puts("body") }; 4`, "3", ""),
	fnCase("scope body value", "let v = scope { return 3 }; puts(v); 4", "3", ""),
	fnCase("for iterable", "for x in "+ret("3")+" { puts(x) }; 4", "3", ""),
	fnCase("for body value", "let xs = for x in [1, 2] { if (x == 2) { return x } else { x } }; puts(xs); 0", "2", ""),
	fnCase("await operand", "scope { await "+ret("3")+" }; 4", "3", ""),
	fnCase("spawn argument", `scope { let t = spawn puts(`+ret("3")+`); await t }; 4`, "3", ""),
	fnCase("spawn callee", `scope { await spawn (if (true) { return 3 } else { puts })("x") }; 4`, "3", ""),
	fnCase("spawn with list", `scope { await spawn puts("x") with [`+ret("3")+`] }; 4`, "3", ""),
	fnCase("nested positions", `puts([{"k": -(1 + `+ret("3")+`)}]); 4`, "3", ""),
	fnCase("return of a return", "let g = fn() { return "+ret("3")+" }; g(); 4", "4", ""),
	fnCase("inner function keeps its own return", "let g = fn() { puts("+ret("3")+"); 9 }; [g(), 4]", "[3, 4]", ""),
	// Top level: a return ends the program with its value, as before.
	{"top-level let", "let r = " + ret("7") + "; puts(r); puts(9)", "7", ""},
	{"top-level return", "puts(0); return 5; puts(1)", "5", "0\n"},
}

// The roadmap's three probes, and every other value position: a return
// leaves the function, on both engines, and nothing after it runs (I7).
func TestReturnLeavesEveryValuePosition(t *testing.T) {
	for _, c := range returnCases {
		t.Run(c.name, func(t *testing.T) {
			for _, walk := range []bool{true, false} {
				o := runEngine(t, walk, c.src, runtime.Limits{})
				if o.inspect != c.want || o.out != c.out {
					t.Errorf("walk=%v: got %s, output %q; want %s, output %q\n%s", walk, o.inspect, o.out, c.want, c.out, c.src)
				}
			}
		})
	}
}

// Both engines agree on each case under tight limits too, and charge the
// same steps and bytes (Phase 8.4).
func TestReturnCasesAgreeOnBothEngines(t *testing.T) {
	for _, c := range returnCases {
		t.Run(c.name, func(t *testing.T) { sameOnBothEngines(t, c.src) })
	}
}

// The roadmap's probes, word for word: 1, then only 3 (from the outer
// puts), then 5.
func TestReturnRoadmapProbes(t *testing.T) {
	src := strings.Join([]string{
		`let f = fn() { let x = if (true) { return 1 } else { 0 }; 2 };`, `puts(f());`,
		`let g = fn() { puts(if (true) { return 3 } else { 0 }); 4 };`, `puts(g());`,
		`let h = fn() { let s = "${if (true) { return 5 } else { 0 }}"; 6 };`, `puts(h());`,
	}, "\n")
	for _, walk := range []bool{true, false} {
		if o := runEngine(t, walk, src, runtime.Limits{}); o.out != "1\n3\n5\n" {
			t.Errorf("walk=%v: output %q, want \"1\\n3\\n5\\n\"", walk, o.out)
		}
	}
}
