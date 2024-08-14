package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type ExpressionStatement struct {
	Token      monkeylexer.Token // the first token of the expression Expression Expression
	Expression Expression
}

func (es *ExpressionStatement) statementNode()       {}
func (es *ExpressionStatement) TokenLiteral() string { return es.Token.Literal }
