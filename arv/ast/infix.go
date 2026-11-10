package ast

import (
	"strings"

	"github.com/delavalom/arvlang/arv/token"
)

type InfixExpression struct {
	Token    token.Token // The operator token, e.g. + Left Expression
	Operator string
	Right    Expression
	Left     Expression
}

func (oe *InfixExpression) expressionNode()      {}
func (oe *InfixExpression) TokenLiteral() string { return oe.Token.Literal }

// String prints the expression fully parenthesised, e.g. ((1 + 2) + 3).
// The parser accepts left-associative chains of any length, so it walks
// the left spine iteratively into one builder: linear time, and no Go
// recursion per operator.
func (oe *InfixExpression) String() string {
	var b strings.Builder
	oe.writeTo(&b)
	return b.String()
}

func (oe *InfixExpression) writeTo(b *strings.Builder) {
	spine := []*InfixExpression{oe}
	for {
		left, ok := spine[len(spine)-1].Left.(*InfixExpression)
		if !ok {
			break
		}
		spine = append(spine, left)
	}
	b.WriteString(strings.Repeat("(", len(spine)))
	writeOperand(b, spine[len(spine)-1].Left)
	for i := len(spine) - 1; i >= 0; i-- {
		b.WriteString(" " + spine[i].Operator + " ")
		writeOperand(b, spine[i].Right)
		b.WriteByte(')')
	}
}

func writeOperand(b *strings.Builder, e Expression) {
	if ie, ok := e.(*InfixExpression); ok {
		ie.writeTo(b)
		return
	}
	b.WriteString(e.String())
}
