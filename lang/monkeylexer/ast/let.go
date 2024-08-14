package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type LetStatement struct {
	Token monkeylexer.Token // the token.LET token Name *Identifier
	Name  *Identifier
	Value Expression
}

func (ls *LetStatement) statementNode()       {}
func (ls *LetStatement) TokenLiteral() string { return ls.Token.Literal }

type Identifier struct {
	Token monkeylexer.Token // the token.IDENT token Value string
	Value string
}

func (i *Identifier) expressionNode()      {}
func (i *Identifier) TokenLiteral() string { return i.Token.Literal }
