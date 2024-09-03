package ast

import (
	"github.com/delavalom/arvlang/lang/monkeylexer/token"
)

type InfixExpression struct {
	Token    token.Token // The operator token, e.g. + Left Expression
	Operator string
	Right    Expression
	Left     Expression
}

func (oe *InfixExpression) expressionNode()      {}
func (oe *InfixExpression) TokenLiteral() string { return oe.Token.Literal }
func (oe *InfixExpression) String() string {
	return "(" + oe.Left.String() + " " + oe.Operator + " " + oe.Right.String() + ")"
}
