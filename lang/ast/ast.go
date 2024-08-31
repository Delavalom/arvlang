package ast

import "github.com/delavalom/arvlang/lang/tokens"

type Node interface {
	GetToken() *tokens.Token
	String() string
	Children() []Node
	Traverse(int, func(int, Node))
}
