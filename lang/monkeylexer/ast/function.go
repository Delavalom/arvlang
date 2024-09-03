package ast

import "github.com/delavalom/arvlang/lang/monkeylexer/token"

type FunctionLiteral struct {
	Token      token.Token // The 'fn' token
	Parameters []*Identifier
	Body       *BlockStatement
}

func (fl *FunctionLiteral) expressionNode()      {}
func (fl *FunctionLiteral) TokenLiteral() string { return fl.Token.Literal }
func (fl *FunctionLiteral) String() string {
	out := fl.TokenLiteral() + "("
	for i, param := range fl.Parameters {
		if i != 0 {
			out += ", "
		}
		out += param.String()
	}
	out += ") "
	out += fl.Body.String()
	return out
}
