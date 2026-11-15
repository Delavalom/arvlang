package ast

import "github.com/delavalom/arvlang/arv/token"

type IfExpression struct {
	Token       token.Token // The 'if' token Condition Expression
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
}

func (ie *IfExpression) expressionNode()      {}
func (ie *IfExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IfExpression) String() string {
	if s, ok := ie.logical(); ok {
		return s
	}
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

// logical prints an if expression the parser made from a && b or a || b
// (its token is the operator) as (a && b) or (a || b).
func (ie *IfExpression) logical() (string, bool) {
	right := ie.Consequence
	switch ie.Token.Type {
	case token.AND:
	case token.OR:
		right = ie.Alternative
	default:
		return "", false
	}
	if right == nil || len(right.Statements) != 1 {
		return "", false
	}
	return "(" + ie.Condition.String() + " " + ie.Token.Literal + " " + right.Statements[0].String() + ")", true
}
