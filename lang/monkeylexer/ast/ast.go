package ast

import (
	"bytes"

	"github.com/delavalom/arvlang/lang/monkeylexer/token"
)

type Node interface {
	TokenLiteral() string
}
type Statement interface {
	Node
	statementNode()
	String() string
}
type Expression interface {
	Node
	expressionNode()
	String() string
}

type BlockStatement struct {
	Token      token.Token // the { token Statements []Statement
	Statements []Statement
}

func (bs *BlockStatement) statementNode()       {}
func (bs *BlockStatement) TokenLiteral() string { return bs.Token.Literal }
func (bs *BlockStatement) String() string {
	var out bytes.Buffer

	for _, s := range bs.Statements {
		out.WriteString(s.String())
	}

	return out.String()
}

type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	} else {
		return ""
	}
}

func (p *Program) String() string {
	var out bytes.Buffer

	for _, s := range p.Statements {
		out.WriteString(s.String())
	}

	return out.String()
}
