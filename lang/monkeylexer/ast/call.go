package ast

import "github.com/delavalom/arvlang/lang/monkeylexer/token"

type CallExpression struct {
	Token     token.Token // The '(' token
	Function  Expression  // Identifier or FunctionLiteral
	Arguments []Expression
}

func (ce *CallExpression) expressionNode()      {}
func (ce *CallExpression) TokenLiteral() string { return ce.Token.Literal }
func (ce *CallExpression) String() string {
	out := ce.Function.String() + "("
	for i, arg := range ce.Arguments {
		if i != 0 {
			out += ", "
		}
		out += arg.String()
	}
	out += ")"
	return out
}
