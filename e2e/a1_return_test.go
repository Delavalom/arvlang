package e2e

import "testing"

// Roadmap A1 through the binary: a return reaches a value position only
// through a block expression, and from there it leaves the function on
// both engines. Nothing after it runs, so no effect starts after a
// return (I7); a return in a spawned call's argument or with list leaves
// the spawning function before any task starts (I2).
func TestA1ReturnLeavesValuePositions(t *testing.T) {
	t.Parallel()
	runEngineCases(t, []engineCase{
		{name: "roadmap probes", stdout: "1\n3\n5\n",
			src: `let f = fn() { let x = if (true) { return 1 } else { 0 }; 2 };
puts(f());
let g = fn() { puts(if (true) { return 3 } else { 0 }); 4 };
puts(g());
let h = fn() { let s = "${if (true) { return 5 } else { 0 }}"; 6 };
puts(h());`},
		{name: "operands, literals and index", stdout: "[1, 2, 3, 4, 5, 6]\n",
			src: `let r = fn(v) { v };
let a = fn() { [1, if (true) { return 1 } else { 0 }, puts("no")] };
let b = fn() { {"k": if (true) { return 2 } else { 0 }}.k };
let c = fn() { -(if (true) { return 3 } else { 0 }) + puts("no") };
let d = fn() { [0][if (true) { return 4 } else { 0 }] };
let e = fn() { r(if (true) { return 5 } else { 0 }, puts("no")) };
let g = fn() { (if (true) { return 6 } else { 0 }).field };
puts([a(), b(), c(), d(), e(), g()]);`},
		{name: "loops, match and scopes", stdout: "[1, 2, 3, 4]\n",
			src: `let a = fn() { for x in (if (true) { return 1 } else { [9] }) { puts(x) } };
let b = fn() { let v = match 0 { ok(v) => if (true) { return 2 } else { 0 }, err(e) => 0 }; puts(v) };
let c = fn() { scope (steps: if (true) { return 3 } else { 9 }) { puts("no") } };
let d = fn() { let v = scope { return 4 }; puts(v) };
puts([a(), b(), c(), d()]);`},
		{name: "spawn and await", stdout: "[1, 2, 3]\n",
			src: `let a = fn() { scope { await spawn puts(if (true) { return 1 } else { "no" }) } };
let b = fn() { scope { await spawn puts("no") with [if (true) { return 2 } else { 0 }] } };
let c = fn() { scope { await (if (true) { return 3 } else { 0 }) } };
puts([a(), b(), c()]);`},
		{name: "top-level return in a let", src: "puts(0);\nlet r = if (true) { return 7 } else { 0 };\nputs(r)", stdout: "0\n"},
	})
}
