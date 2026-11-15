package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
)

// Integer literals are runs of decimal digits (the lexer reads nothing
// else), so a leading zero does not change the base. Before the fix,
// parseIntegerLiteral called strconv.ParseInt with base 0, so 010 was
// octal 8, 08 was a parse error, and scope (timeout_ms: 0100) { ... } set
// a 64 ms timeout.
func TestLeadingZeroIsDecimal(t *testing.T) {
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
