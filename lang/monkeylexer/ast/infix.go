package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type InfixExpression struct {
	Token    monkeylexer.Token // The operator token, e.g. + Left Expression
	Operator string
	Right    Expression
	Left     Expression
}

func (oe *InfixExpression) expressionNode()      {}
func (oe *InfixExpression) TokenLiteral() string { return oe.Token.Literal }
