package ast

import (
	"bytes"
	"strings"

	"github.com/delavalom/arvlang/arv/token"
)

type HashLiteral struct {
	Token token.Token // the '{' token
	Pairs map[Expression]Expression
	Keys  []Expression // Pairs' keys in source order: the order they are evaluated in
}

func (hl *HashLiteral) expressionNode()      {}
func (hl *HashLiteral) TokenLiteral() string { return hl.Token.Literal }
func (hl *HashLiteral) String() string {
	var out bytes.Buffer
	pairs := []string{}
	for _, key := range hl.Keys {
		pairs = append(pairs, key.String()+":"+hl.Pairs[key].String())
	}
	out.WriteString("{")
	out.WriteString(strings.Join(pairs, ", "))
	out.WriteString("}")
	return out.String()
}
