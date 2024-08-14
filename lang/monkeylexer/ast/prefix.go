package ast

import (
	"github.com/delavalom/arvlang/lang/monkeylexer"
)

type PrefixExpression struct {
	Token    monkeylexer.Token // The prefix token, e.g. !
	Operator string
	Right    Expression
}

func (pe *PrefixExpression) expressionNode()      {}
func (pe *PrefixExpression) TokenLiteral() string { return pe.Token.Literal }
