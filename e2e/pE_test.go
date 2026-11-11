package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// Phase 5E ergonomics through the real binary: .field, <= and >=, && and
// || (and their short circuit), and string interpolation. Each case names
// the guarantee of pE-plan-1.md it covers (G1-G6). Costs, nesting and the
// ergonomic uptime example are in pE_cost_test.go.

// progSpec writes src (after {{SERVER}} / {{ID}} expansion) to a fresh
// prog.arv and returns a spec running it with flags, plus the file's path
// for "(at PATH:LINE:COL)" checks.
func progSpec(t *testing.T, src string, flags ...string) (spec, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prog.arv")
	mustWrite(t, path, expand(t, src, nil))
	return spec{args: append(append([]string{"run"}, flags...), path)}, path
}

// G3: r.field is r["field"]: same value, nil for a missing key, the same
// error at the same place (the start of r).
func TestPEField(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "value", src: `let r = {"up": true, "n": 3}; puts(r.up); puts(r.n + 1)`, stdout: "true\n4\n"},
		{name: "missing key is nil", src: `let r = {"a": 1}; puts(r.b); puts(r.b == r["b"])`, stdout: "nil\ntrue\n"},
		{name: "chain", src: `let r = {"a": {"b": {"c": 7}}}; puts(r.a.b.c)`, stdout: "7\n"},
		{name: "keyword names", src: `let r = {"in": 1, "match": 2, "fn": 3}; puts(r.in + r.match + r.fn)`, stdout: "6\n"},
		{name: "mixes with index and call", stdout: "x\n",
			src: `let r = {"f": fn() { [{"s": "x"}] }}; puts(r.f()[0].s)`},
		{name: "on an integer", src: "let n = 5;\n  n.x", err: "index operator not supported: INTEGER", at: "2:3"},
		{name: "same as the index error", src: "let n = 5;\n  n[\"x\"]", err: "index operator not supported: INTEGER", at: "2:3"},
		{name: "through nil", src: "let r = {\"a\": 1};\nputs(r.b.c)", err: "index operator not supported: NIL", at: "2:6"},
		{name: "on an array", src: "let xs = [1];\nxs.len", err: "index operator not supported: ARRAY", at: "2:1"},
		{name: "a number after the dot", src: "let r = {};\nr.5",
			err: "parse error: expected a field name after ., got INT instead", at: "2:3"},
		{name: "1.5 is not a float", src: "1.5",
			err: "parse error: expected a field name after ., got INT instead", at: "1:3"},
	})
}

// <= and >= compare integers, and fail as < and > do on anything else.
func TestPECompare(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "integers", stdout: "true\ntrue\nfalse\ntrue\ntrue\nfalse\n",
			src: "puts(1 <= 2); puts(2 <= 2); puts(3 <= 2); puts(3 >= 2); puts(2 >= 2); puts(-1 >= 0)"},
		{name: "below arithmetic, above ==", src: "puts(1 + 1 <= 2 == true)", stdout: "true\n"},
		{name: "strings", src: "let a = \"a\";\nputs(a <= a)", err: "unknown operator: STRING <= STRING", at: "2:6"},
		{name: "mixed", src: "puts(1 >= true)", err: "type mismatch: INTEGER >= BOOLEAN", at: "1:6"},
		{name: "a space splits it", src: "puts(1 < = 2)", err: "parse error: no prefix parse function for = found", at: "1:10"},
	})
}

// && and || are the if they mean: only true is true, the right side's
// value is the result when it runs, || is below && is below ==.
func TestPEAndOr(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "truth table", stdout: "true\nfalse\nfalse\nfalse\ntrue\ntrue\ntrue\nfalse\n",
			src: "puts(true && true); puts(true && false); puts(false && true); puts(false && false);\n" +
				"puts(true || true); puts(true || false); puts(false || true); puts(false || false)"},
		{name: "only true is true", src: `puts(1 && true); puts(true && 5); puts(5 || 6); puts(false || "x")`,
			stdout: "false\n5\n6\nx\n"},
		{name: "|| is no default", src: `let r = {"name": "bob"}; puts(r.name || "anon")`, stdout: "anon\n"},
		{name: "precedence", stdout: "true\ntrue\nfalse\n",
			src: "puts(false && false || true); puts(true || false && false); puts(1 < 2 && 3 > 4 || 2 >= 3)"},
		{name: "parentheses", src: "puts(true || false && false); puts((true || false) && false)", stdout: "true\nfalse\n"},
		{name: "!binds tighter", src: "puts(!false && !false)", stdout: "true\n"},
		{name: "error on the left", src: "let a = 1;\nputs(nope && a)", err: "identifier not found: nope", at: "2:6"},
		{name: "error on the right", src: "let a = true;\nputs(a &&\n  1 / 0)", err: "division by zero", at: "3:3"},
		{name: "error on the right of ||", src: "puts(false || nope)", err: "identifier not found: nope", at: "1:15"},
		{name: "a lone &", src: "true & false", err: "parse error: no prefix parse function for ILLEGAL found", at: "1:6"},
		{name: "a lone |", src: "true | false", err: "parse error: no prefix parse function for ILLEGAL found", at: "1:6"},
	})
}

// G2 and G6 through the binary: when the left side decides, the right side
// is never run: no request reaches the server, no denial is reported
// (there is no net grant), no task is spawned, and no step or byte is
// spent. When it runs, it is checked and charged as anywhere else.
func TestPEShortCircuit(t *testing.T) {
	t.Parallel()
	get := `http_get("{{SERVER}}/arrive?id={{ID}}&k=sc")`
	recurse := "let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };\n"
	allowLoop := []string{"--allow", "net=127.0.0.1"}
	grow := `let grow = fn(n, acc) { if (n == 0) { acc } else { grow(n - 1, push(acc, "xxxxxxxxxxxx")) } };` + "\n"
	cases := []struct {
		name, src, stdout, err, at string
		flags                      []string
		hits                       int
	}{
		{name: "false && effect", src: "puts(false && " + get + ")", stdout: "false\n"},
		{name: "true || effect", src: "puts(true || " + get + ")", stdout: "true\n"},
		{name: "false && spawn", stdout: "false\n",
			src: "let g = fn() { " + get + " };\nputs(false && (await spawn g() with [net(\"127.0.0.1\")]))"},
		{name: "nested", src: "puts(false && (true || " + get + "))", stdout: "false\n"},
		{name: "true && effect is denied", src: "let ok = true;\nputs(ok && " + get + ")",
			err: `capability denied: net("127.0.0.1:`, at: "2:12"},
		{name: "true && effect runs", src: "puts(true && " + get + "[\"status\"] == 200)",
			flags: allowLoop, stdout: "true\n", hits: 1},
		{name: "false || effect runs", src: "puts(false || " + get + ".status == 200)",
			flags: allowLoop, stdout: "true\n", hits: 1},
		{name: "steps not spent", src: recurse + "puts(false && f(5000))", flags: []string{"--steps", "1000"}, stdout: "false\n"},
		{name: "steps spent", src: recurse + "puts(true && f(5000))", flags: []string{"--steps", "1000"},
			err: "budget exceeded: steps (limit 1000)"},
		{name: "bytes not spent", src: grow + "puts(true || len(grow(200000, [])))", flags: []string{"--alloc-mb", "1"}, stdout: "true\n"},
		{name: "bytes spent", src: grow + "puts(false || len(grow(200000, [])))", flags: []string{"--alloc-mb", "1"},
			err: "budget exceeded: alloc (limit 1048576 bytes)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, path := progSpec(t, c.src, c.flags...)
			s.stdout = c.stdout
			if c.err != "" {
				s.code, s.stderrHas = 1, []string{"error: " + c.err}
				if c.at != "" {
					s.stderrHas = append(s.stderrHas, "(at "+path+":"+c.at+")\n")
				}
			}
			check(t, s)
			if got := hits(t, "/arrive"); got != c.hits {
				t.Fatalf("the server saw %d requests, want %d", got, c.hits)
			}
		})
	}
}

// G4: an interpolated value prints exactly as puts prints it, alone and
// inside text.
func TestPEInterpolationRendersAsPuts(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]string{
		"integer": "-42", "true": "true", "false": "false", "string": `"a b"`,
		"empty string": `""`, "array": `[1, "a", [true, false], []]`, "hash": `{"k": [1]}`,
		"empty hash": "{}", "nil": `{"a": 1}.missing`, "function": "fn(x, y) { x + y }",
		"builtin": "len", "awaited task": "await spawn fn() { [7] }()",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, _ := progSpec(t, "let v = "+v+";\nputs(v);\nputs(\"${v}\");\nputs(\"<${v}>\")")
			res := run(t, s)
			if res.code != 0 || res.stderr != "" {
				t.Fatalf("run failed\n%s", res)
			}
			out := res.stdout // v\n v\n <v>\n: 3 copies of the text, 2 brackets
			if len(out) < 5 {
				t.Fatalf("output too short\n%s", res)
			}
			want := out[:(len(out)-2)/3]
			if out != want+want+"<"+strings.TrimSuffix(want, "\n")+">\n" {
				t.Fatalf("puts and interpolation differ for %s\n%s", v, res)
			}
		})
	}
}

// The syntax, its escape, and G1: text around ${...} keeps its meaning.
func TestPEInterpolation(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "pieces", src: "let n = 2; let total = 40;\nputs(\"${n} of ${total} down\")", stdout: "2 of 40 down\n"},
		{name: "expressions", src: `let r = {"up": true, "n": [1, 2]}; puts("${r.up && r.n[1] >= 2}/${len(r.n) * 10}")`, stdout: "true/20\n"},
		{name: "a hash literal inside", src: `puts("${ {1: 2}[1] }")`, stdout: "2\n"},
		{name: "a block inside", src: `puts("${ if (1 <= 2) { 3 } else { 4 } }!")`, stdout: "3!\n"},
		{name: "the escape", src: `puts("\${x} costs $5 {not} $ {x} $x")`, stdout: "${x} costs $5 {not} $ {x} $x\n"},
		{name: "backslashes stay", src: `let v = 1; puts("a\nb\\${v}")`, stdout: "a\\nb\\${v}\n"},
		{name: "backslash before a value", src: `let v = 1; puts("\" + "${v}")`, stdout: "\\1\n"},
		{name: "across lines", src: "let a = 1;\nputs(\"x\n${a +\n  1}y\")", stdout: "x\n2y\n"},
		{name: "a comment inside", src: "let a = 1;\nputs(\"${a // a } brace\n}!\")", stdout: "1!\n"},
		{name: "str is not $str", src: `let str = fn(x) { "mine" }; puts(str(1)); puts("${1}")`, stdout: "mine\n1\n"},
		{name: "str is a builtin", src: `puts(str([1, "a"]) + str("b"))`, stdout: "[1, a]b\n"},
		{name: "many pieces", src: `puts("` + strings.Repeat("${1}-", 2000) + `")`, stdout: strings.Repeat("1-", 2000) + "\n"},
		// G5: errors inside ${...} are at their own line:col.
		{name: "error inside", src: "let r = {};\nputs(\"first\n  ${r.a +\n  nope} end\")",
			err: "identifier not found: nope", at: "4:3"},
		{name: "type error inside", src: "puts(\"ab ${1 + true}\")", err: "type mismatch: INTEGER + BOOLEAN", at: "1:12"},
		{name: "denial inside", src: "let p = \"/etc/hosts\";\nputs(\"x ${read_file(p)}\")",
			err: `capability denied: fs_read("/etc/hosts") in task root (holds: [])`, at: "2:11"},
		{name: "unterminated", src: "let x = 1;\nputs(\"a ${x\")", err: "parse error: unterminated ${ in string", at: "2:9"},
		{name: "empty", src: `puts("${ }")`, err: "parse error: empty ${} in string", at: "1:7"},
		{name: "two expressions", src: `puts("${x; y}")`, err: "parse error: expected } to end ${, got ; instead", at: "1:10"},
		{name: "no string inside", src: `puts("${len("x")}")`, err: "parse error: unterminated ${ in string", at: "1:7"},
		{name: "parse error inside", src: "puts(\"a\n ${1 +}\")", err: "parse error: no prefix parse function for EOF found", at: "2:7"},
	})
}
