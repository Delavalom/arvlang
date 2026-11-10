package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// Integer literals are runs of decimal digits (the lexer reads nothing
// else), so a leading zero should not change the base. Found while
// hardening Phase 3: parseIntegerLiteral calls strconv.ParseInt with base
// 0, so 010 is octal 8 and 08 is a parse error, and
// scope (timeout_ms: 0100) { ... } sets a 64 ms timeout. It predates
// Phase 3 (Phase 0 parser), so it is reported, not fixed, here.
func TestLeadingZeroIsDecimal(t *testing.T) {
	t.Skip(`BUG: integer literals with a leading zero parse as octal: "puts(010)" prints 8 and "08" is a parse error`)
	for input, want := range map[string]int64{"010": 10, "08": 8, "0100": 100, "0": 0} {
		p := New(lexer.New(input))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Errorf("%s: %v", input, errs)
			continue
		}
		lit := prog.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.IntegerLiteral)
		if lit.Value != want {
			t.Errorf("%s = %d, want %d", input, lit.Value, want)
		}
	}
}
