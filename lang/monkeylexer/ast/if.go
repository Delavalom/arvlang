package ast

import "github.com/delavalom/arvlang/lang/monkeylexer/token"

type IfExpression struct {
	Token       token.Token // The 'if' token Condition Expression
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
}

func (ie *IfExpression) expressionNode()      {}
func (ie *IfExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IfExpression) String() string {
	out := "if"
	out += ie.Condition.String()
	out += " "
	out += ie.Consequence.String()
	if ie.Alternative != nil {
		out += "else"
		out += ie.Alternative.String()
	}
	return out
}
