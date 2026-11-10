package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// fuzzSeeds cover every construct, with the Phase 3 syntax first: scope
// options (valid, empty, unknown, malformed), for ... in, await on arrays.
var fuzzSeeds = []string{
	"scope (timeout_ms: 2000, tasks: 20, steps: 1000000, alloc_mb: 64) { spawn f() }",
	"scope () { 1 }",
	"scope (timout_ms: 1 + 2, tasks: n) { 1 }",
	"scope (steps 5) { 1 }",
	"scope (steps: ) { 1 }",
	"scope (steps: 1,) { 1 }",
	"scope (steps: 1 { 1 }",
	"scope (1: 2) { }",
	"scope (a: scope (b: 1) { 2 }) { 3 }",
	"for x in [1, 2, 3] { x * 2 }",
	"for in xs { 1 }",
	"for x xs { 1 }",
	"for x in { 1 }",
	"for x in for y in ys { y } { x }",
	"let statuses = scope (tasks: 20, timeout_ms: 5000) {\n  await for url in urls { spawn check(url) with [net(\"api.github.com\")] }\n};",
	"await [spawn f(), spawn g()]",
	"await [",
	"let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } }; scope (steps: 1000) { f(5000) }",
	"let grow = fn(n, acc) { if (n == 0) { acc } else { grow(n - 1, push(acc, \"x\")) } };",
	"{\"k\": [1, {}], 2: true}[\"k\"]",
	"spawn x with",
	"return; return 1; let; let x; let x =",
	"puts(\"abc",
	"!-!-a(b)[c](d)",
	"99999999999999999999",
	"((((((((((1))))))))))",
}

func addSeeds(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	for _, glob := range []string{"../../examples/*.arv", "../../e2e/testdata/*.arv"} {
		files, _ := filepath.Glob(glob)
		for _, name := range files {
			if b, err := os.ReadFile(name); err == nil {
				f.Add(string(b))
			}
		}
	}
}

var atPos = regexp.MustCompile(` \(at (\d+):(\d+)\)$`)

// FuzzParser checks, for any input, that parsing never panics and is
// deterministic; that every error but the nesting limit ends with a
// position inside the input; and that a program without errors has no nil
// statement and prints without panicking.
func FuzzParser(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, input string) {
		prog, errs := fuzzParse(input)
		if _, errs2 := fuzzParse(input); !reflect.DeepEqual(errs, errs2) {
			t.Fatalf("two parses of %q gave different errors: %q and %q", input, errs, errs2)
		}
		lines := strings.Count(input, "\n") + 1
		for _, e := range errs {
			if strings.HasPrefix(e, "nesting deeper than ") {
				continue
			}
			m := atPos.FindStringSubmatch(e)
			if m == nil {
				t.Fatalf("error %q for %q has no (at line:col)", e, input)
			}
			line, _ := strconv.Atoi(m[1])
			col, _ := strconv.Atoi(m[2])
			if line < 1 || line > lines || col < 1 || col > len(input)+1 {
				t.Fatalf("error %q is outside the %d lines of %q", e, lines, input)
			}
		}
		if len(errs) > 0 {
			return
		}
		for i, s := range prog.Statements {
			if s == nil || reflect.ValueOf(s).IsNil() {
				t.Fatalf("statement %d of %q is nil with no parse error", i, input)
			}
		}
		// The printed form is for humans (fn and if bodies lose their
		// braces), not a round trip, so it must only not panic; parsing
		// it again must not either.
		fuzzParse(prog.String())
	})
}

func fuzzParse(input string) (*ast.Program, []string) {
	p := New(lexer.New(input))
	prog := p.ParseProgram()
	return prog, p.Errors()
}
