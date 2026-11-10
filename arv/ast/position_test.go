package ast

import (
	"testing"

	"github.com/delavalom/arvlang/arv/token"
)

func at(line, col int) token.Token { return token.Token{Line: line, Col: col} }

func TestPosUsesLeftmostOperand(t *testing.T) {
	a := &Identifier{Token: at(2, 3), Value: "a"}
	sum := &InfixExpression{Token: at(2, 5), Operator: "+", Left: a, Right: &StringLiteral{Token: at(2, 7)}}
	call := &CallExpression{Token: at(4, 2), Function: &Identifier{Token: at(4, 1)}}
	index := &IndexExpression{Token: at(5, 4), Left: call, Index: &IntegerLiteral{Token: at(5, 5)}}
	for _, tt := range []struct {
		n         Node
		line, col int
	}{
		{a, 2, 3}, {sum, 2, 3}, {call, 4, 1}, {index, 4, 1},
		{&ScopeExpression{Token: at(7, 9)}, 7, 9},
		{&Program{}, 0, 0}, {nil, 0, 0}, {&Identifier{}, 0, 0},
	} {
		if line, col := Pos(tt.n); line != tt.line || col != tt.col {
			t.Errorf("Pos(%T) = %d:%d, want %d:%d", tt.n, line, col, tt.line, tt.col)
		}
	}
}

// A chain as long as the parser allows is walked without recursion.
func TestPosLongInfixChain(t *testing.T) {
	var e Expression = &IntegerLiteral{Token: at(1, 1)}
	for i := 0; i < 1_000_000; i++ {
		e = &InfixExpression{Token: at(1, 3), Operator: "+", Left: e, Right: &IntegerLiteral{Token: at(1, 5)}}
	}
	if line, col := Pos(e); line != 1 || col != 1 {
		t.Fatalf("Pos = %d:%d, want 1:1", line, col)
	}
}
