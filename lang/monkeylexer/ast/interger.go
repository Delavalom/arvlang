package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type IntegerLiteral struct {
	Token monkeylexer.Token
	Value int64
}

func (il *IntegerLiteral) expressionNode()      {}
func (il *IntegerLiteral) TokenLiteral() string { return il.Token.Literal }
func (il *IntegerLiteral) String() string       { return il.Token.Literal }
