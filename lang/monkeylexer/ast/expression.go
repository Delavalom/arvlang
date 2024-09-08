package ast

import "github.com/delavalom/arvlang/lang/monkeylexer/token"

type ExpressionStatement struct {
	Token      token.Token // the first token of the expression Expression Expression
	Expression Expression
}

func (es *ExpressionStatement) statementNode()       {}
func (es *ExpressionStatement) TokenLiteral() string { return es.Token.Literal }
func (es *ExpressionStatement) String() string {
	if es.Expression != nil {
		return es.Expression.String()
	}
	return ""
}
