package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// Phase 3 language features through the binary: scope options (and how
// they fail), for ... in and await on arrays. Each case is a small inline
// program; positions are checked as "(at FILE:LINE:COL)" exactly.

// inline is one inline program run with `arv run FLAGS FILE`.
type inline struct {
	name   string
	src    string   // the program; {{SERVER}} and {{ID}} are expanded
	flags  []string // before the file
	stdout string
	err    string // the whole stderr line without its position; "" for success
	at     string // "LINE:COL" of the error
	usage  bool   // the CLI rejects the flags: exit 2, nothing runs
}

// runInline runs every case as a parallel subtest.
func runInline(t *testing.T, cases []inline) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "prog.arv")
			mustWrite(t, path, expand(t, c.src, nil))
			s := spec{args: append(append([]string{"run"}, c.flags...), path), stdout: c.stdout}
			switch {
			case c.usage:
				s.code, s.stderrHas = 2, []string{"error: " + c.err + "\n"}
			case strings.HasPrefix(c.err, "parse error: "): // the first of the errors
				s.code, s.stderrHas = 1, []string{"error: " + c.err + " (at " + path + ":" + c.at + ")"}
			case c.err != "":
				s.code, s.stderrHas = 1, []string{"error: " + c.err + " (at " + path + ":" + c.at + ")\n"}
			}
			check(t, s)
		})
	}
}

func TestScopeOptions(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "all four", stdout: "7\n",
			src: "puts(scope (timeout_ms: 2000, tasks: 20, steps: 1000000, alloc_mb: 64) { 7 })"},
		{name: "empty list", src: "puts(scope () { 7 })", stdout: "7\n"},
		{name: "values are expressions in scope", stdout: "7\n",
			src: "let n = 5;\nputs(scope (steps: n * 1000, tasks: n + 1) { 7 })"},
		{name: "unknown name", src: "let x = 1;\nscope (tasks: 2,\n  timout_ms: 1) { 1 }",
			err: `unknown scope option "timout_ms"`, at: "3:3"},
		{name: "duplicate", src: "scope (steps: 1, steps: 2) { 1 }",
			err: `duplicate scope option "steps"`, at: "1:18"},
		{name: "string value", src: `scope (steps: "10") { 1 }`,
			err: "scope option steps: want a positive INTEGER, got STRING", at: "1:8"},
		{name: "boolean value", src: `scope (tasks: true) { 1 }`,
			err: "scope option tasks: want a positive INTEGER, got BOOLEAN", at: "1:8"},
		{name: "zero", src: "scope (alloc_mb: 0) { 1 }",
			err: "scope option alloc_mb: want a positive INTEGER, got 0", at: "1:8"},
		{name: "negative", src: "scope (timeout_ms: -5) { 1 }",
			err: "scope option timeout_ms: want a positive INTEGER, got -5", at: "1:8"},
		{name: "timeout overflow", src: "scope (timeout_ms: 9223372036854775807) { 1 }",
			err: "scope option timeout_ms: 9223372036854775807 is too large (max 9223372036854)", at: "1:8"},
		{name: "alloc overflow", src: "scope (alloc_mb: 9223372036854775807) { 1 }",
			err: "scope option alloc_mb: 9223372036854775807 is too large (max 8796093022207)", at: "1:8"},
		{name: "tasks overflow", src: "scope (tasks: 2147483648) { 1 }",
			err: "scope option tasks: 2147483648 is too large (max 2147483647)", at: "1:8"},
		{name: "largest values run", src: "puts(scope (timeout_ms: 9223372036854, alloc_mb: 8796093022207, tasks: 2147483647, steps: 9223372036854775807) { 7 })",
			stdout: "7\n"},
		{name: "error in a value", src: "scope (steps: 1,\n  tasks: nope) { 1 }",
			err: "identifier not found: nope", at: "2:10"},
		{name: "options run before the body", src: `scope (steps: 1 / 0) { puts("body ran") }`,
			err: "division by zero", at: "1:15"},
		{name: "missing colon", src: "let x = 1;\nscope (steps 5) { 1 }",
			err: "parse error: expected next token to be :, got INT instead", at: "2:14"},
		{name: "missing value", src: "scope (steps: ) { 1 }",
			err: "parse error: no prefix parse function for ) found", at: "1:15"},
		{name: "trailing comma", src: "scope (steps: 1,) { 1 }",
			err: "parse error: expected next token to be IDENT, got ) instead", at: "1:17"},
		{name: "unclosed list", src: "scope (steps: 1 { 1 }",
			err: "parse error: expected next token to be ), got { instead", at: "1:17"},
	})
}

func TestForIn(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "comprehension", src: "puts(for x in [1, 2, 3] { x * 2 })", stdout: "[2, 4, 6]\n"},
		{name: "empty", src: "puts(for x in [] { x })", stdout: "[]\n"},
		{name: "nested", src: "puts(for x in [1, 2] { for y in [10, 20] { x + y } })", stdout: "[[11, 21], [12, 22]]\n"},
		{name: "each closure has its own x", stdout: "[1, 2, 3]\n",
			src: "let fs = for x in [1, 2, 3] { fn() { x } };\nputs(for f in fs { f() })"},
		{name: "x does not leak", src: "for x in [1] { x };\nx",
			err: "identifier not found: x", at: "2:1"},
		{name: "a let in the body does not leak", src: "for x in [1] { let y = x; y };\ny",
			err: "identifier not found: y", at: "2:1"},
		{name: "return leaves the function", stdout: "99\n",
			src: "let g = fn() { for x in [1, 2, 3] { if (x == 2) { return 99 } else { x } }; 0 };\nputs(g())"},
		{name: "an error stops the loop", src: "for x in [1, 0, 2] {\n  puts(x);\n  10 / x\n}",
			stdout: "1\n0\n", err: "division by zero", at: "3:3"},
		{name: "only arrays", src: "let s = \"abc\";\n  for c in s { c }",
			err: "for: want ARRAY, got STRING", at: "2:3"},
		{name: "fan-out in one expression", stdout: "[2, 4, 6]\n",
			src: "let dbl = fn(x) { x * 2 };\nputs(await for x in [1, 2, 3] { spawn dbl(x) })"},
		{name: "missing in", src: "for x [1] { x }",
			err: "parse error: expected next token to be IN, got [ instead", at: "1:7"},
	})
}

func TestAwaitArray(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "in order", stdout: "[1, abc, true]\n",
			src: `let id = fn(x) { x };` + "\n" + `puts(await [spawn id(1), spawn id("abc"), spawn id(true)])`},
		{name: "empty", src: "puts(await [])", stdout: "[]\n"},
		{name: "a non-task element", src: "let id = fn(x) { x };\nawait [spawn id(1), 2]",
			err: "await: want TASK, got INTEGER (element 1)", at: "2:1"},
		// bad fails at once; the third task, after it in the array, would
		// fail only once its gate opens, which never happens: the first
		// failure cancels it, and its cancellation is not reported.
		{name: "the first error wins", flags: []string{"--allow", "net=127.0.0.1"},
			src: "let bad = fn(x) { 1 / x };\nlet ok = fn() { 1 };\n" +
				`let later = fn() { http_get("{{SERVER}}/wait?id={{ID}}&k=never&n=1"); boom() };` + "\n" +
				`await [spawn ok(), spawn bad(0), spawn later() with [net("127.0.0.1")]]`,
			err: "division by zero", at: "1:19"},
		// The tasks finish in reverse order: each waits until the server
		// has seen the one after it. The array keeps spawn order.
		{name: "results keep spawn order", stdout: "[a, b, c]\n", flags: []string{"--allow", "net=127.0.0.1"},
			src: `let step = fn(name, pre) {
  for u in pre { http_get(u) };
  http_get("{{SERVER}}/arrive?id={{ID}}&k=" + name);
  name
};
let gate = fn(k) { "{{SERVER}}/wait?id={{ID}}&n=1&k=" + k };
let n = [net("127.0.0.1")];
puts(await [spawn step("a", [gate("b")]) with n, spawn step("b", [gate("c")]) with n, spawn step("c", []) with n])`},
	})
}
