package check

import (
	"os"
	"path/filepath"
	"testing"

	arvast "github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
)

// ast.Size, the node count a program's code is charged by (A7), counts
// exactly the nodes walk visits: every node type, nil fields included,
// and every example program and every form of syntax. So syntax added
// later, which walk must learn (TestNodeFieldsAreKnown), is counted too.
func TestSizeCountsWhatWalkVisits(t *testing.T) {
	count := func(n arvast.Node) int {
		c := 0
		if err := walk(n, func(arvast.Node) bool { c++; return true }); err != nil {
			t.Fatal(err)
		}
		return c
	}
	for name, n := range nodeTypes {
		if got, want := arvast.Size(n), count(n); got != want {
			t.Errorf("zero %s: Size %d, walk %d", name, got, want)
		}
	}
	srcs := []string{
		`let h = {"a": [1, -2], "b": fn(x, y) { return x + y * 2 }}; h.a[0] && !h.b(1, 2) || "s${h.a}t"`,
		`let r = scope (timeout_ms: 5, supervise: "restart") { let t = spawn f(1) with [net("x")]; await t };
match r { ok(v) => if (v) { v } else { 0 }, err(e) => e.message }; for x in [1, 2] { puts(x) }`,
	}
	files, _ := filepath.Glob("../../examples/*.arv")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, string(b))
	}
	for _, src := range srcs {
		p := parser.New(lexer.New(src))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("%s: %v", src, p.Errors())
		}
		if got, want := arvast.Size(prog), count(prog); got != want || prog.Nodes != got {
			t.Errorf("Size %d, walk %d, Nodes %d for\n%s", got, want, prog.Nodes, src)
		}
	}
}
