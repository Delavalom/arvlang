package ast

import "github.com/delavalom/arvlang/arv/token"

// ForExpression is a comprehension: for x in xs { body } evaluates body
// once per element of xs, with x bound to it, and yields the array of the
// body's values.
type ForExpression struct {
	Token    token.Token // the 'for' token
	Var      *Identifier
	Iterable Expression
	Body     *BlockStatement
}

func (fe *ForExpression) expressionNode()      {}
func (fe *ForExpression) TokenLiteral() string { return fe.Token.Literal }
func (fe *ForExpression) String() string {
	return "for " + fe.Var.String() + " in " + fe.Iterable.String() + " { " + fe.Body.String() + " }"
}
