package ast

import "github.com/delavalom/arvlang/lang/monkeylexer"

type CallExpression struct {
	Token     monkeylexer.Token // The '(' token
	Function  Expression        // Identifier or FunctionLiteral
	Arguments []Expression
}

func (ce *CallExpression) expressionNode()      {}
func (ce *CallExpression) TokenLiteral() string { return ce.Token.Literal }
