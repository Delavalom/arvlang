package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type Boolean struct {
	Token monkeylexer.Token
	Value bool
}

func (b *Boolean) expressionNode()      {}
func (b *Boolean) TokenLiteral() string { return b.Token.Literal }
func (b *Boolean) String() string       { return b.Token.Literal }
