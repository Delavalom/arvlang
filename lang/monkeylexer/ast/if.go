package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type IfExpression struct {
	Token       monkeylexer.Token // The 'if' token Condition Expression
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
}

func (ie *IfExpression) expressionNode()      {}
func (ie *IfExpression) TokenLiteral() string { return ie.Token.Literal }
