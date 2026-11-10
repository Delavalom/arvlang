package ast

import (
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/token"
)

func intLit(s string) *IntegerLiteral { return &IntegerLiteral{Token: token.Token{Literal: s}} }

func infix(left Expression, op string, right Expression) *InfixExpression {
	return &InfixExpression{Token: token.Token{Literal: op}, Operator: op, Left: left, Right: right}
}

func TestInfixStringFormat(t *testing.T) {
	for _, tt := range []struct {
		e    Expression
		want string
	}{
		{infix(intLit("1"), "+", intLit("2")), "(1 + 2)"},
		{infix(infix(intLit("1"), "+", intLit("2")), "-", intLit("3")), "((1 + 2) - 3)"},
		{infix(intLit("1"), "+", infix(intLit("2"), "*", intLit("3"))), "(1 + (2 * 3))"},
		{infix(infix(intLit("a"), "*", intLit("b")), "+", infix(infix(intLit("c"), "/", intLit("d")), "-", intLit("e"))),
			"((a * b) + ((c / d) - e))"},
	} {
		if got := tt.e.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

// The parser accepts left-associative chains of any length (they are not
// nesting), so printing one must be linear and must not recurse down the
// left spine: 1M terms print in well under 2 s on a 16 MB stack.
func TestLongInfixChainPrintsInLinearTime(t *testing.T) {
	old := debug.SetMaxStack(16 << 20)
	defer debug.SetMaxStack(old)
	const n = 1_000_000
	var e Expression = intLit("1")
	for i := 1; i < n; i++ {
		e = infix(e, "+", intLit("1"))
	}
	start := time.Now()
	s := e.String()
	elapsed := time.Since(start)
	want := strings.Repeat("(", n-1) + "1" + strings.Repeat(" + 1)", n-1)
	if s != want {
		t.Fatalf("String() of a %d-term chain is wrong (len %d, want %d)", n, len(s), len(want))
	}
	if elapsed > 2*time.Second {
		t.Errorf("String() of a %d-term chain took %v, want < 2s", n, elapsed)
	}
}
