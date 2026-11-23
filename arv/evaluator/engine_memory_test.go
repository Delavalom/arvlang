package evaluator

import (
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
)

// Compiled code is charged to alloc_mb by the program's size, 64 bytes
// per AST node on both engines (chargeCode, A7), not by what compile
// allocates; compile allocates less than the parser does (about 0.6
// times in the scratch copy, 16p84-speed.md). This keeps it that way: at
// most 1.5 times.
func TestCompileAllocatesLessThanParsing(t *testing.T) {
	src := strings.Repeat(`let f = fn(a, b) { if (a < b) { [a, b, {"k": a + b * 2}] } else { f(b, a) } }; `, 2000)
	var m0, m1, m2 goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&m0)
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	goruntime.ReadMemStats(&m1)
	c := compile(program)
	goruntime.ReadMemStats(&m2)
	if len(p.Errors()) > 0 || c.walks {
		t.Fatalf("parse errors %v, walks %v", p.Errors(), c.walks)
	}
	parse, comp := m1.TotalAlloc-m0.TotalAlloc, m2.TotalAlloc-m1.TotalAlloc
	t.Logf("source %d bytes: parse %d, compile %d bytes (%.2f of parse)", len(src), parse, comp, float64(comp)/float64(parse))
	if comp > parse*3/2 {
		t.Errorf("compile allocated %d bytes, parse %d: more than 1.5 times", comp, parse)
	}
}
