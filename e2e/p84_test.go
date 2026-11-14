package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// Phase 8.4 acceptance through the binary (15-phase8-tooling.md, 8.4:
// "keep the tree-walker as the reference and run the whole suite against
// both"; guarantees S1-S5 of p84-plan-1.md). Every case runs on the
// tree-walker's binary and on the compiled one and must print the same
// stdout and stderr and exit with the same code (S1); it must also print
// what the case says, so the engines cannot agree on a wrong answer.

// engineCase is one program and what both engines must make of it.
type engineCase struct {
	name   string
	src    string   // {{SERVER}} and {{ID}} are expanded
	flags  []string // run flags before the file
	stdout string
	err    string // the error message; "" for success
	at     string // the error's LINE:COL
}

func runEngineCases(t *testing.T, cases []engineCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := writeProg(t, c.src)
			res := bothEngines(t, spec{args: append(append([]string{"run"}, c.flags...), path)})
			want := spec{stdout: c.stdout}
			if c.err != "" {
				want.code, want.stderrHas = 1, []string{"error: " + c.err + " (at " + path + ":" + c.at + ")\n"}
			}
			expect(t, want, res)
		})
	}
}

// The two binaries are the two engines: the walk one was built with the
// arv_treewalk tag and the compiled one without, whatever ARV_E2E_TAGS
// made arvBin.
func TestP84BinariesAreTheTwoEngines(t *testing.T) {
	t.Parallel()
	for _, e := range engines {
		out, err := exec.Command("go", "version", "-m", engineBinary(t, e)).CombinedOutput()
		if err != nil {
			t.Fatalf("go version -m: %v\n%s", err, out)
		}
		if tagged := strings.Contains(string(out), "arv_treewalk"); tagged != (e == walkEngine) {
			t.Fatalf("%s binary: arv_treewalk in its build settings = %v\n%s", e, tagged, out)
		}
	}
}

// S1: every node type compile has a case for, the ones it walks (spawn,
// await, scope options), environments past four names, shared integers
// and the Phase 5E forms give the same output on both engines.
func TestP84EnginesAgreeOnValues(t *testing.T) {
	t.Parallel()
	runEngineCases(t, []engineCase{
		{name: "many names and closures", stdout: "21\n[11, 12, 13]\n[10, 10]\n",
			src: `let f = fn(a, b, c, d, e, g) { let h = a + b; let i = c * d; let j = e - g; let k = fn() { h + i + j + a + g }; k() };
puts(f(1, 2, 3, 4, 5, 6));
let adders = for i in [1, 2, 3] { fn(x) { x + i } };
puts(for a in adders { a(10) });
let x = 1; let x = x + 1; let get = fn() { x }; let x = 10; puts([x, get()]);`},
		{name: "shared integers", stdout: "[1024, -129, true, -129, -128, 1023, true]\n",
			src: `let big = 1000 + 23; puts([1023 + 1, -129, big == 1023, -128 - 1, 0 - 128, {big: big}[1023], 1023 == big]);`},
		{name: "hashes and fields", stdout: "[one, yes, 2, 1, nil]\n",
			src: `let h = {1: "one", true: "yes", "k": [1, {"z": 2}]};
puts([h[1], h[true], h["k"][1]["z"], h.k[0], h.nope]);`},
		{name: "recursion", stdout: "610\n",
			src: `let fib = fn(n) { if (n < 2) { n } else { fib(n - 1) + fib(n - 2) } }; puts(fib(15));`},
		{name: "return from nested loops", stdout: "[3, 4]\nnone\n",
			src: `let find = fn(xs, want) { for x in xs { for y in xs { if (x * y == want) { return [x, y] } } }; "none" };
puts(find([1, 2, 3, 4, 5], 12)); puts(find([1], 7));`},
		{name: "match", stdout: "[runtime, division by zero, 1:12]\n43\nuser:mine\n",
			src: `puts(match 10 / 0 { ok(v) => v, err(e) => [e.kind, e.message, e.pos] });
puts(match 42 { ok(v) => v + 1, err(e) => 0 });
puts(match error("mine") { ok(v) => v, err(e) => e.kind + ":" + e.message });`},
		{name: "builtins", stdout: "5\n[1, 2]\n2\n[1, a]\n[a, b]\n{\"a\":[1,\"b\"]}\n128\n",
			src: `puts(len("abc") + len([1, 2]), push([1], 2), last([1, 2]), str([1, "a"]), split("a,b", ","));
puts(json({"a": [1, "b"]}));
let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } }; puts(len(d("ab", 6)))`},
		{name: "ergonomics", stdout: "true\ntrue\nfalse\ntrue\nhi bob, n=4 [1, 2] true ${raw}\n77[7, 7]\n",
			src: `let r = {"up": true, "n": 3, "name": "bob", "sub": {"k": [1, 2]}};
puts(r.up && r.n >= 3, r.n <= 2 || r.name == "bob", false && (1 / 0), true || (1 / 0));
puts("hi ${r.name}, n=${r.n + 1} ${r.sub.k} ${r.n >= 3 && r.up} \${raw}");
let x = 7; let xs = [x, x]; puts("${x}${x}" + "${xs}");`},
		// A return that ends the left side of && leaves the function (fixed in
		// the final fixer round, 16zg; both engines printed false before).
		{name: "return inside &&", stdout: "5\n",
			src: `let f = fn() { (if (true) { return 5 } else { 0 }) && 2 }; puts(f())`},
		{name: "spawn and await compiled functions", stdout: "[2, 4, 6]\n",
			src: `let f = fn(x) { x * 2 }; puts(scope { let ts = for i in [1, 2, 3] { spawn f(i) }; for t in ts { await t } })`},
		{name: "collect", stdout: "no\n",
			src: `puts(scope (supervise: "collect") { let t = spawn error("no"); match await t { ok(v) => v, err(e) => e.message } })`},
		{name: "scope steps", stdout: "[budget, budget exceeded: steps (limit 30), 2:12]\n",
			src: "let spin = fn() { for a in [1, 2, 3] { for b in [1, 2, 3] { a * b } } };\n" +
				`puts(match scope (steps: 30) { spin() } { ok(v) => v, err(e) => [e.kind, e.message, e.pos] })`},
		{name: "top-level return", src: "return 5; puts(6)"},
	})
}

// S1 and S4: each error a compiled node can raise, with its message and
// position, the depth limits among them; and the capability errors (I1,
// I2), which compiled code reaches through the same builtins.
func TestP84EnginesAgreeOnErrors(t *testing.T) {
	t.Parallel()
	runEngineCases(t, []engineCase{
		{name: "call depth", src: "let f = fn(n) { f(n + 1) };\nf(0)",
			err: "stack overflow: call depth exceeded 10000", at: "1:17"},
		{name: "eval depth", at: "1:68", err: "stack overflow: evaluation nested deeper than 100000 levels",
			src: "let f = fn(n) { 1 + (1 + (1 + (1 + (1 + (1 + (1 + (1 + (1 + (1 + f(n + 1)))))))))) };\nf(0)"},
		{name: "type mismatch", src: "let a = 1;\nputs(a);\na + true", stdout: "1\n",
			err: "type mismatch: INTEGER + BOOLEAN", at: "3:1"},
		{name: "arguments", src: "puts(\"x\");\nlet g = fn(a) { a };\ng(1, 2)", stdout: "x\n",
			err: "wrong number of arguments: want=1, got=2", at: "3:1"},
		{name: "field of an integer", src: "let n = 5;\nputs(n.x)", err: "index operator not supported: INTEGER", at: "2:6"},
		{name: "field through nil", src: "let r = {\"a\": 1};\nputs(\"v=${r.b.c}\")",
			err: "index operator not supported: NIL", at: "2:11"},
		{name: "unknown name", src: "puts(nope)", err: "identifier not found: nope", at: "1:6"},
		{name: "not a function", src: "let s = 3;\ns(1)", err: "not a function: INTEGER", at: "2:1"},
		{name: "<= on a string", src: "let r = {\"a\": 1};\nputs(r.a <= \"b\")", err: "type mismatch: INTEGER <= STRING", at: "2:6"},
		{name: ">= under ||", src: "puts(1 >= true || 2)", err: "type mismatch: INTEGER >= BOOLEAN", at: "1:6"},
		{name: "division", src: "let z = 0;\n[1, 2 / z]", err: "division by zero", at: "2:5"},
		{name: "for over an integer", src: "for x in 5 { x }", err: "for: want ARRAY, got INTEGER", at: "1:1"},
		{name: "hash key", src: "puts({fn() { 1 }: 2})", err: "unusable as hash key: FUNCTION", at: "1:6"},
		{name: "a task's error", src: "let f = fn() { 1 / 0 };\nscope { let t = spawn f(); await t }",
			err: "division by zero", at: "1:16"},
		{name: "restart gives up", err: "again", at: "1:16",
			src: `let f = fn() { error("again") };` + "\n" +
				`puts(scope (supervise: "restart", retries: 2, backoff_ms: 1, retry_on: ["user"]) { let t = spawn f(); await t })`},
		{name: "I1 no ambient authority", src: `puts(getenv("HOME"))`,
			err: `capability denied: env("HOME") in task root (holds: [])`, at: "1:6"},
		{name: "I2 attenuation only", src: "let f = fn() { getenv(\"HOME\") };\nscope { await spawn f() with [env(\"HOME\")] }",
			err: `spawn in task root: capability denied: env("HOME") (holds: [])`, at: "2:15"},
	})
}

// The REPL runs each line as compiled code, and a function made on one
// line is called, compiled, on later lines and in a task.
func TestP84REPL(t *testing.T) {
	t.Parallel()
	in := strings.Join([]string{
		`let add = fn(n) { fn(m) { n + m } };`,
		`let g = add(10);`,
		`[g(1), g(2), add(3)(4)]`,
		`let r = {"n": 3};`,
		`"n=${r.n} ${r.n >= 3 && true}"`,
		`let t = spawn g(5);`,
		`await t`,
		`1 / 0`,
		`g(1, 2)`,
	}, "\n") + "\n"
	want := "Arv REPL. Ctrl-D to exit.\n>> >> >> [11, 12, 7]\n>> >> n=3 true\n>> >> 15\n" +
		">> ERROR: division by zero\n>> ERROR: wrong number of arguments: want=1, got=2\n>> "
	var outs [2]string
	for i, e := range engines {
		cmd := exec.Command(engineBinary(t, e))
		cmd.Env, cmd.Dir, cmd.Stdin = []string{}, t.TempDir(), strings.NewReader(in)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s REPL: %v\n%s", e, err, out)
		}
		outs[i] = string(out)
	}
	if outs[0] != outs[1] || outs[1] != want {
		t.Fatalf("REPL output\ntree-walker: %q\ncompiled:    %q\nwant:        %q", outs[0], outs[1], want)
	}
}
