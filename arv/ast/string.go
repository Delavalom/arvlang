package ast

import "github.com/delavalom/arvlang/arv/token"

// StrBuiltin is the name an interpolated string calls the str builtin by:
// the parser turns "a ${x}" into "a " + $str(x). No program can bind it,
// since an identifier cannot contain $, so a program's own str never
// changes what interpolation prints.
const StrBuiltin = "$str"

type StringLiteral struct {
	Token token.Token
	Value string
}

func (sl *StringLiteral) expressionNode()      {}
func (sl *StringLiteral) TokenLiteral() string { return sl.Token.Literal }
func (sl *StringLiteral) String() string       { return sl.Token.Literal }
