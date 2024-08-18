package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type FunctionLiteral struct {
	Token      monkeylexer.Token // The 'fn' token
	Parameters []*Identifier
	Body       *BlockStatement
}

func (fl *FunctionLiteral) expressionNode()      {}
func (fl *FunctionLiteral) TokenLiteral() string { return fl.Token.Literal }
