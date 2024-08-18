package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type ReturnStatement struct {
	Token       monkeylexer.Token // the 'return' token
	ReturnValue Expression
}

func (rs *ReturnStatement) statementNode()       {}
func (rs *ReturnStatement) TokenLiteral() string { return rs.Token.Literal }
func (rs *ReturnStatement) String() string {
	return rs.Token.Literal
}
