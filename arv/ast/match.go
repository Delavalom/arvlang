package ast

import "github.com/delavalom/arvlang/arv/token"

// MatchExpression looks at a value that may be an error:
// match <subject> { ok(<name>) => <expression>, err(<name>) => <expression> }.
// It has exactly one arm of each kind. The ok arm sees the value as Ok's
// name, the err arm sees the error, as a hash, as Err's name.
type MatchExpression struct {
	Token   token.Token // the 'match' token
	Subject Expression
	OkName  *Identifier
	Ok      Expression
	ErrName *Identifier
	Err     Expression
}

func (me *MatchExpression) expressionNode()      {}
func (me *MatchExpression) TokenLiteral() string { return me.Token.Literal }
func (me *MatchExpression) String() string {
	return "match " + me.Subject.String() + " { ok(" + me.OkName.String() + ") => " + me.Ok.String() +
		", err(" + me.ErrName.String() + ") => " + me.Err.String() + " }"
}
